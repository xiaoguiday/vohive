package voicehost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
)

var ErrIMSInboundAgentNotReady = errors.New("ims inbound voice agent not ready")

type IMSInboundAgent struct {
	ClientTransport  voiceclient.SIPRequestTransport
	Profile          voiceclient.IMSProfile
	Registration     voiceclient.RegistrationBinding
	ClientContactURI string
	LocalContactURI  string
	LocalTag         string
	UserAgent        string
	MediaRelay       *RTPRelayConfig

	mu      sync.Mutex
	dialogs map[string]imsInboundDialogState
}

type InboundCallRequest struct {
	DeviceID        string
	CallID          string
	CallerURI       string
	CalleeURI       string
	RemoteTag       string
	RemoteTargetURI string
	CSeq            int
	RemoteSDP       SDPInfo
	RawSDP          []byte
	Headers         map[string][]string
}

type InboundCallResult struct {
	Accepted   bool
	StatusCode int
	Reason     string
	LocalSDP   SDPInfo
	RawSDP     []byte
}

type InboundDialogRequest struct {
	CallID    string
	CSeq      int
	RawSDP    []byte
	RemoteSDP SDPInfo
	Headers   map[string][]string
	RAck      string
}

type imsInboundDialogState struct {
	clientCfg  voiceclient.DialogRequestConfig
	inviteCSeq int
	relay      *RTPRelaySession
}

func (a *IMSInboundAgent) HandleInboundInvite(ctx context.Context, req InboundCallRequest) (InboundCallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client voice transport unavailable"}, ErrIMSInboundAgentNotReady
	}
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		return InboundCallResult{Accepted: false, StatusCode: 400, Reason: "Call-ID empty"}, errors.New("Call-ID is empty")
	}
	if state, ok := a.inboundDialog(callID); ok {
		return a.handleInboundReinvite(ctx, req, state)
	}
	callerURI := strings.TrimSpace(req.CallerURI)
	if callerURI == "" {
		return InboundCallResult{Accepted: false, StatusCode: 400, Reason: "caller URI empty"}, errors.New("caller URI is empty")
	}
	calleeURI := firstVoiceNonEmpty(req.CalleeURI, a.Registration.PublicIdentity, a.Profile.IMPU)
	if calleeURI == "" {
		return InboundCallResult{Accepted: false, StatusCode: 400, Reason: "callee URI empty"}, errors.New("callee URI is empty")
	}
	clientTarget := strings.TrimSpace(a.ClientContactURI)
	if clientTarget == "" {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client contact unavailable"}, ErrIMSInboundAgentNotReady
	}
	remoteSDP, offerBody, err := inboundOfferSDP(req)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid IMS SDP offer"}, err
	}
	var relay *RTPRelaySession
	if a.MediaRelay != nil {
		createdRelay, relayErr := NewRTPRelaySessionForIMSRemote(ctx, *a.MediaRelay, remoteSDP)
		if relayErr != nil {
			return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay setup failed"}, relayErr
		}
		relay = createdRelay
		offerBody = RewriteSDPMediaEndpoint(offerBody, relay.ClientEndpoint())
	}
	closeRelayOnError := true
	defer func() {
		if closeRelayOnError && relay != nil {
			_ = relay.Close()
		}
	}()
	cfg := voiceclient.DialogRequestConfig{
		Profile:         a.Profile,
		Registration:    voiceclient.RegistrationBinding{},
		LocalURI:        callerURI,
		ContactURI:      firstVoiceNonEmpty(a.LocalContactURI, a.Registration.ContactURI, a.Profile.IMPU),
		RemoteURI:       calleeURI,
		RemoteTargetURI: clientTarget,
		CallID:          callID,
		LocalTag:        firstVoiceNonEmpty(req.RemoteTag, a.LocalTag, "ims"),
		CSeq:            inboundCSeq(req.CSeq),
		UserAgent:       firstVoiceNonEmpty(a.UserAgent, a.Profile.UserAgent, "vowifi-go"),
	}
	invite, err := voiceclient.BuildInviteRequest(cfg, offerBody)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "build client INVITE failed"}, err
	}
	a.storeInboundDialog(callID, imsInboundDialogState{clientCfg: cfg, inviteCSeq: cfg.CSeq, relay: relay})
	resp, err := a.roundTripClientInvite(ctx, invite)
	if err != nil {
		a.deleteInboundDialog(callID)
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client INVITE failed"}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 300 {
			if err := a.ackRejectedClientInvite(ctx, cfg, invite, resp); err != nil {
				a.deleteInboundDialog(callID)
				return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "client INVITE rejected ACK failed"}, err
			}
		}
		a.deleteInboundDialog(callID)
		return InboundCallResult{
			Accepted:   false,
			StatusCode: inboundStatusCode(resp.StatusCode, 486),
			Reason:     firstVoiceNonEmpty(resp.Reason, "Busy Here"),
		}, nil
	}
	localSDP, err := ParseSDP(resp.Body)
	if err != nil {
		a.deleteInboundDialog(callID)
		return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid client SDP answer"}, err
	}
	answerBody := append([]byte(nil), resp.Body...)
	if relay != nil {
		if err := relay.SetClientRemote(localSDP); err != nil {
			a.deleteInboundDialog(callID)
			return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay client setup failed"}, err
		}
		answerBody = RewriteSDPMediaEndpoint(resp.Body, relay.IMSEndpoint())
		localSDP, err = ParseSDP(answerBody)
		if err != nil {
			a.deleteInboundDialog(callID)
			return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid RTP relay SDP answer"}, err
		}
	}
	cfg.RemoteTag = sipHeaderTag(firstVoiceHeader(resp.Headers, "To"))
	if contact := sipHeaderURI(firstVoiceHeader(resp.Headers, "Contact")); contact != "" {
		cfg.RemoteTargetURI = contact
	}
	if routeSet := recordRouteSet(resp.Headers); len(routeSet) > 0 {
		cfg.RouteSet = routeSet
	}
	a.storeInboundDialog(callID, imsInboundDialogState{clientCfg: cfg, inviteCSeq: inboundCSeq(req.CSeq), relay: relay})
	closeRelayOnError = false
	return InboundCallResult{
		Accepted:   true,
		StatusCode: inboundStatusCode(resp.StatusCode, 200),
		Reason:     firstVoiceNonEmpty(resp.Reason, "OK"),
		LocalSDP:   localSDP,
		RawSDP:     answerBody,
	}, nil
}

