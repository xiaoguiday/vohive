package runtimehost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	swusim "github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
	"github.com/iniwex5/vowifi-go/runtimehost/voicehost"
)

var ErrAPDUBusy = errors.New("apdu busy")

type ctxKey string

const traceIDKey ctxKey = "trace_id"

func NewTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return "trace-" + hex.EncodeToString(b[:])
}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceIDKey, strings.TrimSpace(traceID))
}

func SetLogger(any) {}

type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseSIMReady Phase = "sim_ready"
	PhaseReady    Phase = "ready"
	PhaseStopped  Phase = "stopped"
	PhaseError    Phase = "error"
)

type State struct {
	DeviceID       string
	Phase          Phase
	DataplaneMode  string
	SIMReady       bool
	AccessReady    bool
	TunnelReady    bool
	IMSReady       bool
	SMSReady       bool
	RegStatus      int
	RegStatusText  string
	NetworkMode    string
	LastErrorClass string
	LastError      string
	LastReason     string
	UpdatedAt      time.Time
}

type Event struct {
	State State
}

type Observer interface {
	OnRuntimeEvent(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) OnRuntimeEvent(ctx context.Context, ev Event) {
	if f != nil {
		f(ctx, ev)
	}
}

type Modem interface {
	DeviceID() string
	IsHealthy() bool
	IsSimInserted() bool
	QuerySIMInserted() (bool, error)
	GetRegStatus() (int, string)
	GetNetworkMode() string
	Stop()
}

type APDUAccess interface {
	ExecuteATSilent(cmd string, timeout time.Duration) (string, error)
	OpenLogicalChannel(aid string) (int, error)
	CloseLogicalChannel(channel int) error
	TransmitAPDU(channel int, hexAPDU string) (string, error)
}

type IdentityReader interface {
	GetISIMIdentity() (identity.Identity, error)
}

type ModemAccess interface {
	GetISIMIdentity() (identity.Identity, error)
	RuntimeModem() Modem
}

type modemAccessAdapter struct {
	modem Modem
}

func NewModemAccessAdapter(m Modem) ModemAccess {
	if m == nil {
		return nil
	}
	return &modemAccessAdapter{modem: m}
}

func (a *modemAccessAdapter) RuntimeModem() Modem {
	if a == nil {
		return nil
	}
	return a.modem
}

func (a *modemAccessAdapter) GetISIMIdentity() (identity.Identity, error) {
	if a == nil || a.modem == nil {
		return identity.Identity{}, errors.New("modem is nil")
	}
	if r, ok := a.modem.(IdentityReader); ok {
		return r.GetISIMIdentity()
	}
	return identity.Identity{}, errors.New("modem does not expose ISIM identity")
}

type SIMAdapter interface {
	GetIMSI() (string, error)
	CalculateAKA(rand16, autn16 []byte) (swusim.AKAResult, error)
	Close() error
}

type readerSIMAdapter struct {
	provider swusim.AKAProvider
}

func NewReaderSIMAdapter(provider swusim.AKAProvider) SIMAdapter {
	return &readerSIMAdapter{provider: provider}
}

func (a *readerSIMAdapter) GetIMSI() (string, error) { return "", nil }

func (a *readerSIMAdapter) CalculateAKA(rand16, autn16 []byte) (swusim.AKAResult, error) {
	if a == nil || a.provider == nil {
		return swusim.AKAResult{}, errors.New("aka provider is nil")
	}
	return a.provider.CalculateAKA(rand16, autn16)
}

func (a *readerSIMAdapter) Close() error { return nil }

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

type DataplanePolicy struct {
	Mode                 string
	TUNName              string
	TUNMTU               int
	DisableTUNRouting    bool
	TUNAddresses         []string
	TUNEPDGExclusions    []swu.EPDGRouteExclusion
	TUNRoutes            []swu.TUNRoute
	TUNRules             []swu.TUNRule
	TunnelManager        swu.TunnelManager
	TunnelManagerFactory TunnelManagerFactory
}

type SessionConfig struct {
	DeviceID      string
	TraceID       string
	Profile       identity.Profile
	Prepared      *identity.PreparedSession
	DataplaneMode string
	Proxy         *ProxyConfig
}

type IMSRegistrationConfig struct {
	DeviceID    string
	TraceID     string
	Profile     identity.Profile
	Prepared    *identity.PreparedSession
	SIM         SIMAdapter
	Access      ModemAccess
	NetworkMode string
	Dataplane   DataplanePolicy
	Tunnel      swu.TunnelResult
	Proxy       *ProxyConfig
}

type IMSRegistrationResult struct {
	Registered     bool
	StatusCode     int
	Reason         string
	Server         string
	Profile        voiceclient.IMSProfile
	Binding        voiceclient.RegistrationBinding
	VoiceTransport voiceclient.SIPRequestTransport
	SMSTransport   messaging.SMSTransport
	USSDTransport  messaging.USSDTransport
	Close          func(context.Context) error
	Recover        func(context.Context) (IMSRegistrationResult, error)
}

type IMSRegistrar interface {
	RegisterIMS(context.Context, IMSRegistrationConfig) (IMSRegistrationResult, error)
}

const StartModeMain = "main"

type TunnelManagerFactory func(StartRequest) (swu.TunnelManager, error)

type StartRequest struct {
	Mode                       string
	DeviceID                   string
	TraceID                    string
	Profile                    identity.Profile
	Prepared                   *identity.PreparedSession
	NetworkMode                string
	VoiceGateway               *voicehost.Gateway
	SIM                        SIMAdapter
	Access                     ModemAccess
	Dataplane                  DataplanePolicy
	Proxy                      *ProxyConfig
	EAPReauthentication        swu.EAPReauthenticationState
	OnEAPReauthenticationState func(swu.EAPReauthenticationState)
	TunnelManager              swu.TunnelManager
	TunnelManagerFactory       TunnelManagerFactory
	IMSRegistrar               IMSRegistrar
	VoiceTransport             voiceclient.SIPRequestTransport
	VoiceUserAgent             string
	VoiceSessionExpires        int
	VoiceMediaRelay            *voicehost.RTPRelayConfig
	SMSTransport               messaging.SMSTransport
	USSDTransport              messaging.USSDTransport
	DeliveryStore              messaging.DeliveryStore
	Dispatch                   eventhost.Dispatcher
	BeforeStart                func(context.Context, SessionConfig) error
	ShouldRun                  func() bool
}

type runtimeVoiceAgentConfig struct {
	transport      voiceclient.SIPRequestTransport
	userAgent      string
	sessionExpires int
	mediaRelay     *voicehost.RTPRelayConfig
}

type Instance struct {
	mu           sync.RWMutex
	imsRecoverMu sync.Mutex
	state        State
	service      *messaging.Service
	observers    []Observer
	notifier     func(string)
	smsNotify    func(deviceID, sender, content string, ts time.Time)
	tunnel       swu.TunnelSession
	voice        voicehost.Agent
	voiceConfig  runtimeVoiceAgentConfig
	imsClose     func(context.Context) error
	imsRecover   func(context.Context) (IMSRegistrationResult, error)
	stopped      bool
}

func Start(ctx context.Context, req StartRequest) (*Instance, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(req.DeviceID) == "" {
		return nil, errors.New("device_id is empty")
	}
	if req.ShouldRun != nil && !req.ShouldRun() {
		return nil, errors.New("runtime start canceled")
	}
	cfg := SessionConfig{
		DeviceID:      req.DeviceID,
		TraceID:       req.TraceID,
		Profile:       req.Profile,
		Prepared:      req.Prepared,
		DataplaneMode: req.Dataplane.Mode,
		Proxy:         req.Proxy,
	}
	if req.BeforeStart != nil {
		if err := req.BeforeStart(ctx, cfg); err != nil {
			return nil, err
		}
	}
	regStatus, regText := 0, ""
	modem := Modem(nil)
	if req.Access != nil {
		modem = req.Access.RuntimeModem()
	}
	if modem != nil {
		regStatus, regText = modem.GetRegStatus()
	}
	var tunnel swu.TunnelSession
	var tunnelResult swu.TunnelResult
	var tunnelReady bool
	tunnelManager, err := tunnelManagerForStart(req)
	if err != nil {
		return nil, err
	}
	if tunnelManager != nil && strings.TrimSpace(req.Dataplane.Mode) != swu.DataplaneModeDisabled {
		tunnelConfig := buildTunnelConfig(req, modem)
		if err := tunnelConfig.Validate(); err != nil {
			return nil, err
		}
		session, err := tunnelManager.EstablishTunnel(ctx, tunnelConfig)
		if err != nil {
			return nil, fmt.Errorf("SWU tunnel establishment failed: %w", err)
		}
		if session == nil {
			return nil, errors.New("SWU tunnel establishment failed: nil tunnel session")
		}
		tunnel = session
		tunnelResult = session.Result()
		tunnelReady = tunnelResult.IsReady()
		if !tunnelReady {
			_ = session.Close(ctx)
			return nil, fmt.Errorf("SWU tunnel establishment incomplete: %s", firstRuntimeNonEmpty(tunnelResult.Reason, "not ready"))
		}
	}
	imsReady := req.IMSRegistrar == nil
	imsReason := ""
	imsResult := IMSRegistrationResult{}
	if req.IMSRegistrar != nil {
		imsCfg := IMSRegistrationConfig{
			DeviceID:    req.DeviceID,
			TraceID:     req.TraceID,
			Profile:     req.Profile,
			Prepared:    req.Prepared,
			SIM:         req.SIM,
			Access:      req.Access,
			NetworkMode: req.NetworkMode,
			Dataplane:   req.Dataplane,
			Tunnel:      tunnelResult,
			Proxy:       req.Proxy,
		}
		res, err := req.IMSRegistrar.RegisterIMS(ctx, imsCfg)
		if err != nil {
			return nil, fmt.Errorf("IMS registration failed: %w", err)
		}
		if !res.Registered {
			return nil, fmt.Errorf("IMS registration rejected: %d %s", res.StatusCode, strings.TrimSpace(res.Reason))
		}
		imsReady = true
		imsReason = firstRuntimeNonEmpty(res.Reason, res.Server)
		imsResult = res
	}
	state := State{
		DeviceID:      req.DeviceID,
		Phase:         PhaseReady,
		DataplaneMode: req.Dataplane.Mode,
		SIMReady:      req.SIM != nil,
		AccessReady:   modem != nil,
		TunnelReady:   tunnelReady,
		IMSReady:      imsReady,
		SMSReady:      true,
		RegStatus:     regStatus,
		RegStatusText: regText,
		NetworkMode:   strings.TrimSpace(req.NetworkMode),
		LastReason:    firstRuntimeNonEmpty(imsReason, tunnelResult.Reason, "started"),
		UpdatedAt:     time.Now(),
	}
	if state.NetworkMode == "" && modem != nil {
		state.NetworkMode = modem.GetNetworkMode()
	}
	svc := messaging.NewService(req.DeviceID, req.Profile.IMSI, req.DeliveryStore, req.Dispatch)
	smsTransport := req.SMSTransport
	if smsTransport == nil {
		smsTransport = imsResult.SMSTransport
	}
	ussdTransport := req.USSDTransport
	if ussdTransport == nil {
		ussdTransport = imsResult.USSDTransport
	}
	voiceConfig := runtimeVoiceAgentConfigFromStart(req)
	inst := &Instance{
		state:       state,
		service:     svc,
		tunnel:      tunnel,
		voice:       buildRuntimeVoiceAgentWithConfig(voiceConfig, imsResult),
		voiceConfig: voiceConfig,
		imsClose:    imsResult.Close,
		imsRecover:  imsResult.Recover,
	}
	svc.SetSMSTransport(inst.wrapSMSTransport(smsTransport))
	svc.SetUSSDTransport(inst.wrapUSSDTransport(ussdTransport))
	if req.VoiceGateway != nil {
		req.VoiceGateway.RegisterAgent(req.DeviceID, inst)
	}
	inst.notify(ctx)
	return inst, nil
}

func buildRuntimeVoiceAgent(req StartRequest, reg IMSRegistrationResult) voicehost.Agent {
	return buildRuntimeVoiceAgentWithConfig(runtimeVoiceAgentConfigFromStart(req), reg)
}

func runtimeVoiceAgentConfigFromStart(req StartRequest) runtimeVoiceAgentConfig {
	return runtimeVoiceAgentConfig{
		transport:      req.VoiceTransport,
		userAgent:      req.VoiceUserAgent,
		sessionExpires: req.VoiceSessionExpires,
		mediaRelay:     req.VoiceMediaRelay,
	}
}

func buildRuntimeVoiceAgentWithConfig(cfg runtimeVoiceAgentConfig, reg IMSRegistrationResult) voicehost.Agent {
	transport := cfg.transport
	if transport == nil {
		transport = reg.VoiceTransport
	}
	if transport == nil || !reg.Registered {
		return nil
	}
	binding := reg.Binding
	if strings.TrimSpace(binding.ContactURI) == "" {
		return nil
	}
	profile := reg.Profile
	if strings.TrimSpace(profile.IMPU) == "" {
		profile.IMPU = strings.TrimSpace(binding.PublicIdentity)
	}
	if strings.TrimSpace(profile.Domain) == "" {
		profile.Domain = sipDomainRuntime(profile.IMPU)
	}
	return &voicehost.IMSOutboundAgent{
		Transport:      transport,
		Profile:        profile,
		Registration:   binding,
		Domain:         profile.Domain,
		UserAgent:      firstRuntimeNonEmpty(cfg.userAgent, profile.UserAgent),
		SessionExpires: cfg.sessionExpires,
		MediaRelay:     cfg.mediaRelay,
	}
}

func buildRuntimeVoiceRegistrationUpdate(cfg runtimeVoiceAgentConfig, reg IMSRegistrationResult) (voicehost.IMSRegistrationUpdate, bool) {
	if !reg.Registered {
		return voicehost.IMSRegistrationUpdate{}, false
	}
	binding := reg.Binding
	if strings.TrimSpace(binding.ContactURI) == "" {
		return voicehost.IMSRegistrationUpdate{}, false
	}
	profile := reg.Profile
	if strings.TrimSpace(profile.IMPU) == "" {
		profile.IMPU = strings.TrimSpace(binding.PublicIdentity)
	}
	if strings.TrimSpace(profile.Domain) == "" {
		profile.Domain = sipDomainRuntime(profile.IMPU)
	}
	transport := cfg.transport
	if transport == nil {
		transport = reg.VoiceTransport
	}
	return voicehost.IMSRegistrationUpdate{
		Transport:      transport,
		Profile:        profile,
		Registration:   binding,
		Domain:         profile.Domain,
		UserAgent:      firstRuntimeNonEmpty(cfg.userAgent, profile.UserAgent),
		SessionExpires: cfg.sessionExpires,
		MediaRelay:     cfg.mediaRelay,
	}, true
}

func (i *Instance) AddObserver(o Observer) {
	if i == nil || o == nil {
		return
	}
	i.mu.Lock()
	i.observers = append(i.observers, o)
	state := i.state
	i.mu.Unlock()
	o.OnRuntimeEvent(context.Background(), Event{State: state})
}

func (i *Instance) notify(ctx context.Context) {
	i.mu.RLock()
	observers := append([]Observer(nil), i.observers...)
	state := i.state
	i.mu.RUnlock()
	for _, o := range observers {
		o.OnRuntimeEvent(ctx, Event{State: state})
	}
}

func (i *Instance) Stop(ctx context.Context) error {
	if i == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	i.mu.Lock()
	tunnel := i.tunnel
	imsClose := i.imsClose
	i.tunnel = nil
	i.imsClose = nil
	i.stopped = true
	i.state.Phase = PhaseStopped
	i.state.TunnelReady = false
	i.state.LastReason = "stopped"
	i.state.UpdatedAt = time.Now()
	i.mu.Unlock()
	var err error
	if tunnel != nil {
		err = tunnel.Close(ctx)
	}
	if imsClose != nil {
		err = errors.Join(err, imsClose(ctx))
	}
	i.notify(ctx)
	return err
}

func (i *Instance) StartOutboundCall(ctx context.Context, req voicehost.OutboundCallRequest) (voicehost.OutboundCallResult, error) {
	agent := i.outboundVoiceAgent()
	if agent == nil {
		return voicehost.OutboundCallResult{Accepted: false, Reason: "IMS voice agent unavailable"}, voicehost.ErrIMSVoiceAgentNotReady
	}
	result, err := agent.StartOutboundCall(ctx, req)
	if !result.RegistrationRecoveryNeeded {
		return result, err
	}
	_, recovered, recoveryErr := i.recoverIMSRegistration(ctx, result.Reason, true)
	if recoveryErr != nil {
		return result, runtimeOperationRecoveryError(err, recoveryErr)
	}
	if err == nil || !recovered {
		return result, nil
	}
	agent = i.outboundVoiceAgent()
	if agent == nil {
		return result, errors.Join(err, voicehost.ErrIMSVoiceAgentNotReady)
	}
	return agent.StartOutboundCall(ctx, req)
}

func (i *Instance) EndVoiceCall(ctx context.Context, info voicehost.DialogInfo) error {
	agent := i.dialogTerminator()
	if agent == nil {
		return voicehost.ErrIMSVoiceAgentNotReady
	}
	return agent.EndVoiceCall(ctx, info)
}

func (i *Instance) CancelVoiceCall(ctx context.Context, info voicehost.DialogInfo) error {
	agent := i.dialogCanceller()
	if agent == nil {
		return voicehost.ErrIMSVoiceAgentNotReady
	}
	return agent.CancelVoiceCall(ctx, info)
}

func (i *Instance) SendDialogInfo(ctx context.Context, req voicehost.DialogInfoRequest) (voicehost.DialogInfoResult, error) {
	agent := i.dialogInfoSender()
	if agent == nil {
		return voicehost.DialogInfoResult{Accepted: false, Reason: "IMS voice agent unavailable"}, voicehost.ErrIMSVoiceAgentNotReady
	}
	result, err := agent.SendDialogInfo(ctx, req)
	if result.RegistrationRecoveryNeeded {
		if _, _, recoveryErr := i.recoverIMSRegistration(ctx, result.Reason, true); recoveryErr != nil {
			return result, runtimeOperationRecoveryError(err, recoveryErr)
		}
	}
	return result, err
}

func (i *Instance) SendDialogUpdate(ctx context.Context, req voicehost.DialogUpdateRequest) (voicehost.DialogUpdateResult, error) {
	agent := i.dialogUpdater()
	if agent == nil {
		return voicehost.DialogUpdateResult{Accepted: false, Reason: "IMS voice agent unavailable"}, voicehost.ErrIMSVoiceAgentNotReady
	}
	result, err := agent.SendDialogUpdate(ctx, req)
	if result.RegistrationRecoveryNeeded {
		if _, _, recoveryErr := i.recoverIMSRegistration(ctx, result.Reason, true); recoveryErr != nil {
			return result, runtimeOperationRecoveryError(err, recoveryErr)
		}
	}
	return result, err
}

func (i *Instance) SendDialogReinvite(ctx context.Context, req voicehost.DialogReinviteRequest) (voicehost.DialogReinviteResult, error) {
	agent := i.dialogReinviter()
	if agent == nil {
		return voicehost.DialogReinviteResult{Accepted: false, Reason: "IMS voice agent unavailable"}, voicehost.ErrIMSVoiceAgentNotReady
	}
	result, err := agent.SendDialogReinvite(ctx, req)
	if result.RegistrationRecoveryNeeded {
		if _, _, recoveryErr := i.recoverIMSRegistration(ctx, result.Reason, true); recoveryErr != nil {
			return result, runtimeOperationRecoveryError(err, recoveryErr)
		}
	}
	return result, err
}

func runtimeOperationRecoveryError(operationErr, recoveryErr error) error {
	if recoveryErr == nil {
		return operationErr
	}
	if operationErr == nil {
		return recoveryErr
	}
	return errors.Join(operationErr, recoveryErr)
}

func (i *Instance) recoverIMSRegistration(ctx context.Context, reason string, updateVoice bool) (IMSRegistrationResult, bool, error) {
	if i == nil {
		return IMSRegistrationResult{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	i.imsRecoverMu.Lock()
	defer i.imsRecoverMu.Unlock()

	i.mu.RLock()
	recover := i.imsRecover
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return IMSRegistrationResult{}, false, voicehost.ErrIMSVoiceAgentNotReady
	}
	if recover == nil {
		return IMSRegistrationResult{}, false, nil
	}
	result, err := recover(ctx)
	if err != nil {
		i.recordIMSRecoveryFailure(ctx, err)
		return IMSRegistrationResult{}, false, err
	}
	if !result.Registered {
		err := fmt.Errorf("IMS registration recovery did not register: %d %s", result.StatusCode, strings.TrimSpace(result.Reason))
		i.recordIMSRecoveryFailure(ctx, err)
		return result, false, err
	}
	i.applyIMSRegistrationResult(ctx, result, firstRuntimeNonEmpty(result.Reason, reason, "IMS registration recovered"), updateVoice)
	return result, true, nil
}

func (i *Instance) recordIMSRecoveryFailure(ctx context.Context, err error) {
	if i == nil || err == nil {
		return
	}
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return
	}
	i.state.IMSReady = false
	i.state.LastReason = "IMS registration recovery failed: " + err.Error()
	i.state.UpdatedAt = time.Now()
	i.mu.Unlock()
	i.notify(ctx)
}

func (i *Instance) applyIMSRegistrationResult(ctx context.Context, result IMSRegistrationResult, reason string, updateVoice bool) {
	if i == nil {
		return
	}
	smsTransport := result.SMSTransport
	ussdTransport := result.USSDTransport
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return
	}
	if result.Close != nil {
		i.imsClose = result.Close
	}
	if result.Recover != nil {
		i.imsRecover = result.Recover
	}
	if updateVoice {
		if update, ok := buildRuntimeVoiceRegistrationUpdate(i.voiceConfig, result); ok {
			if updater, ok := i.voice.(voicehost.IMSRegistrationUpdater); ok {
				updater.UpdateIMSRegistration(update)
			} else {
				i.voice = buildRuntimeVoiceAgentWithConfig(i.voiceConfig, result)
			}
		} else {
			i.voice = nil
		}
	}
	i.state.IMSReady = result.Registered
	i.state.LastReason = firstRuntimeNonEmpty(reason, result.Server, "IMS registration recovered")
	i.state.UpdatedAt = time.Now()
	svc := i.service
	i.mu.Unlock()

	if svc != nil {
		if smsTransport != nil {
			svc.SetSMSTransport(i.wrapSMSTransport(smsTransport))
		}
		if ussdTransport != nil {
			svc.SetUSSDTransport(i.wrapUSSDTransport(ussdTransport))
		}
	}
	i.notify(ctx)
}

