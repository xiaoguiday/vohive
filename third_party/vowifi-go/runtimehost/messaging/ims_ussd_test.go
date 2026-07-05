package messaging

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
)

func TestIMSUSSDTransportExecuteAndContinue(t *testing.T) {
	replyXML, err := BuildIMSUSSDXML(IMSUSSDPayload{Text: "Balance: 10", Operation: IMSUSSDOperationNotify})
	if err != nil {
		t.Fatalf("BuildIMSUSSDXML() error = %v", err)
	}
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":           {"<sip:*100%23@ims.example;user=dialstring>;tag=as-tag"},
				"Contact":      {"<sip:ussd-as@ims.example>"},
				"Record-Route": {"<sip:dialog1.ims.example;lr>, <sip:dialog2.ims.example;lr>"},
			},
		},
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers:    map[string][]string{"Content-Type": {IMSUSSDContentType}},
			Body:       replyXML,
		},
	}}
	ussd := &IMSUSSDTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example", LocalIP: "192.0.2.10"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
			ServiceRoutes:  []string{"<sip:pcscf.ims.example;lr>"},
		},
	}
	first, err := ussd.ExecuteUSSD(context.Background(), USSDRequest{SessionID: "session-1", Command: "*100#"})
	if err != nil {
		t.Fatalf("ExecuteUSSD() error = %v", err)
	}
	if first.Done || first.SessionID != "session-1" || first.Status != 200 {
		t.Fatalf("first=%+v", first)
	}
	if len(transport.requests) != 1 {
		t.Fatalf("requests=%d", len(transport.requests))
	}
	invite := transport.requests[0]
	if invite.Method != "INVITE" || invite.URI != "sip:*100%23@ims.example;user=dialstring" || invite.Headers["Recv-Info"] != IMSUSSDInfoPackage {
		t.Fatalf("invite=%+v", invite)
	}
	if invite.Headers["Route"] != "<sip:pcscf.ims.example;lr>" || !strings.Contains(invite.Headers["Content-Type"], "multipart/mixed") {
		t.Fatalf("invite headers=%+v", invite.Headers)
	}
	payload, ok, err := DecodeIMSUSSDDocument(invite.Headers["Content-Type"], invite.Body)
	if err != nil || !ok || payload.Text != "*100#" || payload.Operation != IMSUSSDOperationRequest {
		t.Fatalf("payload=%+v ok=%v err=%v", payload, ok, err)
	}
	if len(transport.writes) != 1 || transport.writes[0].Method != "ACK" || transport.writes[0].Headers["CSeq"] != "1 ACK" {
		t.Fatalf("ACK writes=%+v", transport.writes)
	}
	if route := transport.writes[0].Headers["Route"]; route != "<sip:dialog2.ims.example;lr>, <sip:dialog1.ims.example;lr>" {
		t.Fatalf("ACK Route=%q", route)
	}

	next, err := ussd.ContinueUSSD(context.Background(), USSDRequest{SessionID: "session-1", Input: "1"})
	if err != nil {
		t.Fatalf("ContinueUSSD() error = %v", err)
	}
	if !next.Done || next.Text != "Balance: 10" || next.Status != 200 {
		t.Fatalf("next=%+v", next)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("requests=%d", len(transport.requests))
	}
	info := transport.requests[1]
	if info.Method != "INFO" || info.Headers["CSeq"] != "2 INFO" || info.Headers["Info-Package"] != IMSUSSDInfoPackage || info.Headers["Content-Disposition"] != IMSUSSDContentDisposition {
		t.Fatalf("info=%+v", info)
	}
	if info.URI != "sip:ussd-as@ims.example" || info.Headers["Route"] != "<sip:dialog2.ims.example;lr>, <sip:dialog1.ims.example;lr>" {
		t.Fatalf("INFO route/target=%+v", info)
	}
	if _, err := ussd.ContinueUSSD(context.Background(), USSDRequest{SessionID: "session-1", Input: "1"}); err == nil {
		t.Fatal("ContinueUSSD() err=nil after terminal notify, want inactive session")
	}
}

