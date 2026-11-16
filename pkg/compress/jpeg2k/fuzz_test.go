package jpeg2k

import (
	"bytes"
	"errors"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// fuzzMaxBytes keeps fuzz inputs from requesting large images; the limit
// itself is covered by limits_test.go.
const fuzzMaxBytes = 1 << 17

// FuzzDecode: malformed input must produce a classified error, never a panic
// or an oversized image.
//
// Run it in a bounded child process, e.g.
//
//	capped -mem 2048 -timeout 120s -- go test -run '^$' -fuzz FuzzDecode -fuzztime 30s -parallel 2 ./pkg/compress/jpeg2k
func FuzzDecode(f *testing.F) {
	for _, dir := range []string{"legacy", "interop"} {
		files, err := filepath.Glob(filepath.Join("testdata", dir, "*.j*"))
		if err != nil {
			f.Fatal(err)
		}
		for _, p := range files {
			data, err := os.ReadFile(p)
			if err != nil {
				f.Fatal(err)
			}
			if len(data) <= 4096 {
				f.Add(data)
			}
		}
	}
	for _, o := range []*Options{
		DefaultOptions(),
		{DecompLevels: 0, CodeBlockWidthExp: 2, CodeBlockHeightExp: 2},
		{DecompLevels: 2, CodeBlockWidthExp: 3, CodeBlockHeightExp: 5},
	} {
		f.Add(encodeBytes(f, gray16From(9, 7, patternPixels(2, 9, 7)), o))
	}

	// Fuzz workers are separate processes; each lowers the cap for itself.
	MaxDecodedBytes = fuzzMaxBytes
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, p := range []LegacyPolicy{LegacyAuto, StandardOnly, LegacyOnly} {
			img, err := DecodeWithPolicy(bytes.NewReader(data), p)
			switch {
			case err == nil:
				if b := img.Bounds(); b.Empty() || b.Dx()*b.Dy() > fuzzMaxBytes {
					t.Fatalf("decoded image has bounds %v", b)
				}
			case !errors.Is(err, ErrInvalidData) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrImageTooLarge):
				t.Fatalf("unclassified error: %v", err)
			}
		}
		if _, err := DecodeConfig(bytes.NewReader(data)); err != nil &&
			!errors.Is(err, ErrInvalidData) && !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unclassified DecodeConfig error: %v", err)
		}
	})
}

// FuzzRoundTrip: any small 16-bit image survives encode / StandardOnly
// decode exactly under any valid options.
func FuzzRoundTrip(f *testing.F) {
	f.Add(uint8(5), uint8(4), uint8(5), uint8(6), uint8(6), []byte{1, 2, 3, 0xFF, 0x80})
	f.Add(uint8(1), uint8(1), uint8(0), uint8(2), uint8(2), []byte{0xFF, 0xFF})
	f.Add(uint8(33), uint8(17), uint8(8), uint8(10), uint8(2), []byte{0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, w, h, levels, xcb, ycb uint8, seed []byte) {
		width, height := 1+int(w%64), 1+int(h%64)
		opts := &Options{DecompLevels: int(levels % 33), CodeBlockWidthExp: 2 + int(xcb%9), CodeBlockHeightExp: 2 + int(ycb%9)}
		if opts.Validate() != nil {
			return
		}
		img := image.NewGray16(image.Rect(0, 0, width, height))
		for i := range img.Pix {
			if len(seed) > 0 {
				img.Pix[i] = seed[i%len(seed)] ^ byte(i*31)
			}
		}
		var buf bytes.Buffer
		if err := Encode(&buf, img, opts); err != nil {
			t.Fatalf("encode %dx%d %+v: %v", width, height, opts, err)
		}
		got, err := DecodeWithPolicy(&buf, StandardOnly)
		if err != nil {
			t.Fatalf("decode %dx%d %+v: %v", width, height, opts, err)
		}
		if !bytes.Equal(got.(*image.Gray16).Pix, img.Pix) {
			t.Fatalf("round trip mismatch %dx%d %+v", width, height, opts)
		}
	})
}
