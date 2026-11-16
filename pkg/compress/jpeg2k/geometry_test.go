package jpeg2k

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findBand(t *testing.T, r resolution, kind Subband) band {
	t.Helper()
	for _, b := range r.bands {
		if b.kind == kind {
			return b
		}
	}
	t.Fatalf("band %v missing at r=%d", kind, r.r)
	return band{}
}

func dims(r rect) [2]int { return [2]int{r.width(), r.height()} }

func TestGeometryRes64x64Levels5(t *testing.T) {
	g := buildGeometry(64, 64, 5, 6, 6)
	require.Len(t, g, 6)
	for r, e := range []int{2, 4, 8, 16, 32, 64} {
		assert.Equal(t, [2]int{e, e}, dims(g[r].rect), "res %d", r)
	}
	assert.Equal(t, [2]int{2, 2}, dims(findBand(t, g[0], SubbandLL).rect))
	for r := 1; r <= 5; r++ {
		prev := ceilShift(64, 5-r+1)
		detail := ceilShift(64, 5-r) - prev
		assert.Equal(t, [2]int{detail, prev}, dims(findBand(t, g[r], SubbandHL).rect), "HL r%d", r)
		assert.Equal(t, [2]int{prev, detail}, dims(findBand(t, g[r], SubbandLH).rect), "LH r%d", r)
		assert.Equal(t, [2]int{detail, detail}, dims(findBand(t, g[r], SubbandHH).rect), "HH r%d", r)
	}
}

func TestGeometrySingleCodeBlockUntilExceeded(t *testing.T) {
	for _, res := range buildGeometry(64, 64, 5, 6, 6) {
		for _, b := range res.bands {
			if b.rect.empty() {
				continue
			}
			require.Len(t, b.cbs, 1)
			assert.Equal(t, [2]int{1, 1}, [2]int{b.gridW, b.gridH})
			assert.Equal(t, b.rect, b.cbs[0].rect)
		}
	}
	g := buildGeometry(200, 64, 1, 6, 6)
	hl := findBand(t, g[1], SubbandHL)
	assert.Equal(t, 100, hl.rect.width())
	assert.Equal(t, 2, hl.gridW)
	assert.Equal(t, 64, hl.cbs[0].rect.width())
	assert.Equal(t, 36, hl.cbs[1].rect.width())
}

func TestGeometryBands13x7Levels2(t *testing.T) {
	g := buildGeometry(13, 7, 2, 6, 6)
	require.Len(t, g, 3)
	assert.Equal(t, [2]int{4, 2}, dims(g[0].rect))
	assert.Equal(t, [2]int{7, 4}, dims(g[1].rect))
	assert.Equal(t, [2]int{13, 7}, dims(g[2].rect))
	assert.Equal(t, [2]int{4, 2}, dims(findBand(t, g[0], SubbandLL).rect))

	assert.Equal(t, [2]int{3, 2}, dims(findBand(t, g[1], SubbandHL).rect))
	assert.Equal(t, [2]int{4, 2}, dims(findBand(t, g[1], SubbandLH).rect))
	assert.Equal(t, [2]int{3, 2}, dims(findBand(t, g[1], SubbandHH).rect))

	hl, lh, hh := findBand(t, g[2], SubbandHL), findBand(t, g[2], SubbandLH), findBand(t, g[2], SubbandHH)
	assert.Equal(t, [2]int{6, 4}, dims(hl.rect))
	assert.Equal(t, [2]int{7, 3}, dims(lh.rect))
	assert.Equal(t, [2]int{6, 3}, dims(hh.rect))
	assert.Equal(t, [2]int{7, 0}, [2]int{hl.xOff, hl.yOff})
	assert.Equal(t, [2]int{0, 4}, [2]int{lh.xOff, lh.yOff})
	assert.Equal(t, [2]int{7, 4}, [2]int{hh.xOff, hh.yOff})
}

