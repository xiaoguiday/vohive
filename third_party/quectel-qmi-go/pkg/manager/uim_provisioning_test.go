package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestEnsureSIMProvisioned(t *testing.T) {
	detected := UIMReadiness{CardPresent: true, AppState: qmi.UIMAppStateDetected, NeedsProvisioning: true, Reason: UIMReadinessNeedsProvisioning}
	ready := UIMReadiness{CardPresent: true, AppState: qmi.UIMAppStateReady, ProvisioningActive: true, UIMReady: true, Reason: UIMReadinessReady}
	absent := UIMReadiness{Reason: UIMReadinessCardAbsent}

	t.Run("ready is no-op", func(t *testing.T) {
		rebinds := 0
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) { return ready, nil },
			usimAID:   func(context.Context) ([]byte, error) { return []byte{0xA0}, nil },
			rebind:    func(context.Context, uint8, []byte) error { rebinds++; return nil },
			sleep:     func(context.Context, time.Duration) error { return nil },
		}
		r, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{}, deps)
		if err != nil || !r.UIMReady || rebinds != 0 {
			t.Fatalf("ready no-op failed: r=%+v err=%v rebinds=%d", r, err, rebinds)
		}
	})

	t.Run("detected activates then becomes ready", func(t *testing.T) {
		calls := 0
		rebinds := 0
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) {
				calls++
				if calls == 1 {
					return detected, nil
				}
				return ready, nil
			},
			usimAID: func(context.Context) ([]byte, error) { return []byte{0xA0, 0x00, 0x00}, nil },
			rebind:  func(context.Context, uint8, []byte) error { rebinds++; return nil },
			sleep:   func(context.Context, time.Duration) error { return nil },
		}
		r, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{MaxAttempts: 5}, deps)
		if err != nil || !r.UIMReady || rebinds != 1 {
			t.Fatalf("detected->ready failed: r=%+v err=%v rebinds=%d", r, err, rebinds)
		}
	})

	t.Run("detected stays detected does not thrash rebind", func(t *testing.T) {
		// 真实场景：激活后卡需要多轮才转 ready，期间持续上报 detected。
		reads := 0
		rebinds := 0
		detectedStuck := UIMReadiness{CardPresent: true, AppState: qmi.UIMAppStateDetected, NeedsProvisioning: true, Reason: UIMReadinessNeedsProvisioning}
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) {
				reads++
				if reads >= 6 { // settle 后终于 ready
					return UIMReadiness{CardPresent: true, AppState: qmi.UIMAppStateReady, ProvisioningActive: true, UIMReady: true, Reason: UIMReadinessReady}, nil
				}
				return detectedStuck, nil
			},
			usimAID: func(context.Context) ([]byte, error) { return []byte{0xA0, 0x00}, nil },
			rebind:  func(context.Context, uint8, []byte) error { rebinds++; return nil },
			sleep:   func(context.Context, time.Duration) error { return nil },
		}
		r, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{MaxAttempts: 12}, deps)
		if err != nil || !r.UIMReady {
			t.Fatalf("expected eventual ready: r=%+v err=%v", r, err)
		}
		if rebinds > 2 {
			t.Fatalf("rebind thrash: expected <=2 activations across stuck-detected polls, got %d", rebinds)
		}
	})

	t.Run("absent is no-op", func(t *testing.T) {
		rebinds := 0
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) { return absent, nil },
			usimAID:   func(context.Context) ([]byte, error) { return []byte{0xA0}, nil },
			rebind:    func(context.Context, uint8, []byte) error { rebinds++; return nil },
			sleep:     func(context.Context, time.Duration) error { return nil },
		}
		_, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{}, deps)
		if err != nil || rebinds != 0 {
			t.Fatalf("absent no-op failed: err=%v rebinds=%d", err, rebinds)
		}
	})

	t.Run("not-supported stops trying, non-fatal", func(t *testing.T) {
		rebinds := 0
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) { return detected, nil },
			usimAID:   func(context.Context) ([]byte, error) { return []byte{0xA0}, nil },
			rebind: func(context.Context, uint8, []byte) error {
				rebinds++
				return &qmi.NotSupportedError{Operation: "change provisioning session"}
			},
			sleep: func(context.Context, time.Duration) error { return nil },
		}
		r, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{MaxAttempts: 5}, deps)
		if err != nil || rebinds != 1 {
			t.Fatalf("not-supported should stop after one try, non-fatal: r=%+v err=%v rebinds=%d", r, err, rebinds)
		}
	})

	t.Run("unknown appstate backstop activates once", func(t *testing.T) {
		unknown := UIMReadiness{CardPresent: true, AppState: qmi.UIMAppStateUnknown, Reason: UIMReadinessCardResetting}
		rebinds := 0
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) { return unknown, nil },
			usimAID:   func(context.Context) ([]byte, error) { return []byte{0xA0}, nil },
			rebind:    func(context.Context, uint8, []byte) error { rebinds++; return nil },
			sleep:     func(context.Context, time.Duration) error { return nil },
		}
		_, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{MaxAttempts: 5, UnknownAppStateBackstop: 3}, deps)
		if err != nil || rebinds != 1 {
			t.Fatalf("unknown backstop should activate exactly once: err=%v rebinds=%d", err, rebinds)
		}
	})

	t.Run("readiness transport error propagates", func(t *testing.T) {
		wantErr := errors.New("qmi: read failed")
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) {
				return UIMReadiness{Reason: UIMReadinessTransportFatal}, wantErr
			},
			usimAID: func(context.Context) ([]byte, error) { return nil, nil },
			rebind:  func(context.Context, uint8, []byte) error { return nil },
			sleep:   func(context.Context, time.Duration) error { return nil },
		}
		_, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{}, deps)
		if !errors.Is(err, wantErr) {
			t.Fatalf("transport error must propagate, got %v", err)
		}
	})

	t.Run("rebind transient error exhausts activation budget", func(t *testing.T) {
		// 若 rebind 持续返回非 NotSupported 错误，计数仍消耗预算，不会无限重试。
		rebinds := 0
		transientErr := errors.New("qmi: device busy")
		deps := ensureProvisioningDeps{
			readiness: func(context.Context) (UIMReadiness, error) { return detected, nil },
			usimAID:   func(context.Context) ([]byte, error) { return []byte{0xA0, 0x00}, nil },
			rebind:    func(context.Context, uint8, []byte) error { rebinds++; return transientErr },
			sleep:     func(context.Context, time.Duration) error { return nil },
		}
		_, err := ensureSIMProvisioned(context.Background(), EnsureSIMProvisionedOptions{
			MaxAttempts:    10,
			MaxActivations: 2,
		}, deps)
		if err != nil {
			t.Fatalf("transient rebind error should be non-fatal: err=%v", err)
		}
		if rebinds > 2 {
			t.Fatalf("transient rebind errors must be bounded by MaxActivations=2, got %d rebinds", rebinds)
		}
	})
}
