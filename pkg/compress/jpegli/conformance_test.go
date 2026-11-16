package jpegli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Interop fixtures from libjpeg-turbo cjpeg (dicos.rs tests/interop.rs
// decode_cjpeg_fixtures, plus Go's predictor 5/6 additions).
func TestInterop_CjpegFixtures(t *testing.T) {
	cases := []struct {
		jpg, src string
		pt       int
	}{
		{"g16_psv1.jpg", "g16.pgm", 0},
		{"g16_psv2.jpg", "g16.pgm", 0},
		{"g16_psv3.jpg", "g16.pgm", 0},
		{"g16_psv4.jpg", "g16.pgm", 0},
		{"g16_psv5.jpg", "g16.pgm", 0},
		{"g16_psv6.jpg", "g16.pgm", 0},
		{"g16_psv7.jpg", "g16.pgm", 0},
		{"g16_restart.jpg", "g16.pgm", 0},
		{"g16_psv1_pt1.jpg", "g16.pgm", 1},
		{"r16_psv1.jpg", "r16.pgm", 0},
		{"g8_psv1.jpg", "g8.pgm", 0},
		// Q12: negative odd (Rb-Rc), (Ra-Rc) distinguish >>1 from /2.
		{"q12_r12_psv5.jpg", "q12_r12.pgm", 0},
		{"q12_r12_psv6.jpg", "q12_r12.pgm", 0},
		{"q12_r16_psv5.jpg", "q12_r16.pgm", 0},
		{"q12_r16_psv6.jpg", "q12_r16.pgm", 0},
	}
	for _, c := range cases {
		t.Run(c.jpg, func(t *testing.T) {
			w, h, want := readPGM(t, testdata("interop", c.src))
			data, err := os.ReadFile(testdata("interop", c.jpg))
			require.NoError(t, err)
			img, err := decode(data, profileT81)
			require.NoError(t, err)
			require.Equal(t, w, img.Bounds().Dx())
			require.Equal(t, h, img.Bounds().Dy())
			assert.Equal(t, shifted(want, c.pt), samplesOf(t, img))
		})
	}
}

// TestFrame356 decodes a third-party 312x312 16-bit stream whose DHT
// includes SSSS=16. The expected hash is of the big-endian samples written
// by libjpeg-turbo 3.2.1 djpeg (pylibjpeg-libjpeg and dicos.rs agree). The
// pre-conformance decoder read 16 appended bits after SSSS=16, desynced,
// and returned a wrong partial image without error.
func TestFrame356(t *testing.T) {
	data, err := os.ReadFile(testdata("frame356.jpg"))
	require.NoError(t, err)
	img, err := decode(data, profileT81)
	require.NoError(t, err)
	px := samplesOf(t, img)
	require.Len(t, px, 312*312)
	be := make([]byte, 0, 2*len(px))
	for _, v := range px {
		be = append(be, byte(v>>8), byte(v))
	}
	sum := sha256.Sum256(be)
	assert.Equal(t, "55589fe11cebee787a1a431ca3d8f2e7093bdce54e0bde703f97260a7a035309", hex.EncodeToString(sum[:]))
}

// Legacy fixtures were written by the v0.2.0 encoder (testdata/legacy).
func TestLegacyFixtures(t *testing.T) {
	cases := []struct {
		name   string
		ssss16 bool // contains SSSS=16: the T.81 reading is wrong or fails
		pred56 bool // predictor 5/6 without SSSS=16: undetectable, reads as T.81
	}{
		{name: "ssss16-p1-16x8", ssss16: true},
		{name: "ssss16-p5-16x8", ssss16: true},
		{name: "ssss16-p6-16x8", pred56: true},
		{name: "ssss16-p7-16x8", ssss16: true},
		{name: "random16-p1-13x7"},
		{name: "gradient8-p1-16x16"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, want := readPGM(t, testdata("legacy", c.name+".pgm"))
			data, err := os.ReadFile(testdata("legacy", c.name+".jpg"))
			require.NoError(t, err)

			auto := decodeSamples(t, data)
			strict, err := decode(data, profileT81)
			switch {
			case c.ssss16:
				assert.Equal(t, want, auto)
				if err == nil {
					assert.NotEqual(t, want, samplesOf(t, strict))
				}
				legacy, err := decode(data, profileLegacy)
				require.NoError(t, err)
				assert.Equal(t, want, samplesOf(t, legacy))
			case c.pred56:
				// Truncating /2 vs >>1 changes only predictions, never
				// the parse, so nothing marks the stream as legacy.
				require.NoError(t, err)
				assert.Equal(t, samplesOf(t, strict), auto)
				assert.NotEqual(t, want, auto)
				legacy, err := decode(data, profileLegacy)
				require.NoError(t, err)
				assert.Equal(t, want, samplesOf(t, legacy))
			default:
				require.NoError(t, err)
				assert.Equal(t, want, samplesOf(t, strict))
				assert.Equal(t, want, auto)
			}
		})
	}
}

