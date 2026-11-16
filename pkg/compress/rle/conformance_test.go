package rle

import (
	"bytes"
	"encoding/binary"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests ported from dicos.rs crates/jpegrle (decode.rs, encode.rs,
// packbits.rs, tests/mutation.rs), plus regressions for the two defects
// recorded in dicos.rs docs/reviews/codec-review.md (P1 cross-row runs, P2
// segment offsets inside the header), which Rust itself still has.

// gray16From builds a w×h Gray16 image from row-major samples.
func gray16From(w, h int, px []uint16) *image.Gray16 {
	img := image.NewGray16(image.Rect(0, 0, w, h))
	for i, v := range px {
		img.Pix[2*i] = byte(v >> 8)
		img.Pix[2*i+1] = byte(v)
	}
	return img
}

func samples16(img *image.Gray16) []uint16 {
	b := img.Bounds()
	out := make([]uint16, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out = append(out, img.Gray16At(x, y).Y)
		}
	}
	return out
}

func encode16(t *testing.T, w, h int, px []uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, gray16From(w, h, px)))
	return buf.Bytes()
}

// header builds a 64-byte RLE header.
func header(count uint32, offsets ...uint32) []byte {
	h := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(h[0:4], count)
	for i, o := range offsets {
		binary.LittleEndian.PutUint32(h[4+4*i:8+4*i], o)
	}
	return h
}

func TestDecode_RustHeaderCases(t *testing.T) {
	_, err := Decode(make([]byte, 10), 1, 1)
	require.ErrorIs(t, err, ErrTruncated, "too short")

	_, err = Decode(header(0), 1, 1)
	require.ErrorIs(t, err, ErrInvalidData, "zero segments")

	_, err = Decode(header(16), 1, 1)
	require.ErrorIs(t, err, ErrInvalidData, "16 segments")

	_, err = Decode(append(header(3, 64, 66, 68), make([]byte, 6)...), 1, 1)
	require.ErrorIs(t, err, ErrUnsupported, "3 segments (RGB)")

	// Rust decode_huge_dims_rejected_not_abort.
	_, err = Decode(header(1, 64), 0x7FFFFFFF, 0x7FFFFFFF)
	require.ErrorIs(t, err, ErrImageTooLarge)
}

// Review P2: a segment offset must point past the 64-byte header. Rust
// accepts count=1, offset=8 and decodes header bytes as pixels.
func TestDecode_RejectsOffsetInsideHeader(t *testing.T) {
	for _, off := range []uint32{0, 4, 8, 63} {
		data := header(1, off)
		_, err := Decode(data, 1, 1)
		require.ErrorIs(t, err, ErrInvalidData, "offset %d", off)
	}
	// Second segment inside the header.
	_, err := Decode(append(header(2, 64, 8), 0x00, 0x01, 0x00, 0x02), 1, 1)
	require.ErrorIs(t, err, ErrInvalidData)
}

func TestDecode_RejectsBadSegmentOrderAndBounds(t *testing.T) {
	body := []byte{0x00, 0x01, 0x00, 0x02} // two 1-byte literal segments
	cases := []struct {
		name string
		hdr  []byte
		want error
	}{
		{"out_of_order", header(2, 66, 64), ErrInvalidData},
		{"start_past_end", header(1, 200), ErrTruncated},
		{"second_past_end", header(2, 64, 200), ErrTruncated},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode(append(c.hdr, body...), 1, 1)
			require.ErrorIs(t, err, c.want)
		})
	}
	img, err := Decode(append(header(2, 64, 66), body...), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, []uint16{0x0102}, samples16(img.(*image.Gray16)))
}

func TestDecode_TruncatedSegment(t *testing.T) {
	// Literal of 3 declared, 1 present.
	_, err := Decode(append(header(1, 64), 0x02, 0x07), 3, 1)
	require.ErrorIs(t, err, ErrTruncated)
	// Replicate with no value byte.
	_, err = Decode(append(header(1, 64), 0xFE), 3, 1)
	require.ErrorIs(t, err, ErrTruncated)
	// Valid packets, too few samples.
	_, err = Decode(append(header(1, 64), 0x00, 0x07), 3, 1)
	require.ErrorIs(t, err, ErrDimensionMismatch)
}

