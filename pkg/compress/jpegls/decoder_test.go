package jpegls

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"io/fs"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseStream is a small valid T.87 stream:
// SOI(2) SOF55(13) SOS(10) scan EOI(2).
func baseStream(t testing.TB) ([]byte, []uint16) {
	samples := make([]uint16, 16*16)
	for i := range samples {
		samples[i] = uint16((i%16*7 + i/16*13) & 0xFF)
	}
	return mustEncode(t, samples, 16, 16, 8, 0), samples
}

const (
	sofOffset  = 2  // FF F7, length, P, Y, X, Nf, component spec
	sosOffset  = 15 // FF DA, length, Ns, Cs, Tm, NEAR, ILV, Al/Ah
	scanOffset = 25
)

// requireWellFormed: a decode either fails or returns a complete image.
func requireWellFormed(t testing.TB, data []byte) {
	t.Helper()
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		require.True(t, errors.Is(err, ErrInvalidData) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrImageTooLarge), "unclassified error: %v", err)
		return
	}
	require.False(t, img.Bounds().Empty())
}

func TestDecodeTruncationsAreErrors(t *testing.T) {
	base, _ := baseStream(t)
	for n := range len(base) {
		_, err := Decode(bytes.NewReader(base[:n]))
		require.ErrorIs(t, err, ErrInvalidData, "prefix of %d bytes", n)
	}
}

func TestDecodeBitFlipsNeverPanic(t *testing.T) {
	base, _ := baseStream(t)
	for i := range base {
		for bit := range 8 {
			m := slices.Clone(base)
			m[i] ^= 1 << bit
			requireWellFormed(t, m)
		}
	}
}

func TestDecodeGarbageNeverPanics(t *testing.T) {
	base, _ := baseStream(t)
	noise := lcg(99, 64*600, 255)
	for i := range 600 {
		// Random bytes, and a valid header followed by random scan data.
		garbage := make([]byte, 0, 96)
		for _, v := range noise[i*64 : i*64+64] {
			garbage = append(garbage, byte(v))
		}
		requireWellFormed(t, garbage)
		requireWellFormed(t, append(slices.Clone(base[:scanOffset]), garbage...))
	}
}

func TestDecodeTrailer(t *testing.T) {
	base, samples := baseStream(t)
	body := base[:len(base)-2]

	t.Run("EOI is mandatory", func(t *testing.T) {
		_, err := Decode(bytes.NewReader(body))
		require.ErrorIs(t, err, ErrInvalidData)
		_, err = Decode(bytes.NewReader(append(slices.Clone(body), 0xFF, 0xD8)))
		require.ErrorIs(t, err, ErrInvalidData)
	})
	t.Run("fill bytes before EOI", func(t *testing.T) {
		// DCMTK pads odd-length streams this way.
		assert.Equal(t, samples, mustDecodeT87(t, append(slices.Clone(body), 0xFF, 0xFF, 0xFF, 0xD9)))
	})
	t.Run("bytes after EOI are ignored", func(t *testing.T) {
		// DICOM pads fragments to even length after EOI.
		assert.Equal(t, samples, mustDecodeT87(t, append(slices.Clone(base), 0x00)))
	})
	t.Run("scan ending in a stuffed 0xFF", func(t *testing.T) {
		// Search for an image whose last scan byte is 0xFF: the encoder must
		// add the stuff byte so EOI is not swallowed.
		found := false
		for seed := range uint64(4000) {
			s := lcg(seed, 9, 255)
			data := mustEncode(t, s, 3, 3, 8, 0)
			if !bytes.HasSuffix(data, []byte{0xFF, 0x00, 0xFF, 0xD9}) {
				continue
			}
			found = true
			require.Equal(t, s, mustDecodeT87(t, data), "seed %d", seed)
		}
		require.True(t, found, "no stream ending in a stuffed 0xFF was generated")
	})
}

