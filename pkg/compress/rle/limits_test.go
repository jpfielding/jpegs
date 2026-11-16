package rle

import (
	"bytes"
	"image"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gray16Stream(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray16(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, img))
	return buf.Bytes()
}

// TestDecode_RejectsBadDims covers caller-supplied dimensions that previously
// either allocated from the bogus size (2^20 x 2^20 asked for 1 TiB) or were
// silently accepted (negative x negative decoded as 16x16).
func TestDecode_RejectsBadDims(t *testing.T) {
	data := gray16Stream(t)
	cases := []struct {
		name          string
		width, height int
	}{
		{"zero", 0, 0},
		{"neg_width", -1, 16},
		{"neg_x_neg", -16, -16},
		{"overflow_2^32x2^31", 1 << 32, 1 << 31},
		{"huge_2^20x2^20", 1 << 20, 1 << 20},
		{"over_cap_65535x65535", 65535, 65535},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			img, err := Decode(data, c.width, c.height)
			runtime.ReadMemStats(&after)
			require.Error(t, err)
			assert.Nil(t, img)
			assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "must reject before allocating")
		})
	}
}

func TestDecode_CapIsAdjustable(t *testing.T) {
	data := gray16Stream(t)
	old := MaxDecodedBytes
	t.Cleanup(func() { MaxDecodedBytes = old })

	MaxDecodedBytes = 16*16*2 - 1
	_, err := Decode(data, 16, 16)
	require.ErrorIs(t, err, ErrImageTooLarge)

	MaxDecodedBytes = 16 * 16 * 2
	img, err := Decode(data, 16, 16)
	require.NoError(t, err)
	assert.Equal(t, 16, img.Bounds().Dx())
}

func TestDecodePackBits_BogusExpectedLenDoesNotPreallocate(t *testing.T) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, _ = decodePackBits([]byte{0x81, 7}, 1<<40)
	runtime.ReadMemStats(&after)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}
