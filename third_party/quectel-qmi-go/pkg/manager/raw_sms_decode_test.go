package manager

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestDecodeIncomingSMSPDUDecodesStoredPDUWithSMSCHeader(t *testing.T) {
	raw, err := hex.DecodeString(fixedSlotPaddedRawPDU)
	if err != nil {
		t.Fatal(err)
	}

	sms, err := DecodeIncomingSMSPDU(raw, 1, 7)
	if err != nil {
		t.Fatalf("DecodeIncomingSMSPDU() error = %v", err)
	}
	if sms.Index != 7 || sms.Storage != 1 {
		t.Fatalf("index/storage=%d/%d, want 7/1", sms.Index, sms.Storage)
	}
	if strings.TrimSpace(sms.Sender) == "" {
		t.Fatal("sender is empty")
	}
	if strings.TrimSpace(sms.Message) == "" {
		t.Fatal("message is empty")
	}
}

func TestDecodeIncomingSMSPDUDecodesDirectTPDU(t *testing.T) {
	raw, err := hex.DecodeString(fixedSlotPaddedRawPDU)
	if err != nil {
		t.Fatal(err)
	}
	smscLen := int(raw[0])
	tpdu := raw[1+smscLen:]

	sms, err := DecodeIncomingSMSPDU(tpdu, 0xff, ^uint32(0))
	if err != nil {
		t.Fatalf("DecodeIncomingSMSPDU() error = %v", err)
	}
	if strings.TrimSpace(sms.Sender) == "" {
		t.Fatal("sender is empty")
	}
	if strings.TrimSpace(sms.Message) == "" {
		t.Fatal("message is empty")
	}
}

func TestDecodeIncomingSMSPDUDecodesRPDataWrappedTPDU(t *testing.T) {
	raw, err := hex.DecodeString(fixedSlotPaddedRawPDU)
	if err != nil {
		t.Fatal(err)
	}
	smscLen := int(raw[0])
	tpdu := raw[1+smscLen:]
	rpdu := append([]byte{0x01, 0x23, 0x00, 0x00, byte(len(tpdu))}, tpdu...)

	sms, err := DecodeIncomingSMSPDU(rpdu, 0xff, ^uint32(0))
	if err != nil {
		t.Fatalf("DecodeIncomingSMSPDU() error = %v", err)
	}
	if strings.TrimSpace(sms.Sender) == "" {
		t.Fatal("sender is empty")
	}
	if strings.TrimSpace(sms.Message) == "" {
		t.Fatal("message is empty")
	}
}

func TestDecodeIncomingSMSPDURejectsMalformedUDLWithoutPatching(t *testing.T) {
	raw, err := hex.DecodeString(fixedSlotPaddedRawPDU)
	if err != nil {
		t.Fatal(err)
	}
	smscLen := int(raw[0])
	tpdu := raw[1+smscLen:]
	trimmed, ok := trimDeliverTPDUToDeclaredLength(tpdu)
	if !ok {
		t.Fatal("fixture TPDU was not trimmed")
	}
	malformed := incrementDeliverUDLForTest(t, trimmed, 1)

	if _, err := DecodeIncomingSMSPDU(malformed, 0xff, ^uint32(0)); err == nil {
		t.Fatal("DecodeIncomingSMSPDU() error = nil, want malformed UDL to fail")
	}
}

func incrementDeliverUDLForTest(t *testing.T, tpdu []byte, delta int) []byte {
	t.Helper()
	out := append([]byte(nil), tpdu...)
	if len(out) < 1 || out[0]&0x03 != 0 {
		t.Fatalf("fixture is not SMS-DELIVER TPDU: first_octet=0x%02x", out[0])
	}
	i := 1
	if i+2 > len(out) {
		t.Fatal("fixture too short before OA")
	}
	oaLen := int(out[i])
	i += 2 + (oaLen+1)/2
	if i+10 > len(out) {
		t.Fatal("fixture too short before UDL")
	}
	udlIndex := i + 2 + 7
	if udlIndex >= len(out) {
		t.Fatal("fixture missing UDL")
	}
	next := int(out[udlIndex]) + delta
	if next < 0 || next > 255 {
		t.Fatalf("invalid UDL mutation: %d", next)
	}
	out[udlIndex] = byte(next)
	return out
}
