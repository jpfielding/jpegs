package jpeg2k

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func encodeBytes(t testing.TB, img image.Image, opts *Options) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, img, opts))
	return buf.Bytes()
}

func decodeBytes(data []byte) (image.Image, error) { return Decode(bytes.NewReader(data)) }

func gray16From(w, h int, px []uint16) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	for i, v := range px {
		binary.BigEndian.PutUint16(img.Pix[2*i:], v)
	}
	return img
}

func gray16Pixels(img *image.Gray16) []uint16 {
	b := img.Bounds()
	out := make([]uint16, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out = append(out, img.Gray16At(x, y).Y)
		}
	}
	return out
}

// readPGM parses a binary 16-bit PGM (P5, big-endian samples).
func readPGM(t testing.TB, path string) (int, int, []uint16) {
	t.Helper()
	d, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "P5", string(d[:2]))
	i := 2
	var toks []int
	for len(toks) < 3 {
		for d[i] == ' ' || d[i] == '\n' || d[i] == '\r' || d[i] == '\t' {
			i++
		}
		if d[i] == '#' {
			for d[i] != '\n' {
				i++
			}
			continue
		}
		s := i
		for d[i] != ' ' && d[i] != '\n' && d[i] != '\r' && d[i] != '\t' {
			i++
		}
		v, err := strconv.Atoi(string(d[s:i]))
		require.NoError(t, err)
		toks = append(toks, v)
	}
	i++
	w, h := toks[0], toks[1]
	require.Greater(t, toks[2], 255, "expected a 16-bit PGM")
	px := make([]uint16, w*h)
	for k := range px {
		px[k] = binary.BigEndian.Uint16(d[i+2*k:])
	}
	return w, h, px
}

// withMaxDecodedBytes lowers the cap for the test's duration (hostile input
// must never reach the 1 GiB default).
func withMaxDecodedBytes(t testing.TB, n int64) {
	t.Helper()
	old := MaxDecodedBytes
	MaxDecodedBytes = n
	t.Cleanup(func() { MaxDecodedBytes = old })
}