type runtimeRecoveringSMSTransport struct {
	owner *Instance
	inner messaging.SMSTransport
}

func (t *runtimeRecoveringSMSTransport) SendSMSPart(ctx context.Context, req messaging.SMSSendRequest) (messaging.SMSSendResult, error) {
	if t == nil || t.inner == nil {
		return messaging.SMSSendResult{State: "failed", ErrorText: messaging.ErrSMSTransportUnavailable.Error()}, messaging.ErrSMSTransportUnavailable
	}
	result, err := t.inner.SendSMSPart(ctx, req)
	if !result.RegistrationRecoveryNeeded || t.owner == nil {
		return result, err
	}
	retry := err != nil && result.SIPCode == 0
	recovery, recovered, recoveryErr := t.owner.recoverIMSRegistration(ctx, result.ErrorText, true)
	if recoveryErr != nil {
		return result, runtimeOperationRecoveryError(err, recoveryErr)
	}
	if !retry || !recovered || recovery.SMSTransport == nil {
		return result, err
	}
	return recovery.SMSTransport.SendSMSPart(ctx, req)
}

type runtimeRecoveringUSSDTransport struct {
	owner *Instance
	inner messaging.USSDTransport
}

func (t *runtimeRecoveringUSSDTransport) ExecuteUSSD(ctx context.Context, req messaging.USSDRequest) (messaging.USSDResult, error) {
	if t == nil || t.inner == nil {
		return messaging.USSDResult{SessionID: req.SessionID, Done: true}, messaging.ErrUSSDTransportUnavailable
	}
	result, err := t.inner.ExecuteUSSD(ctx, req)
	if !result.RegistrationRecoveryNeeded || t.owner == nil {
		return result, err
	}
	retry := err != nil && result.Status == 0
	recovery, recovered, recoveryErr := t.owner.recoverIMSRegistration(ctx, "USSD registration recovery", true)
	if recoveryErr != nil {
		return result, runtimeOperationRecoveryError(err, recoveryErr)
	}
	if !retry || !recovered || recovery.USSDTransport == nil {
		return result, err
	}
	return recovery.USSDTransport.ExecuteUSSD(ctx, req)
}

