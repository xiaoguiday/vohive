package voicehost

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/emiago/sipgo/sip"
)

type fakeOutboundAgent struct {
	requests       []OutboundCallRequest
	infos          []DialogInfoRequest
	updates        []DialogUpdateRequest
	reinvites      []DialogReinviteRequest
	terminated     []DialogInfo
	canceled       []DialogInfo
	result         OutboundCallResult
	infoResult     DialogInfoResult
	updateResult   DialogUpdateResult
	reinviteResult DialogReinviteResult
	err            error
	infoErr        error
	updateErr      error
	reinviteErr    error
}

func (a *fakeOutboundAgent) StartOutboundCall(ctx context.Context, req OutboundCallRequest) (OutboundCallResult, error) {
	a.requests = append(a.requests, req)
	if a.err != nil {
		return OutboundCallResult{}, a.err
	}
	if !a.result.Accepted {
		return a.result, nil
	}
	return a.result, nil
}

func (a *fakeOutboundAgent) EndVoiceCall(ctx context.Context, info DialogInfo) error {
	a.terminated = append(a.terminated, info)
	return nil
}

func (a *fakeOutboundAgent) CancelVoiceCall(ctx context.Context, info DialogInfo) error {
	a.canceled = append(a.canceled, info)
	return nil
}

func (a *fakeOutboundAgent) SendDialogInfo(ctx context.Context, req DialogInfoRequest) (DialogInfoResult, error) {
	a.infos = append(a.infos, req)
	if a.infoErr != nil {
		return DialogInfoResult{}, a.infoErr
	}
	return a.infoResult, nil
}

func (a *fakeOutboundAgent) SendDialogUpdate(ctx context.Context, req DialogUpdateRequest) (DialogUpdateResult, error) {
	a.updates = append(a.updates, req)
	if a.updateErr != nil {
		return DialogUpdateResult{}, a.updateErr
	}
	return a.updateResult, nil
}

func (a *fakeOutboundAgent) SendDialogReinvite(ctx context.Context, req DialogReinviteRequest) (DialogReinviteResult, error) {
	a.reinvites = append(a.reinvites, req)
	if a.reinviteErr != nil {
		return DialogReinviteResult{}, a.reinviteErr
	}
	return a.reinviteResult, nil
}

type fakeServerTransaction struct {
	responses []*sip.Response
}

func (tx *fakeServerTransaction) Respond(res *sip.Response) error {
	tx.responses = append(tx.responses, res)
	return nil
}

func (tx *fakeServerTransaction) Terminate()                         {}
func (tx *fakeServerTransaction) Done() <-chan struct{}              { return make(chan struct{}) }
func (tx *fakeServerTransaction) Err() error                         { return nil }
func (tx *fakeServerTransaction) Acks() <-chan *sip.Request          { return make(chan *sip.Request) }
func (tx *fakeServerTransaction) OnTerminate(sip.FnTxTerminate) bool { return true }
func (tx *fakeServerTransaction) OnCancel(sip.FnTxCancel) bool       { return true }

func TestGatewayHandleClientInviteStartsOutboundAgent(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{result: OutboundCallResult{
		Accepted: true,
		LocalSDP: SDPInfo{
			ConnectionIP: "192.0.2.20",
			MediaPort:    5004,
			Payloads:     []int{0, 101},
			Direction:    "sendrecv",
		},
	}}
	g.RegisterAgent("dev-1", agent)
	tx := &fakeServerTransaction{}
	req := newInviteRequest("call-1", "18005551212", sampleSDP("198.51.100.10", 4002))

	g.HandleClientInvite("dev-1", req, tx)

	if len(tx.responses) != 2 {
		t.Fatalf("responses=%d, want 100 and 200", len(tx.responses))
	}
	if tx.responses[0].StatusCode != 100 || tx.responses[1].StatusCode != 200 {
		t.Fatalf("status codes=%d/%d", tx.responses[0].StatusCode, tx.responses[1].StatusCode)
	}
	if len(agent.requests) != 1 {
		t.Fatalf("agent requests=%d", len(agent.requests))
	}
	gotReq := agent.requests[0]
	if gotReq.Callee != "18005551212" || gotReq.RemoteSDP.ConnectionIP != "198.51.100.10" || gotReq.RemoteSDP.MediaPort != 4002 {
		t.Fatalf("outbound request=%+v", gotReq)
	}
	if body := string(tx.responses[1].Body()); !strings.Contains(body, "m=audio 5004 RTP/AVP 0 101") {
		t.Fatalf("200 OK SDP body=%q", body)
	}
	status := g.DeviceStatus("dev-1")
	if status["active_dialogs"] != 1 {
		t.Fatalf("DeviceStatus=%+v, want one active dialog", status)
	}
}

