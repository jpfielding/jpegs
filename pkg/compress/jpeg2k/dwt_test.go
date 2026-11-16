package jpeg2k

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fwd1(x []int32) []int32 {
	s := append([]int32(nil), x...)
	forward1D(s, make([]int32, len(s)), make([]int32, len(s)))
	return s
}

func inv1(x []int32) []int32 {
	s := append([]int32(nil), x...)
	inverse1D(s, make([]int32, len(s)), make([]int32, len(s)))
	return s
}

func TestDWT1DRoundTrip(t *testing.T) {
	for _, x := range [][]int32{
		{10, 20, 30, 40, 50, 60, 70, 80}, {5, 15, 25, 35, 45}, {100, 200}, {7, 13, 42}, {42},
		{0, 0, 0, 0, 0, 0, 0, 0}, {42, 42, 42, 42, 42, 42, 42, 42},
	} {
		assert.Equal(t, x, inv1(fwd1(x)))
	}
	assert.Equal(t, []int32{42}, fwd1([]int32{42}), "length 1 is a no-op")
	assert.Equal(t, make([]int32, 8), fwd1(make([]int32, 8)))
}

// mirror is whole-sample symmetric extension of index i into [0, n).
func mirror(n, i int) int {
	if n == 1 {
		return 0
	}
	p := 2 * (n - 1)
	k := ((i % p) + p) % p
	if k >= n {
		k = p - k
	}
	return k
}

func floorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// refForward1D is the direct T.800 F.4.8.2.1 reversible 5/3 with floor
// division, in the [lows, highs] layout.
func refForward1D(x []int32) []int32 {
	n := len(x)
	if n < 2 {
		return append([]int32(nil), x...)
	}
	y := make([]int32, n)
	for i := 1; i < n; i += 2 {
		y[i] = x[i] - floorDiv(x[mirror(n, i-1)]+x[mirror(n, i+1)], 2)
	}
	for i := 0; i < n; i += 2 {
		y[i] = x[i] + floorDiv(y[mirror(n, i-1)]+y[mirror(n, i+1)]+2, 4)
	}
	var out []int32
	for i := 0; i < n; i += 2 {
		out = append(out, y[i])
	}
	for i := 1; i < n; i += 2 {
		out = append(out, y[i])
	}
	return out
}

func signedRamp(n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		out[i] = int32((i*7 - 3*n) * (i%3 - 1))
	}
	return out
}

func TestDWT1DMatchesReference(t *testing.T) {
	for n := 1; n <= 17; n++ {
		x := signedRamp(n)
		require.Equal(t, refForward1D(x), fwd1(x), "n=%d", n)
		require.Equal(t, x, inv1(fwd1(x)), "n=%d", n)
		require.Equal(t, x, inv1(refForward1D(x)), "n=%d", n)
	}
}

func TestDWT1DFloorNotTruncation(t *testing.T) {
	x := []int32{-1, 5, -2} // -1 + -2 = -3: floor(-3/2) = -2, truncation -1
	assert.Equal(t, refForward1D(x), fwd1(x))
	assert.Equal(t, []int32{3, 2, 7}, fwd1(x), "truncation would give high 6")
}

func dwtRoundTrip(t *testing.T, w, h, levels int, seed int32) {
	t.Helper()
	orig := make([]int32, w*h)
	for i := range orig {
		orig[i] = int32(i)*seed%4096 - 2048
	}
	data := append([]int32(nil), orig...)
	forwardDWT(data, w, h, levels)
	inverseDWT(data, w, h, levels)
	require.Equal(t, orig, data, "%dx%d @ %d levels", w, h, levels)
}

func TestDWT2DRoundTrip(t *testing.T) {
	dwtRoundTrip(t, 13, 7, 5, 31)
	dwtRoundTrip(t, 1, 16, 4, 17)
	dwtRoundTrip(t, 16, 1, 4, 23)
	dwtRoundTrip(t, 64, 64, 5, 3)
	dwtRoundTrip(t, 17, 13, 3, 5)
	dwtRoundTrip(t, 2, 2, 1, 9)
	dwtRoundTrip(t, 6, 4, 1, 1)

	one := []int32{42}
	forwardDWT(one, 1, 1, 3)
	assert.Equal(t, []int32{42}, one)
}

// A 1x4 region still transforms its column (the legacy DWT stopped early).
func TestDWTDegenerateRegionTransformed(t *testing.T) {
	orig := []int32{10, 20, 30, 40}
	data := append([]int32(nil), orig...)
	forwardDWT(data, 1, 4, 1)
	assert.NotEqual(t, orig, data)
	inverseDWT(data, 1, 4, 1)
	assert.Equal(t, orig, data)
}
