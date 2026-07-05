package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
)

func TestIMSSMSTransportSendsSIPMessage(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{StatusCode: 202, Reason: "Accepted"}}}
	sms := IMSSMSTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example", UserAgent: "VoHive"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
			ServiceRoutes:  []string{"<sip:pcscf.ims.example;lr>"},
		},
	}

	result, err := sms.SendSMSPart(context.Background(), SMSSendRequest{
		DeviceID:  "dev-1",
		IMSI:      "310280233641503",
		Peer:      "+18005551212",
		MessageID: "msg-1",
		Part:      SMSPart{PartNo: 2, TotalParts: 2, Text: "hello", Encoding: "gsm7"},
	})
	if err != nil {
		t.Fatalf("SendSMSPart() error = %v", err)
	}
	if result.State != "accepted" || result.SIPCode != 202 || result.RPMR != 2 || result.CallID != "msg-1-2@vowifi-go" {
		t.Fatalf("result=%+v", result)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests=%d", len(transport.requests))
	}
	req := transport.requests[0]
	if req.Method != "MESSAGE" || req.URI != "sip:+18005551212@ims.example" {
		t.Fatalf("MESSAGE=%+v body=%q", req, req.Body)
	}
	if req.Headers["Route"] != "<sip:pcscf.ims.example;lr>" || req.Headers["Content-Type"] != IMS3GPPSMSContentType {
		t.Fatalf("headers=%+v", req.Headers)
	}
	rpMR, tpdu, err := ParseSMSRPData(req.Body)
	if err != nil {
		t.Fatalf("ParseSMSRPData() error = %v body=%x", err, req.Body)
	}
	if rpMR != 2 || len(tpdu) == 0 || tpdu[0] != 0x01 || !strings.HasSuffix(strings.ToUpper(hexString(req.Body)), "E8329BFD06") {
		t.Fatalf("RP-DATA rpMR=%d tpdu=%x body=%x", rpMR, tpdu, req.Body)
	}
	if req.Headers["P-Preferred-Service"] != "urn:urn-7:3gpp-service.ims.icsi.sms" || req.Headers["Accept-Contact"] != "*;+g.3gpp.smsip" {
		t.Fatalf("SMS service headers=%+v", req.Headers)
	}
	if !strings.Contains(req.Headers["From"], "sip:user@ims.example") || !strings.Contains(req.Headers["To"], "sip:+18005551212@ims.example") {
		t.Fatalf("dialog headers=%+v", req.Headers)
	}
}

func TestIMSSMSTransportAllowsTextPlainPayload(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{StatusCode: 200, Reason: "OK"}}}
	sms := IMSSMSTransport{
		Transport:   transport,
		ContentType: "text/plain;charset=UTF-8",
		Profile:     voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
		},
	}
	if _, err := sms.SendSMSPart(context.Background(), SMSSendRequest{Peer: "+18005551212", Part: SMSPart{PartNo: 1, Text: "hello"}}); err != nil {
		t.Fatalf("SendSMSPart() error = %v", err)
	}
	if len(transport.requests) != 1 || string(transport.requests[0].Body) != "hello" || transport.requests[0].Headers["Content-Type"] != "text/plain;charset=UTF-8" {
		t.Fatalf("request=%+v", transport.requests)
	}
}

func TestIMSSMSTransportRejectsFailedMessage(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{StatusCode: 403, Reason: "Forbidden"}}}
	sms := IMSSMSTransport{
		Transport:    transport,
		Profile:      voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{ContactURI: "sip:user@192.0.2.10:5060"},
	}

	result, err := sms.SendSMSPart(context.Background(), SMSSendRequest{
		Peer:      "sip:+18005551212@ims.example",
		MessageID: "msg reject",
		Part:      SMSPart{PartNo: 1, TotalParts: 1, Text: "hello"},
	})
	if err == nil {
		t.Fatal("SendSMSPart() err=nil, want rejection")
	}
	if result.State != "failed" || result.SIPCode != 403 || result.CallID != "msg-reject-1@vowifi-go" || result.ErrorText != "Forbidden" {
		t.Fatalf("result=%+v", result)
	}
	if result.RegistrationRecoveryNeeded {
		t.Fatalf("RegistrationRecoveryNeeded=true for non-recoverable 403: %+v", result)
	}
}

func TestIMSSMSTransportFlagsRecoverableFailures(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{StatusCode: 503, Reason: "Service Unavailable"}}}
	sms := IMSSMSTransport{
		Transport:    transport,
		Profile:      voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{ContactURI: "sip:user@192.0.2.10:5060"},
	}

	result, err := sms.SendSMSPart(context.Background(), SMSSendRequest{
		Peer: "+18005551212",
		Part: SMSPart{PartNo: 1, Text: "hello"},
	})
	if err == nil || result.SIPCode != 503 || !result.RegistrationRecoveryNeeded {
		t.Fatalf("SendSMSPart() result=%+v err=%v, want recoverable 503", result, err)
	}

	transport = &fakeSIPRequestTransport{errors: []error{errors.New("pcscf flow reset")}}
	sms.Transport = transport
	result, err = sms.SendSMSPart(context.Background(), SMSSendRequest{
		Peer: "+18005551212",
		Part: SMSPart{PartNo: 1, Text: "hello"},
	})
	if err == nil || result.SIPCode != 0 || !result.RegistrationRecoveryNeeded {
		t.Fatalf("SendSMSPart() result=%+v err=%v, want recoverable transport error", result, err)
	}
}

func TestIMSSMSTransportRequiresSIPTransport(t *testing.T) {
	_, err := IMSSMSTransport{}.SendSMSPart(context.Background(), SMSSendRequest{Peer: "+18005551212", Part: SMSPart{Text: "hello"}})
	if !errors.Is(err, ErrSMSTransportUnavailable) {
		t.Fatalf("err=%v, want ErrSMSTransportUnavailable", err)
	}
}

type fakeSIPRequestTransport struct {
	requests  []voiceclient.SIPRequestMessage
	responses []voiceclient.SIPResponse
	writes    []voiceclient.SIPRequestMessage
	errors    []error
}

func (t *fakeSIPRequestTransport) RoundTripRequest(ctx context.Context, msg voiceclient.SIPRequestMessage) (voiceclient.SIPResponse, error) {
	t.requests = append(t.requests, msg)
	if len(t.errors) > 0 {
		err := t.errors[0]
		t.errors = t.errors[1:]
		if err != nil {
			return voiceclient.SIPResponse{}, err
		}
	}
	if len(t.responses) == 0 {
		return voiceclient.SIPResponse{StatusCode: 500, Reason: "empty"}, nil
	}
	resp := t.responses[0]
	t.responses = t.responses[1:]
	return resp, nil
}

func (t *fakeSIPRequestTransport) WriteRequest(ctx context.Context, msg voiceclient.SIPRequestMessage) error {
	t.writes = append(t.writes, msg)
	return nil
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
