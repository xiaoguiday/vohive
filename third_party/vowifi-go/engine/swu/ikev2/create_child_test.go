package ikev2

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestRunCREATECHILDSADerivesChildKeys(t *testing.T) {
	init := fakeInitResult(t)
	localSPI := []byte{0xca, 0xfe, 0xba, 0xbe}
	remoteSPI := []byte{0xde, 0xad, 0xbe, 0xef}
	requestNonce := bytes.Repeat([]byte{0x11}, 32)
	responseNonce := bytes.Repeat([]byte{0x22}, 32)
	transport := &createChildTransport{
		t:             t,
		init:          init,
		messageID:     5,
		localSPI:      localSPI,
		remoteSPI:     remoteSPI,
		responseNonce: responseNonce,
	}
	res, err := RunCREATE_CHILD_SA(context.Background(), CreateChildSAConfig{
		Transport: transport,
		Init:      init,
		ChildSPI:  localSPI,
		Nonce:     requestNonce,
		MessageID: 5,
		IV:        bytes.Repeat([]byte{0x91}, init.Keys.Profile.EncryptionBlockSize),
	})
	if err != nil {
		t.Fatalf("RunCREATE_CHILD_SA() error = %v", err)
	}
	if transport.requests != 1 || !transport.sawSA || !transport.sawNonce || !transport.sawTSi || !transport.sawTSr {
		t.Fatalf("transport=%+v", transport)
	}
	if !bytes.Equal(res.RequestNonce, requestNonce) || !bytes.Equal(res.ResponseNonce, responseNonce) ||
		res.NextMessageID != 6 || res.Rekeyed {
		t.Fatalf("res=%+v", res)
	}
	if !bytes.Equal(res.ChildSA.LocalSPI, localSPI) || !bytes.Equal(res.ChildSA.RemoteSPI, remoteSPI) {
		t.Fatalf("child SA=%+v", res.ChildSA)
	}
	expected, err := DeriveChildSAKeysWithNonces(init.Keys.Profile.PRF, init.Keys.SKD, requestNonce, responseNonce, DefaultESPProposal(remoteSPI))
	if err != nil {
		t.Fatalf("DeriveChildSAKeysWithNonces() error = %v", err)
	}
	if !bytes.Equal(res.ChildSA.Keys.Outbound.EncryptionKey, expected.Outbound.EncryptionKey) ||
		!bytes.Equal(res.ChildSA.Keys.Inbound.IntegrityKey, expected.Inbound.IntegrityKey) {
		t.Fatalf("child keys=%+v expected=%+v", res.ChildSA.Keys, expected)
	}
}

func TestBuildCreateChildSAPayloadsIncludesRekeyNotify(t *testing.T) {
	init := fakeInitResult(t)
	payloads, nonce, localSPI, err := BuildCreateChildSAPayloads(CreateChildSAConfig{
		Init:     init,
		ChildSPI: []byte{0x01, 0x02, 0x03, 0x04},
		RekeySPI: []byte{0xaa, 0xbb, 0xcc, 0xdd},
		Nonce:    bytes.Repeat([]byte{0x33}, 32),
	})
	if err != nil {
		t.Fatalf("BuildCreateChildSAPayloads() error = %v", err)
	}
	if !bytes.Equal(nonce, bytes.Repeat([]byte{0x33}, 32)) || !bytes.Equal(localSPI, []byte{1, 2, 3, 4}) {
		t.Fatalf("nonce=%x localSPI=%x", nonce, localSPI)
	}
	if len(payloads) != 5 || payloads[0].Type != PayloadNotify || payloads[1].Type != PayloadSA {
		t.Fatalf("payloads=%+v", payloads)
	}
	notify, err := ParseNotify(payloads[0].Body)
	if err != nil {
		t.Fatalf("ParseNotify() error = %v", err)
	}
	if notify.ProtocolID != ProtocolESP || notify.NotifyType != NotifyRekeySA ||
		!bytes.Equal(notify.SPI, []byte{0xaa, 0xbb, 0xcc, 0xdd}) {
		t.Fatalf("notify=%+v", notify)
	}
}