func (a *IMSInboundAgent) handleInboundReinvite(ctx context.Context, req InboundCallRequest, state imsInboundDialogState) (InboundCallResult, error) {
	callID := strings.TrimSpace(req.CallID)
	reinviteCSeq := inboundCSeq(req.CSeq)
	cfg := state.clientCfg
	cfg.CSeq = reinviteCSeq
	body := append([]byte(nil), req.RawSDP...)
	if len(body) > 0 && state.relay != nil {
		remoteSDP, offerBody, err := inboundOfferSDP(req)
		if err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid IMS re-INVITE SDP"}, err
		}
		if err := state.relay.SetIMSRemote(remoteSDP); err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay IMS re-INVITE failed"}, err
		}
		body = RewriteSDPMediaEndpoint(offerBody, state.relay.ClientEndpoint())
	}
	invite, err := voiceclient.BuildInviteRequest(cfg, body)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "build client re-INVITE failed"}, err
	}
	state.clientCfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, reinviteCSeq)
	a.storeInboundDialog(callID, state)
	resp, err := a.roundTripClientInvite(ctx, invite)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client re-INVITE failed"}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 300 {
			if err := a.ackRejectedClientInvite(ctx, cfg, invite, resp); err != nil {
				return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "client re-INVITE rejected ACK failed"}, err
			}
		}
		return InboundCallResult{Accepted: false, StatusCode: inboundStatusCode(resp.StatusCode, 488), Reason: firstVoiceNonEmpty(resp.Reason, "re-INVITE rejected")}, nil
	}
	result := InboundCallResult{Accepted: true, StatusCode: inboundStatusCode(resp.StatusCode, 200), Reason: firstVoiceNonEmpty(resp.Reason, "OK"), RawSDP: append([]byte(nil), resp.Body...)}
	if len(resp.Body) > 0 {
		localSDP, err := ParseSDP(resp.Body)
		if err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid client re-INVITE SDP answer"}, err
		}
		if state.relay != nil {
			if err := state.relay.SetClientRemote(localSDP); err != nil {
				return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay client re-INVITE failed"}, err
			}
			result.RawSDP = RewriteSDPMediaEndpoint(resp.Body, state.relay.IMSEndpoint())
			localSDP, err = ParseSDP(result.RawSDP)
			if err != nil {
				return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid RTP relay re-INVITE SDP answer"}, err
			}
		}
		result.LocalSDP = localSDP
	}
	if contact := sipHeaderURI(firstVoiceHeader(resp.Headers, "Contact")); contact != "" {
		cfg.RemoteTargetURI = contact
	}
	cfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, reinviteCSeq)
	state.clientCfg = cfg
	state.inviteCSeq = reinviteCSeq
	a.storeInboundDialog(callID, state)
	return result, nil
}

