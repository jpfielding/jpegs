package jpegls

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"

	"github.com/stretchr/testify/require"
)

// lcg is the deterministic sample generator shared with the dicos.rs test
// suite (same constants), so fixtures can be regenerated from their names.
func lcg(seed uint64, n int, maxVal uint16) []uint16 {
	out := make([]uint16, n)
	for i := range out {
		seed = seed*6364136223846793005 + 1442695040888963407
		out[i] = uint16(uint32(seed>>33) % (uint32(maxVal) + 1))
	}
	return out
}

// pattern returns a named w*h test image with samples in [0, maxVal].
func pattern(name string, w, h int, maxVal uint16) []uint16 {
	out := make([]uint16, w*h)
	switch name {
	case "constant":
		for i := range out {
			out[i] = maxVal / 2
		}
	case "gradient":
		for i := range out {
			x, y := i%w, i/w
			out[i] = uint16(uint64(x+y) * uint64(maxVal) / uint64(w+h))
		}
	case "checkerboard":
		for i := range out {
			out[i] = maxVal / 4
			if (i%w+i/w)%2 == 0 {
				out[i] = maxVal
			}
		}
	case "random":
		out = lcg(0x12345678^uint64(w*131+h), w*h, maxVal)
	case "randomflat":
		// Noise with a flat middle third on every row: forces run mode and
		// run interruptions.
		out = lcg(0xC0FFEE, w*h, maxVal)
		for y := range h {
			for x := w / 3; x < 2*w/3; x++ {
				out[y*w+x] = maxVal / 3
			}
		}
	case "smooth":
		// Slowly varying signal with small noise: exercises the bias and
		// k == 0 paths, and short runs.
		noise := lcg(0x5EED, w*h, 3)
		for i := range out {
			x, y := i%w, i/w
			v := int(maxVal)/2 + (x*x+3*y)%17 + int(noise[i]) - 9
			out[i] = uint16(min(max(v, 0), int(maxVal)))
		}
	default:
		panic("unknown pattern " + name)
	}
	return out
}

var patternNames = []string{"constant", "gradient", "checkerboard", "random", "randomflat", "smooth"}

func gray16Image(samples []uint16, w, h int) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	for i, v := range samples {
		binary.BigEndian.PutUint16(img.Pix[2*i:], v)
	}
	return img
}

func grayImage(samples []uint16, w, h int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i, v := range samples {
		img.Pix[i] = byte(v)
	}
	return img
}

// samplesOf extracts the coded sample values of a decoded grayscale image.
func samplesOf(t testing.TB, img image.Image) []uint16 {
	t.Helper()
	switch m := img.(type) {
	case *image.Gray:
		out := make([]uint16, len(m.Pix))
		for i, v := range m.Pix {
			out[i] = uint16(v)
		}
		return out
	case *image.Gray16:
		out := make([]uint16, len(m.Pix)/2)
		for i := range out {
			out[i] = binary.BigEndian.Uint16(m.Pix[2*i:])
		}
		return out
	}
	require.Failf(t, "unexpected image type", "%T", img)
	return nil
}

// mustEncode encodes samples at an explicit precision.
func mustEncode(t testing.TB, samples []uint16, w, h, precision, near int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, gray16Image(samples, w, h), &Options{Near: near, Precision: precision}))
	return buf.Bytes()
}

// mustDecodeT87 decodes with the strict conformant profile (no legacy
// fallback) and returns the samples.
func mustDecodeT87(t testing.TB, data []byte) []uint16 {
	t.Helper()
	img, err := decode(data, profileT87)
	require.NoError(t, err)
	return samplesOf(t, img)
}

// requireWithinNear asserts |want - got| <= near for every sample.
func requireWithinNear(t testing.TB, want, got []uint16, near int) {
	t.Helper()
	require.Len(t, got, len(want))
	if near == 0 {
		require.Equal(t, want, got)
		return
	}
	for i := range want {
		if d := abs(int(want[i]) - int(got[i])); d > near {
			require.Failf(t, "near bound violated", "sample %d: want %d got %d (near %d)", i, want[i], got[i], near)
		}
	}
}
