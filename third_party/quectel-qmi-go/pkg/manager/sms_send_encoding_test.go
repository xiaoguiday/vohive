package manager

import (
	"testing"

	"github.com/warthog618/sms/encoding/tpdu"
	"github.com/warthog618/sms/encoding/ucs2"
)

func TestEncodeSMSWithOptionsForcesUCS2(t *testing.T) {
	m := &Manager{}

	pduWithSMSC, err := m.encodeSMSWithOptions("10086", "hello", SendSMSOptions{Encoding: SMSEncodingUCS2})
	if err != nil {
		t.Fatalf("encodeSMSWithOptions() error = %v", err)
	}
	if len(pduWithSMSC) < 2 || pduWithSMSC[0] != 0x00 {
		t.Fatalf("unexpected PDU with SMSC header: %x", pduWithSMSC)
	}

	pdu := &tpdu.TPDU{Direction: tpdu.MO}
	if err := pdu.UnmarshalBinary(pduWithSMSC[1:]); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}
	if pdu.DCS != tpdu.DcsUCS2Data {
		t.Fatalf("DCS=0x%02x want 0x%02x", byte(pdu.DCS), byte(tpdu.DcsUCS2Data))
	}
	if got, want := []byte(pdu.UD), ucs2.Encode([]rune("hello")); string(got) != string(want) {
		t.Fatalf("UD=%x want UCS2 %x", got, want)
	}
}

func TestEncodeSMSKeepsAutoEncodingByDefault(t *testing.T) {
	m := &Manager{}

	pduWithSMSC, err := m.encodeSMS("10086", "hello")
	if err != nil {
		t.Fatalf("encodeSMS() error = %v", err)
	}
	if len(pduWithSMSC) < 2 || pduWithSMSC[0] != 0x00 {
		t.Fatalf("unexpected PDU with SMSC header: %x", pduWithSMSC)
	}

	pdu := &tpdu.TPDU{Direction: tpdu.MO}
	if err := pdu.UnmarshalBinary(pduWithSMSC[1:]); err != nil {
		t.Fatalf("UnmarshalBinary() error = %v", err)
	}
	if pdu.DCS != 0x00 {
		t.Fatalf("DCS=0x%02x want auto GSM7 0x00", byte(pdu.DCS))
	}
}
