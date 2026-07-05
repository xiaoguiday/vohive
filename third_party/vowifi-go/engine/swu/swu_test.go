package swu

import (
	"errors"
	"testing"
)

func TestTunnelConfigValidateAllowsExplicitEPDG(t *testing.T) {
	cfg := TunnelConfig{
		DeviceID:    "dev-1",
		Mode:        DataplaneModeUserspace,
		EPDGAddress: "epdg.example",
		IMSI:        "310280233641503",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTunnelConfigValidateAllowsCarrierDerivedEPDG(t *testing.T) {
	cfg := TunnelConfig{
		DeviceID: "dev-1",
		MCC:      "310",
		MNC:      "280",
		Identity: IMSIdentity{IMPI: "310280233641503@private.att.net"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestTunnelConfigValidateRejectsMissingIdentity(t *testing.T) {
	err := (TunnelConfig{
		DeviceID:    "dev-1",
		EPDGAddress: "epdg.example",
	}).Validate()
	if !errors.Is(err, ErrInvalidTunnelConfig) {
		t.Fatalf("Validate() err=%v, want ErrInvalidTunnelConfig", err)
	}
}

func TestTunnelResultReadyRequiresIKEAndIPsec(t *testing.T) {
	if (TunnelResult{Ready: true, IKEEstablished: true}).IsReady() {
		t.Fatalf("IsReady()=true without IPsec")
	}
	if !(TunnelResult{Ready: true, IKEEstablished: true, IPsecEstablished: true}).IsReady() {
		t.Fatalf("IsReady()=false with all readiness flags")
	}
}
