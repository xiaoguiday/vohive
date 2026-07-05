package manager

import (
	"context"
	"testing"
	"time"
)

func TestSendAPDUUsesStopTimeoutForLegacyMethod(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.Stop = 25 * time.Millisecond
	var gotDeadline time.Time
	m.sendAPDUHook = func(ctx context.Context, slot uint8, channel uint8, command []byte) ([]byte, error) {
		var ok bool
		gotDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("SendAPDU hook ctx has no deadline")
		}
		return []byte{0x90, 0x00}, nil
	}

	if _, err := m.SendAPDU(1, 2, []byte{0x80, 0xE2}); err != nil {
		t.Fatalf("SendAPDU() error = %v", err)
	}
	remaining := time.Until(gotDeadline)
	if remaining <= 0 || remaining > 100*time.Millisecond {
		t.Fatalf("SendAPDU deadline remaining = %s, want close to Stop timeout", remaining)
	}
}

func TestSendAPDUContextPreservesCallerDeadline(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.Stop = 25 * time.Millisecond
	callerCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wantDeadline, _ := callerCtx.Deadline()
	var gotDeadline time.Time
	m.sendAPDUHook = func(ctx context.Context, slot uint8, channel uint8, command []byte) ([]byte, error) {
		var ok bool
		gotDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("SendAPDUContext hook ctx has no deadline")
		}
		return []byte{0x90, 0x00}, nil
	}

	if _, err := m.SendAPDUContext(callerCtx, 1, 2, []byte{0x80, 0xE2}); err != nil {
		t.Fatalf("SendAPDUContext() error = %v", err)
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("SendAPDUContext deadline = %s, want caller deadline %s", gotDeadline, wantDeadline)
	}
}

func TestOpenLogicalChannelContextPreservesCallerDeadline(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.Stop = 25 * time.Millisecond
	callerCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wantDeadline, _ := callerCtx.Deadline()
	var gotDeadline time.Time
	m.openLogicalChannelHook = func(ctx context.Context, slot uint8, aid []byte) (byte, error) {
		var ok bool
		gotDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("OpenLogicalChannelContext hook ctx has no deadline")
		}
		return 2, nil
	}

	if _, err := m.OpenLogicalChannelContext(callerCtx, 1, []byte{0xA0, 0x00}); err != nil {
		t.Fatalf("OpenLogicalChannelContext() error = %v", err)
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("OpenLogicalChannelContext deadline = %s, want caller deadline %s", gotDeadline, wantDeadline)
	}
}

func TestCloseLogicalChannelContextPreservesCallerDeadline(t *testing.T) {
	m := newRecoveryTestManager()
	m.cfg.Timeouts.Stop = 25 * time.Millisecond
	callerCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wantDeadline, _ := callerCtx.Deadline()
	var gotDeadline time.Time
	m.closeLogicalChannelHook = func(ctx context.Context, slot uint8, channel uint8) error {
		var ok bool
		gotDeadline, ok = ctx.Deadline()
		if !ok {
			t.Fatal("CloseLogicalChannelContext hook ctx has no deadline")
		}
		return nil
	}

	if err := m.CloseLogicalChannelContext(callerCtx, 1, 2); err != nil {
		t.Fatalf("CloseLogicalChannelContext() error = %v", err)
	}
	if !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("CloseLogicalChannelContext deadline = %s, want caller deadline %s", gotDeadline, wantDeadline)
	}
}
