package jpeg2k

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mb31(bandKind, int) int { return 31 } // the largest decodable Mb

func tilePattern(kind, w, h int) []int32 {
	n := w * h
	v := make([]int32, n)
	switch kind {
	case 0:
		for i := range v {
			v[i] = 1234
		}
	case 1:
		for i := range v {
			v[i] = int32(((i%w)*3 + (i/w)*5) % 4096)
		}
	case 2:
		s := uint64(0x9E3779B97F4A7C15) ^ uint64(w)<<20 ^ uint64(h)
		for i := range v {
			s = s*6364136223846793005 + 1
			v[i] = int32(s>>40) & 0xFFF
		}
	default:
		v[0], v[n/2], v[n-1] = 2000, -1500, 999
	}
	return v
}

func tileRoundTrip(t *testing.T, comp []int32, w, h, nl, xcb, ycb int) {
	t.Helper()
	enc, err := encodeTile(comp, w, h, nl, xcb, ycb, mb31)
	require.NoError(t, err, "encode %dx%d nl=%d cb=(%d,%d)", w, h, nl, xcb, ycb)
	dec, err := decodeTile(enc, w, h, nl, xcb, ycb, mb31)
	require.NoError(t, err, "decode %dx%d nl=%d cb=(%d,%d)", w, h, nl, xcb, ycb)
	require.Equal(t, comp, dec, "%dx%d nl=%d cb=(%d,%d)", w, h, nl, xcb, ycb)
}

func TestTileRoundTripMatrix(t *testing.T) {
	dims := []int{1, 2, 3, 5, 8, 13, 16, 31, 64, 65, 130}
	if testing.Short() {
		dims = []int{1, 2, 5, 13, 65}
	}
	for _, w := range dims {
		for _, h := range dims {
			for _, nl := range []int{0, 1, 2, 5} {
				for _, cb := range [][2]int{{2, 2}, {6, 6}, {10, 2}} {
					for kind := range 4 {
						tileRoundTrip(t, tilePattern(kind, w, h), w, h, nl, cb[0], cb[1])
					}
				}
			}
		}
	}
}

func TestTileAllZeroIsEmptyPackets(t *testing.T) {
	comp := make([]int32, 64*48)
	enc, err := encodeTile(comp, 64, 48, 5, 6, 6, mb31)
	require.NoError(t, err)
	assert.Equal(t, make([]byte, 6), enc, "one empty packet per resolution")
	dec, err := decodeTile(enc, 64, 48, 5, 6, 6, mb31)
	require.NoError(t, err)
	assert.Equal(t, comp, dec)
}

func TestTileTightMb(t *testing.T) {
	w, h, nl := 31, 17, 3
	comp := tilePattern(2, w, h)
	tight := -1
	for mb := 0; mb <= 31; mb++ {
		if _, err := encodeTile(comp, w, h, nl, 4, 4, func(bandKind, int) int { return mb }); err == nil {
			tight = mb
			break
		}
	}
	require.Positive(t, tight)
	fixed := func(bandKind, int) int { return tight }
	enc, err := encodeTile(comp, w, h, nl, 4, 4, fixed)
	require.NoError(t, err)
	dec, err := decodeTile(enc, w, h, nl, 4, 4, fixed)
	require.NoError(t, err)
	assert.Equal(t, comp, dec)
	_, err = encodeTile(comp, w, h, nl, 4, 4, func(bandKind, int) int { return tight - 1 })
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestTileMismatchedMbErrors(t *testing.T) {
	comp := tilePattern(1, 32, 32)
	enc, err := encodeTile(comp, 32, 32, 2, 6, 6, mb31)
	require.NoError(t, err)
	_, err = decodeTile(enc, 32, 32, 2, 6, 6, func(bandKind, int) int { return 5 })
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestTileTruncatedNeverPanics(t *testing.T) {
	comp := tilePattern(2, 16, 13)
	enc, err := encodeTile(comp, 16, 13, 2, 4, 4, mb31)
	require.NoError(t, err)
	for cut := range len(enc) {
		_, err := decodeTile(enc[:cut], 16, 13, 2, 4, 4, mb31)
		require.Error(t, err, "cut %d", cut)
	}
	dec, err := decodeTile(enc, 16, 13, 2, 4, 4, mb31)
	require.NoError(t, err)
	assert.Equal(t, comp, dec)
}

func TestTileTrailingBytesRejected(t *testing.T) {
	comp := tilePattern(1, 8, 8)
	enc, err := encodeTile(comp, 8, 8, 1, 6, 6, mb31)
	require.NoError(t, err)
	_, err = decodeTile(append(enc, 0), 8, 8, 1, 6, 6, mb31)
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestTileBadDimensions(t *testing.T) {
	_, err := encodeTile(nil, 0, 4, 0, 6, 6, mb31)
	assert.ErrorIs(t, err, ErrInvalidData)
	_, err = decodeTile([]byte{0}, 4, 0, 0, 6, 6, mb31)
	assert.ErrorIs(t, err, ErrInvalidData)
	_, err = encodeTile(make([]int32, 10), 4, 4, 0, 6, 6, mb31)
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestTileSinglePixel(t *testing.T) {
	for _, v := range []int32{0, 1, 2047, -2047} {
		tileRoundTrip(t, []int32{v}, 1, 1, 0, 2, 2)
		tileRoundTrip(t, []int32{v}, 1, 1, 3, 6, 6)
	}
}
