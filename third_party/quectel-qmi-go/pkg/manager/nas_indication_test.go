package manager

import (
	"context"
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestHandleIndicationNASSysInfoUsesIndicationID(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}

	sysInfoTLV := make([]byte, 29)
	sysInfoTLV[12] = 0x78
	sysInfoTLV[13] = 0x56
	sysInfoTLV[14] = 0x34
	sysInfoTLV[15] = 0x12
	sysInfoTLV[27] = 0x64
	sysInfoTLV[28] = 0x00

	m.handleIndication(qmi.Event{
		Type:      qmi.EventServingSystemChanged,
		MessageID: qmi.NASSysInfoInd,
		Packet: &qmi.Packet{
			TLVs: []qmi.TLV{{Type: 0x19, Value: sysInfoTLV}},
		},
	})

	si, _ := m.snapshot.SysInfo()
	if si == nil || si.CellID != 0x12345678 || si.TAC != 100 {
		t.Fatalf("unexpected snapshot sysinfo: %+v", si)
	}
}

func TestHandleIndicationNASOperatorNameChanged(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	ch := make(chan Event, 1)
	snapSeen := make(chan string, 1)
	m.OnEvent(func(evt Event) {
		if evt.Type == EventNASOperatorNameChanged {
			if info, _, valid := m.snapshot.NASOperatorName(); valid && info != nil {
				snapSeen <- info.OperatorStringName
			}
		}
		ch <- evt
	})

	m.handleIndication(qmi.Event{
		Type: qmi.EventNASOperatorNameChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x00, 'C', 'M', 'C', 'C'}},
			{Type: 0x13, Value: []byte("China Mobile")},
		}},
	})

	evt := waitManagerEvent(t, ch)
	if evt.Type != EventNASOperatorNameChanged {
		t.Fatalf("unexpected event type: %v", evt.Type)
	}
	if evt.NASOperatorName == nil || evt.NASOperatorName.OperatorStringName != "China Mobile" {
		t.Fatalf("unexpected nas operator payload: %+v", evt.NASOperatorName)
	}
	select {
	case name := <-snapSeen:
		if name != "China Mobile" {
			t.Fatalf("snapshot operator name mismatch: %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting snapshot check in callback")
	}
	if info, ts, valid := m.snapshot.NASOperatorName(); !valid || info == nil || info.OperatorStringName != "China Mobile" || ts.IsZero() {
		t.Fatalf("unexpected nas operator snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
}

func TestHandleIndicationSuppressesEmptyNASEventReport(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitterWithQueueSize(1),
		eventCh: make(chan internalEvent, 1),
	}

	m.handleIndication(qmi.Event{
		Type:      qmi.EventNASEventReport,
		ServiceID: qmi.ServiceNAS,
		MessageID: qmi.NASEventReportInd,
		Packet: &qmi.Packet{
			ServiceType: qmi.ServiceNAS,
			MessageID:   qmi.NASEventReportInd,
			TLVs:        nil,
		},
	})

	select {
	case evt := <-m.eventCh:
		t.Fatalf("unexpected internal event for empty NAS EventReport: %v", evt)
	default:
	}
}

func TestHandleIndicationEmitsNonEmptyNASEventReport(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitterWithQueueSize(2),
		eventCh: make(chan internalEvent, 1),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	m.handleIndication(qmi.Event{
		Type:      qmi.EventNASEventReport,
		ServiceID: qmi.ServiceNAS,
		MessageID: qmi.NASEventReportInd,
		Packet: &qmi.Packet{
			ServiceType: qmi.ServiceNAS,
			MessageID:   qmi.NASEventReportInd,
			TLVs: []qmi.TLV{
				{Type: 0x10, Value: []byte{0x01}},
			},
		},
	})

	got := waitManagerEvent(t, ch)
	if got.Type != EventNASEventReport {
		t.Fatalf("event type=%v want EventNASEventReport", got.Type)
	}
	if len(got.TLVMeta) != 1 || got.TLVMeta[0].Type != 0x10 {
		t.Fatalf("unexpected TLV meta: %+v", got.TLVMeta)
	}
}

func TestHandleIndicationNASEventReportDoesNotScheduleTargetedCheck(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitterWithQueueSize(2),
		eventCh: make(chan internalEvent, 1),
	}

	m.handleIndication(qmi.Event{
		Type:      qmi.EventNASEventReport,
		ServiceID: qmi.ServiceNAS,
		MessageID: qmi.NASEventReportInd,
		Packet: &qmi.Packet{
			ServiceType: qmi.ServiceNAS,
			MessageID:   qmi.NASEventReportInd,
			TLVs: []qmi.TLV{
				{Type: 0x10, Value: []byte{0x01}},
			},
		},
	})

	select {
	case evt := <-m.eventCh:
		t.Fatalf("NAS EventReport scheduled internal event %v, want none", evt)
	default:
	}
}