func checkGeometryInvariants(t *testing.T, w, h, nl, xcb, ycb int) {
	t.Helper()
	g := buildGeometry(w, h, nl, xcb, ycb)
	for r := 1; r < len(g); r++ {
		area := g[r-1].rect.area()
		for _, b := range g[r].bands {
			area += b.rect.area()
		}
		require.Equal(t, g[r].rect.area(), area, "resolution area at r=%d (%dx%d NL=%d)", r, w, h, nl)
	}
	for _, res := range g {
		for _, b := range res.bands {
			if b.rect.empty() {
				require.Empty(t, b.cbs)
				require.Equal(t, [2]int{0, 0}, [2]int{b.gridW, b.gridH})
				continue
			}
			require.Len(t, b.cbs, b.gridW*b.gridH)
			area := 0
			for _, cb := range b.cbs {
				require.False(t, cb.rect.empty())
				require.LessOrEqual(t, cb.rect.x1, b.rect.width())
				require.LessOrEqual(t, cb.rect.y1, b.rect.height())
				area += cb.rect.area()
			}
			require.Equal(t, b.rect.area(), area)
		}
	}
}

func TestGeometryInvariants(t *testing.T) {
	for _, c := range [][3]int{{64, 64, 5}, {13, 7, 2}, {13, 11, 3}, {200, 137, 4}, {1, 1, 0}, {255, 3, 5}, {68, 68, 5}} {
		checkGeometryInvariants(t, c[0], c[1], c[2], 6, 6)
	}
}

func TestGeometryDegenerate(t *testing.T) {
	g := buildGeometry(1, 16, 4, 6, 6)
	assert.True(t, findBand(t, g[1], SubbandHL).rect.empty())
	assert.True(t, findBand(t, g[1], SubbandHH).rect.empty())
	checkGeometryInvariants(t, 1, 16, 4, 6, 6)

	g = buildGeometry(16, 1, 4, 6, 6)
	assert.True(t, findBand(t, g[1], SubbandLH).rect.empty())
	assert.True(t, findBand(t, g[1], SubbandHH).rect.empty())
	checkGeometryInvariants(t, 16, 1, 4, 6, 6)

	g = buildGeometry(1, 1, 3, 6, 6)
	assert.Equal(t, [2]int{1, 1}, dims(findBand(t, g[0], SubbandLL).rect))
	for _, res := range g[1:] {
		for _, b := range res.bands {
			assert.True(t, b.rect.empty(), "%v r%d", b.kind, res.r)
		}
	}
	checkGeometryInvariants(t, 1, 1, 3, 6, 6)

	g = buildGeometry(37, 29, 0, 6, 6)
	require.Len(t, g, 1)
	assert.Equal(t, [2]int{37, 29}, dims(findBand(t, g[0], SubbandLL).rect))
	checkGeometryInvariants(t, 37, 29, 0, 6, 6)
}

func TestGeometryCodeBlockExtremes(t *testing.T) {
	checkGeometryInvariants(t, 64, 64, 3, 2, 2)
	hl := findBand(t, buildGeometry(64, 64, 1, 2, 2)[1], SubbandHL)
	assert.Equal(t, 8, hl.gridW)
	assert.Equal(t, 8, hl.gridH)
	assert.Len(t, hl.cbs, 64)
	for _, res := range buildGeometry(64, 64, 5, 10, 10) {
		for _, b := range res.bands {
			if !b.rect.empty() {
				assert.Len(t, b.cbs, 1)
			}
		}
	}
	checkGeometryInvariants(t, 200, 200, 4, 10, 10)
}

func TestGeometrySinglePrecinct(t *testing.T) {
	for _, res := range buildGeometry(200, 200, 4, 6, 6) {
		assert.Equal(t, [2]int{1, 1}, [2]int{res.precinctsW, res.precinctsH})
		for _, b := range res.bands {
			for _, cb := range b.cbs {
				assert.Zero(t, cb.precinct)
			}
		}
	}
}
