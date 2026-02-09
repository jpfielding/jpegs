# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Pure Go implementations of lossless image compression codecs for DICOM/DICOS medical imaging. Zero CGO dependencies. The module is `github.com/jpfielding/jpegs` (Go 1.25.5). The project was previously named `goxel` and some references to that name remain in the Makefile and README.

## Common Commands

```bash
make test              # Run short tests on ./pkg/...
make lint              # golangci-lint v2 static analysis on ./pkg/...
make vet               # go vet ./pkg/...
make vulnerability     # govulncheck on ./pkg/...
make test-report       # Tests with JUnit XML output (tmp/report.xml)
make build-ctl         # Build the goxel UI binary to bin/
make clean             # Remove build artifacts
make update-deps       # Clean mod cache, re-vendor, tidy
```

Run a single test:
```bash
go test -v -run TestName ./pkg/compress/jpegls/
```

## Architecture

All codec source lives under `pkg/compress/`. Each codec package follows a consistent pattern: `encoder.go`, `decoder.go`, bitstream utilities, and round-trip tests.

| Package | Algorithm | DICOM Transfer Syntax | SOF Marker |
|---------|-----------|----------------------|------------|
| `jpegls` | LOCO-I (JPEG-LS, ITU-T T.87) | `1.2.840.10008.1.2.4.80/81` | SOF55 `0xFFF7` |
| `jpegli` | DPCM + Huffman (JPEG Lossless, ITU-T T.81 Annex H) | `1.2.840.10008.1.2.4.70` | SOF3 `0xFFC3` |
| `jpeg2k` | DWT + EBCOT (JPEG 2000, ITU-T T.800) | `1.2.840.10008.1.2.4.90` | SOC `0xFF4F` |
| `rle` | PackBits (DICOM RLE) | `1.2.840.10008.1.2.5` | N/A (segment header) |

JPEG Lossless (T.81) and JPEG-LS (T.87) are entirely different formats despite similar names. Go's standard `image/jpeg` only handles baseline/progressive JPEG, not any lossless variant.

### Codec API Pattern

```go
err := codec.Encode(writer, image, options)
img, err := codec.Decode(reader)
```

Exception: `rle.Decode` requires raw data plus width/height parameters.

### Key Implementation Concepts

- **jpegls**: Context-based adaptive prediction (365 contexts), Golomb-Rice entropy coding, gradient quantization, median edge detection predictor. Supports near-lossless via `Near` parameter. Run mode exists but is currently disabled.
- **jpeg2k**: 5/3 reversible DWT, tile-based encoding, MQ arithmetic coder, EBCOT block coding (Tier-1), reversible color transform for RGB.
- **jpegli**: 7 predictor modes (1-7), Huffman entropy coding, DPCM prediction.
- **rle**: Byte-plane separation for 16-bit images, Apple PackBits encoding within DICOM RLE 64-byte header format.

## Testing

All codecs use round-trip testing: encode an image, decode it, verify pixel-perfect reconstruction. Tests cover 8-bit and 16-bit grayscale images with pattern-based test data (gradients, solid colors, high-contrast). Only `testify` is used as an external test dependency.

## Dependencies

The only direct dependency is `github.com/stretchr/testify` (testing only). All codec implementations use the Go standard library exclusively.