func TestGatewayHandleClientInviteWithoutOutboundAgentReturns503(t *testing.T) {
	g := NewGateway()
	g.RegisterAgent("dev-1", struct{}{})
	tx := &fakeServerTransaction{}
	g.HandleClientInvite("dev-1", newInviteRequest("call-1", "18005551212", sampleSDP("198.51.100.10", 4002)), tx)
	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 503 {
		t.Fatalf("responses=%v", responseCodes(tx.responses))
	}
}

func TestGatewayHandleClientInvitePropagatesOutboundRejectStatus(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{result: OutboundCallResult{
		Accepted:   false,
		StatusCode: 404,
		Reason:     "Not Found",
	}}
	g.RegisterAgent("dev-1", agent)
	tx := &fakeServerTransaction{}

	g.HandleClientInvite("dev-1", newInviteRequest("call-404", "18005551212", sampleSDP("198.51.100.10", 4002)), tx)

	if len(tx.responses) != 2 {
		t.Fatalf("responses=%v, want 100 and 404", responseCodes(tx.responses))
	}
	if tx.responses[0].StatusCode != 100 || tx.responses[1].StatusCode != 404 || tx.responses[1].Reason != "Not Found" {
		t.Fatalf("responses=%d/%d reason=%q", tx.responses[0].StatusCode, tx.responses[1].StatusCode, tx.responses[1].Reason)
	}
	if status := g.DeviceStatus("dev-1"); status["active_dialogs"] != 0 {
		t.Fatalf("DeviceStatus=%+v, want no active dialog", status)
	}
}

func TestGatewayHandleClientInviteRoutesEstablishedDialogReinvite(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{
		result: OutboundCallResult{
			Accepted: true,
			LocalSDP: SDPInfo{ConnectionIP: "192.0.2.20", MediaPort: 5004, Payloads: []int{0, 101}},
		},
		reinviteResult: DialogReinviteResult{
			Accepted:    true,
			StatusCode:  200,
			Reason:      "OK",
			ContentType: "application/sdp",
			Body:        []byte(sampleSDP("203.0.113.40", 49180)),
			Headers:     map[string]string{"X-IMS": "reinvite-ok"},
		},
	}
	g.RegisterAgent("dev-1", agent)
	g.HandleClientInvite("dev-1", newInviteRequest("call-reinvite", "18005551212", sampleSDP("198.51.100.10", 4002)), &fakeServerTransaction{})

	tx := &fakeServerTransaction{}
	req := newInviteRequest("call-reinvite", "18005551212", sampleSDP("198.51.100.20", 4010))
	req.AppendHeader(sip.NewHeader("Session-Expires", "1800"))
	g.HandleClientInvite("dev-1", req, tx)

	if len(agent.requests) != 1 {
		t.Fatalf("StartOutboundCall requests=%d, want only initial INVITE", len(agent.requests))
	}
	if len(agent.reinvites) != 1 {
		t.Fatalf("reinvites=%d", len(agent.reinvites))
	}
	got := agent.reinvites[0]
	if got.DeviceID != "dev-1" || got.CallID != "call-reinvite" || got.ContentType != "application/sdp" ||
		got.Headers["Session-Expires"] != "1800" || !strings.Contains(string(got.Body), "m=audio 4010") {
		t.Fatalf("DialogReinviteRequest=%+v body=%q", got, got.Body)
	}
	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 200 ||
		tx.responses[0].GetHeader("Content-Type").Value() != "application/sdp" ||
		tx.responses[0].GetHeader("X-IMS").Value() != "reinvite-ok" ||
		!strings.Contains(string(tx.responses[0].Body()), "m=audio 49180") {
		t.Fatalf("responses=%+v", tx.responses)
	}
}

