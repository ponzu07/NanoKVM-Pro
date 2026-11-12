package hid

import (
	"encoding/hex"
	"math"
	"testing"
)

// Reference values computed by the analysis script used to fit the weights.
func TestAbsReportRisk(t *testing.T) {
	cases := []struct {
		hex  string
		crc  uint16
		risk float64
	}{
		{"008d43004000", 0xbff7, 5.410}, // always failed on the device
		{"00273d004000", 0x8ff7, 4.774}, // always failed on the device
		{"006833004000", 0xa9a0, 2.064}, // always succeeded
		{"00ff5c403f00", 0x84e9, 5.002},
		{"000000000000", 0xe4ff, 3.671},
		{"01ffffffff7f", 0x15ea, 16.361},
	}
	for _, c := range cases {
		d, _ := hex.DecodeString(c.hex)
		if got := crc16USB(d); got != c.crc {
			t.Errorf("%s crc = %04x, want %04x", c.hex, got, c.crc)
		}
		if got := absReportRisk(d); math.Abs(got-c.risk) > 1e-6 {
			t.Errorf("%s risk = %f, want %f", c.hex, got, c.risk)
		}
	}
}

func TestPickAbsReport(t *testing.T) {
	in, _ := hex.DecodeString("018d43004002")
	out := pickAbsReport(in)
	if out[0] != in[0] || out[5] != in[5] {
		t.Fatalf("buttons/wheel changed: %x -> %x", in, out)
	}
	dx := int(out[1]) | int(out[2])<<8 - (int(in[1]) | int(in[2])<<8)
	dy := int(out[3]) | int(out[4])<<8 - (int(in[3]) | int(in[4])<<8)
	if dx < -absJitterRadius || dx > absJitterRadius || dy < -absJitterRadius || dy > absJitterRadius {
		t.Fatalf("moved too far: dx=%d dy=%d", dx, dy)
	}
	if absReportRisk(out) > absReportRisk(in) {
		t.Fatalf("risk increased")
	}
	if hex.EncodeToString(in) != "018d43004002" {
		t.Fatalf("input modified")
	}

	// Edges stay in range.
	for _, h := range []string{"000000000000", "00ff7fff7f00"} {
		e, _ := hex.DecodeString(h)
		o := pickAbsReport(e)
		x := int(o[1]) | int(o[2])<<8
		y := int(o[3]) | int(o[4])<<8
		if x > 0x7fff || y > 0x7fff {
			t.Fatalf("out of range: %x", o)
		}
	}
}
