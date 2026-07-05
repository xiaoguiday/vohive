package e911

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/engine/swu/eapaka"
	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

type fakeHTTPClient struct {
	responses []*HTTPResponse
	requests  []*HTTPRequest
}

func (f *fakeHTTPClient) Do(req *HTTPRequest) (*HTTPResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return &HTTPResponse{StatusCode: 500, Body: []byte(`{}`)}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

type fakeAKAProvider struct {
	rand  []byte
	autn  []byte
	calls int
}

func (f *fakeAKAProvider) CalculateAKA(rand16, autn16 []byte) (sim.AKAResult, error) {
	f.calls++
	f.rand = append([]byte(nil), rand16...)
	f.autn = append([]byte(nil), autn16...)
	return e911AKAResult(), nil
}

type authFailureAKAProvider struct {
	calls int
}

func (p *authFailureAKAProvider) CalculateAKA(rand16, autn16 []byte) (sim.AKAResult, error) {
	p.calls++
	return sim.AKAResult{}, sim.ErrAuthFailure
}

func TestStartEmergencyAddressUpdateReturnsWebsheetFromEntitlementToken(t *testing.T) {
	client := &fakeHTTPClient{responses: []*HTTPResponse{{
		StatusCode: 200,
		Body:       []byte(`[{"status":1000,"token":"abc123","title":"E911"}]`),
	}}}
	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280"},
		Client:   client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.UserData != "abc123" || !strings.Contains(ws.URL, "token=abc123") || ws.Title != "E911" {
		t.Fatalf("websheet=%+v", ws)
	}
	if len(client.requests) != 1 || string(client.requests[0].Body) == "" {
		t.Fatalf("requests=%d body=%q", len(client.requests), client.requests[0].Body)
	}
}

func TestStartEmergencyAddressUpdateHandlesAKAChallenge(t *testing.T) {
	randHex := strings.ToUpper(hex.EncodeToString(bytesFrom(0x10, 16)))
	autnHex := strings.ToUpper(hex.EncodeToString(bytesFrom(0x40, 16)))
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":7,"rand":"` + randHex + `","autn":"` + autnHex + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280"},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests=%d, want challenge response", len(client.requests))
	}
	if got := strings.ToUpper(hex.EncodeToString(aka.rand)); got != randHex {
		t.Fatalf("AKA RAND=%s, want %s", got, randHex)
	}
	if got := string(client.requests[1].Body); !strings.Contains(got, "11223344") || !strings.Contains(got, "response-id") {
		t.Fatalf("AKA answer body=%s", got)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayChallenge(t *testing.T) {
	identity := "310280233641503@private.att.net"
	relayPacket := signedEAPRelayChallenge(t, identity, e911AKAResult())
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":9,"eap-relay-packet":"` + relayPacket + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	_, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests=%d, want relay challenge response", len(client.requests))
	}
	var payload []map[string]any
	if err := json.Unmarshal(client.requests[1].Body, &payload); err != nil {
		t.Fatalf("answer JSON error = %v body=%s", err, client.requests[1].Body)
	}
	relay, _ := payload[0]["eap-relay-packet"].(string)
	raw, err := base64.StdEncoding.DecodeString(relay)
	if err != nil {
		t.Fatalf("relay response base64 error = %v", err)
	}
	packet, err := eapaka.ParsePacket(raw)
	if err != nil {
		t.Fatalf("relay response parse error = %v", err)
	}
	resAttr, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeRES)
	if !ok || packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeChallenge {
		t.Fatalf("relay response packet=%+v", packet)
	}
	res, bits, err := resAttr.RESValue()
	if err != nil {
		t.Fatalf("RESValue() error = %v", err)
	}
	if bits != 32 || strings.ToUpper(hex.EncodeToString(res)) != "11223344" {
		t.Fatalf("RES bits=%d value=%s", bits, strings.ToUpper(hex.EncodeToString(res)))
	}
}