// The fallback must not rescue a corrupt stream, nor apply to streams the
// legacy encoder could not have written.
func TestLegacyFallbackScope(t *testing.T) {
	data, err := os.ReadFile(testdata("legacy", "ssss16-p1-16x8.jpg"))
	require.NoError(t, err)

	// Truncated legacy stream: both decoders fail.
	_, err = Decode(bytes.NewReader(data[:len(data)-6]))
	require.Error(t, err)
	_, err = decode(data[:len(data)-6], profileLegacy)
	require.ErrorIs(t, err, ErrTruncated)

	// The legacy decoder only accepts 0x7FFF after SSSS=16.
	conformant := encodeOpts(t, 1, 1, []int{0}, Options{})
	_, err = decode(append(conformant[:len(conformant)-3:len(conformant)-3], 0xFF, 0x00, 0xFB, 0xFF, 0xFF, 0xFF, 0x00, 0xFF, 0xD9), profileLegacy)
	require.ErrorIs(t, err, ErrInvalidData)

	// Same scan, but a component ID the legacy encoder never wrote.
	m := bytes.Clone(data)
	sof := markerPositions(m, 0xC3)[0]
	sos := markerPositions(m, 0xDA)[0]
	m[sof+10], m[sos+5] = 2, 2
	_, err = Decode(bytes.NewReader(m))
	require.Error(t, err)
	_, err = decode(m, profileLegacy)
	require.ErrorIs(t, err, ErrUnsupported)
}

// The current encoder writes T.81 SSSS=16 (no appended bits). Decode
// (auto) must return the T.81 reading even though the stream carries the
// legacy encoder's headers and so is also tried as legacy.
func TestEncoder_SSSS16IsConformant(t *testing.T) {
	_, _, px := readPGM(t, testdata("legacy", "ssss16-p1-16x8.pgm"))
	for pred := 1; pred <= 7; pred++ {
		data := encodeOpts(t, 16, 8, px, Options{Predictor: pred})
		img, err := decode(data, profileT81)
		require.NoError(t, err)
		assert.Equal(t, px, samplesOf(t, img), "predictor %d", pred)
		assert.Equal(t, px, decodeSamples(t, data), "predictor %d", pred)
	}
	data := encodeOpts(t, 16, 8, px, Options{})
	legacy, err := os.ReadFile(testdata("legacy", "ssss16-p1-16x8.jpg"))
	require.NoError(t, err)
	assert.Less(t, len(data), len(legacy), "no 16 appended bits per SSSS=16")
}

// dicos.rs scan.rs ssss16_emits_no_appended_bits_exact_vector: a 1x1 16-bit
// image of 0 is predicted 32768, so DIFF = -32768 => category 16, whose
// 14-bit code 0b11111111111110 pads to 0xFF (stuffed) 0xFB.
func TestSSSS16_ExactVector(t *testing.T) {
	data := encodeOpts(t, 1, 1, []int{0}, Options{})
	sos := markerPositions(data, 0xDA)[0]
	assert.Equal(t, []byte{0xFF, 0x00, 0xFB, 0xFF, 0xD9}, data[sos+10:])
	assert.Equal(t, []int{0}, decodeSamples(t, data))
}

