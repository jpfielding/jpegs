package jpeg2k

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// Mutation tests (dicos.rs tests/mutation.rs): however a stream is
// corrupted, Decode must not panic, must return a classified error, and
// any image it does return must be within MaxDecodedBytes.

const mutationMaxBytes = 1 << 20

func mutationBase(t *testing.T) []byte {
	px := make([]uint16, 16*16)
	for i := range px {
		px[i] = uint16((i%16 + i/16) & 0xFF)
	}
	return encodeBytes(t, gray16From(16, 16, px), nil)
}

func checkWellFormed(t *testing.T, data []byte, what string) {
	t.Helper()
	for _, p := range []LegacyPolicy{LegacyAuto, StandardOnly, LegacyOnly} {
		img, err := DecodeWithPolicy(bytes.NewReader(data), p)
		if err != nil {
			require.True(t, errors.Is(err, ErrInvalidData) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrImageTooLarge),
				"%s: unclassified error %v", what, err)
			continue
		}
		b := img.Bounds()
		require.False(t, b.Empty(), what)
		require.LessOrEqual(t, int64(b.Dx()*b.Dy()), int64(mutationMaxBytes), what)
	}
}

func mutationStreams(t *testing.T) map[string][]byte {
	return map[string][]byte{
		"t800":         mutationBase(t),
		"legacy-go":    readLegacy(t, "go-gradient-16x16"),
		"legacy-rgb":   readLegacy(t, "go-rgba-odd-7x5"),
		"legacy-rs":    readLegacy(t, "rs-odd-13x7"),
		"legacy-gray8": readLegacy(t, "go-gray8-odd-9x5"),
	}
}

func TestMutationTruncation(t *testing.T) {
	withMaxDecodedBytes(t, mutationMaxBytes)
	for name, base := range mutationStreams(t) {
		for n := range len(base) {
			_, err := Decode(bytes.NewReader(base[:n]))
			require.Error(t, err, "%s truncated to %d", name, n)
			checkWellFormed(t, base[:n], name)
		}
	}
}

// Every single-bit flip in the first 64 and last 16 bytes.
func TestMutationBitFlips(t *testing.T) {
	withMaxDecodedBytes(t, mutationMaxBytes)
	for name, base := range mutationStreams(t) {
		var offsets []int
		for i := range min(64, len(base)) {
			offsets = append(offsets, i)
		}
		for i := max(len(base)-16, 64); i < len(base); i++ {
			offsets = append(offsets, i)
		}
		for _, i := range offsets {
			for bit := range 8 {
				m := bytes.Clone(base)
				m[i] ^= 1 << bit
				checkWellFormed(t, m, name)
			}
		}
	}
}

func TestMutationMissingEOC(t *testing.T) {
	base := mutationBase(t)
	require.Equal(t, []byte{0xFF, 0xD9}, base[len(base)-2:])
	_, err := Decode(bytes.NewReader(base[:len(base)-2]))
	require.ErrorIs(t, err, ErrInvalidData)
}
