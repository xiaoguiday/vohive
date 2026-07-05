package voicehost

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
)

type IMSInboundWireServer struct {
	Agent           *IMSInboundAgent
	MessageHandler  IMSMessageHandler
	InfoHandler     IMSInfoHandler
	ByeHandler      IMSByeHandler
	ContactURI      string
	LocalTag        string
	UserAgent       string
	ResponseHeaders map[string]string
	ReadTimeout     time.Duration
	TransactionTTL  time.Duration

	mu           sync.Mutex
	transactions map[string]imsInboundWireTransaction
}

type IMSInboundWireResponse struct {
	StatusCode int
	Reason     string
	Headers    map[string]string
	Body       []byte
	NoResponse bool
}

type imsInboundWireTransaction struct {
	responses []IMSInboundWireResponse
	expires   time.Time
}

func (s *IMSInboundWireServer) ServePacket(ctx context.Context, pc net.PacketConn) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if pc == nil {
		return ErrIMSInboundAgentNotReady
	}
	buf := make([]byte, 65535)
	for {
		if err := pc.SetReadDeadline(time.Now().Add(s.readTimeout())); err != nil {
			return err
		}
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isTimeout(err) {
				continue
			}
			return err
		}
		raw := append([]byte(nil), buf[:n]...)
		go s.handlePacket(ctx, pc, addr, raw)
	}
}

func (s *IMSInboundWireServer) ServeListener(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if ln == nil {
		return ErrIMSInboundAgentNotReady
	}
	for {
		if deadline, ok := ln.(interface{ SetDeadline(time.Time) error }); ok {
			if err := deadline.SetDeadline(time.Now().Add(s.readTimeout())); err != nil {
				return err
			}
		}
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if isTimeout(err) {
				continue
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *IMSInboundWireServer) HandleRequest(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	key := wireTransactionKey(req)
	if method != "ACK" && key != "" {
		if responses, ok := s.cachedTransaction(key); ok {
			return responses, nil
		}
	}
	var responses []IMSInboundWireResponse
	var err error
	switch method {
	case "INVITE":
		responses, err = s.handleInvite(ctx, req)
	case "ACK":
		if s == nil || s.Agent == nil {
			return nil, ErrIMSInboundAgentNotReady
		}
		return nil, s.Agent.AckInboundCall(ctx, DialogInfo{CallID: wireCallID(req)})
	case "UPDATE":
		responses, err = s.handleUpdate(ctx, req)
	case "PRACK":
		responses, err = s.handlePrack(ctx, req)
	case "INFO":
		responses, err = s.handleInfo(ctx, req)
	case "MESSAGE":
		responses, err = s.handleMessage(ctx, req)
	case "OPTIONS":
		responses = []IMSInboundWireResponse{s.withResponseHeaders(s.optionsResponse())}
	case "BYE":
		if handledResponses, handledErr, handled := s.tryHandleBye(ctx, req); handled {
			responses, err = handledResponses, handledErr
			break
		}
		if s == nil || s.Agent == nil {
			responses, err = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(503, "Service Unavailable"))}, ErrIMSInboundAgentNotReady
			break
		}
		if callErr := s.Agent.EndInboundCall(ctx, DialogInfo{CallID: wireCallID(req)}); callErr != nil {
			responses, err = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(500, callErr.Error()))}, callErr
			break
		}
		responses = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(200, "OK"))}
	case "CANCEL":
		if s == nil || s.Agent == nil {
			responses, err = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(503, "Service Unavailable"))}, ErrIMSInboundAgentNotReady
			break
		}
		if callErr := s.Agent.CancelInboundCall(ctx, DialogInfo{CallID: wireCallID(req)}); callErr != nil {
			responses, err = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(500, callErr.Error()))}, callErr
			break
		}
		responses = []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(200, "OK"))}
	default:
		resp := wireResponse(405, "Method Not Allowed")
		resp.Headers["Allow"] = s.allowHeader()
		responses = []IMSInboundWireResponse{s.withResponseHeaders(resp)}
	}
	if key != "" && len(responses) > 0 {
		s.storeTransaction(key, responses)
	}
	return responses, err
}

