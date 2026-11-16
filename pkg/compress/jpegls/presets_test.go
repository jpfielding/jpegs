package jpegls

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeWithPresets builds a stream carrying an LSE ID=1 segment. The public
// encoder never writes one; this exercises the decoder's preset handling.
func encodeWithPresets(samples []uint16, w, h, precision, near int, p Presets) []byte {
	buf := binary.BigEndian.AppendUint16(nil, MarkerSOI)
	buf = appendSOF(buf, precision, h, w)
	buf = binary.BigEndian.AppendUint16(buf, MarkerLSE)
	buf = binary.BigEndian.AppendUint16(buf, 13)
	buf = append(buf, 1)
	for _, v := range []int{p.MaxVal, p.T1, p.T2, p.T3, p.Reset} {
		buf = binary.BigEndian.AppendUint16(buf, uint16(v))
	}
	buf = appendSOS(buf, 1, near)
	bw := NewBitWriter(buf)
	encodeScan(bw, samples, w, h, NewContextModelPresets(p.MaxVal, near, p.Reset, p.T1, p.T2, p.T3))
	return binary.BigEndian.AppendUint16(bw.Bytes(), MarkerEOI)
}

func TestDecodeLSEPresets(t *testing.T) {
	for _, tc := range []struct {
		name            string
		precision, near int
		presets         Presets
	}{
		{"custom thresholds and reset", 8, 0, Presets{MaxVal: 255, T1: 5, T2: 9, T3: 30, Reset: 31}},
		{"reduced maxval", 12, 0, Presets{MaxVal: 4000, T1: 10, T2: 40, T3: 100, Reset: 64}},
		{"default thresholds minimal reset", 16, 2, Presets{MaxVal: 60000, Reset: 3}},
		{"equal thresholds near-lossless", 8, 1, Presets{MaxVal: 200, T1: 3, T2: 3, T3: 3, Reset: 200}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			samples := pattern("smooth", 30, 20, uint16(tc.presets.MaxVal))
			copy(samples[40:], pattern("randomflat", 30, 10, uint16(tc.presets.MaxVal)))
			data := encodeWithPresets(samples, 30, 20, tc.precision, tc.near, tc.presets)
			requireWithinNear(t, samples, mustDecodeT87(t, data), tc.near)

			// The presets matter: the default model must not decode it.
			plain := mustEncode(t, samples, 30, 20, tc.precision, tc.near)
			assert.NotEqual(t, plain[25:], data[40:], "presets changed the scan")
		})
	}
}

func TestDecodeRejectsInvalidPresets(t *testing.T) {
	samples := pattern("smooth", 8, 8, 255)
	for name, p := range map[string]Presets{
		"reset below 3":           {MaxVal: 255, Reset: 2},
		"maxval above precision":  {MaxVal: 256, Reset: 64},
		"t1 not above near":       {MaxVal: 255, T1: 1, T2: 7, T3: 21, Reset: 64},
		"thresholds out of order": {MaxVal: 255, T1: 9, T2: 7, T3: 21, Reset: 64},
		"t3 above maxval":         {MaxVal: 100, T1: 3, T2: 7, T3: 101, Reset: 64},
	} {
		data := encodeWithPresets(samples, 8, 8, 8, 1, Presets{MaxVal: 255, Reset: 64})
		// Patch the LSE payload in place (it starts after SOI + SOF55 + LSE marker/length/ID).
		off := 2 + 13 + 5
		for i, v := range []int{p.MaxVal, p.T1, p.T2, p.T3, p.Reset} {
			binary.BigEndian.PutUint16(data[off+2*i:], uint16(v))
		}
		_, err := decode(data, profileT87)
		require.ErrorIs(t, err, ErrInvalidData, name)
	}
}
