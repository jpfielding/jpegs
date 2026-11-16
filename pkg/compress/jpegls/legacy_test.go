package jpegls

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyWriter is the pre-conformance bit writer (0xFF 0x00 stuffing,
// uncapped Golomb). Test-only: the package no longer writes this format.
type legacyWriter struct {
	buf   []byte
	bits  uint64
	nBits int
}

func (bw *legacyWriter) writeBits(val, n int) {
	bw.bits = bw.bits<<n | uint64(val)&(uint64(1)<<n-1)
	bw.nBits += n
	for bw.nBits >= 8 {
		bw.nBits -= 8
		b := byte(bw.bits >> bw.nBits)
		bw.buf = append(bw.buf, b)
		if b == 0xFF {
			bw.buf = append(bw.buf, 0x00)
		}
	}
}

func (bw *legacyWriter) flush() {
	if bw.nBits == 0 {
		return
	}
	b := byte(bw.bits << (8 - bw.nBits))
	bw.buf = append(bw.buf, b)
	if b == 0xFF {
		bw.buf = append(bw.buf, 0x00)
	}
	bw.nBits = 0
}

// legacyEncode reproduces the stream the pre-conformance encoder wrote: the
// exact inverse of legacyDecodeScan.
func legacyEncode(samples []uint16, width, height, precision int) []byte {
	maxVal := 1<<precision - 1
	rng := maxVal + 1
	buf := binary.BigEndian.AppendUint16(nil, MarkerSOI)
	buf = appendSOF(buf, precision, height, width)
	buf = appendSOS(buf, 1, 0)
	bw := &legacyWriter{buf: buf}
	cm := newLegacyContext(maxVal, 0)
	prev := make([]int, width)
	cur := make([]int, width)

	for y := range height {
		for x := range width {
			cur[x] = int(samples[y*width+x])
			var ra, rb, rc, rd int
			if y > 0 {
				rb = prev[x]
				rc = prev[max(x-1, 0)]
				rd = prev[min(x+1, width-1)]
				ra = prev[0]
			}
			if x > 0 {
				ra = cur[x-1]
			}
			q, sign := cm.contextIndex(rd-rb, rb-rc, rc-ra)
			px := clip(PredictMED(ra, rb, rc)+sign*cm.c[q], 0, maxVal)
			errVal := (cur[x] - px) * sign
			if errVal < -rng/2 {
				errVal += rng
			}
			if errVal > rng/2 {
				errVal -= rng
			}
			mapped := 2 * errVal
			if errVal < 0 {
				mapped = -2*errVal - 1
			}
			k := golombK(cm.n[q], cm.a[q])
			for range mapped >> k {
				bw.writeBits(0, 1)
			}
			bw.writeBits(1, 1)
			bw.writeBits(mapped, k)
			cm.update(q, errVal)
		}
		prev, cur = cur, prev
	}
	bw.flush()
	return binary.BigEndian.AppendUint16(bw.buf, MarkerEOI)
}

// legacyLCG is the generator the dicos.rs legacy fixtures were made with.
func legacyLCG(seed uint64, n int, maxVal uint16) []uint16 {
	mod := maxVal + 1
	if maxVal == 65535 {
		mod = 65535 // saturating add in the original generator
	}
	out := make([]uint16, n)
	for i := range out {
		seed = seed*6364136223846793005 + 1442695040888963407
		out[i] = uint16(seed>>33) % mod
	}
	return out
}

type legacyFixture struct {
	name            string
	w, h, precision int
	samples         []uint16
}

// legacyFixtures describes testdata/legacy: streams frozen from the
// pre-conformance format (shared with dicos.rs's Profile::LegacyGo tests).
func legacyFixtures() []legacyFixture {
	gradient := make([]uint16, 16*16)
	for i := range gradient {
		gradient[i] = uint16(uint32(i%16)*65535/16 + uint32(i/16)*7)
	}
	flat := make([]uint16, 24*8)
	for i := range flat {
		flat[i] = 777
	}
	return []legacyFixture{
		{"gradient-16x16", 16, 16, 16, gradient},
		{"random-32x32", 32, 32, 16, legacyLCG(0x15D1C05, 32*32, 65535)},
		{"odd-13x7", 13, 7, 12, legacyLCG(0x10DD5, 13*7, 4095)},
		{"flat-24x8", 24, 8, 10, flat},
		{"tall-1x16", 1, 16, 16, legacyLCG(0x17A11, 16, 65535)},
	}
}

func TestLegacyFixtures(t *testing.T) {
	for _, fx := range legacyFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			data, err := testdata.ReadFile("testdata/legacy/" + fx.name + ".jls")
			require.NoError(t, err)

			// The frozen decoder reads them.
			img, err := decode(data, profileLegacy)
			require.NoError(t, err)
			assert.Equal(t, fx.samples, samplesOf(t, img))

			// They are not T.87 streams...
			_, err = decode(data, profileT87)
			require.Error(t, err)

			// ...and the public Decode falls back transparently.
			img, err = Decode(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, fx.samples, samplesOf(t, img))

			// The test-side legacy encoder is byte-identical to the frozen
			// format, so it is a valid generator for the tests below.
			assert.Equal(t, data, legacyEncode(fx.samples, fx.w, fx.h, fx.precision))
		})
	}
}