func TestIMSUSSDTransportCancelSendsBye(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":           {"<sip:*100%23@ims.example;user=dialstring>;tag=as-tag"},
				"Contact":      {"<sip:ussd-as@ims.example>"},
				"Record-Route": {"<sip:dialog-proxy.ims.example;lr>"},
			},
		},
		{StatusCode: 200, Reason: "OK"},
	}}
	ussd := &IMSUSSDTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
		},
	}
	if _, err := ussd.ExecuteUSSD(context.Background(), USSDRequest{SessionID: "session-cancel", Command: "*100#"}); err != nil {
		t.Fatalf("ExecuteUSSD() error = %v", err)
	}
	if err := ussd.CancelUSSD(context.Background(), USSDRequest{SessionID: "session-cancel"}); err != nil {
		t.Fatalf("CancelUSSD() error = %v", err)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("requests=%d", len(transport.requests))
	}
	bye := transport.requests[1]
	if bye.Method != "BYE" || bye.Headers["CSeq"] != "2 BYE" || bye.URI != "sip:ussd-as@ims.example" {
		t.Fatalf("bye=%+v", bye)
	}
	if bye.Headers["Route"] != "<sip:dialog-proxy.ims.example;lr>" {
		t.Fatalf("BYE Route=%q", bye.Headers["Route"])
	}
}

func TestIMSUSSDTransportACKsRejectedInvite(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{
		StatusCode: 486,
		Reason:     "Busy Here",
		Headers: map[string][]string{
			"To":           {"<sip:*100%23@ims.example;user=dialstring>;tag=busy-tag"},
			"Contact":      {"<sip:ussd-as@ims.example>"},
			"Record-Route": {"<sip:reject-proxy1.ims.example;lr>, <sip:reject-proxy2.ims.example;lr>"},
		},
	}}}
	ussd := &IMSUSSDTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
		},
	}

	result, err := ussd.ExecuteUSSD(context.Background(), USSDRequest{SessionID: "session-reject", Command: "*100#"})
	if err == nil || !strings.Contains(err.Error(), "IMS USSD INVITE rejected: 486 Busy Here") {
		t.Fatalf("ExecuteUSSD() result=%+v err=%v, want rejected error", result, err)
	}
	if result.Status != 486 || !result.Done {
		t.Fatalf("result=%+v", result)
	}
	if len(transport.writes) != 1 || transport.writes[0].Method != "ACK" {
		t.Fatalf("ACK writes=%+v", transport.writes)
	}
	ack := transport.writes[0]
	if ack.Headers["CSeq"] != "1 ACK" || !strings.Contains(ack.Headers["To"], "busy-tag") {
		t.Fatalf("ACK=%+v", ack)
	}
	if ack.URI != "sip:ussd-as@ims.example" {
		t.Fatalf("ACK URI=%q", ack.URI)
	}
	if route := ack.Headers["Route"]; route != "<sip:reject-proxy2.ims.example;lr>, <sip:reject-proxy1.ims.example;lr>" {
		t.Fatalf("ACK Route=%q", route)
	}
	if _, ok := ussd.session("session-reject"); ok {
		t.Fatal("rejected USSD INVITE must not leave an active session")
	}
}

func TestIMSUSSDTransportFlagsRecoverableFailures(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{
		StatusCode: 503,
		Reason:     "Service Unavailable",
		Headers: map[string][]string{
			"To": {"<sip:*100%23@ims.example;user=dialstring>;tag=unavailable"},
		},
	}}}
	ussd := &IMSUSSDTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
		},
	}

	result, err := ussd.ExecuteUSSD(context.Background(), USSDRequest{SessionID: "session-503", Command: "*100#"})
	if err == nil || result.Status != 503 || !result.Done || !result.RegistrationRecoveryNeeded {
		t.Fatalf("ExecuteUSSD() result=%+v err=%v, want recoverable 503", result, err)
	}

	transport = &fakeSIPRequestTransport{errors: []error{errors.New("pcscf flow reset")}}
	ussd.Transport = transport
	result, err = ussd.ExecuteUSSD(context.Background(), USSDRequest{SessionID: "session-transport", Command: "*100#"})
	if err == nil || result.Status != 0 || !result.Done || !result.RegistrationRecoveryNeeded {
		t.Fatalf("ExecuteUSSD() result=%+v err=%v, want recoverable transport error", result, err)
	}
}

