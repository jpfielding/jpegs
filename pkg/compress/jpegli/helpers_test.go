package jpegli

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// readPGM parses a binary P5 PGM (8- or 16-bit big-endian) into samples.
func readPGM(t testing.TB, path string) (w, h int, px []int) {
	t.Helper()
	d, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "P5", string(d[:2]), path)
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
	i++ // single whitespace after maxval
	w, h = toks[0], toks[1]
	data := d[i:]
	if toks[2] > 255 {
		require.Len(t, data, 2*w*h, path)
		for k := 0; k < w*h; k++ {
			px = append(px, int(data[2*k])<<8|int(data[2*k+1]))
		}
	} else {
		require.Len(t, data, w*h, path)
		for _, b := range data {
			px = append(px, int(b))
		}
	}
	return w, h, px
}

// samplesOf returns the row-major samples of a decoded Gray or Gray16 image.
func samplesOf(t testing.TB, img image.Image) []int {
	t.Helper()
	b := img.Bounds()
	var px []int
	switch m := img.(type) {
	case *image.Gray:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				px = append(px, int(m.GrayAt(x, y).Y))
			}
		}
	case *image.Gray16:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				px = append(px, int(m.Gray16At(x, y).Y))
			}
		}
	default:
		t.Fatalf("unexpected image type %T", img)
	}
	return px
}

// gray16 builds a w×h Gray16 image from row-major samples.
func gray16(w, h int, px []int) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	for i, v := range px {
		img.Pix[2*i], img.Pix[2*i+1] = byte(v>>8), byte(v)
	}
	return img
}

// detImage is a deterministic image whose samples all fit in precision bits
// (dicos.rs det_image).
func detImage(w, h, precision int) []int {
	px := make([]int, w*h)
	for i := range px {
		px[i] = int((uint64(i)*2_654_435_761 + 12345) % (1 << precision))
	}
	return px
}

func encodeOpts(t testing.TB, w, h int, px []int, o Options) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, gray16(w, h, px), &o))
	return buf.Bytes()
}

func decodeSamples(t testing.TB, data []byte) []int {
	t.Helper()
	img, err := Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return samplesOf(t, img)
}

func shifted(px []int, pt int) []int {
	out := make([]int, len(px))
	for i, v := range px {
		out[i] = v >> pt << pt
	}
	return out
}

// markerPositions returns the offsets of every 0xFF m in data.
func markerPositions(data []byte, m byte) []int {
	var out []int
	for i := 0; i+1 < len(data); i++ {
		if data[i] == 0xFF && data[i+1] == m {
			out = append(out, i)
		}
	}
	return out
}

func restartIndices(data []byte) []int {
	var out []int
	for i := 0; i+1 < len(data); i++ {
		if data[i] == 0xFF && data[i+1] >= 0xD0 && data[i+1] <= 0xD7 {
			out = append(out, int(data[i+1]-0xD0))
			i++
		}
	}
	return out
}

func testdata(name ...string) string {
	return filepath.Join(append([]string{"testdata"}, name...)...)
}

// smallDecodeCap bounds allocations for malformed-input tests.
func smallDecodeCap(t testing.TB) {
	old := MaxDecodedBytes
	MaxDecodedBytes = 4 << 20
	t.Cleanup(func() { MaxDecodedBytes = old })
}
