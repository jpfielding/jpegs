package jpeg2k

import (
	"bytes"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OpenJPEG 2.5.4 opj_compress streams (dicos.rs tests/fixtures/interop; see
// its README for the generator). -cb32 shares the random source.
var interopFixtures = [][2]string{
	{"gradient-64x64.j2k", "gradient-64x64.pgm"},
	{"odd-61x47.j2k", "odd-61x47.pgm"},
	{"random-32x32.j2k", "random-32x32.pgm"},
	{"random-32x32-cb32.j2k", "random-32x32.pgm"},
}

func TestDecodeOpenJPEGFixtures(t *testing.T) {
	dir := filepath.Join("testdata", "interop")
	for _, fx := range interopFixtures {
		t.Run(fx[0], func(t *testing.T) {
			w, h, want := readPGM(t, filepath.Join(dir, fx[1]))
			data, err := os.ReadFile(filepath.Join(dir, fx[0]))
			require.NoError(t, err)
			for _, p := range []LegacyPolicy{LegacyAuto, StandardOnly} {
				img, err := DecodeWithPolicy(bytes.NewReader(data), p)
				require.NoError(t, err)
				g := img.(*image.Gray16)
				assert.Equal(t, image.Rect(0, 0, w, h), g.Bounds())
				assert.Equal(t, want, gray16Pixels(g))
			}
		})
	}
}

// TestEncodeOpenJPEGDecode hands our output to opj_decompress. It runs only
// with JPEG2K_INTEROP set and opj_decompress on PATH.
func TestEncodeOpenJPEGDecode(t *testing.T) {
	if os.Getenv("JPEG2K_INTEROP") == "" {
		t.Skip("set JPEG2K_INTEROP=1 to run against opj_decompress")
	}
	opj, err := exec.LookPath("opj_decompress")
	if err != nil {
		t.Skip("opj_decompress not on PATH")
	}
	dir := filepath.Join("testdata", "interop")
	tmp := t.TempDir()
	for _, pgm := range []string{"gradient-64x64.pgm", "odd-61x47.pgm", "random-32x32.pgm"} {
		w, h, src := readPGM(t, filepath.Join(dir, pgm))
		for _, opts := range []*Options{DefaultOptions(), {DecompLevels: 0, CodeBlockWidthExp: 2, CodeBlockHeightExp: 2}, {DecompLevels: 3, CodeBlockWidthExp: 5, CodeBlockHeightExp: 4}} {
			in := filepath.Join(tmp, "in.j2k")
			out := filepath.Join(tmp, "out.pgm")
			require.NoError(t, os.WriteFile(in, encodeBytes(t, gray16From(w, h, src), opts), 0o644))
			cmd := exec.Command(opj, "-i", in, "-o", out)
			msg, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s %+v: %s", pgm, opts, msg)
			dw, dh, got := readPGM(t, out)
			require.Equal(t, [2]int{w, h}, [2]int{dw, dh})
			require.Equal(t, src, got, "%s %+v", pgm, opts)
		}
	}
}
