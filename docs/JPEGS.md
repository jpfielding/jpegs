# DICOM Loading Implementation Notes

## Transfer Syntaxes and Compression

### JPEG Compression Variants in DICOM

DICOM files can use several JPEG compression variants. It's critical to understand the differences:

| Transfer Syntax UID | Name | Marker | Go Package |
|---------------------|------|--------|------------|
| 1.2.840.10008.1.2.4.50 | JPEG Baseline | SOF0 (0xFFC0) | `image/jpeg` |
| 1.2.840.10008.1.2.4.70 | JPEG Lossless (T.81) | SOF3 (0xFFC3) | `pkg/compress/jpegli` |
| 1.2.840.10008.1.2.4.80 | JPEG-LS Lossless (T.87) | SOF55 (0xFFF7) | `pkg/compress/jpegls` |
| 1.2.840.10008.1.2.4.90 | JPEG 2000 | Various | `pkg/compress/jpeg2k` |

**Key Insight**: JPEG Lossless (T.81) and JPEG-LS (T.87) are completely different formats despite similar names:
- **JPEG Lossless (T.81)**: ITU-T T.81 / ISO 10918-1, uses predictive coding
- **JPEG-LS (T.87)**: ITU-T T.87 / ISO 14495-1, uses LOCO-I algorithm

The standard Go `image/jpeg` package only supports baseline/progressive JPEG, not lossless variants.

### Identifying Compression from Frame Data

When decoding encapsulated frames, check the first few bytes:
- `FF D8 FF C0` - JPEG Baseline (SOF0)
- `FF D8 FF C3` - JPEG Lossless (SOF3) → use `jpegli`
- `FF D8 FF F7` - JPEG-LS (SOF55) → use `jpegls`
