package manager

import "github.com/warthog618/sms/encoding/tpdu"

func trimDeliverTPDUToDeclaredLength(tpduBytes []byte) ([]byte, bool) {
	want, alphabet, udl, ok := deliverTPDUDeclaredLength(tpduBytes)
	if !ok || want > len(tpduBytes) {
		return tpduBytes, false
	}

	trimmed := append([]byte(nil), tpduBytes[:want]...)

	if alphabet == tpdu.Alpha7Bit && want > 0 {
		udOctets := (udl*7 + 7) / 8
		paddingBits := (udOctets * 8) - (udl * 7)
		if paddingBits > 0 && paddingBits < 8 {
			mask := byte(1<<(8-paddingBits) - 1)
			trimmed[want-1] &= mask
		}
	}
	return trimmed, true
}

func deliverTPDUDeclaredLength(tpduBytes []byte) (int, tpdu.Alphabet, int, bool) {
	if len(tpduBytes) < 1 || tpduBytes[0]&0x03 != 0 {
		return 0, 0, 0, false
	}

	i := 1
	if i+2 > len(tpduBytes) {
		return 0, 0, 0, false
	}
	oaLen := int(tpduBytes[i])
	i += 2 // OA length + TOA
	oaOctets := (oaLen + 1) / 2
	if i+oaOctets > len(tpduBytes) {
		return 0, 0, 0, false
	}
	i += oaOctets

	if i+10 > len(tpduBytes) {
		return 0, 0, 0, false
	}
	dcs := tpduBytes[i+1]
	i += 2 + 7
	udl := int(tpduBytes[i])
	i++

	alphabet, err := tpdu.DCS(dcs).Alphabet()
	if err != nil {
		return 0, 0, 0, false
	}

	udOctets := udl
	if alphabet == tpdu.Alpha7Bit {
		udOctets = (udl*7 + 7) / 8
	}
	want := i + udOctets
	if want > len(tpduBytes) {
		return 0, alphabet, udl, false
	}
	return want, alphabet, udl, true
}
