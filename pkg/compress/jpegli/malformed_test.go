package jpegli

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// segment returns the offset of marker m (2 bytes) and the total segment size
// including the marker, for the first occurrence in data.
func segment(t testing.TB, data []byte, m [2]byte) (off, size int) {
	t.Helper()
	off = bytes.Index(data, m[:])
	require.GreaterOrEqual(t, off, 0, "marker %X not found", m)
	return off, 2 + (int(data[off+2])<<8 | int(data[off+3]))
}

// withSegment replaces the segment for marker m with m + length + payload,
// where length is written explicitly (so tests can lie about it).
func withSegment(t testing.TB, data []byte, m [2]byte, length uint16, payload []byte) []byte {
	t.Helper()
	off, size := segment(t, data, m)
	out := append([]byte{}, data[:off]...)
	out = append(out, m[0], m[1], byte(length>>8), byte(length))
	out = append(out, payload...)
	return append(out, data[off+size:]...)
}

var (
	mDHT = [2]byte{0xFF, 0xC4}
	mSOS = [2]byte{0xFF, 0xDA}
	mSOF = [2]byte{0xFF, 0xC3}
)

// dht builds a DHT payload: Tc/Th, counts for lengths 1..16, then vals.
func dht(tcth byte, counts map[int]int, vals []byte) []byte {
	p := []byte{tcth}
	for l := 1; l <= 16; l++ {
		p = append(p, byte(counts[l]))
	}
	return append(p, vals...)
}

func dhtSeg(payload []byte) func(testing.TB, []byte) []byte {
	return func(t testing.TB, d []byte) []byte {
		return withSegment(t, d, mDHT, uint16(len(payload)+2), payload)
	}
}

// sosPatch edits the original SOS payload in place.
func sosPatch(f func(p []byte) []byte) func(testing.TB, []byte) []byte {
	return func(t testing.TB, d []byte) []byte {
		off, size := segment(t, d, mSOS)
		p := f(append([]byte{}, d[off+4:off+size]...))
		return withSegment(t, d, mSOS, uint16(len(p)+2), p)
	}
}

type malformedCase struct {
	name   string
	mutate func(testing.TB, []byte) []byte
}

func malformedCases() []malformedCase {
	return []malformedCase{
		{"sof_tq200_sos_selector_mismatch", func(t testing.TB, d []byte) []byte {
			o, _ := segment(t, d, mSOF)
			d[o+4+6+2] = 200 // Tq of component 0
			d[o+4+6] = 7     // component id
			return sosPatch(func(p []byte) []byte { p[1] = 9; return p })(t, d)
		}},
		{"dht_counts_exceed_payload", dhtSeg(dht(0x00, map[int]int{8: 255}, []byte{0, 1, 2}))},
		{"dht_oversubscribed_3x1bit", dhtSeg(dht(0x00, map[int]int{1: 3}, []byte{0, 1, 2}))},
		{"dht_zero_codes", dhtSeg(dht(0x00, nil, nil))},
		{"dht_payload_1byte", dhtSeg([]byte{0x00})},
		{"dht_ac_class_truncated", dhtSeg([]byte{0x10, 1, 2})},
		{"dht_bad_class", dhtSeg(dht(0x20, map[int]int{1: 1}, []byte{0}))},
		{"dht_bad_id", dhtSeg(dht(0x04, map[int]int{1: 1}, []byte{0}))},
		{"dht_over_256_symbols", dhtSeg(dht(0x00, map[int]int{8: 255, 9: 255}, make([]byte, 510)))},
		{"dht_len0", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mDHT, 0, nil) }},
		{"dht_len1", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mDHT, 1, nil) }},
		{"dht_all_symbols_200", dhtSeg(dht(0x00, map[int]int{1: 1, 2: 1, 3: 1, 4: 1}, []byte{200, 200, 200, 200}))},
		{"sos_td15", sosPatch(func(p []byte) []byte { p[2] = 0xF0; return p })},
		{"sos_td4", sosPatch(func(p []byte) []byte { p[2] = 0x40; return p })},
		{"sos_len3", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mSOS, 3, []byte{1}) }},
		{"sos_len2_empty", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mSOS, 2, nil) }},
		{"sos_len0", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mSOS, 0, nil) }},
		{"sos_ns255", sosPatch(func(p []byte) []byte { p[0] = 255; return p })},
		{"sos_ns0", sosPatch(func(p []byte) []byte { p[0] = 0; return p })},
		{"sos_bad_predictor", sosPatch(func(p []byte) []byte { p[len(p)-3] = 9; return p })},
		{"sof_len0", func(t testing.TB, d []byte) []byte { return withSegment(t, d, mSOF, 0, nil) }},
		{"sos_before_sof", func(t testing.TB, d []byte) []byte {
			off, size := segment(t, d, mSOF)
			return append(append([]byte{}, d[:off]...), d[off+size:]...)
		}},
		{"dri_len0", func(t testing.TB, d []byte) []byte {
			off, _ := segment(t, d, mDHT)
			return append(append(append([]byte{}, d[:off]...), 0xFF, 0xDD, 0, 0), d[off:]...)
		}},
		{"dri_len3", func(t testing.TB, d []byte) []byte {
			off, _ := segment(t, d, mDHT)
			return append(append(append([]byte{}, d[:off]...), 0xFF, 0xDD, 0, 3, 0), d[off:]...)
		}},
		{"com_len1", func(t testing.TB, d []byte) []byte {
			off, _ := segment(t, d, mDHT)
			return append(append(append([]byte{}, d[:off]...), 0xFF, 0xFE, 0, 1), d[off:]...)
		}},
		{"app1_len0", func(t testing.TB, d []byte) []byte {
			off, _ := segment(t, d, mDHT)
			return append(append(append([]byte{}, d[:off]...), 0xFF, 0xE1, 0, 0), d[off:]...)
		}},
	}
}

func TestDecode_Malformed(t *testing.T) {
	base := encodedGray16(t)
	for _, tc := range malformedCases() {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.mutate(t, append([]byte{}, base...))
			start := time.Now()
			assert.NotPanics(t, func() {
				_, err := Decode(bytes.NewReader(data))
				assert.Error(t, err)
			})
			assert.Less(t, time.Since(start), time.Second)
		})
	}
}

func FuzzDecode(f *testing.F) {
	// Keep valid-looking fuzzed headers from legitimately allocating up to the
	// default 1 GiB cap, which would exhaust fuzz worker memory.
	old := MaxDecodedBytes
	MaxDecodedBytes = 4 << 20
	f.Cleanup(func() { MaxDecodedBytes = old })

	base := encodedGray16(f)
	f.Add(base)
	for _, tc := range malformedCases() {
		f.Add(tc.mutate(f, append([]byte{}, base...)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(bytes.NewReader(data))
	})
}