func (s *IMSInboundWireServer) handleInfo(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	infoReq := IMSInfoRequest{
		URI:         strings.TrimSpace(req.URI),
		FromURI:     wireHeaderURI(req, "From"),
		ToURI:       wireCalleeURI(req),
		CallID:      wireCallID(req),
		CSeq:        wireCSeq(req),
		ContentType: firstVoiceHeader(req.Headers, "Content-Type"),
		InfoPackage: firstVoiceHeader(req.Headers, "Info-Package"),
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneSIPHeaders(req.Headers),
	}
	if s != nil && s.InfoHandler != nil {
		result, err := s.InfoHandler.HandleIMSInfo(ctx, infoReq)
		if result.Handled || err != nil {
			return s.infoResultResponse(result, err), err
		}
		if isUSSDInfoRequest(infoReq) {
			resp := wireResponse(415, "Unsupported Media Type")
			return []IMSInboundWireResponse{s.withResponseHeaders(resp)}, nil
		}
	}
	if s != nil && s.Agent != nil {
		result, err := s.Agent.HandleInboundInfo(ctx, infoReq)
		return s.infoResultResponse(result, err), err
	}
	resp := wireResponse(415, "Unsupported Media Type")
	if s == nil || s.InfoHandler == nil {
		resp = wireResponse(405, "Method Not Allowed")
		resp.Headers["Allow"] = s.allowHeader()
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(resp)}, nil
}

func (s *IMSInboundWireServer) infoResultResponse(result IMSInfoResult, err error) []IMSInboundWireResponse {
	resp := wireResponse(inboundStatusCode(result.StatusCode, 200), firstVoiceNonEmpty(result.Reason, "OK"))
	if err != nil && result.StatusCode <= 0 {
		resp = wireResponse(500, firstVoiceNonEmpty(result.Reason, err.Error(), "Server Internal Error"))
	}
	for key, value := range result.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			resp.Headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if len(result.Body) > 0 {
		resp.Body = append([]byte(nil), result.Body...)
		resp.Headers["Content-Type"] = firstVoiceNonEmpty(result.ContentType, "application/octet-stream")
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(resp)}
}

func isUSSDInfoRequest(req IMSInfoRequest) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(req.ContentType)), "vnd.3gpp.ussd") ||
		strings.EqualFold(strings.TrimSpace(req.InfoPackage), "g.3gpp.ussd")
}

func (s *IMSInboundWireServer) tryHandleBye(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error, bool) {
	if s == nil || s.ByeHandler == nil {
		return nil, nil, false
	}
	result, err := s.ByeHandler.HandleIMSBye(ctx, IMSByeRequest{
		URI:         strings.TrimSpace(req.URI),
		FromURI:     wireHeaderURI(req, "From"),
		ToURI:       wireCalleeURI(req),
		CallID:      wireCallID(req),
		CSeq:        wireCSeq(req),
		ContentType: firstVoiceHeader(req.Headers, "Content-Type"),
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneSIPHeaders(req.Headers),
	})
	if !result.Handled && err == nil {
		return nil, nil, false
	}
	resp := wireResponse(inboundStatusCode(result.StatusCode, 200), firstVoiceNonEmpty(result.Reason, "OK"))
	if err != nil && result.StatusCode <= 0 {
		resp = wireResponse(500, firstVoiceNonEmpty(result.Reason, err.Error(), "Server Internal Error"))
	}
	for key, value := range result.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			resp.Headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if len(result.Body) > 0 {
		resp.Body = append([]byte(nil), result.Body...)
		resp.Headers["Content-Type"] = firstVoiceNonEmpty(result.ContentType, "application/octet-stream")
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(resp)}, err, true
}

