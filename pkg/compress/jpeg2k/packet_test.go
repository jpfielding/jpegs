package jpeg2k

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacketBitIORoundTrip(t *testing.T) {
	rng := uint64(0x123456789abcdef0)
	for range 500 {
		rng = rng*6364136223846793005 + 1
		n := int(rng%40) + 1
		in := make([]uint8, n)
		w := newPacketBitWriter()
		for i := range in {
			in[i] = uint8(rng >> (i % 63) & 1)
			w.putBit(in[i])
		}
		out := w.finish()
		require.NotEqual(t, byte(0xFF), out[len(out)-1])
		r := &packetBitReader{data: out}
		for i, b := range in {
			got, err := r.readBit()
			require.NoError(t, err)
			require.Equal(t, b, got, "bit %d", i)
		}
	}
}

func TestPacketBitStuffingAllOnes(t *testing.T) {
	for n := 1; n <= 40; n++ {
		w := newPacketBitWriter()
		for range n {
			w.putBit(1)
		}
		out := w.finish()
		require.NotEqual(t, byte(0xFF), out[len(out)-1], "n=%d", n)
		for i := 1; i < len(out); i++ {
			if out[i-1] == 0xFF {
				require.Zero(t, out[i]&0x80, "stuffed byte MSB set")
			}
		}
		r := &packetBitReader{data: out}
		for range n {
			b, err := r.readBit()
			require.NoError(t, err)
			require.Equal(t, uint8(1), b)
		}
	}
}

func TestPacketBitReaderTruncation(t *testing.T) {
	_, err := (&packetBitReader{}).readBit()
	assert.ErrorIs(t, err, ErrInvalidData)
	w := newPacketBitWriter()
	for range 20 {
		w.putBit(1)
	}
	out := w.finish()
	r := &packetBitReader{data: out}
	for range len(out)*8 + 16 {
		if _, err = r.readBit(); err != nil {
			break
		}
	}
	assert.ErrorIs(t, err, ErrInvalidData)
}

func TestNumPassesFullRange(t *testing.T) {
	for n := 1; n <= 164; n++ {
		w := newPacketBitWriter()
		writeNumPasses(w, n)
		got, err := readNumPasses(&packetBitReader{data: w.finish()})
		require.NoError(t, err)
		require.Equal(t, n, got)
	}
}

func llBand(w, h, levels, xcb, ycb int) band {
	return buildGeometry(w, h, levels, xcb, ycb)[0].bands[0]
}

func synthBlock(nb, passes, n int, fill byte) *codedBlock {
	return &codedBlock{data: bytes.Repeat([]byte{fill}, n), passes: passes, bitplanes: nb}
}

// packetRoundTrip writes one single-band packet, reads it back and checks
// every contribution and body.
func packetRoundTrip(t *testing.T, b band, mb int, blocks []*codedBlock) []byte {
	t.Helper()
	bands, mbs := []band{b}, []int{mb}
	out, err := writePacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, bands, [][]*codedBlock{blocks}, mbs, nil)
	require.NoError(t, err)
	contribs, hdr, err := readPacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, bands, mbs, out)
	require.NoError(t, err)
	pos := hdr
	for k, blk := range blocks {
		c := contribs[0][k]
		if blk == nil || blk.bitplanes == 0 {
			require.Nil(t, c, "block %d", k)
			continue
		}
		require.NotNil(t, c, "block %d", k)
		require.Equal(t, blk.passes, c.passes)
		require.Equal(t, mb-blk.bitplanes, c.zeroBitplanes)
		require.Equal(t, len(blk.data), c.length)
		require.Equal(t, blk.data, out[pos:pos+c.length])
		pos += c.length
	}
	require.Equal(t, len(out), pos, "bodies must fill the packet")
	return out
}

func TestPacketEmpty(t *testing.T) {
	out := packetRoundTrip(t, llBand(64, 64, 0, 6, 6), 18, []*codedBlock{nil})
	assert.Equal(t, []byte{0x00}, out)
}

