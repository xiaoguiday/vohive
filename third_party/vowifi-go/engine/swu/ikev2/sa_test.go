package ikev2

import (
	"encoding/hex"
	"errors"
	"testing"
)

func TestDefaultIKEProposalMarshalParse(t *testing.T) {
	sa := DefaultIKEProposal()
	body, err := sa.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	want := "0000002c010100040300000c0100000c800e00800300000802000005030000080300000c000000080400001f"
	if hex.EncodeToString(body) != want {
		t.Fatalf("SA body=%x, want %s", body, want)
	}
	parsed, err := ParseSecurityAssociation(body)
	if err != nil {
		t.Fatalf("ParseSecurityAssociation() error = %v", err)
	}
	if len(parsed.Proposals) != 1 || len(parsed.Proposals[0].Transforms) != 4 {
		t.Fatalf("parsed=%+v", parsed)
	}
	encr := parsed.Proposals[0].Transforms[0]
	if encr.Type != TransformENCR || encr.ID != ENCR_AES_CBC || len(encr.Attributes) != 1 {
		t.Fatalf("ENCR transform=%+v", encr)
	}
	if encr.Attributes[0].Type != AttributeKeyLength || hex.EncodeToString(encr.Attributes[0].Value) != "0080" {
		t.Fatalf("ENCR attrs=%+v", encr.Attributes)
	}
}

func TestDefaultESPProposalIncludesSPI(t *testing.T) {
	body, err := DefaultESPProposal([]byte{0xaa, 0xbb, 0xcc, 0xdd}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParseSecurityAssociation(body)
	if err != nil {
		t.Fatalf("ParseSecurityAssociation() error = %v", err)
	}
	p := parsed.Proposals[0]
	if p.ProtocolID != ProtocolESP || hex.EncodeToString(p.SPI) != "aabbccdd" || len(p.Transforms) != 3 {
		t.Fatalf("proposal=%+v", p)
	}
}

func TestSecurityAssociationRejectsBadTransformCount(t *testing.T) {
	body := mustHex("0000002c010100050300000c0100000c800e00800300000802000005030000080300000c000000080400001f")
	_, err := ParseSecurityAssociation(body)
	if !errors.Is(err, ErrInvalidSA) {
		t.Fatalf("ParseSecurityAssociation() err=%v, want ErrInvalidSA", err)
	}
}
