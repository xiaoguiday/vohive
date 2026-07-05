package voicehost

import (
	"context"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/runtimehost/voiceclient"
)

func TestIMSInboundAgentInviteAckAndBye(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":           {"<sip:user@ims.example>;tag=client-tag"},
				"Contact":      {"<sip:client@192.0.2.50:5060>"},
				"Record-Route": {"<sip:client-proxy1.example;lr>, <sip:client-proxy2.example;lr>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport: transport,
		Profile: voiceclient.IMSProfile{
			IMPU:      "sip:user@ims.example",
			Domain:    "ims.example",
			UserAgent: "VoHive",
		},
		Registration: voiceclient.RegistrationBinding{
			PublicIdentity: "sip:user@ims.example",
			ContactURI:     "sip:user@192.0.2.10:5060",
		},
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	result, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		DeviceID:  "dev-1",
		CallID:    "in-call-1",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RemoteTag: "ims-tag",
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	})
	if err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if !result.Accepted || result.StatusCode != 200 || result.LocalSDP.ConnectionIP != "192.0.2.50" || result.LocalSDP.MediaPort != 4002 {
		t.Fatalf("result=%+v", result)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "INVITE" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	invite := transport.requests[0]
	if invite.URI != "sip:client@127.0.0.1:5070" || !strings.Contains(invite.Headers["From"], "ims-tag") || invite.Headers["Contact"] != "<sip:vowifi@127.0.0.1:5060>" {
		t.Fatalf("INVITE=%+v", invite)
	}
	if !strings.Contains(string(invite.Body), "m=audio 49170 RTP/AVP") {
		t.Fatalf("INVITE body=%q", invite.Body)
	}

	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-1"}); err != nil {
		t.Fatalf("AckInboundCall() error = %v", err)
	}
	if len(transport.writes) != 1 || transport.writes[0].Method != "ACK" {
		t.Fatalf("writes=%+v", transport.writes)
	}
	if transport.writes[0].URI != "sip:client@192.0.2.50:5060" || !strings.Contains(transport.writes[0].Headers["To"], "client-tag") {
		t.Fatalf("ACK=%+v", transport.writes[0])
	}
	if transport.writes[0].Headers["Route"] != "<sip:client-proxy2.example;lr>, <sip:client-proxy1.example;lr>" {
		t.Fatalf("ACK Route=%q", transport.writes[0].Headers["Route"])
	}

	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-1"}); err != nil {
		t.Fatalf("EndInboundCall() error = %v", err)
	}
	if len(transport.requests) != 2 || transport.requests[1].Method != "BYE" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	bye := transport.requests[1]
	if bye.URI != "sip:client@192.0.2.50:5060" || bye.Headers["CSeq"] != "2 BYE" {
		t.Fatalf("BYE=%+v", bye)
	}
	if bye.Headers["Route"] != "<sip:client-proxy2.example;lr>, <sip:client-proxy1.example;lr>" {
		t.Fatalf("BYE Route=%q", bye.Headers["Route"])
	}
}

func TestIMSInboundAgentRejectedInvite(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{{
		StatusCode: 486,
		Reason:     "Busy Here",
		Headers:    map[string][]string{"To": {"<sip:user@ims.example>;tag=busy-tag"}},
	}}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	result, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-2",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	})
	if err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if result.Accepted || result.StatusCode != 486 || result.Reason != "Busy Here" {
		t.Fatalf("result=%+v", result)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "INVITE" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	if len(transport.writes) != 1 || transport.writes[0].Method != "ACK" {
		t.Fatalf("ACK writes=%+v", transport.writes)
	}
	ack := transport.writes[0]
	if ack.Headers["CSeq"] != "1 ACK" || !strings.Contains(ack.Headers["To"], "busy-tag") {
		t.Fatalf("ACK=%+v", ack)
	}
	if ack.Headers["Via"] == "" || ack.Headers["Via"] != transport.requests[0].Headers["Via"] {
		t.Fatalf("ACK Via=%q INVITE Via=%q", ack.Headers["Via"], transport.requests[0].Headers["Via"])
	}
	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-2"}); err != nil {
		t.Fatalf("AckInboundCall(rejected) error = %v", err)
	}
	if len(transport.writes) != 1 {
		t.Fatalf("writes=%+v, want only rejected INVITE ACK", transport.writes)
	}
}