func TestRoundTrip_Gray16_RustCases(t *testing.T) {
	seq := func(n int, f func(i int) uint16) []uint16 {
		out := make([]uint16, n)
		for i := range out {
			out[i] = f(i)
		}
		return out
	}
	cases := []struct {
		name string
		w, h int
		px   []uint16
	}{
		{"small", 3, 2, []uint16{100, 200, 300, 400, 500, 600}},
		{"uniform", 50, 50, seq(2500, func(int) uint16 { return 0x1234 })},
		{"gradient", 16, 16, seq(256, func(i int) uint16 { return uint16(i) })},
		{"max_values", 10, 10, seq(100, func(int) uint16 { return 0xFFFF })},
		{"alternating", 10, 10, seq(100, func(i int) uint16 { return uint16(-(i % 2)) })},
		{"large", 512, 512, seq(512*512, func(i int) uint16 { return uint16(i) })},
		{"odd_dims", 7, 13, seq(91, func(i int) uint16 { return uint16(i * 137) })},
		{"single_pixel", 1, 1, []uint16{0xABCD}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := encode16(t, c.w, c.h, c.px)
			assertRowAligned(t, data, c.w, c.h)
			img, err := Decode(data, c.w, c.h)
			require.NoError(t, err)
			assert.Equal(t, c.px, samples16(img.(*image.Gray16)))
		})
	}
}

func TestEncode_RustHeaderCases(t *testing.T) {
	data := encode16(t, 2, 2, []uint16{0x0102, 0x0304, 0x0506, 0x0708})
	require.GreaterOrEqual(t, len(data), headerSize)
	assert.EqualValues(t, 2, binary.LittleEndian.Uint32(data[0:4]))
	off1 := binary.LittleEndian.Uint32(data[4:8])
	off2 := binary.LittleEndian.Uint32(data[8:12])
	assert.EqualValues(t, headerSize, off1)
	assert.Greater(t, off2, off1)
	assert.Zero(t, (off2-off1)%2, "segment padded to even length")
	assert.Zero(t, (uint32(len(data))-off2)%2, "segment padded to even length")

	px := make([]uint16, 100*100)
	for i := range px {
		px[i] = 0x1234
	}
	// Per-row encoding costs one 2-byte packet per row and plane: 2×100×2
	// plus the header, far under the 20000 raw bytes.
	assert.Less(t, len(encode16(t, 100, 100, px)), 1000)

	data = encode16(t, 1, 1, []uint16{0xABCD})
	assert.EqualValues(t, 2, binary.LittleEndian.Uint32(data[0:4]))

	var buf bytes.Buffer
	require.ErrorIs(t, Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))), ErrUnsupported)
}

// Review P1: [0x1234]×4 as 2×2 must not produce a 4-sample run. Each plane
// must be two 2-sample replicate runs: FF 12 FF 12 and FF 34 FF 34.
func TestEncode_RunsDoNotCrossRows(t *testing.T) {
	data := encode16(t, 2, 2, []uint16{0x1234, 0x1234, 0x1234, 0x1234})
	want := append(header(2, 64, 68), 0xFF, 0x12, 0xFF, 0x12, 0xFF, 0x34, 0xFF, 0x34)
	assert.Equal(t, want, data)
	assertRowAligned(t, data, 2, 2)
}

func TestEncode_Gray8RowAlignedAndSubimage(t *testing.T) {
	full := image.NewGray(image.Rect(0, 0, 9, 7))
	for i := range full.Pix {
		full.Pix[i] = byte(i / 5) // runs that straddle rows
	}
	sub := full.SubImage(image.Rect(2, 1, 8, 6)).(*image.Gray)
	var buf bytes.Buffer
	require.NoError(t, Encode(&buf, sub))
	assertRowAligned(t, buf.Bytes(), 6, 5)
	img, err := Decode(buf.Bytes(), 6, 5)
	require.NoError(t, err)
	got := img.(*image.Gray)
	for y := 0; y < 5; y++ {
		for x := 0; x < 6; x++ {
			assert.Equal(t, sub.GrayAt(x+2, y+1), got.GrayAt(x, y))
		}
	}
}

// Streams from encoders that let runs cross rows (this package before the
// fix, dicos.rs today) must still decode.
func TestDecode_AcceptsCrossRowRuns(t *testing.T) {
	// 2×2 Gray16 of 0x1234 as one 4-sample run per plane, padded to even.
	data := append(header(2, 64, 66), 0xFD, 0x12, 0xFD, 0x34)
	img, err := Decode(data, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, []uint16{0x1234, 0x1234, 0x1234, 0x1234}, samples16(img.(*image.Gray16)))
}