func (a *IMSInboundAgent) AckInboundCall(ctx context.Context, info DialogInfo) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return ErrIMSInboundAgentNotReady
	}
	state, ok := a.inboundDialog(info.CallID)
	if !ok {
		return nil
	}
	cfg := state.clientCfg
	if state.inviteCSeq > 0 {
		cfg.CSeq = state.inviteCSeq
	}
	ack, err := voiceclient.BuildAckRequest(cfg)
	if err != nil {
		return err
	}
	return a.ClientTransport.WriteRequest(ctx, ack)
}

func (a *IMSInboundAgent) ackRejectedClientInvite(ctx context.Context, cfg voiceclient.DialogRequestConfig, invite voiceclient.SIPRequestMessage, resp voiceclient.SIPResponse) error {
	ackCfg := cfg
	ackCfg.RemoteTag = firstVoiceNonEmpty(sipHeaderTag(firstVoiceHeader(resp.Headers, "To")), cfg.RemoteTag)
	ack, err := voiceclient.BuildAckRequest(ackCfg)
	if err != nil {
		return err
	}
	copyDialogHeader(ack.Headers, invite.Headers, "Via")
	return a.ClientTransport.WriteRequest(ctx, ack)
}

func (a *IMSInboundAgent) HandleInboundUpdate(ctx context.Context, req InboundDialogRequest) (InboundCallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client voice transport unavailable"}, ErrIMSInboundAgentNotReady
	}
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		return InboundCallResult{Accepted: false, StatusCode: 400, Reason: "Call-ID empty"}, errors.New("Call-ID is empty")
	}
	state, ok := a.inboundDialog(callID)
	if !ok {
		return InboundCallResult{Accepted: false, StatusCode: 481, Reason: "dialog not found"}, nil
	}
	cfg := state.clientCfg
	updateCSeq := inboundCSeq(req.CSeq)
	cfg.CSeq = updateCSeq
	body := append([]byte(nil), req.RawSDP...)
	if len(body) > 0 && state.relay != nil {
		remoteSDP, offerBody, err := inboundDialogSDP(req)
		if err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid IMS UPDATE SDP"}, err
		}
		if err := state.relay.SetIMSRemote(remoteSDP); err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay IMS update failed"}, err
		}
		body = RewriteSDPMediaEndpoint(offerBody, state.relay.ClientEndpoint())
	}
	update, err := voiceclient.BuildUpdateRequest(cfg, body)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "build client UPDATE failed"}, err
	}
	state.clientCfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, updateCSeq)
	a.storeInboundDialog(callID, state)
	resp, err := a.ClientTransport.RoundTripRequest(ctx, update)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client UPDATE failed"}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return InboundCallResult{Accepted: false, StatusCode: inboundStatusCode(resp.StatusCode, 488), Reason: firstVoiceNonEmpty(resp.Reason, "UPDATE rejected")}, nil
	}
	result := InboundCallResult{Accepted: true, StatusCode: inboundStatusCode(resp.StatusCode, 200), Reason: firstVoiceNonEmpty(resp.Reason, "OK"), RawSDP: append([]byte(nil), resp.Body...)}
	if len(resp.Body) > 0 {
		localSDP, err := ParseSDP(resp.Body)
		if err != nil {
			return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid client UPDATE SDP answer"}, err
		}
		if state.relay != nil {
			if err := state.relay.SetClientRemote(localSDP); err != nil {
				return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "RTP relay client update failed"}, err
			}
			result.RawSDP = RewriteSDPMediaEndpoint(resp.Body, state.relay.IMSEndpoint())
			localSDP, err = ParseSDP(result.RawSDP)
			if err != nil {
				return InboundCallResult{Accepted: false, StatusCode: 488, Reason: "invalid RTP relay UPDATE SDP answer"}, err
			}
		}
		result.LocalSDP = localSDP
	}
	if contact := sipHeaderURI(firstVoiceHeader(resp.Headers, "Contact")); contact != "" {
		cfg.RemoteTargetURI = contact
	}
	cfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, updateCSeq)
	state.clientCfg = cfg
	a.storeInboundDialog(callID, state)
	return result, nil
}