func TestHandleIndicationNASNetworkTimeChanged(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	m.handleIndication(qmi.Event{
		Type: qmi.EventNASNetworkTimeChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{0xE9, 0x07, 0x04, 0x0F, 0x12, 0x1E, 0x2D, 0x02, 0x08, 0x00, 0x08}},
		}},
	})

	info, ts, valid := m.snapshot.NASNetworkTime()
	if !valid || info == nil || !info.HasThreeGPP || info.ThreeGPP.Year != 2025 || ts.IsZero() {
		t.Fatalf("unexpected nas network time snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
}

func TestHandleIndicationNASNetworkTimeSplitTLVs(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	m.handleIndication(qmi.Event{
		Type: qmi.EventNASNetworkTimeChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{0xEA, 0x07, 0x05, 0x15, 0x14, 0x0D, 0x22, 0x04}},
			{Type: 0x10, Value: []byte{0x20}},
			{Type: 0x11, Value: []byte{0x01}},
			{Type: 0x12, Value: []byte{0x08}},
		}},
	})

	info, ts, valid := m.snapshot.NASNetworkTime()
	if !valid || info == nil || !info.HasThreeGPP || info.ThreeGPP.Year != 2026 || info.ThreeGPP.TimezoneOffsetQuarters != 32 || ts.IsZero() {
		t.Fatalf("unexpected nas network time snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
}

func TestHandleIndicationNASSignalInfoChanged(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	rsrq := int8(-9)
	m.handleIndication(qmi.Event{
		Type: qmi.EventNASSignalInfoChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x14, Value: []byte{0x00, uint8(rsrq), 0x38, 0xFF, 0x32, 0x00}},
			{Type: 0x17, Value: []byte{0xE7, 0xFF, 0x9C, 0xFF, 0x1E, 0x00}},
		}},
	})

	info, ts, valid := m.snapshot.NASSignalInfo()
	if !valid || info == nil || info.LTERSRQ != -9 || info.LTERSRP != -200 || info.NR5GSINR != 30 || ts.IsZero() {
		t.Fatalf("unexpected nas signal snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
}

func TestHandleIndicationNASNetworkReject(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	m.handleIndication(qmi.Event{
		Type: qmi.EventNASNetworkReject,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{0x08, 0x15, 0x00, 0x00, 0x00}},
			{Type: 0x11, Value: []byte("46000")},
		}},
	})

	info, ts, valid := m.snapshot.NASNetworkReject()
	if !valid || info == nil || info.RadioInterface != 0x08 || info.RejectCause != 0x15 || info.PLMN != "46000" || ts.IsZero() {
		t.Fatalf("unexpected nas reject snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
}

func TestHandleIndicationNASIncrementalScanMergesResults(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}

	m.handleIndication(qmi.Event{
		Type: qmi.EventNASIncrementalNetworkScan,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{
				0x01, 0x00, // 1 item
				0xCC, 0x01, // MCC=460
				0x00, 0x00, // MNC=0
				0x02, // Available
				0x03, 'C', 'M', 'C',
			}},
			{Type: 0x12, Value: []byte{0x00}},
		}},
	})
	first, firstTS, firstValid := m.snapshot.NASIncrementalScan()
	if !firstValid || first == nil || first.ScanComplete || len(first.Results) != 1 || firstTS.IsZero() {
		t.Fatalf("unexpected first scan snapshot: valid=%v ts=%v info=%+v", firstValid, firstTS, first)
	}

	time.Sleep(5 * time.Millisecond)
	m.handleIndication(qmi.Event{
		Type: qmi.EventNASIncrementalNetworkScan,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x10, Value: []byte{
				0x01, 0x00, // 1 item
				0xCC, 0x01, // MCC=460
				0x01, 0x00, // MNC=1
				0x01, // Current
				0x03, 'C', 'U', 'M',
			}},
			{Type: 0x12, Value: []byte{0x01}},
		}},
	})

	info, ts, valid := m.snapshot.NASIncrementalScan()
	if !valid || info == nil || !info.ScanComplete || len(info.Results) != 2 || !ts.After(firstTS) {
		t.Fatalf("unexpected merged scan snapshot: valid=%v ts=%v firstTS=%v info=%+v", valid, ts, firstTS, info)
	}
}

