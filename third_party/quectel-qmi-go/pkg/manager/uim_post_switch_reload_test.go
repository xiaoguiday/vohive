package manager

import (
	"context"
	"testing"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestResolveUIMReloadSlotUsesActiveSlot(t *testing.T) {
	slot := resolveUIMReloadSlot(UIMReadiness{
		ActiveSlot: 2,
		SlotKnown:  true,
	}, 1)
	if slot != 2 {
		t.Fatalf("slot=%d want 2", slot)
	}
}

func TestResolveUIMReloadSlotFallsBackToDefault(t *testing.T) {
	slot := resolveUIMReloadSlot(UIMReadiness{
		ActiveSlot: 0,
		SlotKnown:  false,
	}, 1)
	if slot != 1 {
		t.Fatalf("slot=%d want 1", slot)
	}
}

func TestResolveUIMReloadSlotUsesOneWhenDefaultZero(t *testing.T) {
	slot := resolveUIMReloadSlot(UIMReadiness{}, 0)
	if slot != 1 {
		t.Fatalf("slot=%d want 1", slot)
	}
}

func TestUIMRebindPrimaryGWProvisioningRejectsEmptyAID(t *testing.T) {
	m := &Manager{}
	err := m.UIMRebindPrimaryGWProvisioning(context.Background(), 1, nil)
	if err == nil {
		t.Fatal("UIMRebindPrimaryGWProvisioning() error=nil want invalid AID error")
	}
}

func TestUIMRebindPrimaryGWProvisioningBuildsDeactivateThenActivate(t *testing.T) {
	var got []qmi.UIMChangeProvisioningSessionRequest
	err := uimRebindPrimaryGWProvisioningWithSender(context.Background(), 2, []byte{0xA0, 0x00}, 0, func(ctx context.Context, req qmi.UIMChangeProvisioningSessionRequest) error {
		got = append(got, req)
		return nil
	})
	if err != nil {
		t.Fatalf("uimRebindPrimaryGWProvisioningWithSender() error=%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("requests=%d want 2", len(got))
	}
	if got[0].SessionType != qmi.UIMSessionTypePrimaryGWProvisioning || got[0].Activate {
		t.Fatalf("unexpected deactivate request: %+v", got[0])
	}
	if got[1].SessionType != qmi.UIMSessionTypePrimaryGWProvisioning || !got[1].Activate {
		t.Fatalf("unexpected activate request: %+v", got[1])
	}
	if got[1].Slot == nil || *got[1].Slot != 2 {
		t.Fatalf("activate slot=%v want 2", got[1].Slot)
	}
	if string(got[1].ApplicationIdentifier) != string([]byte{0xA0, 0x00}) {
		t.Fatalf("activate aid=%X want A000", got[1].ApplicationIdentifier)
	}
}
