package swu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DataplaneModeDisabled  = "disabled"
	DataplaneModeUserspace = "userspace"
	DataplaneModeKernel    = "kernel"
)

var (
	ErrInvalidTunnelConfig = errors.New("invalid swu tunnel config")
	ErrTunnelNotReady      = errors.New("swu tunnel not ready")
)

type ProxyConfig struct {
	ID       string
	URL      string
	Address  string
	Addr     string
	Username string
	Password string
	Country  string
	Enabled  bool
}

type IMSIdentity struct {
	IMPI   string
	IMPU   string
	Domain string
}

type TunnelConfig struct {
	DeviceID       string
	TraceID        string
	Mode           string
	EPDGAddress    string
	EPDGSource     string
	LocalInterface string
	OuterLocalIP   string
	InnerLocalIP   string
	RemoteInnerIP  string
	IMSI           string
	MCC            string
	MNC            string
	IMEI           string
	Identity       IMSIdentity
	Proxy          *ProxyConfig
	StartedAt      time.Time
}

func (c TunnelConfig) NormalizedMode() string {
	mode := strings.TrimSpace(c.Mode)
	if mode == "" {
		return DataplaneModeUserspace
	}
	return mode
}

func (c TunnelConfig) Validate() error {
	if strings.TrimSpace(c.DeviceID) == "" {
		return fmt.Errorf("%w: device_id is empty", ErrInvalidTunnelConfig)
	}
	if c.NormalizedMode() == DataplaneModeDisabled {
		return nil
	}
	if strings.TrimSpace(c.EPDGAddress) == "" && (strings.TrimSpace(c.MCC) == "" || strings.TrimSpace(c.MNC) == "") {
		return fmt.Errorf("%w: ePDG address or MCC/MNC is required", ErrInvalidTunnelConfig)
	}
	if strings.TrimSpace(c.IMSI) == "" && strings.TrimSpace(c.Identity.IMPI) == "" {
		return fmt.Errorf("%w: IMSI or IMPI is required", ErrInvalidTunnelConfig)
	}
	return nil
}

type TunnelResult struct {
	Ready             bool
	Mode              string
	EPDGAddress       string
	LocalInnerIP      string
	RemoteInnerIP     string
	DNSServers        []string
	IKEEstablished    bool
	IPsecEstablished  bool
	MOBIKESupported   bool
	ChildSAIdentifier string
	Reason            string
	EstablishedAt     time.Time
}

func (r TunnelResult) IsReady() bool {
	return r.Ready && r.IKEEstablished && r.IPsecEstablished
}

func cloneTunnelResult(r TunnelResult) TunnelResult {
	r.DNSServers = append([]string(nil), r.DNSServers...)
	return r
}

func isZeroTunnelResult(r TunnelResult) bool {
	return !r.Ready &&
		strings.TrimSpace(r.Mode) == "" &&
		strings.TrimSpace(r.EPDGAddress) == "" &&
		strings.TrimSpace(r.LocalInnerIP) == "" &&
		strings.TrimSpace(r.RemoteInnerIP) == "" &&
		len(r.DNSServers) == 0 &&
		!r.IKEEstablished &&
		!r.IPsecEstablished &&
		!r.MOBIKESupported &&
		strings.TrimSpace(r.ChildSAIdentifier) == "" &&
		strings.TrimSpace(r.Reason) == "" &&
		r.EstablishedAt.IsZero()
}

type MOBIKERequest struct {
	DeviceID string
	TraceID  string
	OldIP    string
	NewIP    string
	At       time.Time
}

type MOBIKEResult struct {
	Rekeyed          bool
	OuterLocalIP     string
	LocalInnerIP     string
	RemoteInnerIP    string
	DNSServers       []string
	IKEEstablished   bool
	IPsecEstablished bool
	Reason           string
	UpdatedAt        time.Time
}

type TunnelSession interface {
	Result() TunnelResult
	MOBIKE(context.Context, MOBIKERequest) (MOBIKEResult, error)
	Close(context.Context) error
}

type TunnelManager interface {
	EstablishTunnel(context.Context, TunnelConfig) (TunnelSession, error)
}