func TestGatewayHandleClientByeTerminatesDialog(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{result: OutboundCallResult{Accepted: true, LocalSDP: SDPInfo{ConnectionIP: "192.0.2.20", MediaPort: 5004}}}
	g.RegisterAgent("dev-1", agent)
	g.HandleClientInvite("dev-1", newInviteRequest("call-1", "18005551212", sampleSDP("198.51.100.10", 4002)), &fakeServerTransaction{})

	tx := &fakeServerTransaction{}
	g.HandleClientBye("dev-1", newByeRequest("call-1"), tx)

	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 200 {
		t.Fatalf("BYE responses=%v", responseCodes(tx.responses))
	}
	if len(agent.terminated) != 1 || agent.terminated[0].State != DialogStateTerminated {
		t.Fatalf("terminated=%+v", agent.terminated)
	}
	if status := g.DeviceStatus("dev-1"); status["active_dialogs"] != 0 {
		t.Fatalf("DeviceStatus=%+v, want no active dialog", status)
	}
}

func TestGatewayHandleClientCancelCancelsEarlyDialog(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{}
	g.RegisterAgent("dev-1", agent)
	g.recordDialog(DialogInfo{DeviceID: "dev-1", CallID: "call-cancel", Callee: "18005551212", State: DialogStateEarly})

	tx := &fakeServerTransaction{}
	g.HandleClientCancel("dev-1", newCancelRequest("call-cancel"), tx)

	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 200 {
		t.Fatalf("CANCEL responses=%v", responseCodes(tx.responses))
	}
	if len(agent.canceled) != 1 || agent.canceled[0].CallID != "call-cancel" || agent.canceled[0].State != DialogStateTerminated {
		t.Fatalf("canceled=%+v", agent.canceled)
	}
	if status := g.DeviceStatus("dev-1"); status["active_dialogs"] != 0 {
		t.Fatalf("DeviceStatus=%+v, want no active dialog", status)
	}
}

func TestGatewayHandleClientInfoSendsDialogInfo(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{infoResult: DialogInfoResult{
		Accepted:    true,
		StatusCode:  202,
		Reason:      "Accepted",
		ContentType: "application/dtmf-relay",
		Body:        []byte("ok"),
		Headers:     map[string]string{"X-IMS": "info-ok"},
	}}
	g.RegisterAgent("dev-1", agent)
	tx := &fakeServerTransaction{}
	req := newInfoRequest("call-info", "application/dtmf-relay", "Signal=2\r\nDuration=100\r\n")
	req.AppendHeader(sip.NewHeader("Info-Package", "dtmf"))
	req.AppendHeader(sip.NewHeader("X-Client", "info"))

	g.HandleClientInfo("dev-1", req, tx)

	if len(agent.infos) != 1 {
		t.Fatalf("infos=%d", len(agent.infos))
	}
	got := agent.infos[0]
	if got.DeviceID != "dev-1" || got.CallID != "call-info" || got.ContentType != "application/dtmf-relay" ||
		got.InfoPackage != "dtmf" || got.Headers["X-Client"] != "info" || !strings.Contains(string(got.Body), "Signal=2") {
		t.Fatalf("DialogInfoRequest=%+v body=%q", got, got.Body)
	}
	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 202 || tx.responses[0].Reason != "Accepted" ||
		string(tx.responses[0].Body()) != "ok" || tx.responses[0].GetHeader("X-IMS").Value() != "info-ok" {
		t.Fatalf("responses=%+v", tx.responses)
	}
}

func TestGatewayHandleClientUpdateSendsDialogUpdate(t *testing.T) {
	g := NewGateway()
	agent := &fakeOutboundAgent{updateResult: DialogUpdateResult{
		Accepted:    true,
		StatusCode:  200,
		Reason:      "OK",
		ContentType: "application/sdp",
		Body:        []byte(sampleSDP("203.0.113.44", 49180)),
		Headers:     map[string]string{"X-IMS": "update-ok"},
	}}
	g.RegisterAgent("dev-1", agent)
	tx := &fakeServerTransaction{}
	req := newUpdateRequest("call-update", sampleSDP("198.51.100.20", 4010))
	req.AppendHeader(sip.NewHeader("Session-Expires", "1800"))

	g.HandleClientUpdate("dev-1", req, tx)

	if len(agent.updates) != 1 {
		t.Fatalf("updates=%d", len(agent.updates))
	}
	got := agent.updates[0]
	if got.DeviceID != "dev-1" || got.CallID != "call-update" || got.ContentType != "application/sdp" ||
		got.Headers["Session-Expires"] != "1800" || !strings.Contains(string(got.Body), "m=audio 4010") {
		t.Fatalf("DialogUpdateRequest=%+v body=%q", got, got.Body)
	}
	if len(tx.responses) != 1 || tx.responses[0].StatusCode != 200 ||
		tx.responses[0].GetHeader("Content-Type").Value() != "application/sdp" ||
		tx.responses[0].GetHeader("X-IMS").Value() != "update-ok" ||
		!strings.Contains(string(tx.responses[0].Body()), "m=audio 49180") {
		t.Fatalf("responses=%+v", tx.responses)
	}
}

