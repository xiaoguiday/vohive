package manager

import (
	"fmt"
	"time"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func (m *Manager) withUIMRecovery(op string, fn func(uim *qmi.UIMService) error) error {
	_, err := withUIMRecoveryValue(m, op, func(uim *qmi.UIMService) (struct{}, error) {
		return struct{}{}, fn(uim)
	})
	return err
}

func withUIMRecoveryValue[T any](m *Manager, op string, fn func(uim *qmi.UIMService) (T, error)) (T, error) {
	var zero T

	uim, err := m.ensureUIMService()
	if err != nil {
		if m.shouldRecoverUIMError(op, err) {
			m.triggerCoreRecoveryFromService("UIM", op, "initial", err)
		}
		return zero, err
	}

	result, err := fn(uim)
	if err == nil {
		m.noteServiceOperationSuccess("UIM", op)
		return result, nil
	}
	if !m.shouldRecoverUIMError(op, err) {
		return result, err
	}

	m.logServiceRecovery("UIM", op, "initial", err, "UIM operation failed; rebinding UIM service")

	m.uimRecoveryMu.Lock()
	uim, rebindErr := m.rebindUIMService("recover:" + op)
	m.uimRecoveryMu.Unlock()
	if rebindErr != nil {
		m.logServiceRecovery("UIM", op, "rebind", rebindErr, "UIM service rebind failed")
		m.triggerCoreRecoveryFromService("UIM", op, "rebind", rebindErr)
		return zero, fmt.Errorf("%s: UIM rebind failed: %w (initial=%v)", op, rebindErr, err)
	}

	retryResult, retryErr := fn(uim)
	if retryErr == nil {
		m.noteServiceOperationSuccess("UIM", op)
		m.log.WithField("service_name", "UIM").WithField("op", op).WithField("phase", "retry").Info("UIM operation recovered after rebind")
		return retryResult, nil
	}
	if m.shouldRecoverUIMError(op, retryErr) {
		m.logServiceRecovery("UIM", op, "retry", retryErr, "UIM operation still failing after rebind")
		m.triggerCoreRecoveryFromService("UIM", op, "retry", retryErr)
	}
	return retryResult, retryErr
}

func (m *Manager) ensureUIMService() (*qmi.UIMService, error) {
	if m == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	if m.ensureUIMServiceHook != nil {
		return m.ensureUIMServiceHook()
	}

	m.mu.RLock()
	uim := m.uim
	client := m.client
	m.mu.RUnlock()
	if uim != nil {
		return uim, nil
	}
	if client == nil {
		return nil, ErrServiceNotReady("UIM")
	}

	m.uimRecoveryMu.Lock()
	defer m.uimRecoveryMu.Unlock()

	m.mu.RLock()
	uim = m.uim
	client = m.client
	m.mu.RUnlock()
	if uim != nil {
		return uim, nil
	}
	if client == nil {
		return nil, ErrServiceNotReady("UIM")
	}

	allocated, err := qmi.NewUIMService(client)
	if err != nil {
		return nil, fmt.Errorf("allocate UIM client failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.client != client {
		_ = allocated.Close()
		return nil, ErrServiceNotReady("UIM")
	}
	m.uim = allocated
	m.log.Info("UIM service lazily allocated")
	return allocated, nil
}

func (m *Manager) rebindUIMService(reason string) (*qmi.UIMService, error) {
	if m == nil {
		return nil, ErrServiceNotReady("UIM")
	}
	if m.rebindUIMServiceHook != nil {
		return m.rebindUIMServiceHook(reason)
	}

	m.mu.Lock()
	prev := m.uim
	client := m.client
	m.uim = nil
	m.mu.Unlock()

	if prev != nil {
		if err := prev.Close(); err != nil {
			m.log.WithError(err).WithField("reason", reason).Warn("Closing previous UIM client failed during rebind")
		}
	}
	if client == nil {
		return nil, ErrServiceNotReady("UIM")
	}

	allocated, err := qmi.NewUIMService(client)
	if err != nil {
		return nil, fmt.Errorf("allocate UIM client failed: %w", err)
	}

	m.mu.Lock()
	if m.client != client {
		m.mu.Unlock()
		_ = allocated.Close()
		return nil, ErrServiceNotReady("UIM")
	}
	m.uim = allocated
	m.mu.Unlock()

	ctx, cancel := m.opContext(m.cfg.Timeouts.IndicationRegister)
	acceptedMask, registerErr := m.registerUIMIndicationsWithContext(ctx, allocated)
	cancel()
	if registerErr != nil {
		m.log.WithField("reason", reason).WithError(registerErr).Warn("Failed to replay UIM indication registration after rebind")
	} else {
		m.log.WithField("reason", reason).WithField("requested_mask", m.uimIndicationRegistrationMask()).WithField("accepted_mask", acceptedMask).Info("Replayed UIM indication registration after rebind")
	}

	m.log.WithField("reason", reason).Info("UIM service rebound")
	return allocated, nil
}

func (m *Manager) shouldRecoverUIMError(op string, err error) bool {
	return m.shouldRecoverServiceOperationError("UIM", op, err, "uim service not available")
}

func (m *Manager) triggerCoreRecoveryFromUIM(op string, phase string, cause error) {
	m.triggerCoreRecoveryFromService("UIM", op, phase, cause)
}

func (m *Manager) enqueueModemResetEvent(source string) {
	if m == nil {
		return
	}
	m.resetEvents.Add(1)

	now := time.Now()
	m.modemResetMu.Lock()
	if m.modemResetRecovering {
		m.modemResetPending = true
		m.resetCoalesced.Add(1)
		m.modemResetMu.Unlock()
		m.log.WithField("source", source).Debug("Coalesced modem-reset event while recovery is running")
		return
	}
	if !m.modemResetEnqueuedAt.IsZero() && now.Sub(m.modemResetEnqueuedAt) < m.modemResetDedupWindow {
		m.resetCoalesced.Add(1)
		m.modemResetMu.Unlock()
		m.log.WithField("source", source).Debug("Deduplicated modem-reset event inside debounce window")
		return
	}
	m.modemResetEnqueuedAt = now
	m.modemResetMu.Unlock()

	select {
	case m.eventCh <- eventModemReset:
		return
	default:
		m.modemResetMu.Lock()
		if m.modemResetDeferred {
			m.modemResetPending = true
			m.resetCoalesced.Add(1)
			m.modemResetMu.Unlock()
			m.log.WithField("source", source).Debug("Deferred modem-reset enqueue already scheduled")
			return
		}
		m.modemResetDeferred = true
		m.modemResetMu.Unlock()
		m.log.WithField("source", source).Warn("Internal event queue is full; scheduling deferred modem-reset event")
	}

	m.scheduleAfter(200*time.Millisecond, func() {
		m.modemResetMu.Lock()
		m.modemResetDeferred = false
		if m.modemResetRecovering {
			m.modemResetPending = true
			m.resetCoalesced.Add(1)
			m.modemResetMu.Unlock()
			return
		}
		m.modemResetEnqueuedAt = time.Now()
		m.modemResetMu.Unlock()

		select {
		case m.eventCh <- eventModemReset:
		default:
			m.modemResetMu.Lock()
			if !m.modemResetDeferred {
				m.modemResetDeferred = true
			}
			m.modemResetPending = true
			m.resetCoalesced.Add(1)
			m.modemResetMu.Unlock()
			m.log.WithField("source", source).Warn("Deferred modem-reset event still blocked; retrying enqueue")
			m.scheduleAfter(500*time.Millisecond, func() {
				m.modemResetMu.Lock()
				m.modemResetDeferred = false
				// clear debounce timestamp so deferred retry is not swallowed by dedup window
				m.modemResetEnqueuedAt = time.Time{}
				m.modemResetMu.Unlock()
				m.enqueueModemResetEvent(source + "_deferred_retry")
			})
		}
	})
}

func (m *Manager) logUIMRecovery(op string, phase string, err error, message string) {
	m.logServiceRecovery("UIM", op, phase, err, message)
}
