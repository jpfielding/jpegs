package jpegli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ported from dicos.rs crates/jpegli/src/decode.rs tests.

// sof3Stream builds SOI + SOF3 (1x1) with the given precision and components.
func sof3Stream(precision, ncomp byte) []byte {
	payload := []byte{precision, 0x00, 0x01, 0x00, 0x01, ncomp}
	for i := byte(0); i < ncomp; i++ {
		payload = append(payload, i+1, 0x11, 0x00)
	}
	v := []byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, byte(len(payload) + 2)}
	return append(v, payload...)
}

// sosStream builds a valid P=8 header with a DHT and an SOS carrying the
// given Ss, Se and Ah|Al bytes.
func sosStream(ss, se, ahal byte) []byte {
	v := sof3Stream(8, 1)
	v = append(v, 0xFF, 0xC4, 0x00, 0x14, 0x00, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	return append(v, 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0x00, ss, se, ahal)
}

func decodeErr(data []byte) error {
	_, err := Decode(bytes.NewReader(data))
	return err
}

func TestDecode_RustHeaderValidation(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"missing_soi", []byte{0x00, 0x00, 0xFF, 0xD9}, ErrInvalidData},
		{"only_soi", []byte{0xFF, 0xD8}, ErrTruncated},
		{"sof0_baseline", []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x02}, ErrUnsupported},
		{"precision_0", sof3Stream(0, 1), ErrInvalidParameter},
		{"precision_1", sof3Stream(1, 1), ErrInvalidParameter},
		{"precision_17", sof3Stream(17, 1), ErrInvalidParameter},
		{"three_components", sof3Stream(8, 3), ErrUnsupported},
		{"predictor_0", sosStream(0, 0, 0), ErrInvalidParameter},
		{"predictor_8", sosStream(8, 0, 0), ErrInvalidParameter},
		{"se_nonzero", sosStream(1, 5, 0), ErrInvalidParameter},
		{"ah_nonzero", sosStream(1, 0, 0x10), ErrInvalidParameter},
		{"pt_ge_precision", sosStream(1, 0, 0x08), ErrInvalidParameter},
		{"dc_selector_4", append(sof3Stream(8, 1), 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0x40, 1, 0, 0), ErrInvalidParameter},
		{"oversubscribed_dht", append([]byte{0xFF, 0xD8, 0xFF, 0xC4, 0x00, 0x16, 0x00, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 1, 2), ErrInvalidData},
		{"sos_without_dht", append(sof3Stream(8, 1), 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0x00, 1, 0, 0), ErrInvalidData},
		{"rst_before_scan", []byte{0xFF, 0xD8, 0xFF, 0xD0}, ErrInvalidData},
		{"eoi_before_scan", []byte{0xFF, 0xD8, 0xFF, 0xD9}, ErrInvalidData},
		{"second_sof", append(sof3Stream(8, 1), sof3Stream(8, 1)[2:]...), ErrInvalidData},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.ErrorIs(t, decodeErr(c.data), c.want)
		})
	}
}

func TestDecode_RejectsZeroDimensions(t *testing.T) {
	for _, wh := range [][2]byte{{0, 4}, {4, 0}} {
		v := []byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x0B, 8, 0, wh[1], 0, wh[0], 1, 1, 0x11, 0}
		require.ErrorIs(t, decodeErr(v), ErrInvalidParameter, "%dx%d", wh[0], wh[1])
	}
	// DRI before a zero-width SOF3 must not divide by zero.
	v := []byte{0xFF, 0xD8, 0xFF, 0xDD, 0x00, 0x04, 0x00, 0x04, 0xFF, 0xC3, 0x00, 0x0B, 8, 0, 4, 0, 0, 1, 1, 0x11, 0}
	require.Error(t, decodeErr(v))
}

func TestDecode_HugeDimsRejectedBeforeAllocation(t *testing.T) {
	v := []byte{0xFF, 0xD8, 0xFF, 0xC3, 0x00, 0x0B, 16, 0xFF, 0xFF, 0xFF, 0xFF, 1, 1, 0x11, 0}
	smallDecodeCap(t)
	require.ErrorIs(t, decodeErr(v), ErrImageTooLarge)
}

