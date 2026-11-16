package jpeg2k

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func patternPixels(kind, w, h int) []uint16 {
	n := w * h
	v := make([]uint16, n)
	switch kind {
	case 0:
		for i := range v {
			v[i] = 40000
		}
	case 1:
		for i := range v {
			v[i] = uint16(((i%w)*250 + (i/w)*70) % 65536)
		}
	case 2:
		s := uint64(0x9E3779B97F4A7C15) ^ uint64(w)<<20 ^ uint64(h)
		for i := range v {
			s = s*6364136223846793005 + 1
			v[i] = uint16(s >> 40)
		}
	default:
		v[0], v[n/2], v[n-1] = 65535, 32768, 1
	}
	return v
}

func TestRoundTripMatrix(t *testing.T) {
	dims := []int{1, 2, 5, 13, 64, 130, 200}
	if testing.Short() {
		dims = []int{1, 5, 64}
	}
	for _, w := range dims {
		for _, h := range dims {
			for _, nl := range []int{0, 1, 5} {
				for _, cb := range [][2]int{{2, 2}, {6, 6}} {
					for kind := range 4 {
						px := patternPixels(kind, w, h)
						opts := &Options{DecompLevels: nl, CodeBlockWidthExp: cb[0], CodeBlockHeightExp: cb[1]}
						data := encodeBytes(t, gray16From(w, h, px), opts)
						for _, p := range []LegacyPolicy{LegacyAuto, StandardOnly} {
							img, err := DecodeWithPolicy(bytes.NewReader(data), p)
							require.NoError(t, err, "%dx%d %+v k%d", w, h, opts, kind)
							require.Equal(t, px, gray16Pixels(img.(*image.Gray16)), "%dx%d %+v k%d", w, h, opts, kind)
						}
					}
				}
			}
		}
	}
}

func TestEncodeSubImage(t *testing.T) {
	full := gray16From(8, 8, patternPixels(2, 8, 8))
	sub := full.SubImage(image.Rect(3, 2, 8, 7)).(*image.Gray16)
	img, err := decodeBytes(encodeBytes(t, sub, nil))
	require.NoError(t, err)
	assert.Equal(t, image.Rect(0, 0, 5, 5), img.Bounds())
	assert.Equal(t, gray16Pixels(sub), gray16Pixels(img.(*image.Gray16)))
}

func TestCodestreamFraming(t *testing.T) {
	data := encodeBytes(t, gray16From(8, 8, patternPixels(0, 8, 8)), nil)
	assert.Equal(t, []byte{0xFF, 0x4F}, data[:2])
	assert.Equal(t, []byte{0xFF, 0xD9}, data[len(data)-2:])
	assert.True(t, bytes.Contains(data, []byte{0xFF, 0x93}), "conformant SOD")
	assert.False(t, bytes.Contains(data, []byte{0xFF, 0xD3}), "no legacy SOD")

	sot := bytes.Index(data, []byte{0xFF, 0x90})
	psot := binary.BigEndian.Uint32(data[sot+6:])
	assert.Equal(t, len(data)-2, sot+int(psot), "Psot reaches EOC")
}

// εb = 16/17/17/18 for LL/HL/LH/HH; SPqcd bytes are εb << 3, guard bits 2.
func TestQCDExponents(t *testing.T) {
	data := encodeBytes(t, gray16From(4, 4, patternPixels(0, 4, 4)), &Options{DecompLevels: 1, CodeBlockWidthExp: 6, CodeBlockHeightExp: 6})
	q := bytes.Index(data, []byte{0xFF, 0x5C})
	assert.Equal(t, []byte{0x00, 0x07, 0x40, 16 << 3, 17 << 3, 17 << 3, 18 << 3}, data[q+2:q+9])
}

func TestDecodeConfig(t *testing.T) {
	data := encodeBytes(t, gray16From(20, 12, patternPixels(1, 20, 12)), nil)
	cfg, err := DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, image.Config{ColorModel: color.Gray16Model, Width: 20, Height: 12}, cfg)

	_, err = DecodeConfig(bytes.NewReader([]byte{0xFF, 0x4F}))
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestDecodeInvalidData(t *testing.T) {
	for _, d := range [][]byte{nil, {0, 0}, {0xFF, 0x4F}, {0xFF, 0x4F, 0xFF}} {
		_, err := decodeBytes(d)
		assert.ErrorIs(t, err, ErrInvalidData, "% X", d)
	}
}

func TestEncodeRejects(t *testing.T) {
	img := gray16From(8, 8, patternPixels(0, 8, 8))
	for _, opts := range []*Options{
		{DecompLevels: 5, CodeBlockWidthExp: 11, CodeBlockHeightExp: 6},
		{DecompLevels: 5, CodeBlockWidthExp: 6, CodeBlockHeightExp: 1},
		{DecompLevels: 5, CodeBlockWidthExp: 8, CodeBlockHeightExp: 8},
		{DecompLevels: 33, CodeBlockWidthExp: 6, CodeBlockHeightExp: 6},
		{DecompLevels: -1, CodeBlockWidthExp: 6, CodeBlockHeightExp: 6},
		{},
	} {
		assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, opts), ErrInvalidData, "%+v", opts)
	}
	for _, img := range []image.Image{image.NewGray(image.Rect(0, 0, 4, 4)), image.NewRGBA(image.Rect(0, 0, 4, 4))} {
		assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, nil), ErrUnsupported)
	}
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, image.NewGray16(image.Rect(0, 0, 0, 4)), nil), ErrInvalidData)
	// 32769 columns cross the 2^15 default precinct.
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, image.NewGray16(image.Rect(0, 0, 32769, 1)), nil), ErrUnsupported)
}