func (s *IMSInboundWireServer) handleMessage(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	if s == nil || s.MessageHandler == nil {
		resp := wireResponse(405, "Method Not Allowed")
		resp.Headers["Allow"] = s.allowHeader()
		return []IMSInboundWireResponse{s.withResponseHeaders(resp)}, nil
	}
	result, err := s.MessageHandler.HandleIMSMessage(ctx, IMSMessageRequest{
		URI:         strings.TrimSpace(req.URI),
		FromURI:     wireHeaderURI(req, "From"),
		ToURI:       wireCalleeURI(req),
		CallID:      wireCallID(req),
		CSeq:        wireCSeq(req),
		ContentType: firstVoiceHeader(req.Headers, "Content-Type"),
		Body:        append([]byte(nil), req.Body...),
		Headers:     cloneSIPHeaders(req.Headers),
	})
	statusCode := inboundStatusCode(result.StatusCode, 200)
	reason := firstVoiceNonEmpty(result.Reason, "OK")
	if err != nil && result.StatusCode <= 0 {
		statusCode = 500
		reason = firstVoiceNonEmpty(result.Reason, err.Error(), "Server Internal Error")
	}
	resp := wireResponse(statusCode, reason)
	for key, value := range result.Headers {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			resp.Headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if len(result.Body) > 0 {
		resp.Body = append([]byte(nil), result.Body...)
		resp.Headers["Content-Type"] = firstVoiceNonEmpty(result.ContentType, "application/octet-stream")
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(resp)}, err
}

func (s *IMSInboundWireServer) handleUpdate(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	if s == nil || s.Agent == nil {
		return []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(503, "Service Unavailable"))}, ErrIMSInboundAgentNotReady
	}
	result, err := s.Agent.HandleInboundUpdate(ctx, InboundDialogRequest{
		CallID:  wireCallID(req),
		CSeq:    wireCSeq(req),
		RawSDP:  append([]byte(nil), req.Body...),
		Headers: cloneSIPHeaders(req.Headers),
		RAck:    firstVoiceHeader(req.Headers, "RAck"),
	})
	final := wireResponse(inboundStatusCode(result.StatusCode, 500), firstVoiceNonEmpty(result.Reason, "Server Internal Error"))
	if result.Accepted {
		final.StatusCode = inboundStatusCode(result.StatusCode, 200)
		final.Reason = firstVoiceNonEmpty(result.Reason, "OK")
		final.Body = append([]byte(nil), result.RawSDP...)
		if len(final.Body) > 0 {
			final.Headers["Content-Type"] = "application/sdp"
		}
		final.Headers["Contact"] = "<" + s.contactURI() + ">"
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(final)}, err
}

func (s *IMSInboundWireServer) handlePrack(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	if s == nil || s.Agent == nil {
		return []IMSInboundWireResponse{s.withResponseHeaders(wireResponse(503, "Service Unavailable"))}, ErrIMSInboundAgentNotReady
	}
	result, err := s.Agent.HandleInboundPrack(ctx, InboundDialogRequest{
		CallID:  wireCallID(req),
		CSeq:    wireCSeq(req),
		Headers: cloneSIPHeaders(req.Headers),
		RAck:    firstVoiceHeader(req.Headers, "RAck"),
	})
	final := wireResponse(inboundStatusCode(result.StatusCode, 500), firstVoiceNonEmpty(result.Reason, "Server Internal Error"))
	if result.Accepted {
		final.StatusCode = inboundStatusCode(result.StatusCode, 200)
		final.Reason = firstVoiceNonEmpty(result.Reason, "OK")
		final.Body = append([]byte(nil), result.RawSDP...)
		if len(final.Body) > 0 {
			final.Headers["Content-Type"] = "application/sdp"
		}
	}
	return []IMSInboundWireResponse{s.withResponseHeaders(final)}, err
}

func (s *IMSInboundWireServer) handleInvite(ctx context.Context, req voiceclient.SIPIncomingRequest) ([]IMSInboundWireResponse, error) {
	responses := []IMSInboundWireResponse{wireResponse(100, "Trying")}
	if s == nil || s.Agent == nil {
		return append(responses, s.withResponseHeaders(wireResponse(503, "Service Unavailable"))), ErrIMSInboundAgentNotReady
	}
	result, err := s.Agent.HandleInboundInvite(ctx, InboundCallRequest{
		CallID:          wireCallID(req),
		CallerURI:       wireHeaderURI(req, "From"),
		CalleeURI:       wireCalleeURI(req),
		RemoteTag:       sipHeaderTag(firstVoiceHeader(req.Headers, "From")),
		RemoteTargetURI: wireHeaderURI(req, "Contact"),
		CSeq:            wireCSeq(req),
		RawSDP:          append([]byte(nil), req.Body...),
		Headers:         cloneSIPHeaders(req.Headers),
	})
	final := wireResponse(inboundStatusCode(result.StatusCode, 500), firstVoiceNonEmpty(result.Reason, "Server Internal Error"))
	if result.Accepted {
		final.StatusCode = inboundStatusCode(result.StatusCode, 200)
		final.Reason = firstVoiceNonEmpty(result.Reason, "OK")
		final.Body = append([]byte(nil), result.RawSDP...)
		if len(final.Body) == 0 {
			final.Body = BuildSDPAnswer(result.LocalSDP)
		}
		final.Headers["Contact"] = "<" + s.contactURI() + ">"
		if len(final.Body) > 0 {
			final.Headers["Content-Type"] = "application/sdp"
		}
	}
	responses = append(responses, s.withResponseHeaders(final))
	return responses, err
}