func TestRoundTrip_PredictorPointTransformPrecision(t *testing.T) {
	w, h := 5, 4
	for _, prec := range []int{2, 8, 12, 16} {
		px := make([]int, w*h)
		for i := range px {
			px[i] = int(uint64(i) * 2_654_435_761 % (1 << prec))
		}
		for pred := 1; pred <= 7; pred++ {
			for _, pt := range []int{0, 2} {
				if pt >= prec {
					continue
				}
				data := encodeOpts(t, w, h, px, Options{Predictor: pred, PointTransform: pt, Precision: prec})
				assert.Equal(t, shifted(px, pt), decodeSamples(t, data), "P=%d pred=%d pt=%d", prec, pred, pt)
			}
		}
	}
}

func TestRoundTrip_Shapes(t *testing.T) {
	ramp := func(n, mul int) []int {
		px := make([]int, n)
		for i := range px {
			px[i] = i * mul % 65536
		}
		return px
	}
	big := make([]int, 64*64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			big[y*64+x] = (x*100 + y*200) % 65536
		}
	}
	cases := []struct {
		name string
		w, h int
		px   []int
	}{
		{"constant", 8, 8, func() []int {
			p := make([]int, 64)
			for i := range p {
				p[i] = 12345
			}
			return p
		}()},
		{"gradient", 16, 16, ramp(256, 1)},
		{"full_range", 4, 2, []int{0, 1, 65534, 65535, 32768, 32767, 100, 60000}},
		{"large", 64, 64, big},
		{"single_pixel", 1, 1, []int{42000}},
		{"single_row", 128, 1, ramp(128, 512)},
		{"single_column", 1, 128, ramp(128, 512)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for pred := 1; pred <= 7; pred++ {
				assert.Equal(t, c.px, decodeSamples(t, encodeOpts(t, c.w, c.h, c.px, Options{Predictor: pred})), "predictor %d", pred)
			}
		})
	}
}

func TestRestart_RoundTripMatrix(t *testing.T) {
	for _, wh := range [][2]int{{8, 6}, {5, 5}, {1, 4}} {
		w, h := wh[0], wh[1]
		for _, prec := range []int{8, 16} {
			px := detImage(w, h, prec)
			for _, rows := range []int{1, 2, 3, h} {
				for _, pred := range []int{1, 4, 7} {
					for _, pt := range []int{0, 2} {
						data := encodeOpts(t, w, h, px, Options{Predictor: pred, PointTransform: pt, Precision: prec, RestartIntervalRows: rows})
						assert.Equal(t, shifted(px, pt), decodeSamples(t, data), "%dx%d P=%d rows=%d pred=%d pt=%d", w, h, prec, rows, pred, pt)
					}
				}
			}
		}
	}
}

func TestRestart_MarkersCycleMod8(t *testing.T) {
	px := detImage(1, 10, 8)
	data := encodeOpts(t, 1, 10, px, Options{Precision: 8, RestartIntervalRows: 1})
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 0}, restartIndices(data))
	assert.Equal(t, px, decodeSamples(t, data))
}

func TestRestart_DRIValueIsRowsTimesWidth(t *testing.T) {
	data := encodeOpts(t, 6, 4, detImage(6, 4, 8), Options{Precision: 8, RestartIntervalRows: 2})
	dri := markerPositions(data, 0xDD)
	require.Len(t, dri, 1)
	assert.Equal(t, []byte{0x00, 0x04, 0x00, 12}, data[dri[0]+2:dri[0]+6])
}

// A restart resets prediction (H.1.2.1): two images sharing row 1 but not
// row 0 produce identical entropy after RST0.
func TestRestart_ResetsPrediction(t *testing.T) {
	a := []int{10, 20, 30, 100, 110, 120}
	b := []int{200, 150, 90, 100, 110, 120}
	o := Options{Predictor: 4, Precision: 8, RestartIntervalRows: 1}
	ea, eb := encodeOpts(t, 3, 2, a, o), encodeOpts(t, 3, 2, b, o)
	tail := func(d []byte) []byte { return d[markerPositions(d, 0xD0)[0]:] }
	assert.Equal(t, tail(ea), tail(eb))
	assert.Equal(t, a, decodeSamples(t, ea))
	assert.Equal(t, b, decodeSamples(t, eb))

	// 1x2 with a jump across the restart: row 1 predicts 2^(P'-1).
	d := encodeOpts(t, 1, 2, []int{250, 3}, Options{Precision: 8, RestartIntervalRows: 1})
	assert.NotEmpty(t, markerPositions(d, 0xD0))
	assert.Equal(t, []int{250, 3}, decodeSamples(t, d))
}

