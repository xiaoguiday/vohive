package messaging

import (
	"context"
	"errors"
	"strings"
)

type IMSUSSDDialogRequest struct {
	URI         string
	FromURI     string
	ToURI       string
	CallID      string
	CSeq        int
	ContentType string
	InfoPackage string
	Body        []byte
	Headers     map[string][]string
}

type IMSUSSDDialogResult struct {
	Handled     bool
	StatusCode  int
	Reason      string
	ContentType string
	Body        []byte
	Headers     map[string]string
	USSD        USSDResult
}

type IMSUSSDDialogTransport interface {
	HandleIMSInfo(context.Context, IMSUSSDDialogRequest) (IMSUSSDDialogResult, error)
	HandleIMSBye(context.Context, IMSUSSDDialogRequest) (IMSUSSDDialogResult, error)
}

func (s *Service) HandleIMSUSSDInfo(ctx context.Context, req IMSUSSDDialogRequest) (IMSUSSDDialogResult, error) {
	if s == nil || s.ussdTransport == nil {
		if isIMSUSSDDialogRequest(req) {
			err := ErrUSSDTransportUnavailable
			return IMSUSSDDialogResult{Handled: true, StatusCode: 503, Reason: err.Error()}, err
		}
		return IMSUSSDDialogResult{}, nil
	}
	transport, ok := s.ussdTransport.(IMSUSSDDialogTransport)
	if !ok {
		if isIMSUSSDDialogRequest(req) {
			err := errors.New("USSD transport does not handle IMS dialog requests")
			return IMSUSSDDialogResult{Handled: true, StatusCode: 501, Reason: err.Error()}, err
		}
		return IMSUSSDDialogResult{}, nil
	}
	result, err := transport.HandleIMSInfo(ctx, req)
	if strings.TrimSpace(result.USSD.SessionID) != "" {
		result.USSD = normalizeUSSDResult(result.USSD, result.USSD.SessionID)
		s.recordUSSDSession(result.USSD)
		s.dispatchUSSDUpdated(ctx, result.USSD)
	}
	return result, err
}

func (s *Service) HandleIMSUSSDBye(ctx context.Context, req IMSUSSDDialogRequest) (IMSUSSDDialogResult, error) {
	if s == nil || s.ussdTransport == nil {
		if isIMSUSSDDialogRequest(req) {
			err := ErrUSSDTransportUnavailable
			return IMSUSSDDialogResult{Handled: true, StatusCode: 503, Reason: err.Error()}, err
		}
		return IMSUSSDDialogResult{}, nil
	}
	transport, ok := s.ussdTransport.(IMSUSSDDialogTransport)
	if !ok {
		if isIMSUSSDDialogRequest(req) {
			err := errors.New("USSD transport does not handle IMS dialog requests")
			return IMSUSSDDialogResult{Handled: true, StatusCode: 501, Reason: err.Error()}, err
		}
		return IMSUSSDDialogResult{}, nil
	}
	result, err := transport.HandleIMSBye(ctx, req)
	if strings.TrimSpace(result.USSD.SessionID) != "" {
		result.USSD = normalizeUSSDResult(result.USSD, result.USSD.SessionID)
		s.recordUSSDSession(result.USSD)
		s.dispatchUSSDUpdated(ctx, result.USSD)
	}
	return result, err
}

func (t *IMSUSSDTransport) HandleIMSInfo(ctx context.Context, req IMSUSSDDialogRequest) (IMSUSSDDialogResult, error) {
	if !isIMSUSSDDialogRequest(req) {
		return IMSUSSDDialogResult{}, nil
	}
	sessionID, state, ok := t.sessionByCallID(req.CallID)
	if !ok {
		return IMSUSSDDialogResult{Handled: true, StatusCode: 481, Reason: "USSD dialog not found"}, nil
	}
	payload, ok, err := DecodeIMSUSSDDocument(req.ContentType, req.Body)
	if err != nil {
		return IMSUSSDDialogResult{Handled: true, StatusCode: 400, Reason: err.Error()}, err
	}
	if !ok {
		err := errors.New("IMS USSD INFO body is missing USSD XML")
		return IMSUSSDDialogResult{Handled: true, StatusCode: 400, Reason: err.Error()}, err
	}
	result := ussdResultFromPayload(sessionID, payload, 200)
	if result.Done {
		t.clearSession(sessionID)
	} else {
		t.storeSession(sessionID, state)
	}
	return IMSUSSDDialogResult{Handled: true, StatusCode: 200, Reason: "OK", USSD: result}, nil
}

func (t *IMSUSSDTransport) HandleIMSBye(ctx context.Context, req IMSUSSDDialogRequest) (IMSUSSDDialogResult, error) {
	looksUSSD := isIMSUSSDDialogRequest(req)
	sessionID, _, ok := t.sessionByCallID(req.CallID)
	if !ok {
		if !looksUSSD {
			return IMSUSSDDialogResult{}, nil
		}
		return IMSUSSDDialogResult{Handled: true, StatusCode: 481, Reason: "USSD dialog not found"}, nil
	}
	result := USSDResult{SessionID: sessionID, Status: 200, Done: true}
	if len(req.Body) > 0 {
		payload, parsed, err := DecodeIMSUSSDDocument(req.ContentType, req.Body)
		if err != nil {
			return IMSUSSDDialogResult{Handled: true, StatusCode: 400, Reason: err.Error()}, err
		}
		if parsed {
			result = ussdResultFromPayload(sessionID, payload, 200)
			result.Done = true
		}
	}
	t.clearSession(sessionID)
	return IMSUSSDDialogResult{Handled: true, StatusCode: 200, Reason: "OK", USSD: result}, nil
}

func (t *IMSUSSDTransport) sessionByCallID(callID string) (string, imsUSSDSession, bool) {
	callID = strings.TrimSpace(callID)
	if t == nil || callID == "" {
		return "", imsUSSDSession{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for sessionID, state := range t.sessions {
		if strings.EqualFold(strings.TrimSpace(state.cfg.CallID), callID) {
			return sessionID, state, true
		}
	}
	return "", imsUSSDSession{}, false
}

func isIMSUSSDDialogRequest(req IMSUSSDDialogRequest) bool {
	if strings.EqualFold(strings.TrimSpace(req.InfoPackage), IMSUSSDInfoPackage) {
		return true
	}
	if normalizeUSSDContentType(req.ContentType) == IMSUSSDContentType {
		return true
	}
	contentType := strings.ToLower(strings.TrimSpace(req.ContentType))
	if strings.HasPrefix(contentType, "multipart/") && len(req.Body) > 0 {
		if _, ok, _ := DecodeIMSUSSDDocument(req.ContentType, req.Body); ok {
			return true
		}
	}
	return false
}
