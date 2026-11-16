package jpeg2k

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tagEncodeSweep(vals [][]uint32, maxT uint32) []uint8 {
	h, w := len(vals), len(vals[0])
	tree := newTagTree(w, h)
	for y, row := range vals {
		for x, v := range row {
			tree.set(x, y, v)
		}
	}
	var out []uint8
	put := func(b uint8) { out = append(out, b) }
	for t := uint32(1); t <= maxT; t++ {
		for y := range h {
			for x := range w {
				tree.encode(put, x, y, t)
			}
		}
	}
	return out
}

func tagRoundTrip(t *testing.T, vals [][]uint32) {
	t.Helper()
	h, w := len(vals), len(vals[0])
	var maxV uint32
	for _, row := range vals {
		for _, v := range row {
			maxV = max(maxV, v)
		}
	}
	bitsOut := tagEncodeSweep(vals, maxV+1)
	tree := newTagTree(w, h)
	pos := 0
	next := func() (uint8, error) {
		if pos >= len(bitsOut) {
			return 0, errors.New("out of bits")
		}
		pos++
		return bitsOut[pos-1], nil
	}
	for th := uint32(1); th <= maxV+1; th++ {
		for y := range h {
			for x := range w {
				got, err := tree.decode(next, x, y, th)
				require.NoError(t, err)
				require.Equal(t, vals[y][x] < th, got, "leaf (%d,%d) @ t=%d", x, y, th)
			}
		}
	}
	require.Equal(t, len(bitsOut), pos, "decoder must consume every bit")
}

func TestTagTreeRoundTrips(t *testing.T) {
	tagRoundTrip(t, [][]uint32{{1, 3, 2}, {2, 2, 3}})
	tagRoundTrip(t, [][]uint32{{0}})
	tagRoundTrip(t, [][]uint32{{5}})
	tagRoundTrip(t, [][]uint32{{1}})
	tagRoundTrip(t, [][]uint32{{0, 0, 0}, {0, 0, 0}})
	tagRoundTrip(t, [][]uint32{{7, 7}, {7, 7}, {7, 7}})
}

func TestTagTreeThresholdLearnsValue(t *testing.T) {
	enc := newTagTree(1, 1)
	enc.set(0, 0, 3)
	var out []uint8
	for th := uint32(1); th <= 5; th++ {
		enc.encode(func(b uint8) { out = append(out, b) }, 0, 0, th)
	}
	dec := newTagTree(1, 1)
	pos := 0
	for th := uint32(1); th <= 5; th++ {
		got, err := dec.decode(func() (uint8, error) { pos++; return out[pos-1], nil }, 0, 0, th)
		require.NoError(t, err)
		assert.Equal(t, 3 < th, got, "t=%d", th)
	}
	assert.Equal(t, len(out), pos)
}

func TestTagTreeResetKeepsValues(t *testing.T) {
	vals := [][]uint32{{1, 4}, {2, 0}}
	want := tagEncodeSweep(vals, 5)
	tree := newTagTree(2, 2)
	for y, row := range vals {
		for x, v := range row {
			tree.set(x, y, v)
		}
	}
	sweep := func(put func(uint8)) {
		for th := uint32(1); th <= 5; th++ {
			for y := range 2 {
				for x := range 2 {
					tree.encode(put, x, y, th)
				}
			}
		}
	}
	sweep(func(uint8) {})
	tree.reset()
	var got []uint8
	sweep(func(b uint8) { got = append(got, b) })
	assert.Equal(t, want, got)
}

func TestTagTreeEmpty(t *testing.T) {
	for _, d := range [][2]int{{0, 5}, {4, 0}} {
		tree := newTagTree(d[0], d[1])
		tree.set(0, 0, 3)
		tree.encode(func(uint8) { t.Fatal("no bits for an empty tree") }, 0, 0, 4)
		got, err := tree.decode(func() (uint8, error) { t.Fatal("no reads for an empty tree"); return 0, nil }, 0, 0, 4)
		require.NoError(t, err)
		assert.False(t, got)
	}
}

func TestTagTreeOutOfBits(t *testing.T) {
	tree := newTagTree(2, 2)
	_, err := tree.decode(func() (uint8, error) { return 0, ErrInvalidData }, 0, 0, 3)
	assert.ErrorIs(t, err, ErrInvalidData)
}

// Seeded stand-in for dicos.rs's prop_incremental_roundtrip (200 cases).
func TestTagTreeRandomRoundTrip(t *testing.T) {
	state := uint64(0x5EED)
	next := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	for range 200 {
		w, h := 1+int(next()%17), 1+int(next()%13)
		vals := make([][]uint32, h)
		for y := range vals {
			vals[y] = make([]uint32, w)
			for x := range vals[y] {
				vals[y][x] = uint32(next() % 21)
			}
		}
		tagRoundTrip(t, vals)
	}
}
