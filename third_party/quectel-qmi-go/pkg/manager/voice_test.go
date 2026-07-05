package manager

import (
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func waitManagerEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case evt := <-ch:
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for manager event")
		return Event{}
	}
}

func TestHandleIndicationVoiceCallStatus(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{
				Type: 0x01,
				Value: []byte{
					0x01,
					0x05, 0x02, 0x00, 0x01, 0x03, 0x00, 0x00,
				},
			},
			{
				Type: 0x10,
				Value: []byte{
					0x01,
					0x05, 0x00, 0x04, '1', '0', '0', '0',
				},
			},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventVoiceCallStatus, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventVoiceCallStatus {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.VoiceCalls == nil || len(evt.VoiceCalls.Calls) != 1 || evt.VoiceCalls.Calls[0].ID != 0x05 {
		t.Fatalf("unexpected voice call payload: %+v", evt.VoiceCalls)
	}
}

func TestHandleIndicationVoiceUSSD(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnVoiceUSSD(func(info *qmi.VoiceUSSDIndication) {
		ch <- Event{Type: EventVoiceUSSD, VoiceUSSD: info}
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{0x01}},
			{Type: 0x10, Value: []byte{0x0F, 0x03, '1', '2', '3'}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventUSSD, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.VoiceUSSD == nil || evt.VoiceUSSD.USSData == nil || evt.VoiceUSSD.USSData.Text != "123" {
		t.Fatalf("unexpected voice USSD payload: %+v", evt.VoiceUSSD)
	}
}

func TestHandleIndicationVoiceUSSDReleased(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	m.handleIndication(qmi.Event{Type: qmi.EventVoiceUSSDReleased, Packet: &qmi.Packet{}})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventVoiceUSSDReleased {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
}

func TestHandleIndicationVoiceUSSDNoWaitResult(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x34, 0x12}},
			{Type: 0x12, Value: []byte{0x0F, 0x02, 'O', 'K'}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventVoiceUSSDNoWaitResult, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventVoiceUSSDNoWaitResult {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.VoiceUSSDNoWait == nil || !evt.VoiceUSSDNoWait.HasErrorCode || evt.VoiceUSSDNoWait.ErrorCode != 0x1234 {
		t.Fatalf("unexpected no-wait payload: %+v", evt.VoiceUSSDNoWait)
	}
}

func TestHandleIndicationVoiceSupplementaryService(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{0x04, 0x08}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventVoiceSupplementaryService, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventVoiceSupplementaryService {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.VoiceSupplementary == nil || evt.VoiceSupplementary.CallID != 0x04 || evt.VoiceSupplementary.NotificationType != 0x08 {
		t.Fatalf("unexpected supplementary payload: %+v", evt.VoiceSupplementary)
	}
}

func TestHandleIndicationVoiceSupplementaryServiceRequest(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{
		TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{0x07, 0x01}},
			{Type: 0x14, Value: []byte{0x01, 0x05, '*', '1', '0', '0', '#'}},
			{Type: 0x21, Value: []byte{0x02, 0x4f, 0x00, 0x4b, 0x00}},
		},
	}

	m.handleIndication(qmi.Event{Type: qmi.EventVoiceSupplementaryServiceRequest, Packet: packet})
	evt := waitManagerEvent(t, ch)
	if evt.Type != EventVoiceSupplementaryServiceRequest {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.VoiceSupplementaryRequest == nil || !evt.VoiceSupplementaryRequest.HasInfo || evt.VoiceSupplementaryRequest.Request != 0x07 {
		t.Fatalf("unexpected supplementary request payload: %+v", evt.VoiceSupplementaryRequest)
	}
	if evt.VoiceSupplementaryRequest.USSData == nil || evt.VoiceSupplementaryRequest.USSData.Text != "*100#" {
		t.Fatalf("unexpected supplementary request USS data: %+v", evt.VoiceSupplementaryRequest)
	}
}

func TestVoiceIndicationRegistrationDisabled(t *testing.T) {
	m := &Manager{cfg: Config{DisableVOICEInd: true}}
	cfg, ok := m.voiceIndicationRegistration()
	if ok {
		t.Fatalf("expected VOICE indications to be disabled, got cfg=%+v", cfg)
	}
}

func TestVoiceIndicationRegistrationEnabledDefaults(t *testing.T) {
	m := &Manager{}
	cfg, ok := m.voiceIndicationRegistration()
	if !ok {
		t.Fatal("expected VOICE indications to be enabled")
	}
	if !cfg.CallNotificationEvents || !cfg.SupplementaryServiceNotificationEvents || !cfg.USSDNotificationEvents {
		t.Fatalf("unexpected default VOICE indication config: %+v", cfg)
	}
	if cfg.DTMFEvents || cfg.HandoverEvents || cfg.AOCEvents {
		t.Fatalf("unexpected extra indication flags enabled: %+v", cfg)
	}
}

func TestEventEmitterIsolatesPayloadAcrossHandlers(t *testing.T) {
	e := NewEventEmitterWithQueueSize(8)
	firstDone := make(chan struct{}, 1)
	secondSeen := make(chan int8, 1)

	e.On(func(evt Event) {
		if evt.Signal == nil {
			return
		}
		evt.Signal.RSSI = -10
		close(firstDone)
	})
	e.On(func(evt Event) {
		<-firstDone
		if evt.Signal == nil {
			secondSeen <- 0
			return
		}
		secondSeen <- evt.Signal.RSSI
	})

	e.Emit(Event{Type: EventSignalUpdate, Signal: &qmi.SignalStrength{RSSI: -60}})

	select {
	case got := <-secondSeen:
		if got != -60 {
			t.Fatalf("expected isolated payload RSSI=-60, got %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second handler")
	}
}