func TestIMSInboundAgentHandlesPrackAndUpdate(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":           {"<sip:user@ims.example>;tag=client-tag"},
				"Contact":      {"<sip:client@192.0.2.50:5060>"},
				"Record-Route": {"<sip:client-proxy1.example;lr>, <sip:client-proxy2.example;lr>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{StatusCode: 200, Reason: "OK"},
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers:    map[string][]string{"Contact": {"<sip:client@192.0.2.60:5060>"}},
			Body:       []byte(sampleSDP("192.0.2.60", 5000)),
		},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	if _, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-update",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	}); err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if result, err := agent.HandleInboundPrack(context.Background(), InboundDialogRequest{
		CallID: "in-call-update",
		CSeq:   2,
		RAck:   "1 1 INVITE",
	}); err != nil || !result.Accepted {
		t.Fatalf("HandleInboundPrack() result=%+v err=%v", result, err)
	}
	if len(transport.requests) != 2 || transport.requests[1].Method != "PRACK" || transport.requests[1].Headers["RAck"] != "1 1 INVITE" {
		t.Fatalf("PRACK requests=%+v", transport.requests)
	}
	if transport.requests[1].Headers["Route"] != "<sip:client-proxy2.example;lr>, <sip:client-proxy1.example;lr>" {
		t.Fatalf("PRACK Route=%q", transport.requests[1].Headers["Route"])
	}
	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-update"}); err != nil {
		t.Fatalf("AckInboundCall() error = %v", err)
	}
	if len(transport.writes) != 1 || transport.writes[0].Method != "ACK" || transport.writes[0].Headers["CSeq"] != "1 ACK" {
		t.Fatalf("ACK writes=%+v", transport.writes)
	}
	result, err := agent.HandleInboundUpdate(context.Background(), InboundDialogRequest{
		CallID: "in-call-update",
		CSeq:   3,
		RawSDP: []byte(sampleSDP("203.0.113.11", 49172)),
	})
	if err != nil {
		t.Fatalf("HandleInboundUpdate() error = %v", err)
	}
	if !result.Accepted || result.LocalSDP.ConnectionIP != "192.0.2.60" || result.LocalSDP.MediaPort != 5000 {
		t.Fatalf("UPDATE result=%+v", result)
	}
	if len(transport.requests) != 3 || transport.requests[2].Method != "UPDATE" || !strings.Contains(string(transport.requests[2].Body), "m=audio 49172 RTP/AVP") {
		t.Fatalf("UPDATE requests=%+v", transport.requests)
	}
	if transport.requests[2].Headers["Route"] != "<sip:client-proxy2.example;lr>, <sip:client-proxy1.example;lr>" {
		t.Fatalf("UPDATE Route=%q", transport.requests[2].Headers["Route"])
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-update"}); err != nil {
		t.Fatalf("EndInboundCall() error = %v", err)
	}
	if len(transport.requests) != 4 || transport.requests[3].Method != "BYE" || transport.requests[3].Headers["CSeq"] != "4 BYE" {
		t.Fatalf("BYE after UPDATE=%+v", transport.requests)
	}
}

func TestIMSInboundAgentForwardsInDialogInfoToClient(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":      {"<sip:user@ims.example>;tag=client-tag"},
				"Contact": {"<sip:client@192.0.2.50:5060>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{StatusCode: 200, Reason: "OK", Headers: map[string][]string{"Contact": {"<sip:client@192.0.2.60:5060>"}, "X-Client": {"info-ok"}}},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	if _, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-info",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RemoteTag: "ims-tag",
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	}); err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	result, err := agent.HandleInboundInfo(context.Background(), IMSInfoRequest{
		CallID:      "in-call-info",
		CSeq:        7,
		ContentType: "application/dtmf-relay",
		InfoPackage: "dtmf",
		Body:        []byte("Signal=5\r\nDuration=120\r\n"),
		Headers:     map[string][]string{"X-IMS": {"info"}},
	})
	if err != nil || !result.Handled || result.StatusCode != 200 || result.Headers["X-Client"] != "info-ok" {
		t.Fatalf("HandleInboundInfo() result=%+v err=%v", result, err)
	}
	if len(transport.requests) != 2 || transport.requests[1].Method != "INFO" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	info := transport.requests[1]
	if info.URI != "sip:client@192.0.2.50:5060" || info.Headers["CSeq"] != "7 INFO" ||
		info.Headers["Content-Type"] != "application/dtmf-relay" || info.Headers["Info-Package"] != "dtmf" ||
		info.Headers["X-IMS"] != "info" || !strings.Contains(string(info.Body), "Signal=5") {
		t.Fatalf("INFO=%+v body=%q", info, info.Body)
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-info"}); err != nil {
		t.Fatalf("EndInboundCall() error = %v", err)
	}
	if len(transport.requests) != 3 || transport.requests[2].Method != "BYE" || transport.requests[2].URI != "sip:client@192.0.2.60:5060" ||
		transport.requests[2].Headers["CSeq"] != "8 BYE" {
		t.Fatalf("BYE after INFO=%+v", transport.requests)
	}
}

