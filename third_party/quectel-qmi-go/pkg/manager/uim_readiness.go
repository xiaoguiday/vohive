package manager

import (
	"context"
	"strings"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

type UIMReadinessReason string

const (
	UIMReadinessReady              UIMReadinessReason = "ready"
	UIMReadinessTransportFatal     UIMReadinessReason = "transport_fatal"
	UIMReadinessControlUnavailable UIMReadinessReason = "control_unavailable"
	UIMReadinessCardAbsent         UIMReadinessReason = "card_absent"
	UIMReadinessCardResetting      UIMReadinessReason = "card_resetting"
	UIMReadinessSIMBlocked         UIMReadinessReason = "sim_blocked"
	UIMReadinessIdentityEmpty      UIMReadinessReason = "identity_empty"
	UIMReadinessNeedsProvisioning  UIMReadinessReason = "needs_provisioning"
)

type UIMReadiness struct {
	TransportReady bool
	ControlReady   bool
	UIMReady       bool
	CardPresent    bool
	SIMStatus      qmi.SIMStatus
	ActiveSlot     uint8
	SlotKnown      bool
	SlotSource     string
	ICCID              string
	IMSI               string
	AppState           uint8
	ProvisioningActive bool
	NeedsProvisioning  bool
	Reason             UIMReadinessReason
	Err            error
}

func isUIMReadinessTransportFatal(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "qmi-proxy") && strings.Contains(msg, "broken pipe") {
		return true
	}
	for _, fragment := range []string{
		"qmi: read failed",
		"qmi read failed",
		"failed to open qmi device",
		"no such device",
		"no such file or directory",
		"client closed",
		"read failed: eof",
		"read failed eof",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func resolveActiveUIMSlot(info *qmi.UIMSlotStatus) (uint8, bool, string) {
	if info == nil {
		return 0, false, ""
	}
	for idx, slot := range info.Slots {
		if slot.PhysicalCardStatus != qmi.UIMPhysicalCardStatePresent {
			continue
		}
		if slot.PhysicalSlotStatus != qmi.UIMSlotStateActive {
			continue
		}
		if slot.LogicalSlot != 0 {
			return slot.LogicalSlot, true, "uim_slot_status"
		}
		return uint8(idx + 1), true, "uim_slot_status_index"
	}
	return 0, false, ""
}

func buildUIMReadiness(status qmi.SIMStatus, details *qmi.CardStatusDetails, slotInfo *qmi.UIMSlotStatus, ids DeviceIdentities, sourceErr error) UIMReadiness {
	return buildUIMReadinessWithSlotError(status, details, slotInfo, ids, sourceErr, nil)
}

func buildUIMReadinessWithSlotError(status qmi.SIMStatus, details *qmi.CardStatusDetails, slotInfo *qmi.UIMSlotStatus, ids DeviceIdentities, cardErr error, slotErr error) UIMReadiness {
	slot, slotKnown, slotSource := resolveActiveUIMSlot(slotInfo)
	sourceErr := cardErr
	if sourceErr == nil {
		sourceErr = slotErr
	}
	out := UIMReadiness{
		TransportReady: true,
		ControlReady:   true,
		SIMStatus:      status,
		ActiveSlot:     slot,
		SlotKnown:      slotKnown,
		SlotSource:     slotSource,
		ICCID:          strings.TrimSpace(ids.ICCID),
		IMSI:           strings.TrimSpace(ids.IMSI),
		Err:            sourceErr,
	}

	if cardErr != nil {
		if isUIMReadinessTransportFatal(cardErr) {
			out.TransportReady = false
			out.ControlReady = false
			out.Reason = UIMReadinessTransportFatal
			return out
		}
		out.ControlReady = false
		out.Reason = UIMReadinessControlUnavailable
		return out
	}
	if isUIMReadinessTransportFatal(slotErr) {
		out.TransportReady = false
		out.ControlReady = false
		out.Reason = UIMReadinessTransportFatal
		return out
	}

	out.CardPresent = status != qmi.SIMAbsent
	if details != nil {
		out.AppState = details.AppState
		out.ProvisioningActive = details.AppState == qmi.UIMAppStateReady

		switch details.CardState {
		case 0x00:
			out.CardPresent = false
		case 0x01, 0x02:
			out.CardPresent = true
		}
	}
	if !out.CardPresent {
		out.Reason = UIMReadinessCardAbsent
		return out
	}
	if status == qmi.SIMBlocked || status == qmi.SIMPINRequired || status == qmi.SIMPUKRequired || status == qmi.SIMNetworkLocked {
		out.Reason = UIMReadinessSIMBlocked
		return out
	}

	if out.CardPresent && details != nil && details.AppState == qmi.UIMAppStateDetected {
		out.NeedsProvisioning = true
		out.Reason = UIMReadinessNeedsProvisioning
		return out
	}

	if status != qmi.SIMReady {
		out.Reason = UIMReadinessCardResetting
		return out
	}

	out.UIMReady = true
	if out.ICCID == "" && out.IMSI == "" {
		out.Reason = UIMReadinessIdentityEmpty
		return out
	}
	out.Reason = UIMReadinessReady
	return out
}

func (m *Manager) GetUIMReadiness(ctx context.Context) (UIMReadiness, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		err := ErrServiceNotReady("UIM")
		return buildUIMReadiness(qmi.SIMNotReady, nil, nil, DeviceIdentities{}, err), err
	}

	type cardStatusResult struct {
		details *qmi.CardStatusDetails
		status  qmi.SIMStatus
	}
	var details *qmi.CardStatusDetails
	status := qmi.SIMNotReady
	cardStatus, cardErr := withUIMRecoveryValue(m, "GetUIMReadiness.GetCardStatusDetails", func(uim *qmi.UIMService) (cardStatusResult, error) {
		details, status, err := uim.GetCardStatusDetails(ctx)
		return cardStatusResult{details: details, status: status}, err
	})
	if cardErr == nil {
		details = cardStatus.details
		status = cardStatus.status
	}

	var slotInfo *qmi.UIMSlotStatus
	var slotErr error
	if cardErr == nil {
		slotInfo, slotErr = withUIMRecoveryValue(m, "GetUIMReadiness.GetSlotStatus", func(uim *qmi.UIMService) (*qmi.UIMSlotStatus, error) {
			return uim.GetSlotStatus(ctx)
		})
	}

	ids, _ := m.GetCachedIdentities()
	if cardErr == nil && strings.TrimSpace(ids.ICCID) == "" {
		if iccid, err := m.GetICCID(ctx); err == nil {
			ids.ICCID = iccid
		}
	}
	if cardErr == nil && strings.TrimSpace(ids.IMSI) == "" {
		if imsi, err := m.GetIMSI(ctx); err == nil {
			ids.IMSI = imsi
		}
	}

	readiness := buildUIMReadinessWithSlotError(status, details, slotInfo, ids, cardErr, slotErr)
	if cardErr != nil {
		return readiness, cardErr
	}
	if isUIMReadinessTransportFatal(slotErr) {
		return readiness, slotErr
	}
	return readiness, nil
}
