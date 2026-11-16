package jpegls

import "embed"

// testdata holds the conformance and legacy fixtures (see README.md).
//
//go:embed testdata
var testdata embed.FS