func restartStream(t *testing.T, w, h int) []byte {
	return encodeOpts(t, w, h, detImage(w, h, 8), Options{Precision: 8, RestartIntervalRows: 1})
}

func TestRestart_Errors(t *testing.T) {
	t.Run("ri_not_multiple_of_width", func(t *testing.T) {
		d := restartStream(t, 4, 4)
		dri := markerPositions(d, 0xDD)[0]
		d[dri+4], d[dri+5] = 0, 3
		require.ErrorIs(t, decodeErr(d), ErrInvalidData)
	})
	t.Run("out_of_order", func(t *testing.T) {
		d := restartStream(t, 4, 4)
		d[markerPositions(d, 0xD0)[0]+1] = 0xD3
		require.ErrorIs(t, decodeErr(d), ErrInvalidData)
	})
	t.Run("without_dri", func(t *testing.T) {
		d := restartStream(t, 4, 4)
		dri := markerPositions(d, 0xDD)[0]
		d = append(d[:dri:dri], d[dri+6:]...)
		require.ErrorIs(t, decodeErr(d), ErrInvalidData)
	})
	t.Run("missing_marker", func(t *testing.T) {
		d := restartStream(t, 4, 6)
		rst := markerPositions(d, 0xD0)[0]
		d = append(d[:rst:rst], d[rst+2:]...)
		require.Error(t, decodeErr(d))
	})
	t.Run("truncated_inside_interval", func(t *testing.T) {
		d := restartStream(t, 4, 8)
		sos := markerPositions(d, 0xDA)[0]
		require.ErrorIs(t, decodeErr(d[:sos+10+3]), ErrTruncated)
	})
	t.Run("rst_after_last_interval", func(t *testing.T) {
		d := restartStream(t, 4, 2)
		d = append(d[:len(d)-2:len(d)-2], 0xFF, 0xD1, 0xFF, 0xD9)
		require.ErrorIs(t, decodeErr(d), ErrInvalidData)
	})
}

// Beyond dicos.rs (review P2): the scan must end at EOI, with nothing but
// padding bits before it. APPn/COM may precede EOI; bytes after EOI are
// ignored (DICOM pads fragments to even length).
func TestDecode_EndOfScan(t *testing.T) {
	px := detImage(5, 3, 16)
	base := encodeOpts(t, 5, 3, px, Options{})
	body := base[:len(base)-2]

	require.ErrorIs(t, decodeErr(body), ErrTruncated, "missing EOI")
	require.ErrorIs(t, decodeErr(append(bytes.Clone(body), 0x12, 0x34, 0xFF, 0xD9)), ErrInvalidData, "extra entropy bytes")
	require.ErrorIs(t, decodeErr(append(bytes.Clone(body), 0xFF, 0xDA, 0x00, 0x08, 1, 1, 0, 1, 0, 0, 0xFF, 0xD9)), ErrUnsupported, "second scan")
	require.ErrorIs(t, decodeErr(append(bytes.Clone(body), 0xFF, 0xDB, 0x00, 0x02, 0xFF, 0xD9)), ErrInvalidData, "DQT after scan")

	assert.Equal(t, px, decodeSamples(t, append(bytes.Clone(body), 0xFF, 0xFE, 0x00, 0x04, 'h', 'i', 0xFF, 0xFF, 0xD9)), "COM and fill bytes before EOI")
	assert.Equal(t, px, decodeSamples(t, append(bytes.Clone(base), 0x00)), "DICOM pad byte after EOI")
}

// Q4: a stream cut anywhere inside the scan is an error, never a partial
// image (the pre-conformance decoder returned partial images past 99%).
func TestDecode_StrictTruncation(t *testing.T) {
	px := detImage(64, 64, 16)
	data := encodeOpts(t, 64, 64, px, Options{})
	for _, cut := range []int{1, 2, 3, 8, 64} {
		_, err := Decode(bytes.NewReader(data[:len(data)-2-cut]))
		require.ErrorIs(t, err, ErrTruncated, "cut %d", cut)
	}
}