// dicos.rs ssss16_rejected_below_full_precision: the same entropy decoded
// as an 8-bit stream must be rejected, not masked into a wrong sample.
func TestSSSS16_RejectedBelowP16(t *testing.T) {
	data := encodeOpts(t, 1, 1, []int{0}, Options{})
	sof := markerPositions(data, 0xC3)[0]
	data[sof+4] = 8
	_, err := Decode(bytes.NewReader(data))
	require.ErrorIs(t, err, ErrInvalidData)

	// Pt = 1 at P = 16 makes P' = 15: also illegal.
	data = encodeOpts(t, 1, 1, []int{0}, Options{})
	sos := markerPositions(data, 0xDA)[0]
	data[sos+9] = 1
	_, err = decode(data, profileT81)
	require.ErrorIs(t, err, ErrInvalidData)
}

func TestSSSS16_RoundTripBoundaryValues(t *testing.T) {
	px := []int{0, 32768, 65535, 32767, 0, 32768}
	assert.Equal(t, px, decodeSamples(t, encodeOpts(t, 3, 2, px, Options{})))
}

// Ported from scan.rs predictor tests, plus the Q12 cases: T.81 Table H.1
// halves with an arithmetic shift, which floors negative odd values.
func TestPredict(t *testing.T) {
	// Interior pixel: Ra=10, Rb=20, Rc=5.
	want := map[int]int{1: 10, 2: 20, 3: 5, 4: 25, 5: 17, 6: 22, 7: 15}
	for sel, v := range want {
		assert.Equal(t, v, predict(sel, 10, 20, 5), "predictor %d", sel)
	}
	// Rb-Rc = -3, Ra-Rc = -5: shift gives -2 and -3 where /2 gives -1, -2.
	assert.Equal(t, 100-2, predict(5, 100, 7, 10))
	assert.Equal(t, 7-3, predict(6, 5, 7, 10))
	assert.Equal(t, 100-1, predictLegacy(5, 100, 7, 10))
	assert.Equal(t, 7-2, predictLegacy(6, 5, 7, 10))
}

func TestBitWriterReaderRoundTrip(t *testing.T) {
	bw := bitWriter{}
	bw.writeBits(0b101, 3)
	bw.writeBits(0b1100, 4)
	bw.writeBits(0b1, 1)
	bw.writeBits(0xFF, 8) // stuffed
	bw.flush()
	assert.Equal(t, []byte{0xB9, 0xFF, 0x00}, bw.out)

	br := &bitReader{data: bw.out}
	for _, c := range []struct{ n, want int }{{3, 0b101}, {4, 0b1100}, {1, 1}, {8, 0xFF}} {
		v, err := br.readBits(c.n)
		require.NoError(t, err)
		assert.Equal(t, c.want, v)
	}
	_, err := br.readBits(1)
	require.ErrorIs(t, err, ErrTruncated)
}

func TestBitReader_StopsAtMarker(t *testing.T) {
	// One data byte, then fill bytes and RST0: never read as data.
	br := &bitReader{data: []byte{0xAA, 0xFF, 0xFF, 0xD0, 0x55}}
	v, err := br.readBits(8)
	require.NoError(t, err)
	assert.Equal(t, 0xAA, v)
	_, err = br.readBits(1)
	require.ErrorIs(t, err, ErrInvalidData, "RST without DRI")

	br = &bitReader{data: []byte{0xAA, 0xFF, 0xD0, 0x55}, restarts: true}
	_, err = br.readBits(12)
	require.ErrorIs(t, err, ErrTruncated, "no zero padding")
	var te *TruncatedError
	require.ErrorAs(t, err, &te)
	assert.Equal(t, 1, te.Offset)
}