func TestRunCREATECHILDSARejectsMissingResponseNonce(t *testing.T) {
	init := fakeInitResult(t)
	transport := &createChildTransport{
		t:                 t,
		init:              init,
		messageID:         7,
		localSPI:          []byte{0xca, 0xfe, 0xba, 0xbe},
		remoteSPI:         []byte{0xde, 0xad, 0xbe, 0xef},
		omitResponseNonce: true,
	}
	_, err := RunCREATE_CHILD_SA(context.Background(), CreateChildSAConfig{
		Transport: transport,
		Init:      init,
		ChildSPI:  transport.localSPI,
		Nonce:     bytes.Repeat([]byte{0x44}, 32),
		MessageID: 7,
		IV:        bytes.Repeat([]byte{0x92}, init.Keys.Profile.EncryptionBlockSize),
	})
	if !errors.Is(err, ErrInvalidCreateChild) {
		t.Fatalf("RunCREATE_CHILD_SA() err=%v, want ErrInvalidCreateChild", err)
	}
}

type createChildTransport struct {
	t                 *testing.T
	init              InitResult
	messageID         uint32
	localSPI          []byte
	remoteSPI         []byte
	responseNonce     []byte
	omitResponseNonce bool
	requests          int
	sawSA             bool
	sawNonce          bool
	sawTSi            bool
	sawTSr            bool
}

func (tr *createChildTransport) ExchangeIKE(ctx context.Context, request []byte) ([]byte, error) {
	tr.t.Helper()
	msg, inner, err := UnprotectMessage(request, tr.init.Keys, true)
	if err != nil {
		tr.t.Fatalf("UnprotectMessage(request) error = %v", err)
	}
	if msg.Header.ExchangeType != ExchangeCREATE_CHILD_SA || msg.Header.MessageID != tr.messageID ||
		msg.Header.Flags&FlagInitiator == 0 {
		tr.t.Fatalf("request header=%+v", msg.Header)
	}
	tr.requests++
	for _, payload := range inner {
		switch payload.Type {
		case PayloadSA:
			sa, err := ParseSecurityAssociation(payload.Body)
			if err != nil {
				tr.t.Fatalf("ParseSecurityAssociation() error = %v", err)
			}
			if len(sa.Proposals) != 1 || !bytes.Equal(sa.Proposals[0].SPI, tr.localSPI) {
				tr.t.Fatalf("request SA=%+v", sa)
			}
			tr.sawSA = true
		case PayloadNonce:
			if len(payload.Body) == 0 {
				tr.t.Fatalf("request nonce is empty")
			}
			tr.sawNonce = true
		case PayloadTSi:
			tr.sawTSi = true
		case PayloadTSr:
			tr.sawTSr = true
		}
	}
	saPayload, err := SecurityAssociationPayload(DefaultESPProposal(tr.remoteSPI))
	if err != nil {
		tr.t.Fatalf("SecurityAssociationPayload() error = %v", err)
	}
	tsiPayload, err := TrafficSelectorsPayload(PayloadTSi, IPv4AnyTrafficSelectors())
	if err != nil {
		tr.t.Fatalf("TrafficSelectorsPayload(TSi) error = %v", err)
	}
	tsrPayload, err := TrafficSelectorsPayload(PayloadTSr, IPv4AnyTrafficSelectors())
	if err != nil {
		tr.t.Fatalf("TrafficSelectorsPayload(TSr) error = %v", err)
	}
	responseInner := []Payload{saPayload}
	if !tr.omitResponseNonce {
		nonce := tr.responseNonce
		if len(nonce) == 0 {
			nonce = bytes.Repeat([]byte{0x55}, 32)
		}
		responseInner = append(responseInner, NoncePayload(nonce))
	}
	responseInner = append(responseInner, tsiPayload, tsrPayload)
	_, raw, err := ProtectMessage(
		createChildHeader(tr.init, tr.messageID, false),
		tr.init.Keys,
		false,
		responseInner,
		bytes.Repeat([]byte{0x93}, tr.init.Keys.Profile.EncryptionBlockSize),
	)
	if err != nil {
		tr.t.Fatalf("ProtectMessage(response) error = %v", err)
	}
	return raw, nil
}
