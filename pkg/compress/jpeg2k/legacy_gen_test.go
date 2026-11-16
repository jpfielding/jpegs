package jpeg2k

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGenerateLegacyFixtures writes testdata/legacy/go-*.j2c with the
// pre-T.800 encoder. It only runs with JPEG2K_GEN_LEGACY=1 and only works
// at jpegs v0.2.0 (the last version that wrote this format).
func TestGenerateLegacyFixtures(t *testing.T) {
	if os.Getenv("JPEG2K_GEN_LEGACY") == "" {
		t.Skip("set JPEG2K_GEN_LEGACY=1 to regenerate (v0.2.0 encoder only)")
	}
	for _, fx := range legacyGoFixtures() {
		opts := DefaultOptions()
		opts.DecompLevels = fx.levels
		var buf bytes.Buffer
		require.NoError(t, Encode(&buf, fx.img, opts), fx.name)
		require.NoError(t, os.WriteFile(filepath.Join("testdata", "legacy", fx.name+".j2c"), buf.Bytes(), 0o644))
	}
}