func (t *runtimeRecoveringUSSDTransport) ContinueUSSD(ctx context.Context, req messaging.USSDRequest) (messaging.USSDResult, error) {
	if t == nil || t.inner == nil {
		return messaging.USSDResult{SessionID: req.SessionID, Done: true}, messaging.ErrUSSDTransportUnavailable
	}
	result, err := t.inner.ContinueUSSD(ctx, req)
	if result.RegistrationRecoveryNeeded && t.owner != nil {
		if _, _, recoveryErr := t.owner.recoverIMSRegistration(ctx, "USSD registration recovery", true); recoveryErr != nil {
			return result, runtimeOperationRecoveryError(err, recoveryErr)
		}
	}
	return result, err
}

func (t *runtimeRecoveringUSSDTransport) CancelUSSD(ctx context.Context, req messaging.USSDRequest) error {
	if t == nil || t.inner == nil {
		return messaging.ErrUSSDTransportUnavailable
	}
	err := t.inner.CancelUSSD(ctx, req)
	if err != nil && t.owner != nil && messaging.IsIMSRegistrationRecoveryError(err) {
		if _, _, recoveryErr := t.owner.recoverIMSRegistration(ctx, "USSD registration recovery", true); recoveryErr != nil {
			return errors.Join(err, recoveryErr)
		}
	}
	return err
}