// --- support matrix -------------------------------------------------------

func validStream(t *testing.T) []byte {
	t.Helper()
	px := make([]uint16, 256)
	for i := range px {
		px[i] = uint16((i%16)*400 + (i/16)*40)
	}
	return encodeBytes(t, gray16From(16, 16, px), &Options{DecompLevels: 2, CodeBlockWidthExp: 6, CodeBlockHeightExp: 6})
}

func markerOff(t *testing.T, data []byte, m uint16) int {
	t.Helper()
	i := bytes.Index(data, binary.BigEndian.AppendUint16(nil, m))
	require.GreaterOrEqual(t, i, 0)
	return i
}

func TestSupportMatrixRejections(t *testing.T) {
	type mutate func(t *testing.T, d []byte) []byte
	set := func(m uint16, off int, b ...byte) mutate {
		return func(t *testing.T, d []byte) []byte {
			copy(d[markerOff(t, d, m)+off:], b)
			return d
		}
	}
	insertBeforeSOT := func(seg ...byte) mutate {
		return func(t *testing.T, d []byte) []byte {
			sot := markerOff(t, d, MarkerSOT)
			return append(append(append([]byte(nil), d[:sot]...), seg...), d[sot:]...)
		}
	}
	for _, tc := range []struct {
		name string
		f    mutate
		want error
	}{
		{"8-bit Ssiz", set(MarkerSIZ, 40, 7), ErrUnsupported},
		{"signed Ssiz", set(MarkerSIZ, 40, 0x8F), ErrUnsupported},
		{"Csiz=3", set(MarkerSIZ, 38, 0, 3), ErrInvalidData}, // Lsiz no longer matches
		{"multi-tile", set(MarkerSIZ, 22, 0, 0, 0, 8), ErrUnsupported},
		{"non-zero origin", set(MarkerSIZ, 14, 0, 0, 0, 1), ErrUnsupported},
		{"RLCP progression", set(MarkerCOD, 5, 1), ErrUnsupported},
		{"two layers", set(MarkerCOD, 6, 0, 2), ErrUnsupported},
		{"MCT", set(MarkerCOD, 8, 1), ErrUnsupported},
		{"cb_style", set(MarkerCOD, 12, 1), ErrUnsupported},
		{"9/7 transform", set(MarkerCOD, 13, 0), ErrUnsupported},
		{"SOP flag", set(MarkerCOD, 4, 2), ErrUnsupported},
		{"precinct flag", set(MarkerCOD, 4, 1), ErrInvalidData}, // flag implies precinct bytes the COD lacks
		{"quantized QCD", set(MarkerQCD, 4, 0x41), ErrUnsupported},
		{"Mb >= 32", set(MarkerQCD, 4, 0xE0, 0xF8), ErrUnsupported},
		{"εb = G = 0", set(MarkerQCD, 4, 0x00, 0x00), ErrInvalidData},
		{"TNsot=2", set(MarkerSOT, 11, 2), ErrUnsupported},
		{"tile index 1", set(MarkerSOT, 4, 0, 1), ErrUnsupported},
		{"COC", insertBeforeSOT(0xFF, 0x53, 0x00, 0x03, 0x00), ErrUnsupported},
		{"POC", insertBeforeSOT(0xFF, 0x5F, 0x00, 0x03, 0x00), ErrUnsupported},
		{"unknown marker", insertBeforeSOT(0xFF, 0x30, 0x00, 0x02), ErrInvalidData},
		{"duplicate COD", func(t *testing.T, d []byte) []byte {
			c := markerOff(t, d, MarkerCOD)
			return insertBeforeSOT(d[c:c+14]...)(t, d)
		}, ErrInvalidData},
		{"trailing garbage", func(_ *testing.T, d []byte) []byte { return append(d, 0) }, ErrInvalidData},
		{"missing EOC", func(_ *testing.T, d []byte) []byte { return d[:len(d)-2] }, ErrInvalidData},
		{"Go legacy Psot on a T.800 stream", func(t *testing.T, d []byte) []byte {
			s := markerOff(t, d, MarkerSOT)
			binary.BigEndian.PutUint32(d[s+6:], binary.BigEndian.Uint32(d[s+6:])-2)
			return d
		}, ErrInvalidData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeBytes(tc.f(t, validStream(t)))
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

// Psot = 0 means "to EOC" and is accepted; COM segments are skipped.
func TestAcceptsPsotZeroAndComments(t *testing.T) {
	d := validStream(t)
	s := markerOff(t, d, MarkerSOT)
	binary.BigEndian.PutUint32(d[s+6:], 0)
	com := []byte{0xFF, 0x64, 0x00, 0x06, 0x00, 0x01, 'h', 'i'}
	d = append(append(append([]byte(nil), d[:s]...), com...), d[s:]...)
	_, err := decodeBytes(d)
	require.NoError(t, err)
}

func TestDecodeRejectsMultiPrecinct(t *testing.T) {
	d := validStream(t)
	s := markerOff(t, d, MarkerSIZ)
	binary.BigEndian.PutUint32(d[s+6:], 32769)  // Xsiz
	binary.BigEndian.PutUint32(d[s+22:], 32769) // XTsiz
	_, err := decodeBytes(d)
	assert.ErrorIs(t, err, ErrUnsupported)
}

func TestTruncationNeverPanics(t *testing.T) {
	withMaxDecodedBytes(t, 1<<20)
	d := validStream(t)
	for cut := range len(d) {
		_, err := decodeBytes(d[:cut])
		require.Error(t, err, "cut %d", cut)
	}
	_, err := decodeBytes(d)
	require.NoError(t, err)
}
