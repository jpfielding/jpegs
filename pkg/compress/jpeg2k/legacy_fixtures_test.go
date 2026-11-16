package jpeg2k

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyGoFixturesDecode: every file the pre-T.800 Go encoder wrote must
// keep decoding to its exact source pixels.
func TestLegacyGoFixturesDecode(t *testing.T) {
	for _, fx := range legacyGoFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "legacy", fx.name+".j2c"))
			require.NoError(t, err)
			got, err := Decode(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, fx.img, got)
		})
	}
}

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

func TestLegacyRustFixturesDecode(t *testing.T) {
	for _, fx := range legacyRustFixtures() {
		t.Run(fx.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "legacy", fx.name+".j2c"))
			require.NoError(t, err)
			got, err := Decode(bytes.NewReader(data))
			require.NoError(t, err)
			assert.Equal(t, fx.img, got)
		})
	}
}