type runtimeRecoveringIMSUSSDDialogTransport struct {
	*runtimeRecoveringUSSDTransport
	dialog messaging.IMSUSSDDialogTransport
}

func (t *runtimeRecoveringIMSUSSDDialogTransport) HandleIMSInfo(ctx context.Context, req messaging.IMSUSSDDialogRequest) (messaging.IMSUSSDDialogResult, error) {
	if t == nil || t.dialog == nil {
		return messaging.IMSUSSDDialogResult{}, nil
	}
	return t.dialog.HandleIMSInfo(ctx, req)
}

func (t *runtimeRecoveringIMSUSSDDialogTransport) HandleIMSBye(ctx context.Context, req messaging.IMSUSSDDialogRequest) (messaging.IMSUSSDDialogResult, error) {
	if t == nil || t.dialog == nil {
		return messaging.IMSUSSDDialogResult{}, nil
	}
	return t.dialog.HandleIMSBye(ctx, req)
}

func (i *Instance) wrapSMSTransport(t messaging.SMSTransport) messaging.SMSTransport {
	if i == nil || t == nil {
		return t
	}
	if _, ok := t.(*runtimeRecoveringSMSTransport); ok {
		return t
	}
	return &runtimeRecoveringSMSTransport{owner: i, inner: t}
}

