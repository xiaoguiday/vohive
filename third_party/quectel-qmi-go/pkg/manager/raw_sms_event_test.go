package manager

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestHandleIndicationEmitsRawSMSMetadata(t *testing.T) {
	m := newRecoveryTestManager()
	gotCh := make(chan Event, 1)
	m.OnEvent(func(event Event) {
		if event.Type == EventNewSMSRaw {
			gotCh <- event
		}
	})

	rawPDU := []byte{0x04, 0x0b, 0x91, 0x68, 0x31, 0x08}
	value := make([]byte, 8+len(rawPDU))
	value[0] = 0
	binary.LittleEndian.PutUint32(value[1:5], 0x11223344)
	value[5] = 0x06
	binary.LittleEndian.PutUint16(value[6:8], uint16(len(rawPDU)))
	copy(value[8:], rawPDU)

	m.handleIndication(qmi.Event{
		Type:      qmi.EventNewMessage,
		ServiceID: qmi.ServiceWMS,
		MessageID: qmi.WMSEventReportInd,
		Packet: &qmi.Packet{
			ServiceType:  qmi.ServiceWMS,
			MessageID:    qmi.WMSEventReportInd,
			IsIndication: true,
			TLVs:         []qmi.TLV{{Type: 0x11, Value: value}},
		},
	})

	select {
	case got := <-gotCh:
		if !got.SMSAckRequired {
			t.Fatal("SMSAckRequired=false, want true")
		}
		if got.SMSTransactionID != 0x11223344 {
			t.Fatalf("SMSTransactionID=0x%x, want 0x11223344", got.SMSTransactionID)
		}
		if got.SMSFormat != 0x06 {
			t.Fatalf("SMSFormat=0x%x, want 0x06", got.SMSFormat)
		}
		if string(got.Pdu) != string(rawPDU) {
			t.Fatalf("Pdu=%x, want %x", got.Pdu, rawPDU)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for EventNewSMSRaw")
	}
}

func TestHandleIndicationEmitsRawSMSAckNotRequired(t *testing.T) {
	m := newRecoveryTestManager()
	gotCh := make(chan Event, 1)
	m.OnEvent(func(event Event) {
		if event.Type == EventNewSMSRaw {
			gotCh <- event
		}
	})

	rawPDU := []byte{0x00, 0x0e, 0xd0}
	value := make([]byte, 8+len(rawPDU))
	value[0] = 1
	binary.LittleEndian.PutUint32(value[1:5], 0x00000003)
	value[5] = 0x06
	binary.LittleEndian.PutUint16(value[6:8], uint16(len(rawPDU)))
	copy(value[8:], rawPDU)

	m.handleIndication(qmi.Event{
		Type:      qmi.EventNewMessage,
		ServiceID: qmi.ServiceWMS,
		MessageID: qmi.WMSEventReportInd,
		Packet: &qmi.Packet{
			ServiceType:  qmi.ServiceWMS,
			MessageID:    qmi.WMSEventReportInd,
			IsIndication: true,
			TLVs:         []qmi.TLV{{Type: 0x11, Value: value}},
		},
	})

	select {
	case got := <-gotCh:
		if got.SMSAckRequired {
			t.Fatal("SMSAckRequired=true, want false")
		}
		if string(got.Pdu) != string(rawPDU) {
			t.Fatalf("Pdu=%x, want %x", got.Pdu, rawPDU)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for EventNewSMSRaw")
	}
}
