package manager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iniwex5/quectel-qmi-go/pkg/qmi"
)

func TestResetExistingDataConnectionUsesExistingCoreClient(t *testing.T) {
	m := newRecoveryTestManager()
	coreClient := &qmi.Client{}
	m.client = coreClient
	m.coreReady = true

	var allocatedWith *qmi.Client
	m.newWDSService = func(ctx context.Context, client *qmi.Client) (*qmi.WDSService, error) {
		allocatedWith = client
		return &qmi.WDSService{}, nil
	}
	queryCount := 0
	m.queryExistingPacketServiceState = func(ctx context.Context, wds *qmi.WDSService) (qmi.ConnectionStatus, error) {
		queryCount++
		if queryCount == 1 {
			return qmi.StatusConnected, nil
		}
		return qmi.StatusDisconnected, nil
	}
	stopCalls := 0
	m.stopExistingDataCall = func(ctx context.Context, wds *qmi.WDSService) error {
		stopCalls++
		return nil
	}
	m.closeWDSService = func(*qmi.WDSService) error { return nil }

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err != nil {
		t.Fatalf("ResetExistingDataConnection() error = %v", err)
	}
	if !reset {
		t.Fatal("reset=false, want true")
	}
	if allocatedWith != coreClient {
		t.Fatalf("WDS allocated with client %p, want existing core client %p", allocatedWith, coreClient)
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls=%d, want 1", stopCalls)
	}
}

func TestResetExistingDataConnectionSkipsStopWhenDisconnected(t *testing.T) {
	m := newRecoveryTestManager()
	coreClient := &qmi.Client{}
	m.client = coreClient
	m.coreReady = true

	var allocatedWith *qmi.Client
	m.newWDSService = func(ctx context.Context, client *qmi.Client) (*qmi.WDSService, error) {
		allocatedWith = client
		return &qmi.WDSService{}, nil
	}
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		return qmi.StatusDisconnected, nil
	}
	m.stopExistingDataCall = func(context.Context, *qmi.WDSService) error {
		t.Fatal("stopExistingDataCall should not be called")
		return nil
	}
	m.closeWDSService = func(*qmi.WDSService) error { return nil }

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err != nil {
		t.Fatalf("ResetExistingDataConnection() error = %v", err)
	}
	if reset {
		t.Fatal("reset=true, want false when packet service is disconnected")
	}
	if allocatedWith != coreClient {
		t.Fatalf("WDS allocated with client %p, want existing core client %p", allocatedWith, coreClient)
	}
}

func TestResetExistingDataConnectionConnectedStateUsesVerifiedStopPath(t *testing.T) {
	m := newRecoveryTestManager()
	m.client = &qmi.Client{}
	m.coreReady = true
	m.state = StateConnected
	m.wds = &qmi.WDSService{}
	m.handleV4 = 0x11
	m.handleV6 = 0x22
	m.settings = &qmi.RuntimeSettings{}
	m.desiredConnection = true

	queryCount := 0
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		queryCount++
		if queryCount == 1 {
			return qmi.StatusConnected, nil
		}
		return qmi.StatusDisconnected, nil
	}
	stopCalls := 0
	m.stopExistingDataCall = func(context.Context, *qmi.WDSService) error {
		stopCalls++
		return nil
	}

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err != nil {
		t.Fatalf("ResetExistingDataConnection() error = %v", err)
	}
	if !reset {
		t.Fatal("reset=false, want true")
	}
	if stopCalls != 1 {
		t.Fatalf("stop calls=%d, want 1", stopCalls)
	}
	if queryCount != 2 {
		t.Fatalf("query count=%d, want 2", queryCount)
	}
	if m.state != StateDisconnected {
		t.Fatalf("state=%s, want %s", m.state, StateDisconnected)
	}
	if m.handleV4 != 0 || m.handleV6 != 0 {
		t.Fatalf("handles=(%#x,%#x), want zero", m.handleV4, m.handleV6)
	}
	if m.settings != nil {
		t.Fatal("settings not cleared")
	}
	if m.desiredConnection {
		t.Fatal("desiredConnection=true, want false")
	}
}

