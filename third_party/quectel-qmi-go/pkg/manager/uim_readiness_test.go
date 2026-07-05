package manager

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestBuildUIMReadinessReadyWithActiveSlotAndIdentity(t *testing.T) {
	slot := &qmi.UIMSlotStatus{Slots: []qmi.UIMSlotStatusSlot{{
		PhysicalCardStatus: qmi.UIMPhysicalCardStatePresent,
		PhysicalSlotStatus: qmi.UIMSlotStateActive,
		LogicalSlot:        1,
		ICCID:              "8985203103011907194",
	}}}
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadiness(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, slot, ids, nil)

	if !got.TransportReady || !got.ControlReady || !got.UIMReady || !got.CardPresent {
		t.Fatalf("readiness flags = %+v, want all ready", got)
	}
	if got.Reason != UIMReadinessReady {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessReady)
	}
	if !got.SlotKnown || got.ActiveSlot != 1 {
		t.Fatalf("slot known=%v slot=%d, want slot 1", got.SlotKnown, got.ActiveSlot)
	}
}

func TestBuildUIMReadinessBlockedIsNotTransportFatal(t *testing.T) {
	got := buildUIMReadiness(qmi.SIMBlocked, &qmi.CardStatusDetails{CardState: 0x02}, nil, DeviceIdentities{}, nil)

	if got.Reason != UIMReadinessSIMBlocked {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessSIMBlocked)
	}
	if !got.TransportReady || !got.ControlReady {
		t.Fatalf("blocked SIM should keep transport/control ready: %+v", got)
	}
}

func TestBuildUIMReadinessPINRequiredIsActionableNotResetting(t *testing.T) {
	got := buildUIMReadiness(qmi.SIMPINRequired, &qmi.CardStatusDetails{CardState: 0x01}, nil, DeviceIdentities{}, nil)

	if got.Reason != UIMReadinessSIMBlocked {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessSIMBlocked)
	}
	if !got.TransportReady || !got.ControlReady {
		t.Fatalf("pin required should keep transport/control ready: %+v", got)
	}
}

func TestBuildUIMReadinessTransportFatalFromDeviceOpenError(t *testing.T) {
	err := errors.New("failed to open qmi device /dev/cdc-wdm1: no such device")
	got := buildUIMReadiness(qmi.SIMNotReady, nil, nil, DeviceIdentities{}, err)

	if got.Reason != UIMReadinessTransportFatal {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessTransportFatal)
	}
	if got.TransportReady {
		t.Fatalf("TransportReady=true for fatal transport error: %+v", got)
	}
}

func TestBuildUIMReadinessControlUnavailableForTimeout(t *testing.T) {
	err := errors.New("UIM GetCardStatus: context deadline exceeded")
	got := buildUIMReadiness(qmi.SIMNotReady, nil, nil, DeviceIdentities{}, err)

	if got.Reason != UIMReadinessControlUnavailable {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessControlUnavailable)
	}
	if !got.TransportReady || got.ControlReady {
		t.Fatalf("timeout should mean transport ready but control unavailable: %+v", got)
	}
}

func TestBuildUIMReadinessIgnoresNonFatalSlotStatusErrorWhenIdentityReady(t *testing.T) {
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadinessWithSlotError(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, nil, ids, nil, errors.New("QMI error: service=0x0b msg=0x0047 result=0x0001 error=0x0034"))

	if got.Reason != UIMReadinessReady {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessReady)
	}
	if got.SlotKnown {
		t.Fatalf("SlotKnown=true, want false when slot status failed nonfatally: %+v", got)
	}
}

func TestBuildUIMReadinessPromotesFatalSlotStatusError(t *testing.T) {
	ids := DeviceIdentities{ICCID: "8985203103011907194", IMSI: "460011234567890"}

	got := buildUIMReadinessWithSlotError(qmi.SIMReady, &qmi.CardStatusDetails{CardState: 0x01}, nil, ids, nil, errors.New("failed to open qmi device /dev/cdc-wdm1: no such device"))

	if got.Reason != UIMReadinessTransportFatal {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessTransportFatal)
	}
	if got.TransportReady || got.ControlReady {
		t.Fatalf("fatal slot error should mark transport/control not ready: %+v", got)
	}
}