// assertRowAligned walks every segment's packets and fails if any packet's
// output crosses a row boundary.
func assertRowAligned(t *testing.T, data []byte, w, h int) {
	t.Helper()
	n := binary.LittleEndian.Uint32(data[0:4])
	for s := uint32(0); s < n; s++ {
		start := binary.LittleEndian.Uint32(data[4+4*s:])
		end := uint32(len(data))
		if s+1 < n {
			end = binary.LittleEndian.Uint32(data[8+4*s:])
		}
		seg := data[start:end]
		out := 0
		for i := 0; i < len(seg) && out < w*h; {
			c := int8(seg[i])
			i++
			var cnt int
			switch {
			case c == -128:
				continue
			case c >= 0:
				cnt = int(c) + 1
				i += cnt
			default:
				cnt = int(-c) + 1
				i++
			}
			require.Equal(t, out/w, (out+cnt-1)/w, "segment %d: packet at sample %d (len %d) crosses a row", s, out, cnt)
			out += cnt
		}
		require.Equal(t, w*h, out, "segment %d sample count", s)
	}
}

// Mutation tests (Rust tests/mutation.rs): no panic, and an Ok result always
// has exactly W×H samples.
func mutationBase(t *testing.T) []byte {
	px := make([]uint16, 16*16)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			px[y*16+x] = uint16((x*7 + y*13) & 0xFF)
		}
	}
	return encode16(t, 16, 16, px)
}

func assertWellFormed(t *testing.T, img image.Image, err error) {
	t.Helper()
	if err != nil {
		return
	}
	require.Equal(t, image.Rect(0, 0, 16, 16), img.Bounds())
}

func smallDecodeCap(t *testing.T) {
	old := MaxDecodedBytes
	MaxDecodedBytes = 4 << 20
	t.Cleanup(func() { MaxDecodedBytes = old })
}

func TestMutation_TruncationPrefixes(t *testing.T) {
	smallDecodeCap(t)
	base := mutationBase(t)
	for n := 0; n < len(base); n++ {
		img, err := Decode(base[:n], 16, 16)
		assertWellFormed(t, img, err)
	}
}

func TestMutation_BitFlips(t *testing.T) {
	smallDecodeCap(t)
	base := mutationBase(t)
	flip := func(i int) {
		for bit := 0; bit < 8; bit++ {
			m := bytes.Clone(base)
			m[i] ^= 1 << bit
			img, err := Decode(m, 16, 16)
			assertWellFormed(t, img, err)
		}
	}
	for i := 0; i < min(64, len(base)); i++ {
		flip(i)
	}
	for i := max(0, len(base)-16); i < len(base); i++ {
		flip(i)
	}
}

// Ported from packbits.rs tests.
func TestPackBits_RustCases(t *testing.T) {
	enc := encodePackBits(makeBytes(0xBB, 128))
	assert.Len(t, enc, 2, "128-byte run is one packet")

	got, err := decodePackBits([]byte{0x80, 0x00, 0x42}, 0)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x42}, got, "0x80 is a no-op")

	got, err = decodePackBits(encodePackBits(makeBytes(0xAA, 100)), 50)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(got), 50, "expectedLen stops early")

	var alt []byte
	for i := byte(0); i < 20; i++ {
		for j := byte(0); j < 10; j++ {
			if i%2 == 0 {
				alt = append(alt, i)
			} else {
				alt = append(alt, i*10+j)
			}
		}
	}
	for _, p := range [][]byte{{0}, {0, 0}, {0, 1}, {0, 0, 0}, {0, 0, 1, 1, 1}, {1, 2, 3, 3, 3, 4, 5}, makeSequence(0, 255), make([]byte, 1000), alt} {
		got, err := decodePackBits(encodePackBits(p), len(p))
		require.NoError(t, err)
		assert.Equal(t, p, got)
	}
}

func FuzzDecode(f *testing.F) {
	var buf bytes.Buffer
	_ = Encode(&buf, gray16From(4, 3, []uint16{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}))
	f.Add(buf.Bytes(), 4, 3)
	f.Add(append(header(1, 64), 0xFD, 0x07), 2, 2)
	f.Fuzz(func(t *testing.T, data []byte, w, h int) {
		old := MaxDecodedBytes
		MaxDecodedBytes = 1 << 20
		defer func() { MaxDecodedBytes = old }()
		img, err := Decode(data, w, h)
		if err != nil {
			return
		}
		if img.Bounds() != image.Rect(0, 0, w, h) {
			t.Fatalf("bounds %v for %dx%d", img.Bounds(), w, h)
		}
		// Anything that decodes must survive a re-encode/decode round trip.
		var out bytes.Buffer
		if err := Encode(&out, img); err != nil {
			t.Fatal(err)
		}
		back, err := Decode(out.Bytes(), w, h)
		if err != nil {
			t.Fatal(err)
		}
		assertRowAligned(t, out.Bytes(), w, h)
		if !bytes.Equal(pix(img), pix(back)) {
			t.Fatal("round trip mismatch")
		}
	})
}

func pix(img image.Image) []byte {
	switch m := img.(type) {
	case *image.Gray:
		return m.Pix
	case *image.Gray16:
		return m.Pix
	}
	return nil
}
