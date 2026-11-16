package jpegls

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPredictMED(t *testing.T) {
	assert.Equal(t, 15, PredictMED(10, 20, 15), "no edge: a + b - c")
	assert.Equal(t, 3, PredictMED(5, 3, 10), "c >= max(a, b): min(a, b)")
	assert.Equal(t, 5, PredictMED(5, 3, 1), "c <= min(a, b): max(a, b)")
	assert.Equal(t, 100, PredictMED(100, 100, 100))
	assert.Equal(t, 55000, PredictMED(60000, 50000, 55000))
}

func TestContextModelDerivedParameters(t *testing.T) {
	for _, tc := range []struct {
		maxVal, near            int
		rng, qbpp, limit, aInit int
		t1, t2, t3              int
	}{
		{255, 0, 256, 8, 32, 4, 3, 7, 21},
		{65535, 0, 65536, 16, 64, 1024, 18, 67, 276},
		{4095, 0, 4096, 12, 48, 64, 18, 67, 276},
		{1023, 0, 1024, 10, 40, 16, 6, 19, 72},
		{255, 2, 52, 6, 32, 2, 9, 17, 35},
		{100, 0, 101, 7, 30, 2, 2, 3, 10},
		{3, 0, 4, 2, 20, 2, 2, 3, 3},
		{3, 1, 2, 1, 20, 2, 3, 3, 3},
	} {
		cm := NewContextModel(tc.maxVal, tc.near, DefaultReset)
		assert.Equal(t, tc.rng, cm.Range, "RANGE for %+v", tc)
		assert.Equal(t, tc.qbpp, cm.Qbpp, "qbpp for %+v", tc)
		assert.Equal(t, tc.limit, cm.Limit, "LIMIT for %+v", tc)
		assert.Equal(t, [3]int{tc.t1, tc.t2, tc.t3}, [3]int{cm.T1, cm.T2, cm.T3}, "thresholds for %+v", tc)
		for i := range cm.A {
			assert.Equal(t, tc.aInit, cm.A[i])
			assert.Equal(t, 1, cm.N[i])
		}
	}
}

func TestContextModelPresets(t *testing.T) {
	cm := NewContextModelPresets(255, 0, 31, 5, 0, 30)
	assert.Equal(t, [3]int{5, 7, 30}, [3]int{cm.T1, cm.T2, cm.T3}, "zero keeps the default")
	assert.Equal(t, 31, cm.Reset)
}

func TestQuantizeGradient(t *testing.T) {
	cm := NewContextModel(255, 0, DefaultReset) // T1=3 T2=7 T3=21
	for d, want := range map[int]int{0: 0, 1: 1, 2: 1, 3: 2, 6: 2, 7: 3, 20: 3, 21: 4, -1: -1, -3: -2, -7: -3, -21: -4} {
		assert.Equal(t, want, cm.QuantizeGradient(d), "d=%d", d)
	}
	near := NewContextModel(255, 2, DefaultReset) // T1=9 T2=17 T3=35
	for d, want := range map[int]int{0: 0, 2: 0, -2: 0, 3: 1, -3: -1, 9: 2, -9: -2, 17: 3, 35: 4, -35: -4} {
		assert.Equal(t, want, near.QuantizeGradient(d), "near d=%d", d)
	}
}

func TestGetContextIndex(t *testing.T) {
	cm := NewContextModel(255, 0, DefaultReset)
	q, sign := cm.GetContextIndex(0, 0, 0)
	assert.Equal(t, [2]int{0, 1}, [2]int{q, sign})
	q, sign = cm.GetContextIndex(5, 0, 0)
	assert.Equal(t, [2]int{162, 1}, [2]int{q, sign})
	q, sign = cm.GetContextIndex(-5, 0, 0)
	assert.Equal(t, [2]int{162, -1}, [2]int{q, sign}, "sign flip shares the context")
	q, sign = cm.GetContextIndex(0, 0, -30)
	assert.Equal(t, [2]int{4, -1}, [2]int{q, sign})
	q, _ = cm.GetContextIndex(30, 30, 30)
	assert.Equal(t, 364, q, "largest index")
	q, _ = cm.GetContextIndex(30, -30, -30)
	assert.Equal(t, 284, q)
}

