package jpegls

import (
	"errors"
	"io/fs"
	"testing"
)

// fuzzMaxBytes keeps fuzz inputs from requesting large frames; the limit
// itself is covered by TestDecodeSizeLimit.
const fuzzMaxBytes = 1 << 17

// FuzzDecode: malformed input must produce an error, never a panic.
//
// Run it in a bounded child process:
//
//	GOMEMLIMIT=1GiB timeout 120 go test -run '^$' -fuzz FuzzDecode -fuzztime 30s -parallel 2 ./pkg/compress/jpegls
//
// (GOMEMLIMIT is a soft limit; use a runner that enforces an RSS cap where
// one is available.)
func FuzzDecode(f *testing.F) {
	err := fs.WalkDir(testdata, "testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := testdata.ReadFile(path)
		if err != nil {
			return err
		}
		// Large seeds only slow the engine's minimizer down.
		if len(data) <= 4096 {
			f.Add(data)
		}
		return nil
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(legacyEncode(pattern("smooth", 9, 7, 255), 9, 7, 8))
	f.Add(encodeWithPresets(pattern("smooth", 9, 7, 255), 9, 7, 8, 1, Presets{MaxVal: 200, T1: 4, T2: 8, T3: 20, Reset: 5}))

	// Fuzz workers are separate processes; each lowers the cap for itself.
	MaxDecodedBytes = fuzzMaxBytes
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, p := range []profile{profileAuto, profileT87, profileLegacy} {
			img, err := decode(data, p)
			switch {
			case err == nil:
				if b := img.Bounds(); b.Empty() || b.Dx()*b.Dy() > fuzzMaxBytes {
					t.Fatalf("decoded image has bounds %v", b)
				}
			case !errors.Is(err, ErrInvalidData) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrImageTooLarge):
				t.Fatalf("unclassified error: %v", err)
			}
		}
	})
}

// FuzzRoundTrip: any image at any precision and NEAR survives
// encode / strict decode within the NEAR bound.
func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 1, 2, 3, 200, 200, 200, 7, 7}, uint8(4), uint8(8), uint8(0))
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0xFF, 0xFE}, uint8(2), uint8(16), uint8(3))
	f.Add([]byte{1, 2, 3, 0, 1, 2, 3, 0, 3, 3}, uint8(5), uint8(2), uint8(1))

	f.Fuzz(func(t *testing.T, data []byte, width, precision, near uint8) {
		p := 2 + int(precision)%15
		maxVal := 1<<p - 1
		n := min(int(near), maxVal/2)
		samples := make([]uint16, len(data)/2)
		for i := range samples {
			samples[i] = (uint16(data[2*i])<<8 | uint16(data[2*i+1])) & uint16(maxVal)
		}
		w := 1 + int(width)%64
		h := len(samples) / w
		if h == 0 {
			return
		}
		samples = samples[:w*h]

		got := mustDecodeT87(t, mustEncode(t, samples, w, h, p, n))
		requireWithinNear(t, samples, got, n)
	})
}