func TestHandleIndicationUIMRefreshUpdatesSnapshot(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{TLVs: []qmi.TLV{{
		Type: 0x10,
		Value: []byte{
			0x01, 0x02, qmi.UIMSessionTypePrimaryGWProvisioning,
			0x02, 0xA0, 0x00,
			0x01, 0x00,
			0x07, 0x6F, 0x02, 0x00, 0x3F,
		},
	}}}
	m.handleIndication(qmi.Event{Type: qmi.EventUIMRefresh, Packet: packet})

	evt := waitManagerEvent(t, ch)
	if evt.Type != EventUIMRefresh || evt.UIMRefresh == nil {
		t.Fatalf("unexpected event payload: %+v", evt)
	}

	info, ts, valid := m.snapshot.UIMRefresh()
	if !valid || info == nil || ts.IsZero() {
		t.Fatalf("unexpected UIM refresh snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
	if info.Stage != 0x01 || info.Mode != 0x02 || info.SessionType != qmi.UIMSessionTypePrimaryGWProvisioning {
		t.Fatalf("unexpected UIM refresh snapshot body: %+v", info)
	}

	info.ApplicationIdentifier[0] = 0xFF
	if evt.UIMRefresh.ApplicationIdentifier[0] == 0xFF {
		t.Fatal("snapshot/event should not share mutable UIM refresh payload")
	}
}

func TestHandleIndicationUIMSlotStatusUpdatesSnapshot(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	ch := make(chan Event, 1)
	m.OnEvent(func(evt Event) {
		ch <- evt
	})

	packet := &qmi.Packet{TLVs: []qmi.TLV{{Type: 0x10, Value: []byte{0x00}}}}
	m.handleIndication(qmi.Event{Type: qmi.EventUIMSlotStatus, Packet: packet})

	evt := waitManagerEvent(t, ch)
	if evt.Type != EventUIMSlotStatus || evt.UIMSlotStatus == nil {
		t.Fatalf("unexpected event payload: %+v", evt)
	}

	info, ts, valid := m.snapshot.UIMSlotStatus()
	if !valid || info == nil || ts.IsZero() {
		t.Fatalf("unexpected UIM slot status snapshot: valid=%v ts=%v info=%+v", valid, ts, info)
	}
	if len(info.Slots) != 0 {
		t.Fatalf("unexpected UIM slot status slots: %+v", info.Slots)
	}
}

func TestDeviceSnapshotResetClearsUIMFields(t *testing.T) {
	s := &DeviceSnapshot{}
	s.updateUIMRefresh(&qmi.UIMRefreshIndication{Stage: 1, Mode: 2, SessionType: qmi.UIMSessionTypePrimaryGWProvisioning})
	s.updateUIMSlotStatus(&qmi.UIMSlotStatus{Slots: []qmi.UIMSlotStatusSlot{{LogicalSlot: 1}}})

	s.Reset()

	refresh, refreshTS, refreshValid := s.UIMRefresh()
	if refreshValid || refresh != nil || !refreshTS.IsZero() {
		t.Fatalf("UIMRefresh not cleared: valid=%v ts=%v info=%+v", refreshValid, refreshTS, refresh)
	}
	slot, slotTS, slotValid := s.UIMSlotStatus()
	if slotValid || slot != nil || !slotTS.IsZero() {
		t.Fatalf("UIMSlotStatus not cleared: valid=%v ts=%v info=%+v", slotValid, slotTS, slot)
	}
}

func TestHandleIndicationServingSystemPartialPLMNDoesNotOverrideRegistration(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	m.snapshot.updateServingFromQuery(&qmi.ServingSystem{
		RegistrationState: qmi.RegStateRegistered,
		PSAttached:        true,
		RadioInterface:    0x08,
		MCC:               460,
		MNC:               0,
	})

	m.handleIndication(qmi.Event{
		Type: qmi.EventServingSystemChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			// 仅 PLMN TLV，缺失 0x01（serving system）
			{Type: 0x12, Value: []byte{0xD2, 0x00, 0x21, 0x00}}, // MCC=210 MNC=33
		}},
	})

	ss, ts := m.snapshot.ServingSystem()
	if ss == nil || ts.IsZero() {
		t.Fatalf("snapshot serving system not updated: ss=%+v ts=%v", ss, ts)
	}
	if ss.RegistrationState != qmi.RegStateRegistered || !ss.PSAttached || ss.RadioInterface != 0x08 {
		t.Fatalf("registration fields should be preserved, got %+v", ss)
	}
	if ss.MCC != 210 || ss.MNC != 33 {
		t.Fatalf("PLMN should be updated, got mcc=%d mnc=%d", ss.MCC, ss.MNC)
	}
}

