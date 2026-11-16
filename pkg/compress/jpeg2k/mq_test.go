package jpeg2k

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxBit struct {
	ctx int
	bit uint8
}

func mqRoundTrip(t *testing.T, seq []ctxBit) {
	t.Helper()
	ce := defaultContexts()
	enc := newMQEncoder()
	for _, s := range seq {
		enc.encode(s.bit, &ce[s.ctx])
	}
	enc.flush()
	cd := defaultContexts()
	dec := newMQDecoder(enc.bytes())
	for i, s := range seq {
		require.Equal(t, s.bit, dec.decode(&cd[s.ctx]), "symbol %d", i)
	}
}

func TestMQRoundTripSingleContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		bit  func(i int) uint8
		n    int
	}{
		{"zeros", func(int) uint8 { return 0 }, 100},
		{"ones", func(int) uint8 { return 1 }, 100},
		{"alternating", func(i int) uint8 { return uint8(i % 2) }, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seq := make([]ctxBit, tc.n)
			for i := range seq {
				seq[i] = ctxBit{0, tc.bit(i)}
			}
			mqRoundTrip(t, seq)
		})
	}
}

func TestMQRoundTripMultipleContexts(t *testing.T) {
	mqRoundTrip(t, []ctxBit{{0, 1}, {1, 0}, {0, 1}, {2, 1}, {1, 0}, {0, 0}, {2, 1}, {0, 1}, {1, 1}, {2, 0}})
}

func TestMQRoundTripRandom(t *testing.T) {
	rng := uint32(12345)
	seq := make([]ctxBit, 500)
	for i := range seq {
		rng = rng*1103515245 + 12345
		seq[i] = ctxBit{0, uint8(rng >> 16 & 1)}
	}
	mqRoundTrip(t, seq)
}

func TestMQInitialContextStates(t *testing.T) {
	ctx := defaultContexts()
	for i, s := range ctx {
		want := uint8(0)
		switch i {
		case ctxZCStart:
			want = 4
		case ctxRunLength:
			want = 3
		case ctxUniform:
			want = 46
		}
		assert.Equal(t, want, s.index, "context %d", i)
		assert.Equal(t, uint8(0), s.mps, "context %d", i)
	}
}

func TestMQRoundTripAllContexts(t *testing.T) {
	var zeros, ones, alt []ctxBit
	for c := range numMQContexts {
		for range 500 {
			zeros = append(zeros, ctxBit{c, 0})
			ones = append(ones, ctxBit{c, 1})
		}
	}
	for i := range 10_000 {
		alt = append(alt, ctxBit{i % numMQContexts, uint8(i % 2)})
	}
	rng := uint64(0x2545F4914F6CDD1D)
	random := make([]ctxBit, 10_000)
	for i := range random {
		rng = rng*6364136223846793005 + 1
		random[i] = ctxBit{int(rng>>40) % numMQContexts, uint8(rng >> 33 & 1)}
	}
	for name, seq := range map[string][]ctxBit{"zeros": zeros, "ones": ones, "alternating": alt, "random": random} {
		t.Run(name, func(t *testing.T) { mqRoundTrip(t, seq) })
	}
}

// Truncated and empty codewords must decode without panicking or reading
// out of bounds, however many symbols are pulled.
func TestMQDecodeTruncatedNoPanic(t *testing.T) {
	ce := defaultContexts()
	enc := newMQEncoder()
	rng := uint32(0x12345678)
	for range 2_000 {
		rng = rng*1103515245 + 12345
		enc.encode(uint8(rng>>16&1), &ce[int(rng>>20)%numMQContexts])
	}
	enc.flush()
	data := enc.bytes()
	for cut := 0; cut <= len(data); cut++ {
		cd := defaultContexts()
		dec := newMQDecoder(data[:cut])
		for i := range 3_000 {
			dec.decode(&cd[i%numMQContexts])
		}
	}
}

func TestMQDecodeEmptyNoPanic(t *testing.T) {
	cd := defaultContexts()
	dec := newMQDecoder(nil)
	for i := range 1_000 {
		dec.decode(&cd[i%numMQContexts])
	}
}
