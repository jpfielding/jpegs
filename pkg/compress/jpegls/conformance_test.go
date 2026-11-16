package jpegls

import (
	"bytes"
	"fmt"
	"image"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is a testdata stream named <pattern>-<W>x<H>-p<precision>-n<near>.jls;
// its source image is regenerated from the name.
type fixture struct {
	name                  string
	pattern               string
	w, h, precision, near int
	data                  []byte
}

func loadFixtures(t testing.TB, dir string) []fixture {
	t.Helper()
	entries, err := fs.ReadDir(testdata, dir)
	require.NoError(t, err)
	var out []fixture
	for _, e := range entries {
		fx := fixture{name: strings.TrimSuffix(e.Name(), ".jls")}
		_, err := fmt.Sscanf(strings.ReplaceAll(fx.name, "-", " "), "%s %dx%d p%d n%d", &fx.pattern, &fx.w, &fx.h, &fx.precision, &fx.near)
		require.NoError(t, err, e.Name())
		fx.data, err = testdata.ReadFile(path.Join(dir, e.Name()))
		require.NoError(t, err)
		out = append(out, fx)
	}
	require.NotEmpty(t, out)
	return out
}

// planes returns the R, G, B planes of a decoded 3-component image.
func planes(t testing.TB, img image.Image) [3][]uint16 {
	t.Helper()
	var out [3][]uint16
	switch m := img.(type) {
	case *image.RGBA:
		for i := 0; i < len(m.Pix); i += 4 {
			require.Equal(t, byte(0xFF), m.Pix[i+3])
			for c := range 3 {
				out[c] = append(out[c], uint16(m.Pix[i+c]))
			}
		}
	case *image.RGBA64:
		for i := 0; i < len(m.Pix); i += 8 {
			for c := range 3 {
				out[c] = append(out[c], uint16(m.Pix[i+2*c])<<8|uint16(m.Pix[i+2*c+1]))
			}
		}
	default:
		require.Failf(t, "unexpected image type", "%T", img)
	}
	return out
}

// TestRustFixtures: streams written by the dicos.rs encoder (Profile::T87).
// This encoder must produce the same bytes, and the decoder the same image.
func TestRustFixtures(t *testing.T) {
	for _, fx := range loadFixtures(t, "testdata/rust") {
		t.Run(fx.name, func(t *testing.T) {
			maxVal := uint16(1<<fx.precision - 1)
			img, err := decode(fx.data, profileT87)
			require.NoError(t, err)
			require.Equal(t, image.Rect(0, 0, fx.w, fx.h), img.Bounds())

			if fx.pattern == "rgb" {
				// Planar (ILV=0) 3-component stream from encode_image.
				got := planes(t, img)
				for c, name := range []string{"smooth", "randomflat", "gradient"} {
					requireWithinNear(t, pattern(name, fx.w, fx.h, maxVal), got[c], fx.near)
				}
				return
			}
			samples := pattern(fx.pattern, fx.w, fx.h, maxVal)
			requireWithinNear(t, samples, samplesOf(t, img), fx.near)
			assert.Equal(t, fx.data, mustEncode(t, samples, fx.w, fx.h, fx.precision, fx.near), "byte-identical to dicos.rs")
		})
	}
}

// TestCharLSFixtures: streams written by CharLS 2.4.2. For precision > 12
// CharLS adds an LSE segment spelling out the default parameters; the scan
// data is the same as this encoder's.
func TestCharLSFixtures(t *testing.T) {
	for _, fx := range loadFixtures(t, "testdata/charls") {
		t.Run(fx.name, func(t *testing.T) {
			maxVal := uint16(1<<fx.precision - 1)
			if strings.HasPrefix(fx.pattern, "rgbilv") {
				// Line- / sample-interleaved scans are outside the supported subset.
				_, err := Decode(bytes.NewReader(fx.data))
				require.ErrorIs(t, err, ErrUnsupported)
				return
			}
			img, err := decode(fx.data, profileT87)
			require.NoError(t, err)
			require.Equal(t, image.Rect(0, 0, fx.w, fx.h), img.Bounds())

			if fx.pattern == "rgb" {
				// Three scans (ILV=0). The source was a pixel-interleaved
				// buffer filled with three consecutive synthetic planes.
				var src []uint16
				for _, f := range []func(x, y int) int{
					func(x, y int) int { return (x*x + 3*y) % 251 },
					func(x, y int) int {
						if fx.w/3 <= x && x < 2*fx.w/3 {
							return 100
						}
						return (x*37 + y*91) % 256
					},
					func(x, y int) int { return (x + y) * 255 / (fx.w + fx.h) },
				} {
					for i := range fx.w * fx.h {
						src = append(src, uint16(f(i%fx.w, i/fx.w)))
					}
				}
				var want [3][]uint16
				for i, v := range src {
					want[i%3] = append(want[i%3], v)
				}
				assert.Equal(t, want, planes(t, img))
				return
			}
			samples := pattern(fx.pattern, fx.w, fx.h, maxVal)
			got := samplesOf(t, img)
			requireWithinNear(t, samples, got, fx.near)

			// Same reconstruction as this codec's own stream (matters for near > 0).
			own := mustEncode(t, samples, fx.w, fx.h, fx.precision, fx.near)
			assert.Equal(t, mustDecodeT87(t, own), got)
			if fx.precision <= 12 {
				assert.Equal(t, fx.data, own, "byte-identical to CharLS")
				return
			}
			assert.Contains(t, string(fx.data), "\xFF\xF8", "CharLS writes LSE above 12 bits")
			assert.True(t, bytes.HasSuffix(fx.data, own[25:]), "identical scan data")
		})
	}
}