func TestIMSUSSDTransportHandlesInboundInfoAndBye(t *testing.T) {
	transport := &fakeSIPRequestTransport{responses: []voiceclient.SIPResponse{{
		StatusCode: 200,
		Reason:     "OK",
		Headers: map[string][]string{
			"To":      {"<sip:*100%23@ims.example;user=dialstring>;tag=as-tag"},
			"Contact": {"<sip:ussd-as@ims.example>"},
		},
	}}}
	ussd := &IMSUSSDTransport{
		Transport: transport,
		Profile:   voiceclient.IMSProfile{IMPU: "sip:user@ims.example", Domain: "ims.example"},
		Registration: voiceclient.RegistrationBinding{
			ContactURI:     "sip:user@192.0.2.10:5060",
			PublicIdentity: "sip:user@ims.example",
		},
	}
	dispatch := &fakeDispatcher{}
	svc := NewService("dev-1", "310280233641503", nil, dispatch)
	svc.SetUSSDTransport(ussd)
	first, err := svc.SendUSSD(context.Background(), "*100#")
	if err != nil {
		t.Fatalf("SendUSSD() error = %v", err)
	}
	if first.Done || !svc.hasUSSDSession(first.SessionID) {
		t.Fatalf("first=%+v active=%v", first, svc.hasUSSDSession(first.SessionID))
	}

	menuXML, err := BuildIMSUSSDXML(IMSUSSDPayload{Text: "1. Balance\n2. Data", Operation: IMSUSSDOperationRequest})
	if err != nil {
		t.Fatalf("BuildIMSUSSDXML(menu) error = %v", err)
	}
	info, err := svc.HandleIMSUSSDInfo(context.Background(), IMSUSSDDialogRequest{
		CallID:      "ussd-" + smsToken(first.SessionID) + "@vowifi-go",
		CSeq:        2,
		ContentType: IMSUSSDContentType,
		InfoPackage: IMSUSSDInfoPackage,
		Body:        menuXML,
	})
	if err != nil {
		t.Fatalf("HandleIMSUSSDInfo() error = %v", err)
	}
	if !info.Handled || info.StatusCode != 200 || info.USSD.Text != "1. Balance\n2. Data" || info.USSD.Done || !svc.hasUSSDSession(first.SessionID) {
		t.Fatalf("info=%+v active=%v", info, svc.hasUSSDSession(first.SessionID))
	}
	if len(dispatch.events) != 2 {
		t.Fatalf("events=%d", len(dispatch.events))
	}
	infoEvent, ok := dispatch.events[1].(eventhost.USSDUpdated)
	if !ok || infoEvent.DevID != "dev-1" || infoEvent.SessionID != first.SessionID || infoEvent.Text != "1. Balance\n2. Data" || infoEvent.Done || infoEvent.Time.IsZero() {
		t.Fatalf("event=%+v", dispatch.events[1])
	}

	byeXML, err := BuildIMSUSSDXML(IMSUSSDPayload{Text: "Bye", Operation: IMSUSSDOperationNotify})
	if err != nil {
		t.Fatalf("BuildIMSUSSDXML(bye) error = %v", err)
	}
	bye, err := svc.HandleIMSUSSDBye(context.Background(), IMSUSSDDialogRequest{
		CallID:      "ussd-" + smsToken(first.SessionID) + "@vowifi-go",
		CSeq:        3,
		ContentType: IMSUSSDContentType,
		Body:        byeXML,
	})
	if err != nil {
		t.Fatalf("HandleIMSUSSDBye() error = %v", err)
	}
	if !bye.Handled || bye.StatusCode != 200 || bye.USSD.Text != "Bye" || !bye.USSD.Done || svc.hasUSSDSession(first.SessionID) {
		t.Fatalf("bye=%+v active=%v", bye, svc.hasUSSDSession(first.SessionID))
	}
	if len(dispatch.events) != 3 {
		t.Fatalf("events=%d", len(dispatch.events))
	}
	byeEvent, ok := dispatch.events[2].(eventhost.USSDUpdated)
	if !ok || byeEvent.SessionID != first.SessionID || byeEvent.Text != "Bye" || !byeEvent.Done || byeEvent.Time.IsZero() {
		t.Fatalf("event=%+v", dispatch.events[2])
	}
}