func (i *Instance) wrapUSSDTransport(t messaging.USSDTransport) messaging.USSDTransport {
	if i == nil || t == nil {
		return t
	}
	if _, ok := t.(*runtimeRecoveringUSSDTransport); ok {
		return t
	}
	if _, ok := t.(*runtimeRecoveringIMSUSSDDialogTransport); ok {
		return t
	}
	wrapped := &runtimeRecoveringUSSDTransport{owner: i, inner: t}
	if dialog, ok := t.(messaging.IMSUSSDDialogTransport); ok {
		return &runtimeRecoveringIMSUSSDDialogTransport{runtimeRecoveringUSSDTransport: wrapped, dialog: dialog}
	}
	return wrapped
}

func (i *Instance) outboundVoiceAgent() voicehost.OutboundCallAgent {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.OutboundCallAgent)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) dialogTerminator() voicehost.DialogTerminator {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.DialogTerminator)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) dialogCanceller() voicehost.DialogCanceller {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.DialogCanceller)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) dialogInfoSender() voicehost.DialogInfoSender {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.DialogInfoSender)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) dialogUpdater() voicehost.DialogUpdater {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.DialogUpdater)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) dialogReinviter() voicehost.DialogReinviter {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	agent, _ := i.voice.(voicehost.DialogReinviter)
	stopped := i.stopped
	i.mu.RUnlock()
	if stopped {
		return nil
	}
	return agent
}