func TestIMSInboundAgentHandlesReinviteAndTracksAckCSeq(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":      {"<sip:user@ims.example>;tag=client-tag"},
				"Contact": {"<sip:client@192.0.2.50:5060>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers:    map[string][]string{"Contact": {"<sip:client@192.0.2.60:5060>"}},
			Body:       []byte(sampleSDP("192.0.2.60", 5000)),
		},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	if _, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-reinvite",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		CSeq:      1,
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	}); err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-reinvite"}); err != nil {
		t.Fatalf("AckInboundCall(initial) error = %v", err)
	}
	if len(transport.writes) != 1 || transport.writes[0].Headers["CSeq"] != "1 ACK" {
		t.Fatalf("initial ACK writes=%+v", transport.writes)
	}
	result, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-reinvite",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		CSeq:      4,
		RawSDP:    []byte(sampleSDP("203.0.113.20", 49172)),
	})
	if err != nil {
		t.Fatalf("HandleInboundInvite(re-INVITE) error = %v", err)
	}
	if !result.Accepted || result.LocalSDP.ConnectionIP != "192.0.2.60" || result.LocalSDP.MediaPort != 5000 {
		t.Fatalf("re-INVITE result=%+v", result)
	}
	if len(transport.requests) != 2 || transport.requests[1].Method != "INVITE" || transport.requests[1].Headers["CSeq"] != "4 INVITE" || !strings.Contains(string(transport.requests[1].Body), "m=audio 49172 RTP/AVP") {
		t.Fatalf("re-INVITE requests=%+v", transport.requests)
	}
	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-reinvite"}); err != nil {
		t.Fatalf("AckInboundCall(re-INVITE) error = %v", err)
	}
	if len(transport.writes) != 2 || transport.writes[1].Headers["CSeq"] != "4 ACK" {
		t.Fatalf("re-INVITE ACK writes=%+v", transport.writes)
	}
}

func TestIMSInboundAgentRejectedReinviteAcksFinalResponse(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":      {"<sip:user@ims.example>;tag=client-tag"},
				"Contact": {"<sip:client@192.0.2.50:5060>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{
			StatusCode: 488,
			Reason:     "Not Acceptable Here",
			Headers:    map[string][]string{"To": {"<sip:user@ims.example>;tag=client-tag"}},
		},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	if _, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-reinvite-reject",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		CSeq:      1,
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	}); err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if err := agent.AckInboundCall(context.Background(), DialogInfo{CallID: "in-call-reinvite-reject"}); err != nil {
		t.Fatalf("AckInboundCall(initial) error = %v", err)
	}

	result, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-reinvite-reject",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		CSeq:      4,
		RawSDP:    []byte(sampleSDP("203.0.113.20", 49172)),
	})
	if err != nil {
		t.Fatalf("HandleInboundInvite(re-INVITE) error = %v", err)
	}
	if result.Accepted || result.StatusCode != 488 || result.Reason != "Not Acceptable Here" {
		t.Fatalf("re-INVITE result=%+v", result)
	}
	if len(transport.requests) != 2 || transport.requests[1].Method != "INVITE" || transport.requests[1].Headers["CSeq"] != "4 INVITE" {
		t.Fatalf("re-INVITE requests=%+v", transport.requests)
	}
	if len(transport.writes) != 2 || transport.writes[1].Method != "ACK" {
		t.Fatalf("ACK writes=%+v", transport.writes)
	}
	ack := transport.writes[1]
	if ack.Headers["CSeq"] != "4 ACK" || !strings.Contains(ack.Headers["To"], "client-tag") {
		t.Fatalf("ACK=%+v", ack)
	}
	if ack.Headers["Via"] == "" || ack.Headers["Via"] != transport.requests[1].Headers["Via"] {
		t.Fatalf("ACK Via=%q INVITE Via=%q", ack.Headers["Via"], transport.requests[1].Headers["Via"])
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-reinvite-reject"}); err != nil {
		t.Fatalf("EndInboundCall() error = %v", err)
	}
	if len(transport.requests) != 3 || transport.requests[2].Method != "BYE" || transport.requests[2].Headers["CSeq"] != "5 BYE" {
		t.Fatalf("BYE requests=%+v", transport.requests)
	}
}