func TestPacketSingleBlock(t *testing.T) {
	out := packetRoundTrip(t, llBand(64, 64, 0, 6, 6), 18, []*codedBlock{synthBlock(7, 19, 42, 0xAB)})
	assert.Equal(t, bytes.Repeat([]byte{0xAB}, 42), out[len(out)-42:])
}

func TestPacketMultiBlockMixed(t *testing.T) {
	b := llBand(64, 64, 0, 2, 2)
	n := b.gridW * b.gridH
	require.Greater(t, n, 1)
	blocks := make([]*codedBlock, n)
	blocks[0] = synthBlock(3, 7, 1, 0x01)
	blocks[n/3] = synthBlock(12, 34, 200, 0x02)
	blocks[n/2] = synthBlock(1, 1, 5, 0x03)
	blocks[n-1] = synthBlock(20, 58, 999, 0x04)
	packetRoundTrip(t, b, 20, blocks)
}

func TestPacketTruncationNoPanic(t *testing.T) {
	b := llBand(64, 64, 0, 2, 2)
	n := b.gridW * b.gridH
	blocks := make([]*codedBlock, n)
	blocks[1] = synthBlock(9, 25, 300, 0x55)
	blocks[n-2] = synthBlock(4, 10, 7, 0xAA)
	bands, mbs := []band{b}, []int{20}
	out, err := writePacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, bands, [][]*codedBlock{blocks}, mbs, nil)
	require.NoError(t, err)
	for cut := range len(out) {
		_, _, _ = readPacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, bands, mbs, out[:cut])
	}
	_, _, err = readPacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, bands, mbs, out)
	require.NoError(t, err)
}

func TestPacketSequentialShared(t *testing.T) {
	type planned struct {
		b      band
		blocks []*codedBlock
		start  int
	}
	var stream []byte
	var plan []planned
	for _, res := range buildGeometry(64, 64, 2, 3, 3) {
		for _, b := range res.bands {
			if b.rect.empty() {
				continue
			}
			n := b.gridW * b.gridH
			blocks := make([]*codedBlock, n)
			blocks[0] = synthBlock(6, 16, 40+n, 0x11)
			if n > 1 {
				blocks[n-1] = synthBlock(2, 4, 17, 0x22)
			}
			start := len(stream)
			var err error
			stream, err = writePacket([]*precinctState{newPrecinctState(b.gridW, b.gridH)}, []band{b}, [][]*codedBlock{blocks}, []int{22}, stream)
			require.NoError(t, err)
			plan = append(plan, planned{b, blocks, start})
		}
	}
	pos := 0
	for _, p := range plan {
		require.Equal(t, p.start, pos)
		contribs, hdr, err := readPacket([]*precinctState{newPrecinctState(p.b.gridW, p.b.gridH)}, []band{p.b}, []int{22}, stream[pos:])
		require.NoError(t, err)
		body := pos + hdr
		for k, blk := range p.blocks {
			if blk == nil {
				continue
			}
			require.Equal(t, len(blk.data), contribs[0][k].length)
			require.Equal(t, blk.data, stream[body:body+len(blk.data)])
			body += len(blk.data)
		}
		pos = body
	}
	assert.Equal(t, len(stream), pos)
}

// Seeded stand-in for dicos.rs's prop_header_roundtrip (300 cases).
func TestPacketRandomHeaders(t *testing.T) {
	b := llBand(32, 32, 0, 2, 2)
	n := b.gridW * b.gridH
	rng := uint64(0xFACE)
	next := func() uint64 {
		rng = rng*6364136223846793005 + 1
		return rng >> 33
	}
	for range 300 {
		mb := 4 + int(next()%27)
		blocks := make([]*codedBlock, n)
		for k := range blocks {
			if next()%3 == 0 {
				continue
			}
			nb := 1 + int(next()%uint64(mb))
			passes := 1 + min(int(next()%uint64(3*nb-2)), 163)
			blocks[k] = synthBlock(nb, passes, int(next()%2000), byte(next()))
		}
		packetRoundTrip(t, b, mb, blocks)
	}
}
