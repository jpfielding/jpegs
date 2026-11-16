package jpeg2k

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var allBands = []Subband{SubbandLL, SubbandHL, SubbandLH, SubbandHH}

func blockRoundTrip(t *testing.T, coeffs []int32, w, h int, band Subband) {
	t.Helper()
	cb := encodeCodeBlock(coeffs, w, h, band)
	got, err := decodeCodeBlock(cb.data, w, h, band, cb.bitplanes, cb.passes)
	require.NoError(t, err)
	require.Equal(t, coeffs, got, "band %v %dx%d", band, w, h)
}

// A 4x4 constant-1000 image coded by OpenJPEG 2.5.4 (opj_compress -n 1) is a
// single LL block of -31768. Our coder must emit the identical 19-byte
// codeword and decode it back.
func TestEBCOTOpenJPEGCodeBlockInterop(t *testing.T) {
	coeffs := make([]int32, 16)
	for i := range coeffs {
		coeffs[i] = -31768
	}
	opj := []byte{0x11, 0x50, 0x54, 0xa0, 0xe2, 0xa0, 0x00, 0x03, 0x09, 0x09, 0x48, 0x13, 0x00, 0x61, 0x1e, 0x62, 0x9c, 0x48, 0x3f}
	cb := encodeCodeBlock(coeffs, 4, 4, SubbandLL)
	assert.Equal(t, 15, cb.bitplanes)
	assert.Equal(t, 43, cb.passes)
	assert.Equal(t, opj, cb.data, "MQ codeword not byte-identical to OpenJPEG")
	got, err := decodeCodeBlock(opj, 4, 4, SubbandLL, 15, 43)
	require.NoError(t, err)
	assert.Equal(t, coeffs, got)
}

// Golden 8x8 HL block, byte-identical with dicos.rs ebcot.rs golden_8x8.
func TestEBCOTGolden8x8(t *testing.T) {
	coeffs := make([]int32, 64)
	for i := range coeffs {
		coeffs[i] = int32((i*int(2654435761%97))%200) - 100
	}
	golden := []byte{
		17, 122, 213, 200, 44, 48, 19, 55, 152, 3, 114, 203, 128, 94, 228, 236, 23, 50, 37, 67,
		151, 36, 157, 218, 120, 179, 42, 102, 230, 143, 83, 188, 48, 23, 249, 14, 55, 180, 3, 181,
		230, 140, 33, 71, 227, 43, 255, 127,
	}
	cb := encodeCodeBlock(coeffs, 8, 8, SubbandHL)
	assert.Equal(t, golden, cb.data)
	assert.Equal(t, 7, cb.bitplanes)
	assert.Equal(t, 19, cb.passes)
	blockRoundTrip(t, coeffs, 8, 8, SubbandHL)
}

func TestEBCOTAllZeroIsEmpty(t *testing.T) {
	for _, band := range allBands {
		cb := encodeCodeBlock(make([]int32, 16), 4, 4, band)
		assert.Empty(t, cb.data)
		assert.Zero(t, cb.passes)
		assert.Zero(t, cb.bitplanes)
		got, err := decodeCodeBlock(nil, 4, 4, band, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, make([]int32, 16), got)
	}
}

func TestEBCOTPassCount(t *testing.T) {
	coeffs := make([]int32, 64)
	for i := range coeffs {
		coeffs[i] = int32(i - 32)
	}
	for _, band := range allBands {
		cb := encodeCodeBlock(coeffs, 8, 8, band)
		require.Positive(t, cb.bitplanes)
		assert.Equal(t, 3*cb.bitplanes-2, cb.passes)
	}
}

func TestEBCOTExhaustive1x1(t *testing.T) {
	for _, band := range allBands {
		for v := int32(-3); v <= 3; v++ {
			blockRoundTrip(t, []int32{v}, 1, 1, band)
		}
	}
}

func TestEBCOTDeterministic2x2(t *testing.T) {
	rng := uint64(0x9E3779B97F4A7C15)
	next := func() int32 {
		rng = rng*6364136223846793005 + 1
		return int32(rng>>33)%65 - 32
	}
	for _, band := range allBands {
		for range 300 {
			blockRoundTrip(t, []int32{next(), next(), next(), next()}, 2, 2, band)
		}
	}
}

func TestEBCOTRunLength(t *testing.T) {
	for _, band := range allBands {
		deep := make([]int32, 64)
		deep[57] = 12345
		blockRoundTrip(t, deep, 8, 8, band)

		for row := range 4 {
			b := make([]int32, 32)
			b[row*8] = -777
			blockRoundTrip(t, b, 8, 4, band)
		}

		sparse := make([]int32, 64*64)
		sparse[0] = 3
		sparse[64*30+31] = -9
		sparse[64*63+63] = 100
		blockRoundTrip(t, sparse, 64, 64, band)
	}
}

func TestEBCOTStripeBoundaries(t *testing.T) {
	for _, band := range allBands {
		for _, d := range [][2]int{{1, 1}, {3, 5}, {7, 4}, {7, 6}, {13, 13}, {17, 3}, {5, 17}} {
			w, h := d[0], d[1]
			coeffs := make([]int32, w*h)
			for i := range coeffs {
				coeffs[i] = int32(i*37%511) - 255
			}
			blockRoundTrip(t, coeffs, w, h, band)
		}
	}
}

func TestEBCOTTruncatedNoPanic(t *testing.T) {
	coeffs := make([]int32, 64*8)
	for i := range coeffs {
		coeffs[i] = int32(i*101%4096) - 2048
	}
	cb := encodeCodeBlock(coeffs, 64, 8, SubbandLH)
	for cut := 0; cut <= len(cb.data); cut++ {
		_, _ = decodeCodeBlock(cb.data[:cut], 64, 8, SubbandLH, cb.bitplanes, cb.passes)
	}
}

func TestEBCOTInvalidMetadata(t *testing.T) {
	_, err := decodeCodeBlock(nil, 4, 4, SubbandLL, 3, 100)
	assert.ErrorIs(t, err, ErrInvalidData)
	_, err = decodeCodeBlock(nil, 4, 4, SubbandLL, 0, 1)
	assert.ErrorIs(t, err, ErrInvalidData)
	_, err = decodeCodeBlock(nil, 4, 4, SubbandLL, 32, 1)
	assert.ErrorIs(t, err, ErrInvalidData)
}

// Seeded stand-in for dicos.rs's prop_roundtrip (400 cases).
func TestEBCOTRandomRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 400 {
		w, h := 1+r.IntN(68), 1+r.IntN(68)
		band := allBands[r.IntN(4)]
		coeffs := make([]int32, w*h)
		allZero := true
		for i := range coeffs {
			v := int32(r.IntN(1 << 17))
			if r.IntN(2) == 1 {
				v = -v
			}
			coeffs[i] = v
			allZero = allZero && v == 0
		}
		cb := encodeCodeBlock(coeffs, w, h, band)
		if allZero {
			require.Empty(t, cb.data)
		} else {
			require.Equal(t, 3*cb.bitplanes-2, cb.passes)
		}
		blockRoundTrip(t, coeffs, w, h, band)
	}
}
