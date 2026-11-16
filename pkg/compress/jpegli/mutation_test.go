package jpegli

import (
	"bytes"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mutation suite ported from dicos.rs crates/jpegli/tests/mutation.rs:
// whatever the corruption, Decode never panics and never returns an image of
// the wrong size. The base stream has a restart after every row.

const mutW, mutH = 16, 16

func mutationBase(t *testing.T) []byte {
	px := make([]int, mutW*mutH)
	for y := 0; y < mutH; y++ {
		for x := 0; x < mutW; x++ {
			px[y*mutW+x] = (x*7 + y*13) & 0xFF
		}
	}
	return encodeOpts(t, mutW, mutH, px, Options{Precision: 8, RestartIntervalRows: 1})
}

func assertWellFormed(t *testing.T, data []byte) {
	t.Helper()
	img, err := Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	require.Equal(t, image.Rect(0, 0, mutW, mutH), img.Bounds())
}

func TestMutation_TruncationPrefixes(t *testing.T) {
	smallDecodeCap(t)
	base := mutationBase(t)
	for n := 0; n < len(base); n++ {
		_, err := Decode(bytes.NewReader(base[:n]))
		require.Error(t, err, "prefix %d of %d decoded", n, len(base))
	}
}

func TestMutation_BitFlips(t *testing.T) {
	smallDecodeCap(t)
	base := mutationBase(t)
	flip := func(i int) {
		for bit := 0; bit < 8; bit++ {
			m := bytes.Clone(base)
			m[i] ^= 1 << bit
			assertWellFormed(t, m)
		}
	}
	for i := 0; i < min(64, len(base)); i++ {
		flip(i)
	}
	for i := max(0, len(base)-16); i < len(base); i++ {
		flip(i)
	}
}

// dicos.rs accepts a stream without EOI (review P2); Go requires it.
func TestMutation_ChoppedEOI(t *testing.T) {
	base := mutationBase(t)
	require.Equal(t, []byte{0xFF, 0xD9}, base[len(base)-2:])
	_, err := Decode(bytes.NewReader(base[:len(base)-2]))
	require.ErrorIs(t, err, ErrTruncated)
}

func TestMutation_MidEntropyTruncation(t *testing.T) {
	base := mutationBase(t)
	entropy := markerPositions(base, 0xDA)[0] + 10
	for _, extra := range []int{0, 1, 2, 4, 8} {
		_, err := Decode(bytes.NewReader(base[:entropy+extra]))
		var te *TruncatedError
		require.ErrorAs(t, err, &te, "entropy+%d", extra)
		assert.LessOrEqual(t, te.Offset, entropy+extra)
	}
}

func TestMutation_SwappedRestartMarkers(t *testing.T) {
	base := mutationBase(t)
	pos := markerPositions(base, 0xD0)
	pos = append(pos, markerPositions(base, 0xD1)...)
	require.Len(t, pos, 4, "RST0 and RST1 each appear twice in 15 restarts")
	m := bytes.Clone(base)
	m[pos[0]+1], m[pos[2]+1] = m[pos[2]+1], m[pos[0]+1]
	require.ErrorIs(t, decodeErr(m), ErrInvalidData)
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5}, uint8(3), uint8(16), uint8(1), uint8(0), uint8(0))
	f.Add([]byte{0, 0x80, 0xFF, 0x7F}, uint8(2), uint8(12), uint8(5), uint8(2), uint8(1))
	f.Fuzz(func(t *testing.T, raw []byte, w, prec, pred, pt, rows uint8) {
		old := MaxDecodedBytes
		MaxDecodedBytes = 1 << 20
		defer func() { MaxDecodedBytes = old }()
		width := int(w%32) + 1
		if len(raw) < 2*width || len(raw) > 1<<14 {
			return
		}
		height := len(raw) / (2 * width)
		p := int(prec%15) + 2
		o := Options{Precision: p, Predictor: int(pred%7) + 1, PointTransform: int(pt) % p, RestartIntervalRows: int(rows % 4)}
		px := make([]int, width*height)
		for i := range px {
			px[i] = (int(raw[2*i])<<8 | int(raw[2*i+1])) >> (16 - p)
		}
		var buf bytes.Buffer
		if err := Encode(&buf, gray16(width, height, px), &o); err != nil {
			t.Fatal(err)
		}
		img, err := Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		if got, want := samplesOf(t, img), shifted(px, o.PointTransform); !assert.ObjectsAreEqual(want, got) {
			t.Fatalf("%+v: round trip mismatch", o)
		}
	})
}
