package jpegls

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBitWriterStuffing(t *testing.T) {
	t.Run("byte after 0xFF carries 7 bits", func(t *testing.T) {
		bw := NewBitWriter(nil)
		bw.WriteBits(0xFF, 8)
		bw.WriteBits(0xFF, 8)
		bw.Flush()
		// 0xFF, then 7 bits 1111111 with a leading stuffed 0 (0x7F), then
		// the last 1 bit padded: 0x80.
		assert.Equal(t, []byte{0xFF, 0x7F, 0x80}, bw.Bytes())
	})
	t.Run("final 0xFF gets its stuff byte", func(t *testing.T) {
		bw := NewBitWriter(nil)
		bw.WriteBits(0xFF, 8)
		bw.Flush()
		assert.Equal(t, []byte{0xFF, 0x00}, bw.Bytes())
	})
	t.Run("flush pads with zeros", func(t *testing.T) {
		bw := NewBitWriter([]byte{0xAA})
		bw.WriteBits(0b101, 3)
		bw.Flush()
		bw.Flush()
		assert.Equal(t, []byte{0xAA, 0b10100000}, bw.Bytes())
	})
}

func TestBitReader(t *testing.T) {
	t.Run("reads MSB first", func(t *testing.T) {
		br := NewBitReader([]byte{0b10110010, 0b11000011})
		for _, want := range [][2]int{{3, 5}, {5, 18}, {4, 12}, {0, 0}, {4, 3}} {
			v, err := br.ReadBits(want[0])
			require.NoError(t, err)
			assert.Equal(t, uint32(want[1]), v)
		}
		_, err := br.ReadBit()
		require.ErrorIs(t, err, errEndOfData)
	})
	t.Run("unstuffs after 0xFF", func(t *testing.T) {
		br := NewBitReader([]byte{0xFF, 0x7F, 0x80})
		v, err := br.ReadBits(16)
		require.NoError(t, err)
		assert.Equal(t, uint32(0xFFFF), v)
	})
	t.Run("marker ends the scan", func(t *testing.T) {
		br := NewBitReader([]byte{0xA5, 0xFF, 0xD9})
		v, err := br.ReadBits(8)
		require.NoError(t, err)
		assert.Equal(t, uint32(0xA5), v)
		_, err = br.ReadBit()
		var m markerError
		require.ErrorAs(t, err, &m)
		assert.Equal(t, markerError(MarkerEOI), m)
		assert.Equal(t, 1, br.MarkerPos())
		_, err = br.ReadBit()
		require.ErrorAs(t, err, &m, "sticky")
	})
	t.Run("marker position after a trailing 0xFF data byte", func(t *testing.T) {
		br := NewBitReader([]byte{0xFF, 0x00, 0xFF, 0xD9})
		_, err := br.ReadBits(8)
		require.NoError(t, err)
		assert.Equal(t, 1, br.Pos())
		assert.Equal(t, 2, br.MarkerPos(), "skips the unread stuff byte")
	})
}

func TestLimitedGolomb(t *testing.T) {
	t.Run("known codes", func(t *testing.T) {
		bw := NewBitWriter(nil)
		bw.WriteLimitedGolomb(2, 13, 32, 8) // q=3 r=1: 0001 01
		bw.WriteLimitedGolomb(0, 2, 32, 8)  // 001
		// q = 200>>0 >= escape (23): 23 zeros, 1, then 199 in 8 bits.
		bw.WriteLimitedGolomb(0, 200, 32, 8)
		bw.Flush()
		assert.Equal(t, []byte{0b00010100, 0b10000000, 0, 0, 0b11100011, 0b10000000}, bw.Bytes())
	})
	t.Run("round trip incl. escapes and stuffing", func(t *testing.T) {
		type code struct {
			k, glimit, qbpp int
			val             uint32
		}
		var codes []code
		for _, p := range [][2]int{{32, 8}, {64, 16}, {20, 2}, {48, 12}, {32 - 15 - 1, 8}} {
			glimit, qbpp := p[0], p[1]
			for k := 0; k <= qbpp; k++ {
				for _, v := range []uint32{0, 1, 2, 3, 1<<k - 1, 1 << k, 1<<qbpp - 1, 1 << qbpp, uint32(glimit-qbpp-1) << k, uint32(glimit-qbpp-2)<<k | (1<<k - 1)} {
					if v == 0 && k > 0 || v > 1<<qbpp {
						continue
					}
					codes = append(codes, code{k, glimit, qbpp, v}, code{k, glimit, qbpp, 0})
				}
			}
		}
		bw := NewBitWriter(nil)
		for i, c := range codes {
			bw.WriteLimitedGolomb(c.k, c.val, c.glimit, c.qbpp)
			if i%7 == 0 {
				bw.WriteBits(0xFFFF, 16) // force stuffing between codes
			}
		}
		bw.Flush()
		br := NewBitReader(bw.Bytes())
		for i, c := range codes {
			v, err := br.ReadLimitedGolomb(c.k, c.glimit, c.qbpp)
			require.NoError(t, err)
			require.Equal(t, c.val, v, "code %d %+v", i, c)
			if i%7 == 0 {
				v, err := br.ReadBits(16)
				require.NoError(t, err)
				require.Equal(t, uint32(0xFFFF), v)
			}
		}
	})
	t.Run("over-long unary prefix is rejected", func(t *testing.T) {
		br := NewBitReader([]byte{0, 0, 0, 0x01, 0xFF})
		_, err := br.ReadLimitedGolomb(0, 32, 8) // 31 zeros > escape (23)
		require.ErrorIs(t, err, errBadCode)
		br = NewBitReader([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
		_, err = br.ReadLimitedGolomb(0, 32, 8)
		require.ErrorIs(t, err, errBadCode, "rejected without reading to the end")
		assert.Less(t, br.Pos(), 10)
	})
}

func TestRunLengthCoding(t *testing.T) {
	for _, remaining := range []int{1, 2, 5, 16, 100, 40000} {
		for _, run := range []int{0, 1, 2, 3, 15, 16, 17, 99, 100, 39999, 40000} {
			if run > remaining {
				continue
			}
			for _, startIndex := range []int{0, 3, 17, 31} {
				enc := NewContextModel(255, 0, DefaultReset)
				enc.RunIndex = startIndex
				bw := NewBitWriter(nil)
				encodeRunLength(bw, enc, run, run == remaining)
				bw.WriteBits(0b1011, 4) // sentinel: the code must be self-delimiting
				bw.Flush()

				dec := NewContextModel(255, 0, DefaultReset)
				dec.RunIndex = startIndex
				br := NewBitReader(bw.Bytes())
				got, err := decodeRunLength(br, dec, remaining)
				require.NoError(t, err)
				require.Equal(t, run, got, "run %d of %d from index %d", run, remaining, startIndex)
				require.Equal(t, enc.RunIndex, dec.RunIndex)
				sentinel, err := br.ReadBits(4)
				require.NoError(t, err)
				require.Equal(t, uint32(0b1011), sentinel)
			}
		}
	}
	t.Run("run longer than the line is rejected", func(t *testing.T) {
		cm := NewContextModel(255, 0, DefaultReset)
		cm.RunIndex = 8 // J = 2
		_, err := decodeRunLength(NewBitReader([]byte{0b01100000}), cm, 2)
		require.ErrorIs(t, err, errBadCode)
	})
}