// TestLegacyFallback checks that Decode recovers legacy streams across
// geometries and content, including flat images where T.87 would use run
// mode.
func TestLegacyFallback(t *testing.T) {
	dims := [][2]int{{8, 8}, {16, 16}, {33, 9}, {64, 48}, {1, 64}, {64, 1}}
	for _, precision := range []int{8, 16} {
		for _, wh := range dims {
			for _, name := range patternNames {
				samples := pattern(name, wh[0], wh[1], uint16(1<<precision-1))
				data := legacyEncode(samples, wh[0], wh[1], precision)
				img, err := Decode(bytes.NewReader(data))
				require.NoError(t, err, "%s %v p%d", name, wh, precision)
				require.Equal(t, samples, samplesOf(t, img), "%s %v p%d", name, wh, precision)
			}
		}
	}
}

// TestLegacyDetection pins how reliably Decode tells the formats apart
// without out-of-band information.
func TestLegacyDetection(t *testing.T) {
	t.Run("frames of 16x16 and up", func(t *testing.T) {
		for _, wh := range [][2]int{{16, 16}, {32, 32}, {64, 64}} {
			for _, precision := range []int{8, 16} {
				// Flat, low-noise and full-range noise images.
				for _, noiseBits := range []int{0, 2, precision} {
					for seed := range 100 {
						samples := lcg(uint64(seed*7919+wh[0]*31+precision), wh[0]*wh[1], uint16(1<<noiseBits-1))
						base := lcg(uint64(seed), 1, uint16(1<<precision-1))[0] / 2
						for i := range samples {
							samples[i] = uint16(min(int(samples[i])+int(base), 1<<precision-1))
						}
						img, err := Decode(bytes.NewReader(legacyEncode(samples, wh[0], wh[1], precision)))
						require.NoError(t, err)
						require.Equal(t, samples, samplesOf(t, img), "%v p%d noise %d seed %d", wh, precision, noiseBits, seed)
					}
				}
			}
		}
	})
	t.Run("tiny frames can be ambiguous", func(t *testing.T) {
		// Known limitation: with only a few bits of scan data one bit string
		// can be a complete stream in both formats. A legacy 1x1 image of
		// value 1 is also a conformant T.87 image of value 0; T.87 wins.
		data := legacyEncode([]uint16{1}, 1, 1, 8)
		img, err := Decode(bytes.NewReader(data))
		require.NoError(t, err)
		assert.Equal(t, []uint16{0}, samplesOf(t, img))
		img, err = decode(data, profileLegacy)
		require.NoError(t, err)
		assert.Equal(t, []uint16{1}, samplesOf(t, img))
	})
}

// TestLegacyRejectsCorruptStreams: the fallback must not turn a corrupt
// stream into an image.
func TestLegacyRejectsCorruptStreams(t *testing.T) {
	samples := pattern("smooth", 16, 16, 65535)
	data := legacyEncode(samples, 16, 16, 16)
	for n := range len(data) {
		_, err := Decode(bytes.NewReader(data[:n]))
		require.Error(t, err, "prefix of %d bytes", n)
	}
	_, err := decode(append(data[:len(data)-2:len(data)-2], 0xFF, 0xD8), profileLegacy)
	require.ErrorIs(t, err, ErrInvalidData, "EOI is required")
}

// TestLegacyDICOM decodes legacy JPEG-LS frames embedded in a DICOM file
// (frames start at FF D8 FF F7), e.g. goxel's testdata/dicom/example3d.dcm:
//
//	JPEGLS_LEGACY_DICOM=/path/to/example3d.dcm go test -run TestLegacyDICOM -v
//
// Each frame must be rejected by the strict T.87 decoder, decoded by the
// legacy fallback, re-encode byte-identically with the legacy test encoder
// (proving the decode is the exact inverse of what wrote the file), and
// round-trip through the conformant coder.
func TestLegacyDICOM(t *testing.T) {
	path := os.Getenv("JPEGLS_LEGACY_DICOM")
	if path == "" {
		t.Skip("JPEGLS_LEGACY_DICOM not set")
	}
	file, err := os.ReadFile(path)
	require.NoError(t, err)

	frames := 0
	for off := 0; ; frames++ {
		i := bytes.Index(file[off:], []byte{0xFF, 0xD8, 0xFF, 0xF7})
		if i < 0 {
			break
		}
		start := off + i
		// The encapsulated fragment's item length precedes the frame.
		length := int(binary.LittleEndian.Uint32(file[start-4:]))
		require.LessOrEqual(t, start+length, len(file))
		frame := file[start : start+length]
		off = start + length

		_, err := decode(frame, profileT87)
		require.Error(t, err, "frame %d is not T.87", frames)

		img, err := Decode(bytes.NewReader(frame))
		require.NoError(t, err, "frame %d", frames)
		samples := samplesOf(t, img)
		w, h := img.Bounds().Dx(), img.Bounds().Dy()

		legacy := legacyEncode(samples, w, h, int(frame[6]))
		require.Equal(t, bytes.TrimRight(frame, "\x00"), legacy, "frame %d legacy re-encode", frames)

		conformant := mustEncode(t, samples, w, h, 0, 0)
		require.Equal(t, samples, mustDecodeT87(t, conformant), "frame %d T.87 round trip", frames)
		if frames == 0 {
			t.Logf("frame 0: %dx%d precision %d, legacy %d bytes -> T.87 %d bytes", w, h, frame[6], len(frame), len(conformant))
		}
	}
	require.NotZero(t, frames)
	t.Logf("verified %d legacy frames", frames)
}