func TestHandleIndicationServingSystemOnlyServingKeepsPLMN(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}
	m.snapshot.updateServingFromQuery(&qmi.ServingSystem{
		RegistrationState: qmi.RegStateRegistered,
		PSAttached:        true,
		RadioInterface:    0x08,
		MCC:               460,
		MNC:               1,
	})

	m.handleIndication(qmi.Event{
		Type: qmi.EventServingSystemChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{byte(qmi.RegStateRoaming), 0x00, 0x01, 0x00, 0x01, 0x0C}},
		}},
	})

	ss, _ := m.snapshot.ServingSystem()
	if ss == nil {
		t.Fatal("serving snapshot is nil")
	}
	if ss.RegistrationState != qmi.RegStateRoaming || !ss.PSAttached || ss.RadioInterface != 0x0C {
		t.Fatalf("registration fields not updated as expected: %+v", ss)
	}
	if ss.MCC != 460 || ss.MNC != 1 {
		t.Fatalf("PLMN should be preserved, got mcc=%d mnc=%d", ss.MCC, ss.MNC)
	}
}

func TestHandleIndicationServingSystemOnlyServingOnEmptySnapshot(t *testing.T) {
	m := &Manager{
		log:     NewNopLogger(),
		events:  NewEventEmitter(),
		eventCh: make(chan internalEvent, 1),
	}

	m.handleIndication(qmi.Event{
		Type: qmi.EventServingSystemChanged,
		Packet: &qmi.Packet{TLVs: []qmi.TLV{
			{Type: 0x01, Value: []byte{byte(qmi.RegStateRegistered), 0x00, 0x01, 0x00, 0x01, 0x08}},
		}},
	})

	ss, _ := m.snapshot.ServingSystem()
	if ss == nil {
		t.Fatal("serving snapshot is nil")
	}
	if ss.RegistrationState != qmi.RegStateRegistered || !ss.PSAttached || ss.RadioInterface != 0x08 {
		t.Fatalf("unexpected registration fields: %+v", ss)
	}
	if ss.MCC != 0 || ss.MNC != 0 {
		t.Fatalf("PLMN should stay empty on only-serving update, got mcc=%d mnc=%d", ss.MCC, ss.MNC)
	}
}

