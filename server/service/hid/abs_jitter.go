package hid

// Workaround for a NanoKVM-Pro USB high-speed TX weakness.
//
// Some absolute-mouse reports (6 bytes on ep 0x83) are never received cleanly
// by the host: the device retransmits the same report, the host sees -71
// (EPROTO) for ~1 s and then resets the port, freezing all HID input. Whether a
// report fails depends on its bit pattern on the wire (data + CRC16): long runs
// of 1s and single 0s inside runs of 1s are much more likely to fail.
//
// Shifting the pointer by a few units (32767 full scale, so well below one
// pixel) changes the bit pattern completely. We pick the candidate with the
// lowest risk score. Measured on the device: random reports got stuck 53 times
// per 10000; with this selection the model predicts ~20 per 10000.

const absJitterRadius = 4

// crc16USB is the USB data CRC (poly 0x8005 reflected, init 0xffff, inverted).
func crc16USB(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xffff
}

// absReportRisk scores the on-the-wire bit pattern (LSB first) of report+CRC.
// Weights come from a logistic fit on reports that got stuck vs. random ones.
func absReportRisk(report []byte) float64 {
	crc := crc16USB(report)
	var bits [64]byte
	n := 0
	push := func(b byte) {
		for i := 0; i < 8; i++ {
			bits[n] = (b >> i) & 1
			n++
		}
	}
	for _, b := range report {
		push(b)
	}
	push(byte(crc))
	push(byte(crc >> 8))
	s := bits[:n]

	ones, maxRun, run5, stuff := 0, 0, 0, 0
	run, stuffRun := 0, 0
	for _, b := range s {
		if b == 1 {
			ones++
			run++
			stuffRun++
			if run > maxRun {
				maxRun = run
			}
			if stuffRun == 6 {
				stuff++
				stuffRun = 0
			}
		} else {
			if run >= 5 {
				run5++
			}
			run, stuffRun = 0, 0
		}
	}
	if run >= 5 {
		run5++
	}

	allOnes := func(from, to int) bool {
		if from < 0 || to > len(s) {
			return false
		}
		for _, b := range s[from:to] {
			if b != 1 {
				return false
			}
		}
		return true
	}
	iso4, iso3 := 0, 0
	for i, b := range s {
		if b != 0 {
			continue
		}
		if allOnes(i-4, i) && allOnes(i+1, i+5) {
			iso4++
		}
		if allOnes(i-3, i) && allOnes(i+1, i+4) {
			iso3++
		}
	}

	return 0.102*float64(ones) + 0.318*float64(maxRun) + 0.776*float64(iso4) +
		0.083*float64(iso3) - 0.168*float64(stuff) + 0.071*float64(run5)
}

// pickAbsReport returns a copy of a 6-byte absolute report whose X/Y were
// nudged by at most absJitterRadius units to the lowest-risk bit pattern.
func pickAbsReport(data []byte) []byte {
	if len(data) != 6 {
		return data
	}
	x := int(data[1]) | int(data[2])<<8
	y := int(data[3]) | int(data[4])<<8

	best := append([]byte(nil), data...)
	bestRisk := absReportRisk(best)
	cand := make([]byte, 6)
	copy(cand, data)
	for dx := -absJitterRadius; dx <= absJitterRadius; dx++ {
		xx := x + dx
		if xx < 0 || xx > 0x7fff {
			continue
		}
		for dy := -absJitterRadius; dy <= absJitterRadius; dy++ {
			yy := y + dy
			if yy < 0 || yy > 0x7fff {
				continue
			}
			cand[1], cand[2] = byte(xx), byte(xx>>8)
			cand[3], cand[4] = byte(yy), byte(yy>>8)
			if r := absReportRisk(cand); r < bestRisk {
				bestRisk = r
				copy(best, cand)
			}
		}
	}
	return best
}
