package jpeg2k

import (
	"image"
	"image/color"
)

// Legacy fixtures (testdata/legacy/go-*.j2c) were written by the pre-T.800 Go
// encoder (jpegs v0.2.0): raw big-endian int32 5/3 coefficients per component,
// SOD = 0xFFD3, Psot = 12 + len, all-zero QCD exponents. The package can no
// longer write that format, so these images and generators are frozen: the
// decoder must keep reproducing them exactly. Do not change them.

// legacyLCG is the deterministic generator shared with dicos.rs's legacy suite.
func legacyLCG(seed uint64, n int, maxVal uint32) []uint32 {
	out := make([]uint32, n)
	state := seed
	for i := range out {
		state = state*6364136223846793005 + 1442695040888963407
		out[i] = uint32(uint16(state>>33)) % (maxVal + 1)
	}
	return out
}

func legacyGray16(w, h int, px []uint32) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	for i, v := range px {
		img.SetGray16(i%w, i/w, color.Gray16{Y: uint16(v)})
	}
	return img
}

func legacyGray8(w, h int, px []uint32) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i, v := range px {
		img.SetGray(i%w, i/w, color.Gray{Y: uint8(v)})
	}
	return img
}

func legacyRGBA(w, h int, seed uint64) *image.RGBA {
	px := legacyLCG(seed, 3*w*h, 255)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		img.SetRGBA(i%w, i/w, color.RGBA{R: uint8(px[3*i]), G: uint8(px[3*i+1]), B: uint8(px[3*i+2]), A: 255})
	}
	return img
}

func legacyGradient(w, h int) []uint32 {
	out := make([]uint32, 0, w*h)
	for y := range h {
		for x := range w {
			out = append(out, uint32(uint16(x*65535/max(w, 1)+y*7)))
		}
	}
	return out
}

type legacyFixture struct {
	name   string
	img    image.Image
	levels int // DecompLevels the stream was written with
}

// legacyGoFixtures mirrors dicos.rs's legacy image set for 16-bit gray and
// adds the 8-bit gray and RGB images only the Go encoder could write.
func legacyGoFixtures() []legacyFixture {
	flat := make([]uint32, 64)
	for i := range flat {
		flat[i] = 1234
	}
	return []legacyFixture{
		{"go-gradient-16x16", legacyGray16(16, 16, legacyGradient(16, 16)), 5},
		{"go-random-32x32", legacyGray16(32, 32, legacyLCG(0xD1C05, 32*32, 65535)), 5},
		{"go-odd-13x7", legacyGray16(13, 7, legacyLCG(0x0DD5, 13*7, 4095)), 5},
		{"go-flat-8x8", legacyGray16(8, 8, flat), 5},
		{"go-tall-1x16", legacyGray16(1, 16, legacyLCG(0x7A11, 16, 65535)), 5},
		{"go-levels2-20x12", legacyGray16(20, 12, legacyLCG(0x1E7E1, 20*12, 65535)), 2},
		{"go-gray8-16x16", legacyGray8(16, 16, legacyLCG(0x8B17, 16*16, 255)), 5},
		{"go-gray8-odd-9x5", legacyGray8(9, 5, legacyLCG(0x9A5, 9*5, 255)), 5},
		{"go-rgba-8x8", legacyRGBA(8, 8, 0xC010), 5},
		{"go-rgba-odd-7x5", legacyRGBA(7, 5, 0xC011), 5},
	}
}