func TestDoStatusCheckFullUpdatesServingSnapshot(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
		client: &qmi.Client{},
		state:  StateConnected,
		cfg: Config{
			Timeouts: TimeoutConfig{StatusCheck: 50 * time.Millisecond},
		},
	}
	m.querySignalStrength = func(ctx context.Context) (*qmi.SignalStrength, error) {
		return &qmi.SignalStrength{RSSI: -60, RSRP: -95, RSRQ: -10}, nil
	}
	m.queryServingSystem = func(ctx context.Context) (*qmi.ServingSystem, error) {
		return &qmi.ServingSystem{
			RegistrationState: qmi.RegStateRoaming,
			PSAttached:        true,
			RadioInterface:    0x08,
			MCC:               460,
			MNC:               1,
		}, nil
	}
	m.queryPacketServiceState = func(ctx context.Context) (qmi.ConnectionStatus, error) {
		return qmi.StatusConnected, nil
	}

	m.doStatusCheck(true)

	ss, ts := m.snapshot.ServingSystem()
	if ss == nil || ts.IsZero() {
		t.Fatalf("serving snapshot not updated: ss=%+v ts=%v", ss, ts)
	}
	if ss.RegistrationState != qmi.RegStateRoaming || !ss.PSAttached || ss.MCC != 460 || ss.MNC != 1 {
		t.Fatalf("unexpected serving snapshot after status check: %+v", ss)
	}
}

func TestSnapshotServingSystemGetterReturnsCopy(t *testing.T) {
	s := &DeviceSnapshot{}
	s.updateServingFromQuery(&qmi.ServingSystem{RegistrationState: qmi.RegStateRegistered, PSAttached: true, RadioInterface: 0x08, MCC: 460, MNC: 1})

	first, _ := s.ServingSystem()
	if first == nil {
		t.Fatal("expected serving snapshot")
	}
	first.MCC = 999
	first.MNC = 99

	second, _ := s.ServingSystem()
	if second == nil {
		t.Fatal("expected serving snapshot on second read")
	}
	if second.MCC != 460 || second.MNC != 1 {
		t.Fatalf("snapshot should be immutable via getter copy, got mcc=%d mnc=%d", second.MCC, second.MNC)
	}
}

func TestSnapshotNASIncrementalScanGetterReturnsDeepCopy(t *testing.T) {
	s := &DeviceSnapshot{}
	s.updateNASIncrementalScan(&qmi.NASIncrementalNetworkScanInfo{
		ScanComplete: true,
		Results:      []qmi.NetworkScanResult{{MCC: "460", MNC: "00", Description: "CMCC", RATs: []uint8{1, 2}}},
	})

	first, _, valid := s.NASIncrementalScan()
	if !valid || first == nil || len(first.Results) != 1 || len(first.Results[0].RATs) != 2 {
		t.Fatalf("unexpected initial scan snapshot: valid=%v info=%+v", valid, first)
	}
	first.Results[0].RATs[0] = 9

	second, _, valid := s.NASIncrementalScan()
	if !valid || second == nil || len(second.Results) != 1 || len(second.Results[0].RATs) != 2 {
		t.Fatalf("unexpected second scan snapshot: valid=%v info=%+v", valid, second)
	}
	if second.Results[0].RATs[0] != 1 {
		t.Fatalf("snapshot should be deep-copied, got rats=%v", second.Results[0].RATs)
	}
}

func TestSnapshotSignalWriteCopiesInput(t *testing.T) {
	s := &DeviceSnapshot{}
	sig := &qmi.SignalStrength{RSSI: -60, RSRP: -95}
	s.updateSignal(sig)
	sig.RSSI = -10

	cached, _ := s.Signal()
	if cached == nil {
		t.Fatal("expected cached signal")
	}
	if cached.RSSI != -60 {
		t.Fatalf("expected copied signal RSSI=-60, got %d", cached.RSSI)
	}
}
