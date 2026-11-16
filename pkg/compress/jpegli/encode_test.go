package jpegli

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from dicos.rs crates/jpegli/src/encode.rs tests.

func TestEncode_Structure(t *testing.T) {
	data := encodeOpts(t, 2, 2, []int{100, 200, 300, 400}, Options{})
	assert.Equal(t, []byte{0xFF, 0xD8}, data[:2], "SOI")
	assert.Equal(t, []byte{0xFF, 0xD9}, data[len(data)-2:], "EOI")
	for _, m := range []byte{0xC3, 0xC4, 0xDA} {
		assert.Len(t, markerPositions(data, m), 1, "marker FF%02X", m)
	}
	assert.Empty(t, markerPositions(data, 0xDD), "no DRI by default")

	sof := markerPositions(encodeOpts(t, 300, 2, make([]int, 600), Options{}), 0xC3)[0]
	d := encodeOpts(t, 300, 2, make([]int, 600), Options{})
	assert.Equal(t, []byte{16, 0, 2, 0x01, 0x2C, 1}, d[sof+4:sof+10], "P, Y, X, Nf")
}

func TestEncode_DefaultsAndOptionsInSOS(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, image.NewGray(image.Rect(0, 0, 3, 3)), nil))
	sof := markerPositions(buf.Bytes(), 0xC3)[0]
	assert.Equal(t, byte(8), buf.Bytes()[sof+4], "Gray defaults to P=8")

	data := encodeOpts(t, 4, 4, detImage(4, 4, 12), Options{Predictor: 6, PointTransform: 3, Precision: 12})
	sos := markerPositions(data, 0xDA)[0]
	assert.Equal(t, []byte{6, 0, 3}, data[sos+7:sos+10], "Ss, Se, Ah|Al")
}

func TestEncode_RejectsInvalidOptions(t *testing.T) {
	g := gray16(2, 2, []int{1, 2, 3, 4})
	cases := []struct {
		name string
		img  image.Image
		o    Options
	}{
		{"predictor_8", g, Options{Predictor: 8}},
		{"predictor_neg", g, Options{Predictor: -1}},
		{"precision_1", g, Options{Precision: 1}},
		{"precision_17", g, Options{Precision: 17}},
		{"pt_ge_precision", g, Options{Precision: 8, PointTransform: 8}},
		{"pt_neg", g, Options{PointTransform: -1}},
		{"sample_exceeds_precision", gray16(2, 2, []int{1, 2, 3, 256}), Options{Precision: 8}},
		{"restart_overflow", gray16(300, 1, make([]int, 300)), Options{RestartIntervalRows: 219}},
		{"restart_neg", g, Options{RestartIntervalRows: -1}},
		{"zero_width", image.NewGray16(image.Rect(0, 0, 0, 4)), Options{}},
		{"too_wide", image.NewGray(image.Rect(0, 0, 65536, 1)), Options{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.ErrorIs(t, Encode(&buf, c.img, &c.o), ErrInvalidParameter)
			assert.Zero(t, buf.Len(), "nothing written on error")
		})
	}
	// The largest restart interval that fits: 218 rows × 300 = 65400.
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, gray16(300, 1, make([]int, 300)), &Options{RestartIntervalRows: 218}))
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestEncode_PropagatesWriteError(t *testing.T) {
	err := Encode(failWriter{}, gray16(2, 2, []int{1, 2, 3, 4}), nil)
	require.ErrorContains(t, err, "disk full")
}

// The encoder honours bounds.Min (sub-images).
func TestEncode_SubImage(t *testing.T) {
	full := gray16(6, 5, detImage(6, 5, 16))
	sub := full.SubImage(image.Rect(2, 1, 5, 4)).(*image.Gray16)
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, sub, nil))
	img, err := Decode(&buf)
	require.NoError(t, err)
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			assert.Equal(t, sub.Gray16At(x+2, y+1), img.(*image.Gray16).Gray16At(x, y))
		}
	}
}

// T.81 H.1.2.1 takes differences modulo 2^16, not 2^P'. At P = 12, 3 then
// 4000 (predictor 1) must code +3997 (category 12), not 3997-4096 = -99
// (category 7): libjpeg-turbo reconstructs modulo 2^16 and would output
// 0xFFA0 for the wrapped form, as it does for dicos.rs output.
func TestEncode_DifferencesAreModulo2p16(t *testing.T) {
	data := encodeOpts(t, 2, 1, []int{3, 4000}, Options{Precision: 12})
	br := &bitReader{data: data[markerPositions(data, 0xDA)[0]+10:]}
	ht := defaultTable()
	var cats []int
	for range 2 {
		s, err := br.decodeHuffman(ht)
		require.NoError(t, err)
		_, err = br.readBits(s)
		require.NoError(t, err)
		cats = append(cats, s)
	}
	assert.Equal(t, []int{11, 12}, cats, "first: 3-2048 = -2045; second: +3997")
	assert.Equal(t, []int{3, 4000}, decodeSamples(t, data))
}
