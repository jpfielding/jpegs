package jpegli

import (
	"bytes"
	"image"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sofOffset returns the index of the SOF3 marker (FF C3) in a stream.
func sofOffset(t *testing.T, data []byte) int {
	t.Helper()
	i := bytes.Index(data, []byte{0xFF, 0xC3})
	require.GreaterOrEqual(t, i, 0, "SOF3 not found")
	return i
}

func encodedGray16(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray16(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestDecode_RejectsOversizedSOF(t *testing.T) {
	data := encodedGray16(t)
	sof := sofOffset(t, data)
	// SOF payload: len(2) precision(1) height(2) width(2) ...
	data[sof+5], data[sof+6] = 0xFF, 0xFF // height 65535
	data[sof+7], data[sof+8] = 0xFF, 0xFF // width 65535 -> 8 GiB at 16-bit

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := Decode(bytes.NewReader(data))
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrImageTooLarge)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func TestDecode_RejectsShortSOF(t *testing.T) {
	data := encodedGray16(t)
	sof := sofOffset(t, data)
	data[sof+2], data[sof+3] = 0x00, 0x04 // segment length 4 -> 2-byte payload
	_, err := Decode(bytes.NewReader(data))
	require.Error(t, err)
}

func TestDecode_RejectsZeroDims(t *testing.T) {
	data := encodedGray16(t)
	sof := sofOffset(t, data)
	data[sof+5], data[sof+6] = 0, 0
	_, err := Decode(bytes.NewReader(data))
	require.Error(t, err)
}