func (i *Instance) Service() *messaging.Service {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.service
}

func (i *Instance) SendSMSWithOptions(ctx context.Context, to, text string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	svc := i.Service()
	if svc == nil {
		return messaging.SendOutcome{}, errors.New("messaging service is nil")
	}
	return svc.SendSMSWithOptions(ctx, to, text, opts)
}

func (i *Instance) GetSMSDeliveryStatus(messageID string) (*messaging.DeliveryStatus, error) {
	svc := i.Service()
	if svc == nil {
		return nil, errors.New("messaging service is nil")
	}
	if svc == nil {
		return nil, errors.New("messaging service is nil")
	}
	return svc.GetSMSDeliveryStatus(messageID)
}

func (i *Instance) HandleIncomingSMS(ctx context.Context, msg messaging.IncomingSMS) error {
	svc := i.Service()
	if svc == nil {
		return errors.New("messaging service is nil")
	}
	return svc.HandleIncomingSMS(ctx, msg)
}

func (i *Instance) HandleSMSDeliveryReport(ctx context.Context, report messaging.SMSDeliveryReport) (messaging.DeliveryPartMatch, error) {
	svc := i.Service()
	if svc == nil {
		return messaging.DeliveryPartMatch{}, errors.New("messaging service is nil")
	}
	return svc.HandleSMSDeliveryReport(ctx, report)
}

func (i *Instance) HandleIMSMessage(ctx context.Context, req voicehost.IMSMessageRequest) (voicehost.IMSMessageResult, error) {
	svc := i.Service()
	if svc == nil {
		return voicehost.IMSMessageResult{StatusCode: 503, Reason: "messaging service is nil"}, errors.New("messaging service is nil")
	}
	res, err := svc.HandleIMSMessage(ctx, messaging.IMSMessageRequest{
		FromURI:     req.FromURI,
		ToURI:       req.ToURI,
		CallID:      req.CallID,
		CSeq:        req.CSeq,
		ContentType: req.ContentType,
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneRuntimeSIPHeaders(req.Headers),
	})
	return voicehost.IMSMessageResult{
		StatusCode:  res.StatusCode,
		Reason:      res.Reason,
		ContentType: res.ReplyContentType,
		Body:        append([]byte(nil), res.ReplyBody...),
	}, err
}