func TestUpdateStats(t *testing.T) {
	t.Run("single-step bias", func(t *testing.T) {
		cm := NewContextModel(255, 0, DefaultReset)
		assert.Equal(t, 2, cm.ComputeK(0), "A=4 N=1")
		cm.UpdateStats(0, 5)
		// B=5 > 0: C++, B -= N(2) = 3, still > 0 so B = 0.
		assert.Equal(t, [4]int{9, 0, 1, 2}, [4]int{cm.A[0], cm.B[0], cm.C[0], cm.N[0]})
		cm.UpdateStats(1, -5)
		// B=-5 <= -N(2): C--, B += 2 = -3, still <= -2 so B = -N + 1.
		assert.Equal(t, [4]int{9, -1, -1, 2}, [4]int{cm.A[1], cm.B[1], cm.C[1], cm.N[1]})
	})
	t.Run("near scales the bias accumulator", func(t *testing.T) {
		cm := NewContextModel(255, 2, DefaultReset)
		cm.N[0], cm.B[0] = 50, -100
		cm.UpdateStats(0, 3)
		assert.Equal(t, -100+3*5+51, cm.B[0])
	})
	t.Run("reset halves", func(t *testing.T) {
		cm := NewContextModel(255, 0, 4)
		for range 3 {
			cm.UpdateStats(0, 0)
		}
		assert.Equal(t, [2]int{4, 4}, [2]int{cm.A[0], cm.N[0]})
		cm.B[0] = -3
		cm.UpdateStats(0, 0)
		assert.Equal(t, [2]int{2, 3}, [2]int{cm.A[0], cm.N[0]})
		assert.Equal(t, -2, cm.B[0], "negative B halves toward minus infinity: -((1+3)>>1)")
	})
	t.Run("C saturates", func(t *testing.T) {
		cm := NewContextModel(255, 0, DefaultReset)
		for range 1000 {
			cm.UpdateStats(0, -100)
			cm.UpdateStats(1, 100)
		}
		assert.Equal(t, -128, cm.C[0])
		assert.Equal(t, 127, cm.C[1])
	})
}

func TestRunStats(t *testing.T) {
	cm := NewContextModel(255, 0, 4)
	assert.Equal(t, 2, cm.ComputeKRun(0))
	assert.Equal(t, 2, cm.ComputeKRun(1))

	cm.UpdateRunStats(0, 3, true) // A += (3+1-0)>>1
	assert.Equal(t, [3]int{6, 2, 1}, [3]int{cm.A[365], cm.N[365], cm.Nn[0]})
	cm.UpdateRunStats(0, 1, false)
	cm.UpdateRunStats(0, 1, false)
	assert.Equal(t, [2]int{8, 4}, [2]int{cm.A[365], cm.N[365]})
	cm.UpdateRunStats(0, 1, true) // N == RESET: halve A, N and Nn
	assert.Equal(t, [3]int{4, 3, 1}, [3]int{cm.A[365], cm.N[365], cm.Nn[0]})

	cm.UpdateRunStats(1, 3, false) // RItype 1: A += (3+1-1)>>1
	assert.Equal(t, 5, cm.A[366])
	cm.N[366] = 6
	assert.Equal(t, 1, cm.ComputeKRun(1), "TEMP = A + N/2 = 8; 6<<1 >= 8")
}

func TestErrorMapping(t *testing.T) {
	for e, want := range map[int]uint32{0: 0, -1: 1, 1: 2, -2: 3, 2: 4, -128: 255} {
		assert.Equal(t, want, mapError(e, 0), "e=%d", e)
		assert.Equal(t, e, unmapError(want, 0))
	}
	// With the k == 0 bias flip the roles of e and -(e+1) swap.
	for e, want := range map[int]uint32{-1: 0, 0: 1, -2: 2, 1: 3} {
		assert.Equal(t, want, mapError(e, -1), "flipped e=%d", e)
		assert.Equal(t, e, unmapError(want, -1))
	}
}

func TestSampleArithmetic(t *testing.T) {
	assert.Equal(t, 7, quantizeError(7, 0))
	assert.Equal(t, [5]int{0, 1, 1, -1, -1}, [5]int{quantizeError(2, 2), quantizeError(3, 2), quantizeError(7, 2), quantizeError(-3, 2), quantizeError(-7, 2)})

	assert.Equal(t, [4]int{127, -128, -1, 0}, [4]int{moduloRange(127, 256), moduloRange(128, 256), moduloRange(255, 256), moduloRange(-256, 256)})
	assert.Equal(t, [2]int{50, -50}, [2]int{moduloRange(50, 101), moduloRange(51, 101)}, "odd RANGE")

	assert.Equal(t, 255, fixReconstructed(-1, 0, 256, 255), "wraps below zero")
	assert.Equal(t, 0, fixReconstructed(256, 0, 256, 255), "wraps above MAXVAL")
	assert.Equal(t, 0, fixReconstructed(-2, 2, 52, 255), "within NEAR of the range clamps")
	assert.Equal(t, 255, fixReconstructed(257, 2, 52, 255))
	assert.Equal(t, 257, fixReconstructed(-3, 2, 52, 255)+2, "beyond NEAR wraps by RANGE*(2*NEAR+1)")

	assert.True(t, validErrVal(128, 256))
	assert.False(t, validErrVal(-129, 256))
}