func TestStartEmergencyAddressUpdateCapturesEAPRelayEncryptedIdentityState(t *testing.T) {
	identity := "310280233641503@private.att.net"
	akaResult := e911AKAResult()
	keys, err := eapaka.DeriveKeys(identity, akaResult)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	iv := bytesFrom(0x70, 16)
	encrypted, err := eapaka.EncryptAttributes(keys.KEncr, iv, []eapaka.Attribute{
		eapaka.NextPseudonymAttribute("pseudo-e911"),
		eapaka.NextReauthIDAttribute("reauth-e911"),
	})
	if err != nil {
		t.Fatalf("EncryptAttributes() error = %v", err)
	}
	relayPacket := signedEAPRelayChallengeWithEncryptedAttrs(t, identity, akaResult, eapaka.IVAttribute(iv), encrypted)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":19,"eap-relay-packet":"` + relayPacket + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.EAPNextPseudonym != "pseudo-e911" || ws.EAPNextReauthID != "reauth-e911" {
		t.Fatalf("websheet EAP state pseudonym=%q reauth=%q", ws.EAPNextPseudonym, ws.EAPNextReauthID)
	}
	if aka.calls != 1 {
		t.Fatalf("AKA calls=%d, want one AKA calculation", aka.calls)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayAuthenticationReject(t *testing.T) {
	identity := "310280233641503@private.att.net"
	relayPacket := signedEAPRelayChallenge(t, identity, e911AKAResult())
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":18,"eap-relay-packet":"` + relayPacket + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &authFailureAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if aka.calls != 1 || len(client.requests) != 2 {
		t.Fatalf("AKA calls=%d requests=%d", aka.calls, len(client.requests))
	}
	answer := decodeEntitlementAnswer(t, client.requests[1].Body)
	if _, ok := answer["aka-res"]; ok {
		t.Fatalf("authentication reject answer must not include AKA RES: %s", client.requests[1].Body)
	}
	packet := decodeRelayPacket(t, answer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeAuthenticationReject || len(packet.Attributes) != 0 {
		t.Fatalf("authentication reject relay response=%+v", packet)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayIdentityThenChallenge(t *testing.T) {
	identity := "310280233641503@private.att.net"
	identityRequest, identityRequestRaw := eapRelayIdentityRequestRaw(t)
	identityResponseRaw, err := (eapaka.Packet{
		Code:       eapaka.CodeResponse,
		Identifier: 6,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeIdentity,
		Attributes: []eapaka.Attribute{eapaka.IdentityAttribute(identity)},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(identity response) error = %v", err)
	}
	identityTranscript := [][]byte{identityRequestRaw, identityResponseRaw}
	relayPacket := signedEAPRelayChallengeWithCheckcode(t, identity, e911AKAResult(), identityTranscript)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":12,"eap-relay-packet":"` + identityRequest + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":13,"eap-relay-packet":"` + relayPacket + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests=%d, want identity response, AKA response, websheet", len(client.requests))
	}
	first := decodeEntitlementAnswer(t, client.requests[1].Body)
	firstPacket := decodeRelayPacket(t, first)
	if firstPacket.Code != eapaka.CodeResponse || firstPacket.Subtype != eapaka.SubtypeIdentity {
		t.Fatalf("identity relay response=%+v", firstPacket)
	}
	idAttr, ok := eapaka.FindAttribute(firstPacket.Attributes, eapaka.AttributeIdentity)
	if !ok {
		t.Fatalf("identity relay response missing AT_IDENTITY: %+v", firstPacket)
	}
	gotIdentity, err := idAttr.IdentityValue()
	if err != nil {
		t.Fatalf("IdentityValue() error = %v", err)
	}
	if gotIdentity != identity {
		t.Fatalf("identity=%q, want %q", gotIdentity, identity)
	}
	second := decodeEntitlementAnswer(t, client.requests[2].Body)
	secondPacket := decodeRelayPacket(t, second)
	if secondPacket.Code != eapaka.CodeResponse || secondPacket.Subtype != eapaka.SubtypeChallenge {
		t.Fatalf("challenge relay response=%+v", secondPacket)
	}
	checkcodeAttr, ok := eapaka.FindAttribute(secondPacket.Attributes, eapaka.AttributeCheckcode)
	if !ok {
		t.Fatal("challenge relay response missing AT_CHECKCODE")
	}
	if err := eapaka.VerifyCheckcodeAttribute(checkcodeAttr, identityTranscript); err != nil {
		t.Fatalf("VerifyCheckcodeAttribute(response) error = %v", err)
	}
	if aka.calls != 1 {
		t.Fatalf("AKA calls=%d, want one AKA calculation after identity", aka.calls)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayAKAPrimeKDFNegotiation(t *testing.T) {
	identity := "310280233641503@private.att.net"
	kdfOffer := eapRelayAKAPrimeKDFOffer(t, "WLAN", []uint16{99, eapaka.AKAPrimeKDFDefault})
	selectedChallenge := signedEAPRelayAKAPrimeChallenge(t, identity, "WLAN", e911AKAResult(), []uint16{eapaka.AKAPrimeKDFDefault, 99})
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":10,"eap-relay-packet":"` + kdfOffer + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":11,"eap-relay-packet":"` + selectedChallenge + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests=%d, want KDF negotiation then AKA response", len(client.requests))
	}
	first := decodeEntitlementAnswer(t, client.requests[1].Body)
	if _, ok := first["aka-res"]; ok {
		t.Fatalf("KDF negotiation answer must not include AKA RES: %s", client.requests[1].Body)
	}
	firstPacket := decodeRelayPacket(t, first)
	if len(firstPacket.Attributes) != 1 || firstPacket.Attributes[0].Type != eapaka.AttributeKDF {
		t.Fatalf("KDF negotiation packet=%+v", firstPacket)
	}
	kdf, err := firstPacket.Attributes[0].KDFValue()
	if err != nil {
		t.Fatalf("KDFValue() error = %v", err)
	}
	if kdf != eapaka.AKAPrimeKDFDefault {
		t.Fatalf("AT_KDF=%d", kdf)
	}
	second := decodeEntitlementAnswer(t, client.requests[2].Body)
	if strings.ToUpper(stringValue(second["aka-res"])) != "11223344" {
		t.Fatalf("AKA answer body=%s", client.requests[2].Body)
	}
	secondPacket := decodeRelayPacket(t, second)
	if secondPacket.Type != eapaka.TypeAKAPrime {
		t.Fatalf("AKA' relay response=%+v", secondPacket)
	}
	if _, ok := eapaka.FindAttribute(secondPacket.Attributes, eapaka.AttributeRES); !ok {
		t.Fatalf("AKA' relay response missing AT_RES: %+v", secondPacket)
	}
	if aka.calls != 1 {
		t.Fatalf("AKA calls=%d, want only selected challenge to use SIM", aka.calls)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayNotification(t *testing.T) {
	notification := eapRelayNotificationRequest(t, eapaka.NotificationGeneralFailureBeforeAuthentication)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":14,"eap-relay-packet":"` + notification + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280"},
		Client:   client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests=%d, want notification response", len(client.requests))
	}
	answer := decodeEntitlementAnswer(t, client.requests[1].Body)
	if _, ok := answer["aka-res"]; ok {
		t.Fatalf("notification answer must not include AKA RES: %s", client.requests[1].Body)
	}
	packet := decodeRelayPacket(t, answer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeNotification || len(packet.Attributes) != 0 {
		t.Fatalf("notification relay response=%+v", packet)
	}
}

func TestStartEmergencyAddressUpdateHandlesAuthenticatedEAPRelayNotification(t *testing.T) {
	identity := "310280233641503@private.att.net"
	akaResult := e911AKAResult()
	challenge := signedEAPRelayChallenge(t, identity, akaResult)
	notification := signedEAPRelayNotification(t, identity, akaResult, eapaka.NotificationSuccess)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":15,"eap-relay-packet":"` + challenge + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":16,"eap-relay-packet":"` + notification + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}
	aka := &fakeAKAProvider{}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity:    Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: identity},
		AKAProvider: aka,
		Client:      client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 3 {
		t.Fatalf("requests=%d, want challenge response then notification response", len(client.requests))
	}
	notificationAnswer := decodeEntitlementAnswer(t, client.requests[2].Body)
	if _, ok := notificationAnswer["aka-res"]; ok {
		t.Fatalf("notification answer must not include AKA RES: %s", client.requests[2].Body)
	}
	packet := decodeRelayPacket(t, notificationAnswer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeNotification {
		t.Fatalf("authenticated notification response=%+v", packet)
	}
	if len(packet.Attributes) != 1 || packet.Attributes[0].Type != eapaka.AttributeMAC {
		t.Fatalf("authenticated notification attributes=%+v", packet.Attributes)
	}
	keys, err := eapaka.DeriveKeys(identity, akaResult)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	if err := eapaka.VerifyMAC(keys.KAut, raw, nil); err != nil {
		t.Fatalf("VerifyMAC(notification response) error = %v", err)
	}
	if aka.calls != 1 {
		t.Fatalf("AKA calls=%d, want only challenge to use SIM", aka.calls)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayReauthentication(t *testing.T) {
	fullIdentity := "310280233641503@private.att.net"
	reauthIdentity := "reauth-e911"
	akaResult := e911AKAResult()
	keys, err := eapaka.DeriveKeys(fullIdentity, akaResult)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	nonceS := []byte("0123456789abcdef")
	reauth := signedEAPRelayReauthenticationRequest(t, eapaka.TypeAKA, keys, 3, nonceS, []eapaka.Attribute{
		eapaka.NextPseudonymAttribute("pseudo-next"),
		eapaka.NextReauthIDAttribute("reauth-next"),
	}, nil)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":20,"eap-relay-packet":"` + reauth + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: fullIdentity},
		EAPReauthentication: swu.EAPReauthenticationState{
			Identity:  reauthIdentity,
			Counter:   2,
			CounterOK: true,
			Keys:      keys,
		},
		Client: client,
		Random: bytes.NewReader(bytes.Repeat([]byte{0x88}, 32)),
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests=%d, want reauthentication response", len(client.requests))
	}
	answer := decodeEntitlementAnswer(t, client.requests[1].Body)
	if _, ok := answer["aka-res"]; ok {
		t.Fatalf("reauth answer must not include AKA RES: %s", client.requests[1].Body)
	}
	packet := decodeRelayPacket(t, answer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeReauthentication {
		t.Fatalf("reauth relay response=%+v", packet)
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(response) error = %v", err)
	}
	if err := eapaka.VerifyMAC(keys.KAut, raw, nonceS); err != nil {
		t.Fatalf("VerifyMAC(response) error = %v", err)
	}
	attrs := decryptedEAPRelayReauthenticationResponseAttrs(t, keys, packet)
	counterAttr, ok := eapaka.FindAttribute(attrs, eapaka.AttributeCounter)
	if !ok {
		t.Fatalf("reauth response missing AT_COUNTER: %+v", attrs)
	}
	counter, err := counterAttr.CounterValue()
	if err != nil {
		t.Fatalf("CounterValue() error = %v", err)
	}
	if counter != 3 {
		t.Fatalf("counter=%d", counter)
	}
	if _, ok := eapaka.FindAttribute(attrs, eapaka.AttributeCounterTooSmall); ok {
		t.Fatalf("normal reauth response included AT_COUNTER_TOO_SMALL: %+v", attrs)
	}
	expectedKeys, err := eapaka.DeriveReauthenticationKeys(reauthIdentity, keys, 3, nonceS)
	if err != nil {
		t.Fatalf("DeriveReauthenticationKeys() error = %v", err)
	}
	state := ws.EAPReauthentication
	if state.Identity != "reauth-next" || state.NextPseudonym != "pseudo-next" || state.Counter != 3 || !state.CounterOK || !state.Reauthenticated || state.CounterTooSmall {
		t.Fatalf("reauth state=%+v", state)
	}
	if state.LastAcceptedCounter != 3 || state.LastRejectedCounter != 0 {
		t.Fatalf("reauth counters accepted=%d rejected=%d", state.LastAcceptedCounter, state.LastRejectedCounter)
	}
	if !bytes.Equal(state.Keys.MSK, expectedKeys.MSK) || !bytes.Equal(state.Keys.EMSK, expectedKeys.EMSK) {
		t.Fatalf("reauth keys=%+v, want derived keys", state.Keys)
	}
	if ws.EAPNextReauthID != "reauth-next" || ws.EAPNextPseudonym != "pseudo-next" {
		t.Fatalf("websheet EAP aliases reauth=%q pseudonym=%q", ws.EAPNextReauthID, ws.EAPNextPseudonym)
	}
}

func TestStartEmergencyAddressUpdateHandlesEAPRelayReauthenticationCounterTooSmall(t *testing.T) {
	fullIdentity := "310280233641503@private.att.net"
	reauthIdentity := "reauth-e911"
	akaResult := e911AKAResult()
	keys, err := eapaka.DeriveKeys(fullIdentity, akaResult)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	nonceS := []byte("0123456789abcdef")
	reauth := signedEAPRelayReauthenticationRequest(t, eapaka.TypeAKA, keys, 2, nonceS, nil, nil)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":21,"eap-relay-packet":"` + reauth + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280", SIPUsername: fullIdentity},
		EAPReauthentication: swu.EAPReauthenticationState{
			Identity:  reauthIdentity,
			Counter:   5,
			CounterOK: true,
			Keys:      keys,
		},
		Client: client,
		Random: bytes.NewReader(bytes.Repeat([]byte{0x99}, 32)),
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	answer := decodeEntitlementAnswer(t, client.requests[1].Body)
	packet := decodeRelayPacket(t, answer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeReauthentication {
		t.Fatalf("counter-too-small relay response=%+v", packet)
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(response) error = %v", err)
	}
	if err := eapaka.VerifyMAC(keys.KAut, raw, nonceS); err != nil {
		t.Fatalf("VerifyMAC(response) error = %v", err)
	}
	attrs := decryptedEAPRelayReauthenticationResponseAttrs(t, keys, packet)
	if tooSmall, ok := eapaka.FindAttribute(attrs, eapaka.AttributeCounterTooSmall); !ok {
		t.Fatalf("missing AT_COUNTER_TOO_SMALL in attrs=%+v", attrs)
	} else if err := tooSmall.CounterTooSmallValue(); err != nil {
		t.Fatalf("CounterTooSmallValue() error = %v", err)
	}
	state := ws.EAPReauthentication
	if state.Identity != reauthIdentity || state.Counter != 5 || !state.CounterOK || state.Reauthenticated || !state.CounterTooSmall || state.LastRejectedCounter != 2 {
		t.Fatalf("counter-too-small state=%+v", state)
	}
	if !bytes.Equal(state.Keys.MSK, keys.MSK) || !bytes.Equal(state.Keys.EMSK, keys.EMSK) {
		t.Fatal("counter-too-small response must preserve previous reauth keys")
	}
}

func TestStartEmergencyAddressUpdateSendsClientErrorForUnsupportedEAPRelaySubtype(t *testing.T) {
	unsupported := eapRelayUnsupportedRequest(t)
	client := &fakeHTTPClient{responses: []*HTTPResponse{
		{StatusCode: 200, Body: []byte(`{"status":6004,"response-id":17,"eap-relay-packet":"` + unsupported + `"}`)},
		{StatusCode: 200, Body: []byte(`{"status":1000,"websheet-url":"https://example.test/address?ok=1"}`)},
	}}

	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Identity: Identity{IMSI: "310280233641503", IMEI: "356306952701762", MCC: "310", MNC: "280"},
		Client:   client,
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/address?ok=1" {
		t.Fatalf("URL=%q", ws.URL)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests=%d, want client-error response", len(client.requests))
	}
	answer := decodeEntitlementAnswer(t, client.requests[1].Body)
	if _, ok := answer["aka-res"]; ok {
		t.Fatalf("client-error answer must not include AKA RES: %s", client.requests[1].Body)
	}
	packet := decodeRelayPacket(t, answer)
	if packet.Code != eapaka.CodeResponse || packet.Subtype != eapaka.SubtypeClientError {
		t.Fatalf("client-error relay response=%+v", packet)
	}
	attr, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeClientErrorCode)
	if !ok {
		t.Fatalf("client-error relay response missing AT_CLIENT_ERROR_CODE: %+v", packet)
	}
	code, err := attr.ClientErrorCodeValue()
	if err != nil {
		t.Fatalf("ClientErrorCodeValue() error = %v", err)
	}
	if code != eapaka.ClientErrorUnableToProcessPacket {
		t.Fatalf("client error code=%d", code)
	}
}

func TestStartEmergencyAddressUpdateReportsIncompleteChallenge(t *testing.T) {
	client := &fakeHTTPClient{responses: []*HTTPResponse{{
		StatusCode: 200,
		Body:       []byte(`[{"status":6004,"response-id":3}]`),
	}}}
	_, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{
				Provider:            "att-ts43",
				Websheet:            "https://example.test/websheet",
				EntitlementEndpoint: "https://example.test/entitlement",
			},
		},
		Client: client,
	})
	if !errors.Is(err, ErrChallengeNotImplemented) {
		t.Fatalf("err=%v, want ErrChallengeNotImplemented", err)
	}
}

func TestStartEmergencyAddressUpdateFallsBackToConfiguredWebsheet(t *testing.T) {
	ws, err := StartEmergencyAddressUpdate(context.Background(), Request{
		Carrier: carrier.EffectiveCarrierConfig{
			E911: carrier.E911Config{Provider: "att-ts43", Websheet: "https://example.test/static"},
		},
	})
	if err != nil {
		t.Fatalf("StartEmergencyAddressUpdate() error = %v", err)
	}
	if ws.URL != "https://example.test/static" {
		t.Fatalf("URL=%q", ws.URL)
	}
}

func bytesFrom(start byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}

func e911AKAResult() sim.AKAResult {
	return sim.AKAResult{
		RES: []byte{0x11, 0x22, 0x33, 0x44},
		CK:  bytesFrom(0xA0, 16),
		IK:  bytesFrom(0xB0, 16),
	}
}

func eapRelayIdentityRequest(t *testing.T) string {
	t.Helper()
	encoded, _ := eapRelayIdentityRequestRaw(t)
	return encoded
}

func eapRelayIdentityRequestRaw(t *testing.T) (string, []byte) {
	t.Helper()
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 6,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeIdentity,
		Attributes: []eapaka.Attribute{eapaka.AnyIDReqAttribute()},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw), raw
}

func eapRelayNotificationRequest(t *testing.T, code uint16) string {
	t.Helper()
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 10,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeNotification,
		Attributes: []eapaka.Attribute{eapaka.NotificationAttribute(code)},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayChallenge(t *testing.T, identity string, aka sim.AKAResult) string {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, aka)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 7,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: []eapaka.Attribute{
			eapaka.RANDAttribute(bytesFrom(0x10, 16)),
			eapaka.AUTNAttribute(bytesFrom(0x40, 16)),
			eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayChallengeWithEncryptedAttrs(t *testing.T, identity string, aka sim.AKAResult, attrs ...eapaka.Attribute) string {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, aka)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	challengeAttrs := []eapaka.Attribute{
		eapaka.RANDAttribute(bytesFrom(0x10, 16)),
		eapaka.AUTNAttribute(bytesFrom(0x40, 16)),
	}
	challengeAttrs = append(challengeAttrs, attrs...)
	challengeAttrs = append(challengeAttrs, eapaka.MACAttribute(nil))
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 7,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: challengeAttrs,
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayChallengeWithCheckcode(t *testing.T, identity string, aka sim.AKAResult, transcript [][]byte) string {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, aka)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 7,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: []eapaka.Attribute{
			eapaka.RANDAttribute(bytesFrom(0x10, 16)),
			eapaka.AUTNAttribute(bytesFrom(0x40, 16)),
			eapaka.CheckcodeAttributeForPackets(transcript),
			eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayNotification(t *testing.T, identity string, aka sim.AKAResult, code uint16) string {
	t.Helper()
	keys, err := eapaka.DeriveKeys(identity, aka)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 11,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeNotification,
		Attributes: []eapaka.Attribute{
			eapaka.NotificationAttribute(code),
			eapaka.MACAttribute(nil),
		},
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	mac, err := eapaka.CalculateMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayReauthenticationRequest(t *testing.T, eapType uint8, keys eapaka.Keys, counter uint16, nonceS []byte, encryptedExtra, topLevelExtra []eapaka.Attribute) string {
	t.Helper()
	iv := bytes.Repeat([]byte{0x56}, 16)
	encryptedAttrs := []eapaka.Attribute{
		eapaka.CounterAttribute(counter),
		eapaka.NonceSAttribute(nonceS),
	}
	encryptedAttrs = append(encryptedAttrs, encryptedExtra...)
	encrypted, err := eapaka.EncryptAttributes(keys.KEncr, iv, encryptedAttrs)
	if err != nil {
		t.Fatalf("EncryptAttributes() error = %v", err)
	}
	attrs := []eapaka.Attribute{
		eapaka.IVAttribute(iv),
		encrypted,
	}
	attrs = append(attrs, topLevelExtra...)
	attrs = append(attrs, eapaka.MACAttribute(nil))
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 13,
		Type:       eapType,
		Subtype:    eapaka.SubtypeReauthentication,
		Attributes: attrs,
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	var mac []byte
	if eapType == eapaka.TypeAKAPrime {
		mac, err = eapaka.CalculateAKAPrimeMAC(keys.KAut, raw, nil)
	} else {
		mac, err = eapaka.CalculateMAC(keys.KAut, raw, nil)
	}
	if err != nil {
		t.Fatalf("CalculateMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func eapRelayUnsupportedRequest(t *testing.T) string {
	t.Helper()
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 12,
		Type:       eapaka.TypeAKA,
		Subtype:    eapaka.SubtypeReauthentication,
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func eapRelayAKAPrimeKDFOffer(t *testing.T, networkName string, kdfs []uint16) string {
	t.Helper()
	attrs := []eapaka.Attribute{
		eapaka.RANDAttribute(bytesFrom(0x10, 16)),
		eapaka.AUTNAttribute(bytesFrom(0x40, 16)),
		eapaka.KDFInputAttribute(networkName),
	}
	for _, kdf := range kdfs {
		attrs = append(attrs, eapaka.KDFAttribute(kdf))
	}
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 8,
		Type:       eapaka.TypeAKAPrime,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: attrs,
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func signedEAPRelayAKAPrimeChallenge(t *testing.T, identity, networkName string, aka sim.AKAResult, kdfs []uint16) string {
	t.Helper()
	autn := bytesFrom(0x40, 16)
	keys, err := eapaka.DeriveAKAPrimeKeys(identity, networkName, autn, aka)
	if err != nil {
		t.Fatalf("DeriveAKAPrimeKeys() error = %v", err)
	}
	attrs := []eapaka.Attribute{
		eapaka.RANDAttribute(bytesFrom(0x10, 16)),
		eapaka.AUTNAttribute(autn),
		eapaka.KDFInputAttribute(networkName),
	}
	for _, kdf := range kdfs {
		attrs = append(attrs, eapaka.KDFAttribute(kdf))
	}
	attrs = append(attrs, eapaka.MACAttribute(nil))
	packet := eapaka.Packet{
		Code:       eapaka.CodeRequest,
		Identifier: 9,
		Type:       eapaka.TypeAKAPrime,
		Subtype:    eapaka.SubtypeChallenge,
		Attributes: attrs,
	}
	raw, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	mac, err := eapaka.CalculateAKAPrimeMAC(keys.KAut, raw, nil)
	if err != nil {
		t.Fatalf("CalculateAKAPrimeMAC() error = %v", err)
	}
	packet.Attributes[len(packet.Attributes)-1] = eapaka.MACAttribute(mac)
	raw, err = packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func decodeEntitlementAnswer(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload []map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("answer JSON error = %v body=%s", err, body)
	}
	if len(payload) != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	return payload[0]
}

func decodeRelayPacket(t *testing.T, payload map[string]any) eapaka.Packet {
	t.Helper()
	relay, _ := payload["eap-relay-packet"].(string)
	raw, err := base64.StdEncoding.DecodeString(relay)
	if err != nil {
		t.Fatalf("relay response base64 error = %v", err)
	}
	packet, err := eapaka.ParsePacket(raw)
	if err != nil {
		t.Fatalf("relay response parse error = %v", err)
	}
	return packet
}

func decryptedEAPRelayReauthenticationResponseAttrs(t *testing.T, keys eapaka.Keys, packet eapaka.Packet) []eapaka.Attribute {
	t.Helper()
	ivAttr, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeIV)
	if !ok {
		t.Fatal("missing AT_IV")
	}
	encryptedAttr, ok := eapaka.FindAttribute(packet.Attributes, eapaka.AttributeEncrData)
	if !ok {
		t.Fatal("missing AT_ENCR_DATA")
	}
	attrs, err := eapaka.DecryptEncryptedAttributes(keys.KEncr, ivAttr, encryptedAttr)
	if err != nil {
		t.Fatalf("DecryptEncryptedAttributes() error = %v", err)
	}
	return attrs
}