func TestResetExistingDataConnectionErrorsWhenStillConnectedAfterStop(t *testing.T) {
	m := newRecoveryTestManager()
	m.client = &qmi.Client{}
	m.coreReady = true
	m.newWDSService = func(context.Context, *qmi.Client) (*qmi.WDSService, error) {
		return &qmi.WDSService{}, nil
	}
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		return qmi.StatusConnected, nil
	}
	m.stopExistingDataCall = func(context.Context, *qmi.WDSService) error {
		return nil
	}
	m.closeWDSService = func(*qmi.WDSService) error { return nil }

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err == nil || !strings.Contains(err.Error(), "still connected") {
		t.Fatalf("error=%v, want still connected error", err)
	}
	if reset {
		t.Fatal("reset=true, want false")
	}
}

func TestResetExistingDataConnectionReusesExistingWDSWithoutClosingTemporary(t *testing.T) {
	m := newRecoveryTestManager()
	m.client = &qmi.Client{}
	m.coreReady = true
	m.wds = &qmi.WDSService{}
	m.newWDSService = func(context.Context, *qmi.Client) (*qmi.WDSService, error) {
		t.Fatal("newWDSService should not be called when existing WDS is available")
		return nil, nil
	}
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		return qmi.StatusDisconnected, nil
	}
	m.closeWDSService = func(*qmi.WDSService) error {
		t.Fatal("closeWDSService should not be called for existing WDS")
		return nil
	}

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err != nil {
		t.Fatalf("ResetExistingDataConnection() error = %v", err)
	}
	if reset {
		t.Fatal("reset=true, want false when already disconnected")
	}
}

func TestResetExistingDataConnectionIgnoresTemporaryWDSCloseFailureAfterVerifiedStop(t *testing.T) {
	m := newRecoveryTestManager()
	m.client = &qmi.Client{}
	m.coreReady = true
	m.newWDSService = func(context.Context, *qmi.Client) (*qmi.WDSService, error) {
		return &qmi.WDSService{}, nil
	}
	queryCount := 0
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		queryCount++
		if queryCount == 1 {
			return qmi.StatusConnected, nil
		}
		return qmi.StatusDisconnected, nil
	}
	m.stopExistingDataCall = func(context.Context, *qmi.WDSService) error {
		return nil
	}
	m.closeWDSService = func(*qmi.WDSService) error {
		return errors.New("release client id failed")
	}

	reset, err := m.ResetExistingDataConnection(context.Background())
	if err != nil {
		t.Fatalf("ResetExistingDataConnection() error = %v", err)
	}
	if !reset {
		t.Fatal("reset=false, want true")
	}
}

func TestResetExistingDataConnectionRequiresCoreReady(t *testing.T) {
	m := newRecoveryTestManager()
	_, err := m.ResetExistingDataConnection(context.Background())
	if err == nil {
		t.Fatal("ResetExistingDataConnection() error=nil, want core-not-ready error")
	}
}

func TestResetExistingDataConnectionPropagatesStopError(t *testing.T) {
	wantErr := errors.New("stop failed")
	m := newRecoveryTestManager()
	m.client = &qmi.Client{}
	m.coreReady = true
	m.newWDSService = func(context.Context, *qmi.Client) (*qmi.WDSService, error) {
		return &qmi.WDSService{}, nil
	}
	m.queryExistingPacketServiceState = func(context.Context, *qmi.WDSService) (qmi.ConnectionStatus, error) {
		return qmi.StatusConnected, nil
	}
	m.stopExistingDataCall = func(context.Context, *qmi.WDSService) error {
		return wantErr
	}
	m.closeWDSService = func(*qmi.WDSService) error { return nil }

	reset, err := m.ResetExistingDataConnection(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v, want %v", err, wantErr)
	}
	if reset {
		t.Fatal("reset=true, want false on stop error")
	}
}

func TestExistingDataStopNoopErrorRecognizesWrappedQMIResults(t *testing.T) {
	for _, code := range []uint16{qmi.QMIErrOutOfCall, qmi.QMIErrNoEffect} {
		err := fmt.Errorf("stop network failed: %w", &qmi.QMIError{ErrorCode: code})
		if !isExistingDataStopNoopError(err) {
			t.Fatalf("isExistingDataStopNoopError(%v)=false, want true", err)
		}
	}
}