func TestDecodeHeaders(t *testing.T) {
	base, samples := baseStream(t)
	patch := func(offset int, b ...byte) []byte {
		m := slices.Clone(base)
		copy(m[offset:], b)
		return m
	}
	insert := func(offset int, b ...byte) []byte {
		return slices.Concat(base[:offset], b, base[offset:])
	}

	t.Run("accepted", func(t *testing.T) {
		for name, data := range map[string][]byte{
			"fill bytes before a marker": insert(sosOffset, 0xFF, 0xFF),
			"COM segment skipped":        insert(sofOffset, 0xFF, 0xFE, 0x00, 0x05, 'a', 'b', 'c'),
			"APP8 segment skipped":       insert(sofOffset, 0xFF, 0xE8, 0x00, 0x02),
			"DRI segment skipped":        insert(sosOffset, 0xFF, 0xDD, 0x00, 0x04, 0x00, 0x00),
			"component ID 7":             patch(sofOffset+10, 7, 0x11, 0, 0xFF, 0xDA, 0, 8, 1, 7),
		} {
			assert.Equal(t, samples, mustDecodeT87(t, data), name)
		}
	})

	for name, tc := range map[string]struct {
		data []byte
		want error
	}{
		"empty":                       {nil, ErrInvalidData},
		"missing SOI":                 {base[2:], ErrInvalidData},
		"SOS before SOF55":            {slices.Concat(base[:sofOffset], base[sosOffset:]), ErrInvalidData},
		"EOI before SOS":              {insert(sosOffset, 0xFF, 0xD9), ErrInvalidData},
		"precision 1":                 {patch(sofOffset+4, 1), ErrInvalidData},
		"precision 17":                {patch(sofOffset+4, 17), ErrInvalidData},
		"zero height":                 {patch(sofOffset+5, 0, 0), ErrInvalidData},
		"zero width":                  {patch(sofOffset+7, 0, 0), ErrInvalidData},
		"SOF55 length too short":      {patch(sofOffset+2, 0, 5), ErrInvalidData},
		"SOF55 length mismatch":       {patch(sofOffset+2, 0, 12), ErrInvalidData},
		"two components":              {patch(sofOffset+9, 2), ErrUnsupported},
		"subsampled component":        {patch(sofOffset+11, 0x21), ErrUnsupported},
		"component mapping table":     {patch(sofOffset+12, 1), ErrUnsupported},
		"SOS with zero components":    {patch(sosOffset+4, 0), ErrInvalidData},
		"SOS with too many":           {patch(sosOffset+4, 2), ErrInvalidData},
		"unknown component selector":  {patch(sosOffset+5, 9), ErrInvalidData},
		"SOS mapping table":           {patch(sosOffset+6, 1), ErrUnsupported},
		"NEAR above MAXVAL/2":         {patch(sosOffset+7, 128), ErrInvalidData},
		"line interleaved":            {patch(sosOffset+8, 1), ErrUnsupported},
		"point transform":             {patch(sosOffset+9, 1), ErrInvalidData},
		"LSE mapping table":           {insert(sosOffset, 0xFF, 0xF8, 0x00, 0x05, 2, 1, 1), ErrUnsupported},
		"unknown LSE ID":              {insert(sosOffset, 0xFF, 0xF8, 0x00, 0x03, 9), ErrInvalidData},
		"short LSE":                   {insert(sosOffset, 0xFF, 0xF8, 0x00, 0x05, 1, 0, 255), ErrInvalidData},
		"restart marker in scan":      {insert(scanOffset+4, 0xFF, 0xD0), ErrUnsupported},
		"other marker in scan":        {insert(scanOffset+4, 0xFF, 0xDA), ErrInvalidData},
		"segment longer than stream":  {patch(sofOffset+2, 0xFF, 0xFF), ErrInvalidData},
		"frame larger than the limit": {patch(sofOffset+5, 0xFF, 0xFF, 0xFF, 0xFF), ErrImageTooLarge},
	} {
		_, err := decode(tc.data, profileT87)
		assert.ErrorIs(t, err, tc.want, name)
		// The public entry point (with legacy fallback) must reject it too.
		_, err = Decode(bytes.NewReader(tc.data))
		assert.Error(t, err, name)
	}
}

// withMaxDecodedBytes runs fn with a temporary MaxDecodedBytes.
func withMaxDecodedBytes(limit int64, fn func()) {
	old := MaxDecodedBytes
	defer func() { MaxDecodedBytes = old }()
	MaxDecodedBytes = limit
	fn()
}

