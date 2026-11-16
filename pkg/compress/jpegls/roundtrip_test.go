package jpegls

import (
	"bytes"
	"fmt"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoundTripMatrix covers every precision, odd geometries (1xN, Nx1), flat
// images (run mode), noise, and near-lossless bounds.
func TestRoundTripMatrix(t *testing.T) {
	dims := [][2]int{{1, 1}, {1, 16}, {16, 1}, {2, 2}, {7, 5}, {16, 16}, {33, 9}, {70, 3}}
	for precision := 2; precision <= 16; precision++ {
		maxVal := uint16(1<<precision - 1)
		nears := []int{0, 1, 3, min(255, int(maxVal)/2)}
		for _, wh := range dims {
			w, h := wh[0], wh[1]
			for _, name := range patternNames {
				samples := pattern(name, w, h, maxVal)
				for _, near := range nears {
					if near > int(maxVal)/2 {
						continue
					}
					label := fmt.Sprintf("%s %dx%d p%d near%d", name, w, h, precision, near)
					data := mustEncode(t, samples, w, h, precision, near)
					img, err := Decode(bytes.NewReader(data))
					require.NoError(t, err, label)
					require.Equal(t, image.Rect(0, 0, w, h), img.Bounds(), label)
					if precision <= 8 {
						require.IsType(t, &image.Gray{}, img, label)
					} else {
						require.IsType(t, &image.Gray16{}, img, label)
					}
					requireWithinNear(t, samples, samplesOf(t, img), near)
				}
			}
		}
	}
}

func TestRoundTripLarge16(t *testing.T) {
	const w, h = 312, 257
	samples := make([]uint16, w*h)
	noise := lcg(7, w*h, 31)
	for i := range samples {
		x, y := i%w, i/w
		switch {
		case x < 100 && y < 100:
			samples[i] = 0
		case x > 200 && y < 100:
			samples[i] = 65535
		default:
			samples[i] = uint16((x*37+y*w)%65536) + noise[i]
		}
	}
	for _, near := range []int{0, 2} {
		data := mustEncode(t, samples, w, h, 16, near)
		requireWithinNear(t, samples, mustDecodeT87(t, data), near)
	}
}

func TestRunMode(t *testing.T) {
	t.Run("constant multi-line run", func(t *testing.T) {
		samples := make([]uint16, 20*10)
		for i := range samples {
			samples[i] = 1234
		}
		data := mustEncode(t, samples, 20, 10, 12, 0)
		assert.Equal(t, samples, mustDecodeT87(t, data))
	})
	t.Run("flat image is tiny", func(t *testing.T) {
		// 512x512 of one value: run mode codes each line in a few bits. The
		// pre-conformance encoder needed at least one bit per sample.
		samples := make([]uint16, 512*512)
		for i := range samples {
			samples[i] = 777
		}
		data := mustEncode(t, samples, 512, 512, 16, 0)
		assert.Less(t, len(data), 512)
		assert.Equal(t, samples, mustDecodeT87(t, data))
	})
	t.Run("interruption at first column", func(t *testing.T) {
		const w, h = 8, 6
		samples := make([]uint16, w*h)
		for i := range samples {
			samples[i] = 100
		}
		for y := 1; y < h; y += 2 {
			samples[y*w] = 160
		}
		data := mustEncode(t, samples, w, h, 8, 0)
		assert.Equal(t, samples, mustDecodeT87(t, data))
	})
	t.Run("run ending exactly at end of line", func(t *testing.T) {
		for _, w := range []int{1, 2, 3, 7, 16, 31, 1000} {
			samples := make([]uint16, w*4)
			for i := range samples {
				samples[i] = 50
			}
			data := mustEncode(t, samples, w, 4, 8, 0)
			assert.Equal(t, samples, mustDecodeT87(t, data), "w=%d", w)
		}
	})
}

func TestNearLosslessBounds(t *testing.T) {
	const w, h = 24, 16
	samples := lcg(0xABCDEF, w*h, 4095)
	for _, near := range []int{1, 2, 5, 10, 255} {
		data := mustEncode(t, samples, w, h, 12, near)
		requireWithinNear(t, samples, mustDecodeT87(t, data), near)
	}
}

func TestRoundTripRandomized(t *testing.T) {
	seeds := lcg(0xF00D, 300, 65535)
	precisions := []int{2, 3, 7, 8, 10, 12, 15, 16}
	for i, s := range seeds {
		seed := uint64(s)<<16 | uint64(i)
		w, h := 1+int(seed%40), 1+int(seed/40%25)
		precision := precisions[seed/1000%uint64(len(precisions))]
		maxVal := 1<<precision - 1
		near := min(int(seed/8000%5), maxVal/2)
		// Mix noise amplitudes so both regular and run mode are exercised.
		samples := lcg(seed, w*h, uint16(maxVal>>(seed/40000%uint64(precision))))
		data := mustEncode(t, samples, w, h, precision, near)
		requireWithinNear(t, samples, mustDecodeT87(t, data), near)
	}
}

func TestEncodeImageTypes(t *testing.T) {
	t.Run("gray", func(t *testing.T) {
		samples := pattern("smooth", 19, 11, 255)
		var buf bytes.Buffer
		require.NoError(t, Encode(&buf, grayImage(samples, 19, 11), nil))
		assert.Equal(t, byte(8), buf.Bytes()[6], "SOF55 precision")
		img, err := Decode(&buf)
		require.NoError(t, err)
		require.IsType(t, &image.Gray{}, img)
		assert.Equal(t, samples, samplesOf(t, img))
	})
	t.Run("sub-image honours stride and origin", func(t *testing.T) {
		full := gray16Image(pattern("random", 16, 16, 65535), 16, 16)
		sub := full.SubImage(image.Rect(3, 5, 12, 9)).(*image.Gray16)
		var buf bytes.Buffer
		require.NoError(t, Encode(&buf, sub, nil))
		img, err := Decode(&buf)
		require.NoError(t, err)
		require.Equal(t, image.Rect(0, 0, 9, 4), img.Bounds())
		for y := range 4 {
			for x := range 9 {
				require.Equal(t, sub.Gray16At(3+x, 5+y), img.(*image.Gray16).Gray16At(x, y))
			}
		}
		gsub := grayImage(pattern("random", 16, 16, 255), 16, 16).SubImage(image.Rect(1, 2, 8, 16)).(*image.Gray)
		buf.Reset()
		require.NoError(t, Encode(&buf, gsub, nil))
		img, err = Decode(&buf)
		require.NoError(t, err)
		for y := range 14 {
			for x := range 7 {
				require.Equal(t, gsub.GrayAt(1+x, 2+y), img.(*image.Gray).GrayAt(x, y))
			}
		}
	})
	t.Run("unsupported type", func(t *testing.T) {
		err := Encode(&bytes.Buffer{}, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil)
		assert.ErrorIs(t, err, ErrUnsupported)
	})
}

// TestDerivedPrecision pins how a zero Options.Precision is resolved.
func TestDerivedPrecision(t *testing.T) {
	sofPrecision := func(maxSample uint16, opts *Options) int {
		var buf bytes.Buffer
		require.NoError(t, Encode(&buf, gray16Image([]uint16{0, maxSample, 0, 0}, 2, 2), opts))
		return int(buf.Bytes()[6])
	}
	// Derived from the data, but never below 9 for a 16-bit image, so that
	// Decode returns *image.Gray16 again.
	assert.Equal(t, 9, sofPrecision(0, nil))
	assert.Equal(t, 9, sofPrecision(255, nil))
	assert.Equal(t, 9, sofPrecision(511, nil))
	assert.Equal(t, 12, sofPrecision(4095, nil))
	assert.Equal(t, 13, sofPrecision(4096, nil))
	assert.Equal(t, 16, sofPrecision(65535, nil))
	// An explicit precision wins.
	assert.Equal(t, 16, sofPrecision(100, &Options{Precision: 16}))
	assert.Equal(t, 7, sofPrecision(100, &Options{Precision: 7}))
}

func TestEncodeRejectsInvalidParameters(t *testing.T) {
	img := gray16Image([]uint16{10, 10, 10, 300}, 2, 2)
	for _, p := range []int{-1, 1, 17} {
		assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, &Options{Precision: p}), ErrInvalidData, "precision %d", p)
	}
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, &Options{Precision: 8}), ErrInvalidData, "sample exceeds precision")
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, &Options{Precision: 9, Near: 256}), ErrInvalidData)
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, img, &Options{Near: -1}), ErrInvalidData)
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, gray16Image([]uint16{1, 2}, 2, 1), &Options{Precision: 2, Near: 2}), ErrInvalidData,
		"near above MAXVAL/2")
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, image.NewGray(image.Rect(0, 0, 0, 4)), nil), ErrInvalidData)
	assert.ErrorIs(t, Encode(&bytes.Buffer{}, image.NewGray(image.Rect(0, 0, 65536, 1)), nil), ErrInvalidData)
}