func TestIMSInboundAgentAdvancesByeCSeqAfterFailure(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers: map[string][]string{
				"To":      {"<sip:user@ims.example>;tag=client-tag"},
				"Contact": {"<sip:client@192.0.2.50:5060>"},
			},
			Body: []byte(sampleSDP("192.0.2.50", 4002)),
		},
		{StatusCode: 503, Reason: "Try Later"},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
	}
	if _, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-bye-retry",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RawSDP:    []byte(sampleSDP("203.0.113.10", 49170)),
	}); err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-bye-retry"}); err == nil {
		t.Fatal("EndInboundCall() err=nil, want failed BYE")
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-bye-retry"}); err != nil {
		t.Fatalf("EndInboundCall() retry error = %v", err)
	}
	if len(transport.requests) != 3 || transport.requests[1].Method != "BYE" || transport.requests[2].Method != "BYE" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	if transport.requests[1].Headers["CSeq"] != "2 BYE" || transport.requests[2].Headers["CSeq"] != "3 BYE" {
		t.Fatalf("BYE CSeqs=%q/%q", transport.requests[1].Headers["CSeq"], transport.requests[2].Headers["CSeq"])
	}
}

func TestIMSInboundAgentUsesRTPRelay(t *testing.T) {
	transport := &fakeIMSVoiceTransport{responses: []voiceclient.SIPResponse{
		{
			StatusCode: 200,
			Reason:     "OK",
			Headers:    map[string][]string{"To": {"<sip:user@ims.example>;tag=client-tag"}},
			Body:       []byte(sampleSDP("127.0.0.1", 4002)),
		},
		{StatusCode: 200, Reason: "OK"},
	}}
	agent := &IMSInboundAgent{
		ClientTransport:  transport,
		ClientContactURI: "sip:client@127.0.0.1:5070",
		LocalContactURI:  "sip:vowifi@127.0.0.1:5060",
		MediaRelay: &RTPRelayConfig{
			ClientListenIP:    "127.0.0.1",
			ClientAdvertiseIP: "127.0.0.1",
			IMSListenIP:       "127.0.0.1",
			IMSAdvertiseIP:    "127.0.0.1",
		},
	}
	result, err := agent.HandleInboundInvite(context.Background(), InboundCallRequest{
		CallID:    "in-call-relay",
		CallerURI: "sip:+18005551212@ims.example",
		CalleeURI: "sip:user@ims.example",
		RemoteSDP: SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: 49170, Payloads: []int{0, 8, 101}, Direction: "sendrecv"},
		RawSDP:    []byte(sampleSDP("127.0.0.1", 49170)),
	})
	if err != nil {
		t.Fatalf("HandleInboundInvite() error = %v", err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "INVITE" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	clientOffer, err := ParseSDP(transport.requests[0].Body)
	if err != nil {
		t.Fatalf("ParseSDP(client offer) error = %v", err)
	}
	if clientOffer.ConnectionIP != "127.0.0.1" || clientOffer.MediaPort == 49170 || clientOffer.MediaPort <= 0 || clientOffer.RTCPPort <= 0 {
		t.Fatalf("client offer=%+v", clientOffer)
	}
	if result.LocalSDP.ConnectionIP != "127.0.0.1" || result.LocalSDP.MediaPort == 4002 || result.LocalSDP.MediaPort <= 0 || result.LocalSDP.RTCPPort <= 0 {
		t.Fatalf("IMS answer=%+v", result.LocalSDP)
	}
	if answer := string(result.RawSDP); !strings.Contains(answer, "c=IN IP4 127.0.0.1") || !strings.Contains(answer, "a=rtcp:") || strings.Contains(answer, "m=audio 4002") {
		t.Fatalf("IMS answer body=%q", answer)
	}
	if err := agent.EndInboundCall(context.Background(), DialogInfo{CallID: "in-call-relay"}); err != nil {
		t.Fatalf("EndInboundCall() error = %v", err)
	}
}
