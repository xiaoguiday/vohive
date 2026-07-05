package eapaka

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestIdentityResponseMarshalParse(t *testing.T) {
	raw, err := (Packet{
		Code:       CodeResponse,
		Identifier: 7,
		Type:       TypeAKA,
		Subtype:    SubtypeIdentity,
		Attributes: []Attribute{IdentityAttribute("310280233641503")},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := "0207001c170500000e05000f33313032383032333336343135303300"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("packet=%x, want %s", raw, want)
	}
	parsed, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	if parsed.Code != CodeResponse || parsed.Type != TypeAKA || parsed.Subtype != SubtypeIdentity || len(parsed.Attributes) != 1 {
		t.Fatalf("parsed=%+v", parsed)
	}
	if parsed.Attributes[0].Type != AttributeIdentity {
		t.Fatalf("attr=%+v", parsed.Attributes[0])
	}
	identity, err := parsed.Attributes[0].IdentityValue()
	if err != nil {
		t.Fatalf("IdentityValue() error = %v", err)
	}
	if identity != "310280233641503" {
		t.Fatalf("identity=%q", identity)
	}
}

func TestChallengeResponseAttributes(t *testing.T) {
	raw, err := (Packet{
		Code:       CodeResponse,
		Identifier: 9,
		Type:       TypeAKA,
		Subtype:    SubtypeChallenge,
		Attributes: []Attribute{
			RESAttribute([]byte{0x11, 0x22, 0x33, 0x44}),
			FixedAttribute(AttributeMAC, make([]byte, 16)),
		},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	if parsed.Subtype != SubtypeChallenge || len(parsed.Attributes) != 2 {
		t.Fatalf("parsed=%+v", parsed)
	}
	res, bits, err := parsed.Attributes[0].RESValue()
	if err != nil {
		t.Fatalf("RESValue() error = %v", err)
	}
	if bits != 32 || hex.EncodeToString(res) != "11223344" {
		t.Fatalf("RES bits=%d value=%x", bits, res)
	}
	mac, err := parsed.Attributes[1].FixedValue(16)
	if err != nil {
		t.Fatalf("FixedValue() error = %v", err)
	}
	if hex.EncodeToString(mac) != "00000000000000000000000000000000" {
		t.Fatalf("MAC=%x", mac)
	}
}

func TestAKAPrimeKDFAttributes(t *testing.T) {
	raw, err := (Packet{
		Code:       CodeResponse,
		Identifier: 10,
		Type:       TypeAKAPrime,
		Subtype:    SubtypeChallenge,
		Attributes: []Attribute{
			KDFInputAttribute("WLAN"),
			KDFAttribute(1),
		},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	if parsed.Type != TypeAKAPrime || parsed.Attributes[0].Type != AttributeKDFInput || parsed.Attributes[1].Type != AttributeKDF {
		t.Fatalf("parsed=%+v", parsed)
	}
	if len(parsed.Attributes[1].Data) != 2 {
		t.Fatalf("AT_KDF data length=%d, want 2", len(parsed.Attributes[1].Data))
	}
	kdf, err := parsed.Attributes[1].KDFValue()
	if err != nil {
		t.Fatalf("KDFValue() error = %v", err)
	}
	if kdf != 1 {
		t.Fatalf("AT_KDF=%d", kdf)
	}
	networkName, err := parsed.Attributes[0].VariableValue()
	if err != nil {
		t.Fatalf("VariableValue() error = %v", err)
	}
	if string(networkName) != "WLAN" {
		t.Fatalf("networkName=%q", string(networkName))
	}
}

func TestNotificationAndClientErrorAttributes(t *testing.T) {
	raw, err := (Packet{
		Code:       CodeRequest,
		Identifier: 13,
		Type:       TypeAKA,
		Subtype:    SubtypeNotification,
		Attributes: []Attribute{NotificationAttribute(NotificationGeneralFailureBeforeAuthentication)},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(notification) error = %v", err)
	}
	want := "010d000c170c00000c014000"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("notification packet=%x, want %s", raw, want)
	}
	parsed, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket(notification) error = %v", err)
	}
	attr, ok := FindAttribute(parsed.Attributes, AttributeNotification)
	if !ok {
		t.Fatal("missing AT_NOTIFICATION")
	}
	code, err := attr.NotificationValue()
	if err != nil {
		t.Fatalf("NotificationValue() error = %v", err)
	}
	if code != NotificationGeneralFailureBeforeAuthentication {
		t.Fatalf("notification code=%d", code)
	}

	raw, err = (Packet{
		Code:       CodeResponse,
		Identifier: 14,
		Type:       TypeAKA,
		Subtype:    SubtypeClientError,
		Attributes: []Attribute{ClientErrorCodeAttribute(ClientErrorUnableToProcessPacket)},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary(client-error) error = %v", err)
	}
	want = "020e000c170e000016010000"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("client-error packet=%x, want %s", raw, want)
	}
	parsed, err = ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket(client-error) error = %v", err)
	}
	attr, ok = FindAttribute(parsed.Attributes, AttributeClientErrorCode)
	if !ok {
		t.Fatal("missing AT_CLIENT_ERROR_CODE")
	}
	clientError, err := attr.ClientErrorCodeValue()
	if err != nil {
		t.Fatalf("ClientErrorCodeValue() error = %v", err)
	}
	if clientError != ClientErrorUnableToProcessPacket {
		t.Fatalf("client error=%d", clientError)
	}
}

func TestCheckcodeAttribute(t *testing.T) {
	packets := [][]byte{
		{CodeRequest, 1, 0, 8, TypeAKA, SubtypeIdentity, 0, 0},
		{CodeResponse, 1, 0, 8, TypeAKA, SubtypeIdentity, 0, 0},
	}
	attr := CheckcodeAttributeForPackets(packets)
	value, err := attr.CheckcodeValue()
	if err != nil {
		t.Fatalf("CheckcodeValue() error = %v", err)
	}
	if len(value) != 20 {
		t.Fatalf("checkcode length=%d, want 20", len(value))
	}
	if err := VerifyCheckcodeAttribute(attr, packets); err != nil {
		t.Fatalf("VerifyCheckcodeAttribute() error = %v", err)
	}
	if err := VerifyCheckcodeAttribute(attr, [][]byte{packets[1], packets[0]}); !errors.Is(err, ErrInvalidCheckcode) {
		t.Fatalf("VerifyCheckcodeAttribute(reordered) err=%v, want ErrInvalidCheckcode", err)
	}

	empty := CheckcodeAttributeForPackets(nil)
	if value, err := empty.CheckcodeValue(); err != nil || len(value) != 0 {
		t.Fatalf("empty CheckcodeValue() value=%x err=%v", value, err)
	}
	if err := VerifyCheckcodeAttribute(empty, nil); err != nil {
		t.Fatalf("VerifyCheckcodeAttribute(empty) error = %v", err)
	}
}

func TestResultIndAttribute(t *testing.T) {
	attr := ResultIndAttribute()
	if attr.Type != AttributeResultInd {
		t.Fatalf("type=%d, want AT_RESULT_IND", attr.Type)
	}
	if len(attr.Data) != 2 || attr.Data[0] != 0 || attr.Data[1] != 0 {
		t.Fatalf("data=%x, want reserved zero bytes", attr.Data)
	}
	raw, err := attr.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := "87010000"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("AT_RESULT_IND=%x, want %s", raw, want)
	}
}

func TestIdentityStateFromAttributes(t *testing.T) {
	state, err := IdentityStateFromAttributes([]Attribute{
		NextPseudonymAttribute("pseudo-1"),
		NextReauthIDAttribute("reauth-1"),
		ResultIndAttribute(),
	})
	if err != nil {
		t.Fatalf("IdentityStateFromAttributes() error = %v", err)
	}
	if state.NextPseudonym != "pseudo-1" || state.NextReauthID != "reauth-1" {
		t.Fatalf("state=%+v", state)
	}
}

func TestReauthenticationAttributes(t *testing.T) {
	raw, err := MarshalAttributes([]Attribute{
		CounterAttribute(7),
		CounterTooSmallAttribute(),
		NonceSAttribute([]byte("1234567890abcdef")),
	})
	if err != nil {
		t.Fatalf("MarshalAttributes() error = %v", err)
	}
	want := "13010007140100001505000031323334353637383930616263646566"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("reauth attrs=%x, want %s", raw, want)
	}
	attrs, err := ParseAttributes(raw)
	if err != nil {
		t.Fatalf("ParseAttributes() error = %v", err)
	}
	counter, err := attrs[0].CounterValue()
	if err != nil {
		t.Fatalf("CounterValue() error = %v", err)
	}
	if counter != 7 {
		t.Fatalf("counter=%d", counter)
	}
	if err := attrs[1].CounterTooSmallValue(); err != nil {
		t.Fatalf("CounterTooSmallValue() error = %v", err)
	}
	nonce, err := attrs[2].NonceSValue()
	if err != nil {
		t.Fatalf("NonceSValue() error = %v", err)
	}
	if string(nonce) != "1234567890abcdef" {
		t.Fatalf("nonce_s=%q", string(nonce))
	}
}

func TestAKAChallengeAttributes(t *testing.T) {
	raw, err := (Packet{
		Code:       CodeRequest,
		Identifier: 11,
		Type:       TypeAKA,
		Subtype:    SubtypeChallenge,
		Attributes: []Attribute{
			RANDAttribute([]byte("1234567890abcdef")),
			AUTNAttribute([]byte("fedcba0987654321")),
			FullAuthIDReqAttribute(),
		},
	}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	randAttr, ok := FindAttribute(parsed.Attributes, AttributeRAND)
	if !ok {
		t.Fatal("missing AT_RAND")
	}
	rands, err := randAttr.RANDValues()
	if err != nil {
		t.Fatalf("RANDValues() error = %v", err)
	}
	if len(rands) != 1 || string(rands[0]) != "1234567890abcdef" {
		t.Fatalf("RAND=%q", rands)
	}
	autnAttr, ok := FindAttribute(parsed.Attributes, AttributeAUTN)
	if !ok {
		t.Fatal("missing AT_AUTN")
	}
	autn, err := autnAttr.AUTNValue()
	if err != nil {
		t.Fatalf("AUTNValue() error = %v", err)
	}
	if string(autn) != "fedcba0987654321" {
		t.Fatalf("AUTN=%q", string(autn))
	}
	if _, ok := FindAttribute(parsed.Attributes, AttributeFullAuthIDReq); !ok {
		t.Fatal("missing AT_FULLAUTH_ID_REQ")
	}
}

func TestParseRejectsInvalidLengths(t *testing.T) {
	if _, err := ParsePacket([]byte{1, 2, 0, 3}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("ParsePacket() err=%v, want ErrInvalidPacket", err)
	}
	if _, err := ParseAttributes([]byte{AttributeIdentity, 0, 0, 0}); !errors.Is(err, ErrInvalidAttribute) {
		t.Fatalf("ParseAttributes() err=%v, want ErrInvalidAttribute", err)
	}
}