func (s *IMSInboundWireServer) handlePacket(ctx context.Context, pc net.PacketConn, addr net.Addr, raw []byte) {
	req, err := voiceclient.ParseSIPRequest(raw)
	if err != nil {
		_ = writePacketSIPResponse(pc, addr, voiceclient.SIPIncomingRequest{}, wireResponse(400, "Bad Request"))
		return
	}
	responses, _ := s.HandleRequest(ctx, req)
	for _, resp := range responses {
		if resp.NoResponse {
			continue
		}
		_ = writePacketSIPResponse(pc, addr, taggedWireRequest(req, s.localTag()), resp)
	}
}

func (s *IMSInboundWireServer) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		if err := conn.SetDeadline(time.Now().Add(s.readTimeout())); err != nil {
			return
		}
		raw, err := voiceclient.ReadSIPStreamMessage(reader)
		if err != nil {
			return
		}
		req, err := voiceclient.ParseSIPRequest(raw)
		if err != nil {
			_ = writeStreamSIPResponse(conn, voiceclient.SIPIncomingRequest{}, wireResponse(400, "Bad Request"))
			return
		}
		responses, _ := s.HandleRequest(ctx, req)
		for _, resp := range responses {
			if resp.NoResponse {
				continue
			}
			if err := writeStreamSIPResponse(conn, taggedWireRequest(req, s.localTag()), resp); err != nil {
				return
			}
		}
	}
}

func writePacketSIPResponse(pc net.PacketConn, addr net.Addr, req voiceclient.SIPIncomingRequest, resp IMSInboundWireResponse) error {
	wire, err := voiceclient.BuildSIPResponseWire(req, resp.StatusCode, resp.Reason, resp.Headers, resp.Body)
	if err != nil {
		return err
	}
	_, err = pc.WriteTo(wire, addr)
	return err
}

func writeStreamSIPResponse(conn net.Conn, req voiceclient.SIPIncomingRequest, resp IMSInboundWireResponse) error {
	wire, err := voiceclient.BuildSIPResponseWire(req, resp.StatusCode, resp.Reason, resp.Headers, resp.Body)
	if err != nil {
		return err
	}
	_, err = conn.Write(wire)
	return err
}

func wireResponse(statusCode int, reason string) IMSInboundWireResponse {
	return IMSInboundWireResponse{StatusCode: statusCode, Reason: reason, Headers: make(map[string]string)}
}

func (s *IMSInboundWireServer) optionsResponse() IMSInboundWireResponse {
	resp := wireResponse(200, "OK")
	resp.Headers["Allow"] = s.allowHeader()
	resp.Headers["Supported"] = "100rel, timer, replaces, outbound"
	resp.Headers["Accept"] = "application/sdp"
	if s != nil && (s.MessageHandler != nil || s.InfoHandler != nil) {
		accept := []string{"application/sdp"}
		if s.InfoHandler != nil {
			accept = append(accept, "application/vnd.3gpp.ussd+xml")
		}
		if s.MessageHandler != nil {
			accept = append(accept, "application/vnd.3gpp.sms", "text/plain")
		}
		resp.Headers["Accept"] = strings.Join(accept, ", ")
	}
	resp.Headers["Contact"] = "<" + s.contactURI() + ">"
	return resp
}

func (s *IMSInboundWireServer) allowHeader() string {
	allow := "INVITE, ACK, CANCEL, BYE, PRACK, UPDATE, OPTIONS"
	if s != nil && s.InfoHandler != nil {
		allow += ", INFO"
	}
	if s != nil && s.MessageHandler != nil {
		allow += ", MESSAGE"
	}
	return allow
}

