package jpeg2k

import (
	"bytes"
	"encoding/binary"
	"image"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeRejectsOversizedSIZ(t *testing.T) {
	data := encodeBytes(t, image.NewGray16(image.Rect(0, 0, 16, 16)), nil)
	siz := markerOff(t, data, MarkerSIZ)
	for _, off := range []int{6, 10, 22, 26} { // Xsiz, Ysiz, XTsiz, YTsiz
		binary.BigEndian.PutUint32(data[siz+off:], 1<<20)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := Decode(bytes.NewReader(data))
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrImageTooLarge)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func TestDecodeRejectsWrappedSIZOffset(t *testing.T) {
	data := encodeBytes(t, image.NewGray16(image.Rect(0, 0, 16, 16)), nil)
	siz := markerOff(t, data, MarkerSIZ)
	// XOsiz > Xsiz used to wrap the uint32 subtraction to ~4 billion.
	binary.BigEndian.PutUint32(data[siz+14:], 0xFFFFFFF0)
	_, err := Decode(bytes.NewReader(data))
	require.ErrorIs(t, err, ErrInvalidData)
}

// 4x4 code-blocks on a large image cost more in per-block state than in
// coefficients; the cap must count them.
func TestDecodeRejectsCodeBlockStateBomb(t *testing.T) {
	withMaxDecodedBytes(t, 64<<20)
	img := image.NewGray16(image.Rect(0, 0, 16, 16))
	data := encodeBytes(t, img, &Options{DecompLevels: 0, CodeBlockWidthExp: 2, CodeBlockHeightExp: 2})
	siz := markerOff(t, data, MarkerSIZ)
	// 4000 x 4000: 61 MiB of int32 coefficients (under the cap) but 1M
	// code-blocks of state (over it).
	for _, off := range []int{6, 10, 22, 26} {
		binary.BigEndian.PutUint32(data[siz+off:], 4000)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := Decode(bytes.NewReader(data))
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrImageTooLarge)
	assert.Contains(t, err.Error(), "code-blocks")
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func TestCheckDecodedSize(t *testing.T) {
	withMaxDecodedBytes(t, 1000)
	assert.NoError(t, checkDecodedSize(10, 25, 1, 4))
	assert.ErrorIs(t, checkDecodedSize(10, 26, 1, 4), ErrImageTooLarge)
	assert.ErrorIs(t, checkDecodedSize(0, 1, 1, 4), ErrInvalidData)
	MaxDecodedBytes = 0
	assert.ErrorIs(t, checkDecodedSize(1<<40, 1<<40, 1<<40, 4), ErrImageTooLarge, "overflow")
}

func TestCheckCodeBlockStateMatchesGeometry(t *testing.T) {
	withMaxDecodedBytes(t, 0)
	for _, c := range [][5]int{{64, 64, 5, 6, 6}, {13, 7, 2, 2, 2}, {200, 137, 4, 3, 5}, {1, 1, 3, 6, 6}, {255, 3, 5, 2, 10}} {
		n := 0
		for _, r := range buildGeometry(c[0], c[1], c[2], c[3], c[4]) {
			for _, b := range r.bands {
				n += len(b.cbs)
			}
		}
		MaxDecodedBytes = int64(n) * codeBlockStateBytes
		assert.NoError(t, checkCodeBlockState(c[0], c[1], c[2], c[3], c[4]), "%v", c)
		MaxDecodedBytes = int64(n)*codeBlockStateBytes - 1
		assert.ErrorIs(t, checkCodeBlockState(c[0], c[1], c[2], c[3], c[4]), ErrImageTooLarge, "%v", c)
	}
}