func TestParseAndBuildSDP(t *testing.T) {
	info, err := ParseSDP([]byte(sampleSDP("203.0.113.8", 49170) + "a=rtcp:49171 IN IP4 203.0.113.8\r\n"))
	if err != nil {
		t.Fatalf("ParseSDP() error = %v", err)
	}
	if info.ConnectionIP != "203.0.113.8" || info.MediaPort != 49170 || info.RTCPIP != "203.0.113.8" || info.RTCPPort != 49171 || info.Direction != "sendrecv" {
		t.Fatalf("info=%+v", info)
	}
	if len(info.Payloads) != 3 || info.Payloads[0] != 0 || info.Payloads[2] != 101 {
		t.Fatalf("payloads=%+v", info.Payloads)
	}
	answer := string(BuildSDPAnswer(SDPInfo{ConnectionIP: "192.0.2.2", MediaPort: 6000, RTCPPort: 6001, Payloads: []int{8}, Direction: "recvonly"}))
	if !strings.Contains(answer, "m=audio 6000 RTP/AVP 8") || !strings.Contains(answer, "a=rtcp:6001 IN IP4 192.0.2.2") || !strings.Contains(answer, "a=recvonly") {
		t.Fatalf("answer=%q", answer)
	}
}

func newInviteRequest(callID, callee, sdp string) *sip.Request {
	req := sip.NewRequest(sip.INVITE, sip.Uri{Scheme: "sip", User: callee, Host: "ims.example"})
	appendCommonHeaders(req, callID, callee)
	req.SetBody([]byte(sdp))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	return req
}

func newByeRequest(callID string) *sip.Request {
	req := sip.NewRequest(sip.BYE, sip.Uri{Scheme: "sip", User: "18005551212", Host: "ims.example"})
	appendCommonHeaders(req, callID, "18005551212")
	return req
}

func newCancelRequest(callID string) *sip.Request {
	req := sip.NewRequest(sip.CANCEL, sip.Uri{Scheme: "sip", User: "18005551212", Host: "ims.example"})
	appendCommonHeaders(req, callID, "18005551212")
	return req
}

func newInfoRequest(callID, contentType, body string) *sip.Request {
	req := sip.NewRequest(sip.INFO, sip.Uri{Scheme: "sip", User: "18005551212", Host: "ims.example"})
	appendCommonHeaders(req, callID, "18005551212")
	req.SetBody([]byte(body))
	if strings.TrimSpace(contentType) != "" {
		req.AppendHeader(sip.NewHeader("Content-Type", contentType))
	}
	return req
}

func newUpdateRequest(callID, sdp string) *sip.Request {
	req := sip.NewRequest(sip.UPDATE, sip.Uri{Scheme: "sip", User: "18005551212", Host: "ims.example"})
	appendCommonHeaders(req, callID, "18005551212")
	if strings.TrimSpace(sdp) != "" {
		req.SetBody([]byte(sdp))
		req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	}
	return req
}

func appendCommonHeaders(req *sip.Request, callID, user string) {
	req.AppendHeader(sip.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1:5060;branch=z9hG4bK-test"))
	req.AppendHeader(sip.NewHeader("From", "<sip:user@example>;tag=fromtag"))
	req.AppendHeader(sip.NewHeader("To", "<sip:"+user+"@ims.example>"))
	req.AppendHeader(sip.NewHeader("Call-ID", callID))
	req.AppendHeader(sip.NewHeader("CSeq", "1 "+string(req.Method)))
	req.AppendHeader(sip.NewHeader("Max-Forwards", "70"))
}

func sampleSDP(ip string, port int) string {
	return "v=0\r\n" +
		"o=user 0 0 IN IP4 " + ip + "\r\n" +
		"s=-\r\n" +
		"c=IN IP4 " + ip + "\r\n" +
		"t=0 0\r\n" +
		"m=audio " + strconv.Itoa(port) + " RTP/AVP 0 8 101\r\n" +
		"a=sendrecv\r\n"
}

func responseCodes(responses []*sip.Response) []int {
	out := make([]int, len(responses))
	for i, res := range responses {
		out[i] = res.StatusCode
	}
	return out
}
