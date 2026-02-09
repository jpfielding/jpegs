# Compression Codecs

This package provides pure Go implementations of lossless image compression formats.

## Supported Formats

| Package | Format | Description |
|---------|--------|-------------|
| [jpeg2k](jpeg2k/) | JPEG 2000 | Wavelet-based, excellent compression |
| [jpegli](jpegli/) | JPEG Lossless | Traditional DPCM-based |
| [jpegls](jpegls/) | JPEG-LS | LOCO-I algorithm, very efficient |
| [rle](rle/) | RLE (PackBits) | Run-length encoding |

## Feature Comparison

| Feature | jpeg2k | jpegli | jpegls | rle |
|---------|--------|--------|--------|-----|
| Lossless | Yes | Yes | Yes | Yes |
| Near-lossless | No | No | Yes | No |
| 8-bit Gray | Yes | Yes | Yes | Yes |
| 16-bit Gray | Yes | Yes | Yes | Yes |
| RGB | Yes | No | No | No |
| Compression Ratio | Excellent | Good | Excellent | Fair |
| Speed | Moderate | Fast | Fast | Very Fast |

## Usage

All codecs follow a consistent API pattern where possible:

```go
// Encoding
err := codec.Encode(writer, image, options)

// Decoding (most codecs)
img, err := codec.Decode(reader)
```

`rle` decoding requires raw data plus width/height (see its README).
See individual package READMEs for detailed usage and options.

## Pure Go

All implementations are pure Go with no CGO dependencies, enabling:
- Cross-compilation to any Go-supported platform
- No external library dependencies
- Consistent behavior across platforms
