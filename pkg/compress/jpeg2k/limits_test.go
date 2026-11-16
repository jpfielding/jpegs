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

func encodedGray(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, img, nil))
	return buf.Bytes()
}

// sizOffset returns the offset of the SIZ marker (FF 51).
func sizOffset(t *testing.T, data []byte) int {
	t.Helper()
	i := bytes.Index(data, []byte{0xFF, 0x51})
	require.GreaterOrEqual(t, i, 0, "SIZ not found")
	return i
}

func TestDecode_RejectsOversizedSIZ(t *testing.T) {
	data := encodedGray(t)
	siz := sizOffset(t, data)
	// SIZ: marker(2) Lsiz(2) Rsiz(2) Xsiz(4) Ysiz(4) ...
	binary.BigEndian.PutUint32(data[siz+6:], 1<<20)
	binary.BigEndian.PutUint32(data[siz+10:], 1<<20)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := Decode(bytes.NewReader(data))
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, ErrImageTooLarge)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func TestDecode_RejectsWrappedSIZOffset(t *testing.T) {
	data := encodedGray(t)
	siz := sizOffset(t, data)
	// XOsiz > Xsiz used to wrap the uint32 subtraction to ~4 billion.
	binary.BigEndian.PutUint32(data[siz+14:], 0xFFFFFFF0)
	_, err := Decode(bytes.NewReader(data))
	require.Error(t, err)
}