// allocatedBy returns the bytes allocated while fn runs.
func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestDecodeSizeLimit: the frame size claimed by SOF55 is checked against
// MaxDecodedBytes before anything is allocated, on the conformant and the
// legacy path alike.
func TestDecodeSizeLimit(t *testing.T) {
	base, samples := baseStream(t)
	rgb, err := testdata.ReadFile("testdata/rust/rgb-9x9-p16-n2.jls")
	require.NoError(t, err)
	legacy := legacyEncode(samples, 16, 16, 16)

	t.Run("limit is exact", func(t *testing.T) {
		// 16-bit samples count two bytes each, three components three times.
		for name, tc := range map[string]struct {
			data  []byte
			limit int64
		}{
			"16x16 8-bit":       {base, 256},
			"16x16 16-bit":      {mustEncode(t, samples, 16, 16, 16, 0), 512},
			"16x16 legacy":      {legacy, 512},
			"9x9 16-bit 3 comp": {rgb, 9 * 9 * 3 * 2},
		} {
			withMaxDecodedBytes(tc.limit-1, func() {
				_, err := Decode(bytes.NewReader(tc.data))
				require.ErrorIs(t, err, ErrImageTooLarge, name)
			})
			withMaxDecodedBytes(tc.limit, func() {
				_, err := Decode(bytes.NewReader(tc.data))
				require.NoError(t, err, name)
			})
		}
	})

	t.Run("zero or negative disables the cap", func(t *testing.T) {
		for _, limit := range []int64{0, -1} {
			withMaxDecodedBytes(limit, func() {
				_, err := Decode(bytes.NewReader(base))
				require.NoError(t, err)
			})
		}
	})

	t.Run("crafted dimensions are rejected without allocating", func(t *testing.T) {
		require.EqualValues(t, 1<<30, MaxDecodedBytes, "default limit")
		claim := func(stream []byte, height, width uint16) []byte {
			m := slices.Clone(stream)
			binary.BigEndian.PutUint16(m[sofOffset+5:], height)
			binary.BigEndian.PutUint16(m[sofOffset+7:], width)
			return m
		}
		for name, data := range map[string][]byte{
			"65535x65535 8-bit":         claim(base, 65535, 65535),
			"65535x65535 16-bit":        claim(mustEncode(t, samples, 16, 16, 16, 0), 65535, 65535),
			"65535x65535 16-bit legacy": claim(legacy, 65535, 65535),
			"65535x65535 16-bit RGB":    claim(rgb, 65535, 65535),    // 2.6e10 bytes: overflows 32 bits
			"32768x32768 16-bit legacy": claim(legacy, 32768, 32768), // exactly 2^31 bytes
			"just above the limit":      claim(base, 32768, 32769),
		} {
			var errAuto, errT87, errLegacy error
			allocated := allocatedBy(func() {
				_, errAuto = Decode(bytes.NewReader(data))
				_, errT87 = decode(data, profileT87)
				_, errLegacy = decode(data, profileLegacy)
			})
			require.ErrorIs(t, errAuto, ErrImageTooLarge, name)
			require.ErrorIs(t, errT87, ErrImageTooLarge, name)
			require.ErrorIs(t, errLegacy, ErrImageTooLarge, name)
			assert.Less(t, allocated, uint64(1<<20), "%s: bytes allocated while rejecting", name)
		}
	})

	t.Run("allocation follows the data, not the header", func(t *testing.T) {
		// 32768x32768 8-bit is exactly at the default limit, so the header is
		// accepted - but the 25-byte stream has no scan data, and decoding
		// (T.87, then the legacy retry) must fail without allocating the frame.
		data := slices.Clone(base[:scanOffset])
		binary.BigEndian.PutUint16(data[sofOffset+5:], 32768)
		binary.BigEndian.PutUint16(data[sofOffset+7:], 32768)
		var err error
		allocated := allocatedBy(func() { _, err = Decode(bytes.NewReader(data)) })
		require.ErrorIs(t, err, ErrInvalidData)
		require.ErrorContains(t, err, "truncated")
		// Only the per-line working buffers (proportional to the width).
		assert.Less(t, allocated, uint64(8<<20))
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk on fire") }

func TestDecodeReaderError(t *testing.T) {
	_, err := Decode(failingReader{})
	require.ErrorContains(t, err, "disk on fire")
}

// TestDecodeCorruptFixtures replays inputs that crashed or overflowed the
// dicos.rs decoder under fuzzing (error values no conformant encoder emits,
// which used to inflate the context statistics without bound).
func TestDecodeCorruptFixtures(t *testing.T) {
	entries, err := fs.ReadDir(testdata, "testdata/corrupt")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		data, err := testdata.ReadFile("testdata/corrupt/" + e.Name())
		require.NoError(t, err)
		_, err = Decode(bytes.NewReader(data))
		assert.ErrorIs(t, err, ErrInvalidData, e.Name())
	}
}

func TestDecodeThreeComponentErrors(t *testing.T) {
	data, err := testdata.ReadFile("testdata/rust/rgb-8x6-p8-n0.jls")
	require.NoError(t, err)
	img, err := Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.IsType(t, &image.RGBA{}, img)

	sos := []byte{0xFF, 0xDA, 0x00, 0x08}
	first := bytes.Index(data, sos)
	second := first + 10 + bytes.Index(data[first+10:], sos)
	require.Greater(t, second, first+10)
	for name, mutate := range map[string]func(m []byte) []byte{
		"duplicate component selector": func(m []byte) []byte { m[second+5] = m[first+5]; return m },
		"NEAR differs across scans":    func(m []byte) []byte { m[second+7] = 1; return m },
		"missing third scan":           func(m []byte) []byte { return append(m[:bytes.LastIndex(m, sos)], 0xFF, 0xD9) },
		"truncated second scan":        func(m []byte) []byte { return m[:second+14] },
	} {
		_, err := Decode(bytes.NewReader(mutate(slices.Clone(data))))
		assert.ErrorIs(t, err, ErrInvalidData, name)
	}
}