func (i *Instance) HandleIMSInfo(ctx context.Context, req voicehost.IMSInfoRequest) (voicehost.IMSInfoResult, error) {
	svc := i.Service()
	if svc == nil {
		return voicehost.IMSInfoResult{Handled: true, StatusCode: 503, Reason: "messaging service is nil"}, errors.New("messaging service is nil")
	}
	res, err := svc.HandleIMSUSSDInfo(ctx, messaging.IMSUSSDDialogRequest{
		URI:         req.URI,
		FromURI:     req.FromURI,
		ToURI:       req.ToURI,
		CallID:      req.CallID,
		CSeq:        req.CSeq,
		ContentType: req.ContentType,
		InfoPackage: req.InfoPackage,
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneRuntimeSIPHeaders(req.Headers),
	})
	return voicehost.IMSInfoResult{
		Handled:     res.Handled,
		StatusCode:  res.StatusCode,
		Reason:      res.Reason,
		ContentType: res.ContentType,
		Body:        append([]byte(nil), res.Body...),
		Headers:     cloneRuntimeHeaderMap(res.Headers),
	}, err
}

func (i *Instance) HandleIMSBye(ctx context.Context, req voicehost.IMSByeRequest) (voicehost.IMSByeResult, error) {
	svc := i.Service()
	if svc == nil {
		return voicehost.IMSByeResult{Handled: true, StatusCode: 503, Reason: "messaging service is nil"}, errors.New("messaging service is nil")
	}
	res, err := svc.HandleIMSUSSDBye(ctx, messaging.IMSUSSDDialogRequest{
		URI:         req.URI,
		FromURI:     req.FromURI,
		ToURI:       req.ToURI,
		CallID:      req.CallID,
		CSeq:        req.CSeq,
		ContentType: req.ContentType,
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneRuntimeSIPHeaders(req.Headers),
	})
	return voicehost.IMSByeResult{
		Handled:     res.Handled,
		StatusCode:  res.StatusCode,
		Reason:      res.Reason,
		ContentType: res.ContentType,
		Body:        append([]byte(nil), res.Body...),
		Headers:     cloneRuntimeHeaderMap(res.Headers),
	}, err
}