func (a *IMSInboundAgent) HandleInboundPrack(ctx context.Context, req InboundDialogRequest) (InboundCallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client voice transport unavailable"}, ErrIMSInboundAgentNotReady
	}
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		return InboundCallResult{Accepted: false, StatusCode: 400, Reason: "Call-ID empty"}, errors.New("Call-ID is empty")
	}
	state, ok := a.inboundDialog(callID)
	if !ok {
		return InboundCallResult{Accepted: false, StatusCode: 481, Reason: "dialog not found"}, nil
	}
	cfg := state.clientCfg
	prackCSeq := inboundCSeq(req.CSeq)
	cfg.CSeq = prackCSeq
	prack, err := voiceclient.BuildPrackRequest(cfg, firstVoiceNonEmpty(req.RAck, firstVoiceHeader(req.Headers, "RAck")))
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 500, Reason: "build client PRACK failed"}, err
	}
	state.clientCfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, prackCSeq)
	a.storeInboundDialog(callID, state)
	resp, err := a.ClientTransport.RoundTripRequest(ctx, prack)
	if err != nil {
		return InboundCallResult{Accepted: false, StatusCode: 503, Reason: "client PRACK failed"}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return InboundCallResult{Accepted: false, StatusCode: inboundStatusCode(resp.StatusCode, 500), Reason: firstVoiceNonEmpty(resp.Reason, "PRACK rejected")}, nil
	}
	return InboundCallResult{Accepted: true, StatusCode: inboundStatusCode(resp.StatusCode, 200), Reason: firstVoiceNonEmpty(resp.Reason, "OK"), RawSDP: append([]byte(nil), resp.Body...)}, nil
}

func (a *IMSInboundAgent) HandleInboundInfo(ctx context.Context, req IMSInfoRequest) (IMSInfoResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return IMSInfoResult{Handled: true, StatusCode: 503, Reason: "client voice transport unavailable"}, ErrIMSInboundAgentNotReady
	}
	callID := strings.TrimSpace(req.CallID)
	if callID == "" {
		return IMSInfoResult{Handled: true, StatusCode: 400, Reason: "Call-ID empty"}, errors.New("Call-ID is empty")
	}
	state, ok := a.inboundDialog(callID)
	if !ok {
		return IMSInfoResult{Handled: true, StatusCode: 481, Reason: "dialog not found"}, nil
	}
	cfg := state.clientCfg
	infoCSeq := inboundCSeq(req.CSeq)
	cfg.CSeq = infoCSeq
	info, err := voiceclient.BuildInfoRequest(cfg, req.ContentType, req.Body)
	if err != nil {
		return IMSInfoResult{Handled: true, StatusCode: 500, Reason: "build client INFO failed"}, err
	}
	applyIncomingInfoHeaders(info.Headers, req.InfoPackage, req.Headers)
	state.clientCfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, infoCSeq)
	a.storeInboundDialog(callID, state)
	resp, err := a.ClientTransport.RoundTripRequest(ctx, info)
	if err != nil {
		return IMSInfoResult{Handled: true, StatusCode: 503, Reason: "client INFO failed"}, err
	}
	if contact := sipHeaderURI(firstVoiceHeader(resp.Headers, "Contact")); contact != "" {
		cfg.RemoteTargetURI = contact
		cfg.CSeq = maxInboundCSeq(state.clientCfg.CSeq, infoCSeq)
		state.clientCfg = cfg
		a.storeInboundDialog(callID, state)
	}
	return IMSInfoResult{
		Handled:     true,
		StatusCode:  inboundStatusCode(resp.StatusCode, 500),
		Reason:      firstVoiceNonEmpty(resp.Reason, "OK"),
		ContentType: firstVoiceHeader(resp.Headers, "Content-Type"),
		Body:        append([]byte(nil), resp.Body...),
		Headers:     firstValueSIPHeaders(resp.Headers),
	}, nil
}

func (a *IMSInboundAgent) CancelInboundCall(ctx context.Context, info DialogInfo) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return ErrIMSInboundAgentNotReady
	}
	state, ok := a.inboundDialog(info.CallID)
	if !ok {
		return nil
	}
	cancel, err := voiceclient.BuildCancelRequest(state.clientCfg)
	if err != nil {
		return err
	}
	resp, err := a.ClientTransport.RoundTripRequest(ctx, cancel)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("client CANCEL rejected: %d %s", resp.StatusCode, strings.TrimSpace(resp.Reason))
	}
	a.closeInboundDialog(strings.TrimSpace(info.CallID))
	return nil
}

