package jpeg2k

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyRustFixtures are dicos.rs 1.0.0's frozen legacy streams
// (crates/jpeg2k/tests/fixtures/legacy, copied as rs-*.j2c). They differ from
// Go's only in Psot (14 + len instead of 12 + len).
func legacyRustFixtures() []legacyFixture {
	flat := make([]uint32, 64)
	for i := range flat {
		flat[i] = 1234
	}
	return []legacyFixture{
		{"rs-gradient-16x16", legacyGray16(16, 16, legacyGradient(16, 16)), 5},
		{"rs-random-32x32", legacyGray16(32, 32, legacyLCG(0xD1C05, 32*32, 65535)), 5},
		{"rs-odd-13x7", legacyGray16(13, 7, legacyLCG(0x0DD5, 13*7, 4095)), 5},
		{"rs-flat-8x8", legacyGray16(8, 8, flat), 5},
		{"rs-tall-1x16", legacyGray16(1, 16, legacyLCG(0x7A11, 16, 65535)), 5},
	}
}

func allLegacyFixtures() []legacyFixture {
	return append(legacyGoFixtures(), legacyRustFixtures()...)
}

func readLegacy(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "legacy", name+".j2c"))
	require.NoError(t, err)
	return data
}

// Every file the pre-T.800 encoders wrote keeps decoding to its exact source
// pixels, under LegacyAuto and LegacyOnly; StandardOnly rejects it.
func TestLegacyFixtures(t *testing.T) {
	for _, fx := range allLegacyFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			data := readLegacy(t, fx.name)
			for _, p := range []LegacyPolicy{LegacyAuto, LegacyOnly} {
				got, err := DecodeWithPolicy(bytes.NewReader(data), p)
				require.NoError(t, err)
				assert.Equal(t, fx.img, got)
			}
			_, err := DecodeWithPolicy(bytes.NewReader(data), StandardOnly)
			assert.ErrorIs(t, err, ErrInvalidData)

			cfg, err := DecodeConfig(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, fx.img.ColorModel(), cfg.ColorModel)
			assert.Equal(t, fx.img.Bounds().Size(), image.Pt(cfg.Width, cfg.Height))
		})
	}
}

func TestLegacyOnlyRejectsConformant(t *testing.T) {
	data := encodeBytes(t, gray16From(8, 8, patternPixels(2, 8, 8)), nil)
	_, err := DecodeWithPolicy(bytes.NewReader(data), LegacyOnly)
	assert.ErrorIs(t, err, ErrInvalidData)
	_, err = DecodeWithPolicy(bytes.NewReader(data), LegacyPolicy(99))
	assert.ErrorIs(t, err, ErrInvalidData)
}

// The fingerprint must hold in full; a near-miss is decoded as T.800 and
// fails there instead of being half-read by the legacy decoder.
func TestLegacyFingerprintIsStrict(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    func(t *testing.T, d []byte) []byte
	}{
		{"non-zero QCD exponent", func(t *testing.T, d []byte) []byte {
			d[markerOff(t, d, MarkerQCD)+5] = 16 << 3
			return d
		}},
		{"conformant SOD code", func(t *testing.T, d []byte) []byte {
			d[markerOff(t, d, MarkerSODLegacy)+1] = 0x93
			return d
		}},
		{"payload one sample short", func(t *testing.T, d []byte) []byte {
			s := markerOff(t, d, MarkerSOT)
			binary.BigEndian.PutUint32(d[s+6:], binary.BigEndian.Uint32(d[s+6:])-4)
			return append(d[:len(d)-6], 0xFF, 0xD9)
		}},
		{"component header width", func(t *testing.T, d []byte) []byte {
			d[markerOff(t, d, MarkerSODLegacy)+3]++
			return d
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.f(t, readLegacy(t, "go-gradient-16x16"))
			_, err := DecodeWithPolicy(bytes.NewReader(d), LegacyOnly)
			assert.ErrorIs(t, err, ErrInvalidData)
			_, err = Decode(bytes.NewReader(d))
			assert.Error(t, err)
		})
	}
}

// The legacy decoder checks MaxDecodedBytes before allocating.
func TestLegacyRespectsMaxDecodedBytes(t *testing.T) {
	withMaxDecodedBytes(t, 32*32*8-1)
	_, err := Decode(bytes.NewReader(readLegacy(t, "go-random-32x32")))
	assert.ErrorIs(t, err, ErrImageTooLarge)
}