func (i *Instance) State() State {
	if i == nil {
		return State{}
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.state
}

func (i *Instance) SetNotifier(fn func(string)) {
	if i == nil {
		return
	}
	i.mu.Lock()
	i.notifier = fn
	i.mu.Unlock()
}

func (i *Instance) SetSMSNotifier(fn func(deviceID, sender, content string, ts time.Time)) {
	if i == nil {
		return
	}
	i.mu.Lock()
	i.smsNotify = fn
	i.mu.Unlock()
}

func (i *Instance) TriggerMOBIKE(oldIP, newIP string) error {
	if i == nil {
		return errors.New("runtime instance is nil")
	}
	i.mu.RLock()
	tunnel := i.tunnel
	deviceID := i.state.DeviceID
	i.mu.RUnlock()
	reason := "mobike"
	if tunnel != nil {
		res, err := tunnel.MOBIKE(context.Background(), swu.MOBIKERequest{
			DeviceID: deviceID,
			OldIP:    strings.TrimSpace(oldIP),
			NewIP:    strings.TrimSpace(newIP),
			At:       time.Now(),
		})
		if err != nil {
			return fmt.Errorf("MOBIKE update failed: %w", err)
		}
		reason = firstRuntimeNonEmpty(res.Reason, reason)
	}
	i.mu.Lock()
	i.state.LastReason = reason
	i.state.UpdatedAt = time.Now()
	i.mu.Unlock()
	i.notify(context.Background())
	return nil
}

func (i *Instance) Status() string {
	if i == nil {
		return "VoWiFi: STOPPED"
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.stopped {
		return "VoWiFi: STOPPED"
	}
	return "VoWiFi: " + string(i.state.Phase)
}

func (i *Instance) Obs() map[string]interface{} {
	if i == nil {
		return nil
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return map[string]interface{}{
		"device_id": i.state.DeviceID,
		"phase":     string(i.state.Phase),
		"sms_ready": i.state.SMSReady,
		"ims_ready": i.state.IMSReady,
		"updated":   i.state.UpdatedAt,
	}
}

type EventDispatcher = eventhost.Dispatcher
type ModuleEvent = eventhost.Event
type EventSMSReceived = eventhost.SMSReceived
type EventSMSSent = eventhost.SMSSent
type EventUSSDUpdated = eventhost.USSDUpdated
type EventLocalNumberLearned = eventhost.LocalNumberLearned
type EventLogNotify = eventhost.LogNotify

type PrepareStartInput = identity.PrepareStartInput

func firstRuntimeNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func sipDomainRuntime(uri string) string {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(strings.ToLower(uri), "sip:") {
		uri = uri[4:]
	}
	if _, host, ok := strings.Cut(uri, "@"); ok {
		if semi := strings.IndexByte(host, ';'); semi >= 0 {
			host = host[:semi]
		}
		return strings.Trim(strings.TrimSpace(host), "<>")
	}
	return ""
}

func cloneRuntimeSIPHeaders(headers map[string][]string) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func cloneRuntimeHeaderMap(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}

func tunnelManagerForStart(req StartRequest) (swu.TunnelManager, error) {
	if req.TunnelManager != nil {
		return req.TunnelManager, nil
	}
	if req.Dataplane.TunnelManager != nil {
		return req.Dataplane.TunnelManager, nil
	}
	if !explicitUserspaceDataplane(req.Dataplane.Mode) {
		return nil, nil
	}
	factory := req.TunnelManagerFactory
	if factory == nil {
		factory = req.Dataplane.TunnelManagerFactory
	}
	if factory == nil {
		factory = defaultTunnelManagerForStart
	}
	manager, err := factory(req)
	if err != nil {
		return nil, err
	}
	if manager == nil {
		return nil, errors.New("SWU tunnel manager factory returned nil")
	}
	return manager, nil
}

func explicitUserspaceDataplane(mode string) bool {
	return strings.TrimSpace(mode) == swu.DataplaneModeUserspace
}

func defaultTunnelManagerForStart(req StartRequest) (swu.TunnelManager, error) {
	if req.SIM == nil {
		return nil, errors.New("SWU tunnel manager requires SIM AKA provider")
	}
	return swu.NewTUNIKETunnelManager(
		swu.IKEPacketTunnelManagerConfig{
			SIM:                     req.SIM,
			Reauthentication:        req.EAPReauthentication,
			OnReauthenticationState: req.OnEAPReauthenticationState,
		},
		swu.TUNTunnelManagerConfig{
			TUN:                 swu.TUNDeviceConfig{Name: strings.TrimSpace(req.Dataplane.TUNName)},
			DisableRouting:      req.Dataplane.DisableTUNRouting,
			DefaultRoutes:       true,
			ProtectEPDGRoutes:   true,
			MTU:                 req.Dataplane.TUNMTU,
			Addresses:           append([]string(nil), req.Dataplane.TUNAddresses...),
			EPDGRouteExclusions: cloneRuntimeEPDGRouteExclusions(req.Dataplane.TUNEPDGExclusions),
			Routes:              append([]swu.TUNRoute(nil), req.Dataplane.TUNRoutes...),
			Rules:               append([]swu.TUNRule(nil), req.Dataplane.TUNRules...),
		},
	), nil
}

func cloneRuntimeEPDGRouteExclusions(in []swu.EPDGRouteExclusion) []swu.EPDGRouteExclusion {
	out := make([]swu.EPDGRouteExclusion, len(in))
	for i, item := range in {
		out[i] = item
		out[i].Tables = append([]string(nil), item.Tables...)
	}
	return out
}

func buildTunnelConfig(req StartRequest, modem Modem) swu.TunnelConfig {
	cfg := swu.TunnelConfig{
		DeviceID:  strings.TrimSpace(req.DeviceID),
		TraceID:   strings.TrimSpace(req.TraceID),
		Mode:      req.Dataplane.Mode,
		IMSI:      strings.TrimSpace(req.Profile.IMSI),
		MCC:       strings.TrimSpace(req.Profile.MCC),
		MNC:       strings.TrimSpace(req.Profile.MNC),
		IMEI:      strings.TrimSpace(req.Profile.IMEI),
		Proxy:     toSWUProxyConfig(req.Proxy),
		StartedAt: time.Now(),
	}
	if modem != nil {
		cfg.LocalInterface = strings.TrimSpace(modem.DeviceID())
	}
	if req.Prepared != nil {
		cfg.EPDGAddress = strings.TrimSpace(req.Prepared.EPDGAddr)
		cfg.EPDGSource = strings.TrimSpace(req.Prepared.EPDGSource)
		if cfg.IMSI == "" {
			cfg.IMSI = strings.TrimSpace(req.Prepared.Profile.IMSI)
		}
		if cfg.MCC == "" {
			cfg.MCC = strings.TrimSpace(req.Prepared.Profile.MCC)
		}
		if cfg.MNC == "" {
			cfg.MNC = strings.TrimSpace(req.Prepared.Profile.MNC)
		}
		if cfg.IMEI == "" {
			cfg.IMEI = strings.TrimSpace(req.Prepared.Profile.IMEI)
		}
		cfg.Identity = swu.IMSIdentity{
			IMPI:   strings.TrimSpace(req.Prepared.IMSIdentity.IMPI),
			IMPU:   strings.TrimSpace(req.Prepared.IMSIdentity.IMPU),
			Domain: strings.TrimSpace(req.Prepared.IMSIdentity.Domain),
		}
	}
	return cfg
}

func toSWUProxyConfig(p *ProxyConfig) *swu.ProxyConfig {
	if p == nil {
		return nil
	}
	return &swu.ProxyConfig{
		ID:       p.ID,
		URL:      p.URL,
		Address:  p.Address,
		Addr:     p.Addr,
		Username: p.Username,
		Password: p.Password,
		Country:  p.Country,
		Enabled:  p.Enabled,
	}
}