func (a *IMSInboundAgent) EndInboundCall(ctx context.Context, info DialogInfo) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.ClientTransport == nil {
		return ErrIMSInboundAgentNotReady
	}
	state, ok := a.inboundDialog(info.CallID)
	if !ok {
		return nil
	}
	cfg := state.clientCfg
	cfg.CSeq = nextInboundClientCSeq(cfg.CSeq)
	bye, err := voiceclient.BuildByeRequest(cfg)
	if err != nil {
		return err
	}
	state.clientCfg = cfg
	a.storeInboundDialog(strings.TrimSpace(info.CallID), state)
	resp, err := a.ClientTransport.RoundTripRequest(ctx, bye)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("client BYE rejected: %d %s", resp.StatusCode, strings.TrimSpace(resp.Reason))
	}
	a.closeInboundDialog(strings.TrimSpace(info.CallID))
	return nil
}

func inboundOfferSDP(req InboundCallRequest) (SDPInfo, []byte, error) {
	body := append([]byte(nil), req.RawSDP...)
	info := req.RemoteSDP
	if info.MediaPort <= 0 || strings.TrimSpace(info.ConnectionIP) == "" {
		parsed, err := ParseSDP(body)
		if err != nil {
			return SDPInfo{}, nil, err
		}
		info = parsed
	}
	if len(body) == 0 {
		body = BuildSDPAnswer(info)
	}
	return info, body, nil
}

func inboundDialogSDP(req InboundDialogRequest) (SDPInfo, []byte, error) {
	body := append([]byte(nil), req.RawSDP...)
	info := req.RemoteSDP
	if info.MediaPort <= 0 || strings.TrimSpace(info.ConnectionIP) == "" {
		parsed, err := ParseSDP(body)
		if err != nil {
			return SDPInfo{}, nil, err
		}
		info = parsed
	}
	if len(body) == 0 {
		body = BuildSDPAnswer(info)
	}
	return info, body, nil
}

func inboundCSeq(cseq int) int {
	if cseq <= 0 {
		return 1
	}
	return cseq
}

func nextInboundClientCSeq(cseq int) int {
	if cseq <= 0 {
		return 1
	}
	return cseq + 1
}

func maxInboundCSeq(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func inboundStatusCode(code, fallback int) int {
	if code > 0 {
		return code
	}
	return fallback
}

func (a *IMSInboundAgent) storeInboundDialog(callID string, state imsInboundDialogState) {
	if a == nil || strings.TrimSpace(callID) == "" {
		return
	}
	a.mu.Lock()
	if a.dialogs == nil {
		a.dialogs = make(map[string]imsInboundDialogState)
	}
	a.dialogs[strings.TrimSpace(callID)] = state
	a.mu.Unlock()
}

func (a *IMSInboundAgent) inboundDialog(callID string) (imsInboundDialogState, bool) {
	if a == nil || strings.TrimSpace(callID) == "" {
		return imsInboundDialogState{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.dialogs[strings.TrimSpace(callID)]
	return state, ok
}

func (a *IMSInboundAgent) deleteInboundDialog(callID string) {
	if a == nil || strings.TrimSpace(callID) == "" {
		return
	}
	a.mu.Lock()
	state, ok := a.dialogs[strings.TrimSpace(callID)]
	if ok {
		delete(a.dialogs, strings.TrimSpace(callID))
	}
	a.mu.Unlock()
	if ok && state.relay != nil {
		_ = state.relay.Close()
	}
}

func (a *IMSInboundAgent) closeInboundDialog(callID string) {
	a.deleteInboundDialog(callID)
}

func (a *IMSInboundAgent) roundTripClientInvite(ctx context.Context, invite voiceclient.SIPRequestMessage) (voiceclient.SIPResponse, error) {
	if a == nil || a.ClientTransport == nil {
		return voiceclient.SIPResponse{}, ErrIMSInboundAgentNotReady
	}
	if inviteTransport, ok := a.ClientTransport.(voiceclient.SIPInviteTransport); ok {
		return inviteTransport.RoundTripInvite(ctx, invite, nil)
	}
	return a.ClientTransport.RoundTripRequest(ctx, invite)
}
