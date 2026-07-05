package manager

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEventRecoveryExhaustedString(t *testing.T) {
	if got := EventRecoveryExhausted.String(); got != "RecoveryExhausted" {
		t.Fatalf("EventRecoveryExhausted.String() = %q, want %q", got, "RecoveryExhausted")
	}
}

func TestRecoveryExhaustedByAttempts(t *testing.T) {
	m := &Manager{cfg: Config{RecoveryPolicy: RecoveryPolicy{MaxRecoverAttempts: 3}}}
	m.recoverCount = 3
	if m.recoveryExhausted() {
		t.Fatal("should not be exhausted at attempt == limit")
	}
	m.recoverCount = 4
	if !m.recoveryExhausted() {
		t.Fatal("should be exhausted when attempts exceed limit")
	}
}

func TestRecoveryExhaustedByElapsed(t *testing.T) {
	m := &Manager{cfg: Config{RecoveryPolicy: RecoveryPolicy{MaxRecoverElapsed: time.Minute}}}
	m.recoverFirstFailAt = time.Now().Add(-2 * time.Minute)
	if !m.recoveryExhausted() {
		t.Fatal("should be exhausted when elapsed exceeds window")
	}
	m.recoverFirstFailAt = time.Now()
	if m.recoveryExhausted() {
		t.Fatal("should not be exhausted within window")
	}
}

func TestRecoveryExhaustedDisabledByDefault(t *testing.T) {
	m := &Manager{cfg: Config{RecoveryPolicy: RecoveryPolicy{}}}
	m.recoverCount = 1000
	m.recoverFirstFailAt = time.Now().Add(-24 * time.Hour)
	if m.recoveryExhausted() {
		t.Fatal("zero-value policy must keep infinite retry (backward compatible)")
	}
}

func TestScheduleRecoverRetryEmitsExhausted(t *testing.T) {
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
		cfg:    Config{RecoveryPolicy: RecoveryPolicy{MaxRecoverAttempts: 2}},
	}
	// Prevent real time.AfterFunc timers from leaking into the test runtime.
	m.afterFunc = func(_ time.Duration, fn func()) *time.Timer {
		return time.NewTimer(time.Hour)
	}
	got := make(chan Event, 4)
	m.OnEvent(func(e Event) {
		if e.Type == EventRecoveryExhausted {
			got <- e
		}
	})
	m.scheduleRecoverRetry("test")
	m.scheduleRecoverRetry("test")
	m.scheduleRecoverRetry("test") // recoverCount=3 > 2 → terminal
	select {
	case e := <-got:
		if e.Reason != "recovery_exhausted" {
			t.Fatalf("reason = %q, want recovery_exhausted", e.Reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected EventRecoveryExhausted, got none")
	}
	if m.recoverCount != 0 || !m.recoverFirstFailAt.IsZero() {
		t.Fatalf("recover state not reset: count=%d firstFail=%v", m.recoverCount, m.recoverFirstFailAt)
	}
}

func TestIsControlDeviceGone(t *testing.T) {
	m := &Manager{cfg: Config{Device: ModemDevice{ControlPath: "/dev/this-node-does-not-exist-xyz"}}}
	if !m.isControlDeviceGone() {
		t.Fatal("expected gone for non-existent control path")
	}
	m2 := &Manager{cfg: Config{Device: ModemDevice{ControlPath: ""}}}
	if m2.isControlDeviceGone() {
		t.Fatal("empty path must not be treated as gone")
	}
}

func TestDoRecoverFromModemResetEmitsDeviceRemoved(t *testing.T) {
	openErr := errors.New("no such device")
	m := &Manager{
		log:    NewNopLogger(),
		events: NewEventEmitter(),
		cfg: Config{
			Device:         ModemDevice{ControlPath: "/dev/this-node-does-not-exist-xyz"},
			RecoveryPolicy: RecoveryPolicy{},
		},
	}
	// Prevent any real timers from being scheduled.
	m.afterFunc = func(_ time.Duration, fn func()) *time.Timer {
		return time.NewTimer(time.Hour)
	}
	// State defaults to StateIdle (not StateStopping), so isStopping = false.
	// desiredConnection defaults to false; the device_removed branch does not depend on it.

	// Inject hook so the openErr != nil branch is taken.
	m.openClientAndAllocateServicesHook = func(_ context.Context) error {
		return openErr
	}

	got := make(chan Event, 4)
	m.OnEvent(func(e Event) {
		if e.Type == EventRecoveryExhausted {
			got <- e
		}
	})

	m.doRecoverFromModemReset()

	select {
	case e := <-got:
		if e.Reason != "device_removed" {
			t.Fatalf("reason = %q, want device_removed", e.Reason)
		}
		if e.Error == nil {
			t.Fatal("expected non-nil Error on EventRecoveryExhausted device_removed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected EventRecoveryExhausted with device_removed, got none")
	}

	if m.recoverCount != 0 {
		t.Fatalf("recoverCount = %d, want 0", m.recoverCount)
	}
	if !m.recoverFirstFailAt.IsZero() {
		t.Fatalf("recoverFirstFailAt = %v, want zero", m.recoverFirstFailAt)
	}
}