func TestResolveActiveUIMSlotPrefersActivePresentSlot(t *testing.T) {
	info := &qmi.UIMSlotStatus{Slots: []qmi.UIMSlotStatusSlot{
		{PhysicalCardStatus: qmi.UIMPhysicalCardStateAbsent, PhysicalSlotStatus: qmi.UIMSlotStateInactive, LogicalSlot: 1},
		{PhysicalCardStatus: qmi.UIMPhysicalCardStatePresent, PhysicalSlotStatus: qmi.UIMSlotStateActive, LogicalSlot: 2, ICCID: "8985"},
	}}

	slot, ok, source := resolveActiveUIMSlot(info)

	if !ok || slot != 2 || source != "uim_slot_status" {
		t.Fatalf("slot=%d ok=%v source=%q, want slot 2 from uim_slot_status", slot, ok, source)
	}
}

func TestGetUIMReadinessUsesUIMRecoveryWrapperForCardStatus(t *testing.T) {
	wantErr := errors.New("lazy allocation failed")
	var calls int
	m := &Manager{}
	m.ensureUIMServiceHook = func() (*qmi.UIMService, error) {
		calls++
		return nil, wantErr
	}

	got, err := m.GetUIMReadiness(context.Background())

	if !errors.Is(err, wantErr) {
		t.Fatalf("err=%v want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("ensure UIM calls=%d, want 1", calls)
	}
	if got.Reason != UIMReadinessControlUnavailable {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessControlUnavailable)
	}
}

func TestGetUIMReadinessReturnsNilErrorForNonFatalSlotStatusFailure(t *testing.T) {
	slotErr := errors.New("QMI error: service=0x0b msg=0x0047 result=0x0001 error=0x0034")
	var calls int
	m := &Manager{}
	m.ensureUIMServiceHook = func() (*qmi.UIMService, error) {
		calls++
		switch calls {
		case 1:
			return newUIMReadinessTestService(t, func(req *qmi.Packet) (*qmi.Packet, error) {
				if req.MessageID != qmi.UIMGetCardStatus {
					return nil, errors.New("unexpected UIM card status request")
				}
				return uimReadinessCardStatusPacket(0x01), nil
			}), nil
		case 2:
			return newUIMReadinessTestService(t, func(req *qmi.Packet) (*qmi.Packet, error) {
				if req.MessageID != qmi.UIMGetSlotStatus {
					return nil, errors.New("unexpected UIM slot status request")
				}
				return nil, slotErr
			}), nil
		default:
			return nil, errors.New("unexpected UIM ensure call")
		}
	}
	m.getICCIDStrictHook = func(ctx context.Context) (string, error) { return "8985203103011907194", nil }
	m.getIMSIStrictHook = func(ctx context.Context) (string, error) { return "460011234567890", nil }

	got, err := m.GetUIMReadiness(context.Background())

	if err != nil {
		t.Fatalf("GetUIMReadiness() err=%v, want nil for nonfatal slot status failure", err)
	}
	if got.Reason != UIMReadinessReady {
		t.Fatalf("reason=%q want %q", got.Reason, UIMReadinessReady)
	}
	if got.Err == nil || !strings.Contains(got.Err.Error(), slotErr.Error()) {
		t.Fatalf("diagnostic err=%v, want slot status error", got.Err)
	}
}

func newUIMReadinessTestService(t *testing.T, handler func(*qmi.Packet) (*qmi.Packet, error)) *qmi.UIMService {
	t.Helper()
	client := newUIMReadinessTestClient(t)
	serveUIMReadinessTestRequests(t, client, handler)

	uim := &qmi.UIMService{}
	setUnexportedField(t, reflect.ValueOf(uim).Elem().FieldByName("client"), reflect.ValueOf(client))
	setUnexportedField(t, reflect.ValueOf(uim).Elem().FieldByName("clientID"), reflect.ValueOf(uint8(1)))
	return uim
}

func newUIMReadinessTestClient(t *testing.T) *qmi.Client {
	t.Helper()
	client := &qmi.Client{}
	v := reflect.ValueOf(client).Elem()
	for _, field := range []struct {
		name string
		size int
	}{
		{name: "eventCh", size: 1},
		{name: "indicationInCh", size: 1},
		{name: "writeCh", size: 16},
	} {
		f := v.FieldByName(field.name)
		setUnexportedField(t, f, reflect.MakeChan(f.Type(), field.size))
	}
	closeCh := v.FieldByName("closeCh")
	setUnexportedField(t, closeCh, reflect.MakeChan(closeCh.Type(), 0))
	transactions := v.FieldByName("transactions")
	setUnexportedField(t, transactions, reflect.MakeMap(transactions.Type()))
	recentTransactions := v.FieldByName("recentTransactions")
	setUnexportedField(t, recentTransactions, reflect.MakeMap(recentTransactions.Type()))
	setUnexportedField(t, v.FieldByName("opts"), reflect.ValueOf(qmi.DefaultClientOptions()))
	return client
}