func (s *IMSInboundWireServer) withResponseHeaders(resp IMSInboundWireResponse) IMSInboundWireResponse {
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	if s == nil {
		return resp
	}
	for key, value := range s.ResponseHeaders {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			resp.Headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(s.UserAgent) != "" {
		resp.Headers["Server"] = strings.TrimSpace(s.UserAgent)
	}
	return resp
}

func (s *IMSInboundWireServer) contactURI() string {
	if s == nil {
		return "sip:vowifi-go@127.0.0.1:5060"
	}
	contact := firstVoiceNonEmpty(s.ContactURI)
	if contact == "" && s.Agent != nil {
		contact = firstVoiceNonEmpty(s.Agent.LocalContactURI, s.Agent.Registration.ContactURI, s.Agent.Profile.IMPU)
	}
	if contact == "" {
		contact = "sip:vowifi-go@127.0.0.1:5060"
	}
	return strings.Trim(contact, "<>")
}

func (s *IMSInboundWireServer) localTag() string {
	if s == nil {
		return "vowifi-go"
	}
	tag := firstVoiceNonEmpty(s.LocalTag)
	if tag == "" && s.Agent != nil {
		tag = firstVoiceNonEmpty(s.Agent.LocalTag)
	}
	return firstVoiceNonEmpty(tag, "vowifi-go")
}

func (s *IMSInboundWireServer) readTimeout() time.Duration {
	if s == nil || s.ReadTimeout <= 0 {
		return time.Second
	}
	return s.ReadTimeout
}

func (s *IMSInboundWireServer) transactionTTL() time.Duration {
	if s == nil || s.TransactionTTL <= 0 {
		return 32 * time.Second
	}
	return s.TransactionTTL
}

func (s *IMSInboundWireServer) cachedTransaction(key string) ([]IMSInboundWireResponse, bool) {
	if s == nil || strings.TrimSpace(key) == "" {
		return nil, false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for cachedKey, tx := range s.transactions {
		if !tx.expires.IsZero() && now.After(tx.expires) {
			delete(s.transactions, cachedKey)
		}
	}
	tx, ok := s.transactions[key]
	if !ok || (!tx.expires.IsZero() && now.After(tx.expires)) {
		return nil, false
	}
	return cloneWireResponses(tx.responses), true
}

func (s *IMSInboundWireServer) storeTransaction(key string, responses []IMSInboundWireResponse) {
	if s == nil || strings.TrimSpace(key) == "" || len(responses) == 0 {
		return
	}
	s.mu.Lock()
	if s.transactions == nil {
		s.transactions = make(map[string]imsInboundWireTransaction)
	}
	s.transactions[key] = imsInboundWireTransaction{
		responses: cloneWireResponses(responses),
		expires:   time.Now().Add(s.transactionTTL()),
	}
	s.mu.Unlock()
}

func cloneWireResponses(responses []IMSInboundWireResponse) []IMSInboundWireResponse {
	out := make([]IMSInboundWireResponse, len(responses))
	for i, resp := range responses {
		out[i] = resp
		out[i].Body = append([]byte(nil), resp.Body...)
		if resp.Headers != nil {
			out[i].Headers = make(map[string]string, len(resp.Headers))
			for key, value := range resp.Headers {
				out[i].Headers[key] = value
			}
		}
	}
	return out
}

func taggedWireRequest(req voiceclient.SIPIncomingRequest, tag string) voiceclient.SIPIncomingRequest {
	out := req
	out.Headers = cloneSIPHeaders(req.Headers)
	to := firstVoiceHeader(out.Headers, "To")
	if to == "" || sipHeaderTag(to) != "" {
		return out
	}
	out.Headers["To"] = []string{to + ";tag=" + firstVoiceNonEmpty(tag, "vowifi-go")}
	return out
}

func wireCallID(req voiceclient.SIPIncomingRequest) string {
	return firstVoiceHeader(req.Headers, "Call-ID")
}

func wireTransactionKey(req voiceclient.SIPIncomingRequest) string {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	callID := strings.TrimSpace(wireCallID(req))
	cseq := strings.TrimSpace(firstVoiceHeader(req.Headers, "CSeq"))
	branch := wireViaBranch(firstVoiceHeader(req.Headers, "Via"))
	if method == "" || callID == "" || cseq == "" {
		return ""
	}
	if branch == "" {
		branch = firstVoiceHeader(req.Headers, "Via")
	}
	return method + "|" + callID + "|" + cseq + "|" + branch
}

func wireViaBranch(via string) string {
	for _, part := range strings.Split(via, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "branch") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func wireCSeq(req voiceclient.SIPIncomingRequest) int {
	value := firstVoiceHeader(req.Headers, "CSeq")
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 1
	}
	cseq, err := strconv.Atoi(fields[0])
	if err != nil || cseq <= 0 {
		return 1
	}
	return cseq
}

func wireHeaderURI(req voiceclient.SIPIncomingRequest, name string) string {
	return sipHeaderURI(firstVoiceHeader(req.Headers, name))
}

func wireCalleeURI(req voiceclient.SIPIncomingRequest) string {
	if uri := wireHeaderURI(req, "To"); uri != "" {
		return uri
	}
	return strings.TrimSpace(req.URI)
}

func cloneSIPHeaders(headers map[string][]string) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
