package jpegli

import (
	"bytes"
	"math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// encodeLegacy reproduces the v0.2.0 encoder for 16-bit images: the same
// headers as Encode, SSSS=16 followed by 16 appended bits (0x7FFF) and
// truncating /2 in predictors 5 and 6. Test-only.
func encodeLegacy(t testing.TB, w, h int, px []int, pred int) []byte {
	t.Helper()
	hdr := encodeOpts(t, w, h, px, Options{Predictor: pred})
	hdr = hdr[:markerPositions(hdr, 0xDA)[0]+10]
	ht := defaultTable()
	var code, size [17]int
	for k, v := range ht.values {
		code[v], size[v] = int(ht.codes[k]), ht.sizes[k]
	}
	bw := bitWriter{out: bytes.Clone(hdr)}
	prev, cur := make([]int, w), make([]int, w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := px[y*w+x]
			cur[x] = v
			var p int
			switch {
			case y == 0 && x == 0:
				p = 1 << 15
			case y == 0:
				p = cur[x-1]
			case x == 0:
				p = prev[0]
			default:
				p = predictLegacy(pred, cur[x-1], prev[x], prev[x-1])
			}
			diff := (v - p) & 0xFFFF
			if diff > 0x7FFF {
				diff -= 0x10000
			}
			s := categorize(diff)
			bw.writeBits(code[s], size[s])
			if s > 0 {
				if diff < 0 {
					diff += 1<<s - 1
				}
				bw.writeBits(diff, s)
			}
		}
		prev, cur = cur, prev
	}
	bw.flush()
	return append(bw.out, 0xFF, 0xD9)
}

func TestEncodeLegacy_MatchesV020Fixtures(t *testing.T) {
	for _, c := range []struct {
		name string
		pred int
	}{{"ssss16-p1-16x8", 1}, {"ssss16-p5-16x8", 5}, {"ssss16-p6-16x8", 6}, {"ssss16-p7-16x8", 7}, {"random16-p1-13x7", 1}} {
		w, h, px := readPGM(t, testdata("legacy", c.name+".pgm"))
		want, err := os.ReadFile(testdata("legacy", c.name+".jpg"))
		require.NoError(t, err)
		assert.Equal(t, want, encodeLegacy(t, w, h, px, c.pred), c.name)
	}
}

// TestLegacyDetection measures how reliably Decode tells legacy SSSS=16
// streams from T.81 streams with the same headers, on small images built to
// make differences of exactly 32768 frequent (a worst case). With 100000
// draws (94480 containing SSSS=16): 330 legacy streams also decode as T.81
// without error, so "T.81 first, legacy only on failure" would return wrong
// pixels for them; decodeAuto returns all of them correctly. 6 T.81 streams
// pass every legacy check and are misread; all have the pattern sample 0,
// then +3, then a jump of at least 8192 at the start of the scan.
func TestLegacyDetection(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var strictOK, legacyOK, autoBadLegacy, autoBadT81, n int
	for i := 0; i < 20000; i++ {
		w, h := 2+rng.Intn(20), 1+rng.Intn(12)
		px := make([]int, w*h)
		for k := range px {
			// Few distinct values so differences of exactly 32768 are common.
			px[k] = rng.Intn(4) + 32768*rng.Intn(2)
		}
		pred := []int{1, 2, 3, 4, 7}[rng.Intn(5)]
		leg := encodeLegacy(t, w, h, px, pred)
		t81 := encodeOpts(t, w, h, px, Options{Predictor: pred})
		if bytes.Equal(leg, t81) {
			continue // no SSSS=16
		}
		n++
		if img, err := decode(leg, profileT81); err == nil {
			strictOK++
			_ = img
		}
		if _, err := decode(t81, profileLegacy); err == nil {
			legacyOK++
		}
		if img, err := Decode(bytes.NewReader(leg)); err != nil || !assert.ObjectsAreEqual(px, samplesOf(t, img)) {
			autoBadLegacy++
		}
		if img, err := Decode(bytes.NewReader(t81)); err != nil || !assert.ObjectsAreEqual(px, samplesOf(t, img)) {
			autoBadT81++
		}
	}
	t.Logf("%d streams with SSSS=16: legacy read as T.81 without error %d; T.81 read as legacy without error %d; auto wrong: legacy %d, T.81 %d",
		n, strictOK, legacyOK, autoBadLegacy, autoBadT81)
	assert.Zero(t, autoBadLegacy)
	assert.Less(t, autoBadT81*2000, n, "T.81 misread rate must stay below 0.05%")
}