func serveUIMReadinessTestRequests(t *testing.T, client *qmi.Client, handler func(*qmi.Packet) (*qmi.Packet, error)) {
	t.Helper()
	done := make(chan struct{})
	finished := make(chan struct{})
	writeCh := getUnexportedField(t, reflect.ValueOf(client).Elem().FieldByName("writeCh"))

	go func() {
		defer close(finished)
		for {
			chosen, recv, ok := reflect.Select([]reflect.SelectCase{
				{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(done)},
				{Dir: reflect.SelectRecv, Chan: writeCh},
			})
			if chosen == 0 || !ok {
				return
			}

			wr := reflect.New(recv.Type()).Elem()
			wr.Set(recv)
			data := append([]byte(nil), getUnexportedField(t, wr.FieldByName("data")).Bytes()...)
			result := getUnexportedField(t, wr.FieldByName("result"))
			req, err := qmi.UnmarshalPacket(data)
			if err != nil {
				result.Send(reflect.ValueOf(err))
				continue
			}

			resp, handlerErr := handler(req)
			if handlerErr != nil {
				result.Send(reflect.ValueOf(handlerErr))
				continue
			}
			result.Send(reflect.Zero(result.Type().Elem()))
			if resp == nil {
				continue
			}

			key := uint32(req.ServiceType)<<16 | uint32(req.TransactionID)
			transactions := getUnexportedField(t, reflect.ValueOf(client).Elem().FieldByName("transactions"))
			entry := transactions.MapIndex(reflect.ValueOf(key))
			if !entry.IsValid() || entry.IsNil() {
				t.Errorf("response channel not found for key=0x%08x", key)
				continue
			}
			resp.ServiceType = req.ServiceType
			resp.ClientID = req.ClientID
			resp.TransactionID = req.TransactionID
			resp.MessageID = req.MessageID
			ch := getUnexportedField(t, entry.Elem().FieldByName("ch"))
			ch.Send(reflect.ValueOf(resp))
		}
	}()

	t.Cleanup(func() {
		close(done)
		<-finished
	})
}

func uimReadinessCardStatusPacket(cardState uint8) *qmi.Packet {
	value := make([]byte, 15)
	value[8] = 1
	value[9] = cardState
	value[10] = byte(qmi.PINStatusDisabled)
	return &qmi.Packet{TLVs: []qmi.TLV{
		{Type: 0x02, Value: []byte{0x00, 0x00, 0x00, 0x00}},
		{Type: 0x10, Value: value},
	}}
}

func setUnexportedField(t *testing.T, field reflect.Value, value reflect.Value) {
	t.Helper()
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(value)
}

func getUnexportedField(t *testing.T, field reflect.Value) reflect.Value {
	t.Helper()
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
}

func TestBuildUIMReadinessDetectedNeedsProvisioning(t *testing.T) {
	details := &qmi.CardStatusDetails{CardState: 0x01, AppState: qmi.UIMAppStateDetected}
	// 裸系统典型：PIN 已校验 → status 可能算成 SIMReady，但应用仍 detected。
	r := buildUIMReadiness(qmi.SIMReady, details, nil, DeviceIdentities{ICCID: "89860"}, nil)
	if !r.NeedsProvisioning {
		t.Fatalf("expected NeedsProvisioning=true, got readiness=%+v", r)
	}
	if r.Reason != UIMReadinessNeedsProvisioning {
		t.Fatalf("expected reason=%q, got %q", UIMReadinessNeedsProvisioning, r.Reason)
	}
	if r.UIMReady {
		t.Fatalf("detected app must not be reported UIMReady")
	}
	if r.AppState != qmi.UIMAppStateDetected {
		t.Fatalf("expected AppState=%d, got %d", qmi.UIMAppStateDetected, r.AppState)
	}
}

func TestBuildUIMReadinessReadyAppIsProvisioned(t *testing.T) {
	details := &qmi.CardStatusDetails{CardState: 0x01, AppState: qmi.UIMAppStateReady}
	r := buildUIMReadiness(qmi.SIMReady, details, nil, DeviceIdentities{ICCID: "89860"}, nil)
	if r.NeedsProvisioning {
		t.Fatalf("ready app must not need provisioning: %+v", r)
	}
	if !r.ProvisioningActive {
		t.Fatalf("ready app must report ProvisioningActive=true")
	}
	if !r.UIMReady || r.Reason != UIMReadinessReady {
		t.Fatalf("ready app must be UIMReady/Ready, got %+v", r)
	}
}
