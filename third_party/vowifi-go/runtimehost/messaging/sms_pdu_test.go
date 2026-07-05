package messaging

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestBuildSMSSubmitTPDUGSM7(t *testing.T) {
	tpdu, err := BuildSMSSubmitTPDU("+18005551212", SMSPart{PartNo: 2, TotalParts: 2, Text: "hello", Encoding: "gsm7"}, 2)
	if err != nil {
		t.Fatalf("BuildSMSSubmitTPDU() error = %v", err)
	}
	got := strings.ToUpper(hex.EncodeToString(tpdu))
	want := "01020B918100551512F2000005E8329BFD06"
	if got != want {
		t.Fatalf("TPDU=%s want %s", got, want)
	}
}

func TestBuildSMSSubmitTPDUGSM7ExtendedCharacters(t *testing.T) {
	text := "^{}\\[~]|€\f"
	septets, err := encodeGSM7(text)
	if err != nil {
		t.Fatalf("encodeGSM7() error = %v", err)
	}
	gotSeptets := strings.ToUpper(hex.EncodeToString(septets))
	wantSeptets := "1B141B281B291B2F1B3C1B3D1B3E1B401B651B0A"
	if gotSeptets != wantSeptets {
		t.Fatalf("septets=%s want %s", gotSeptets, wantSeptets)
	}
	if decoded := decodeGSM7(septets); decoded != text {
		t.Fatalf("decoded=%q want %q", decoded, text)
	}

	tpdu, err := BuildSMSSubmitTPDU("+18005551212", SMSPart{Text: "cost {10}€", Encoding: "gsm7"}, 3)
	if err != nil {
		t.Fatalf("BuildSMSSubmitTPDU() error = %v", err)
	}
	if tpdu[11] != 0x00 || int(tpdu[12]) != 13 {
		t.Fatalf("DCS=0x%02x UDL=%d want GSM7/13 septets TPDU=%x", tpdu[11], tpdu[12], tpdu)
	}
	textOut, _, err := decodeSMSUserData(tpdu[13:], int(tpdu[12]), tpdu[11], false)
	if err != nil {
		t.Fatalf("decodeSMSUserData() error = %v", err)
	}
	if textOut != "cost {10}€" {
		t.Fatalf("decoded TPDU text=%q", textOut)
	}
}

func TestBuildSMSSubmitTPDUUCS2(t *testing.T) {
	tpdu, err := BuildSMSSubmitTPDU("10086", SMSPart{Text: "你", Encoding: "ucs2"}, 1)
	if err != nil {
		t.Fatalf("BuildSMSSubmitTPDU() error = %v", err)
	}
	got := strings.ToUpper(hex.EncodeToString(tpdu))
	want := "010105810180F60008024F60"
	if got != want {
		t.Fatalf("TPDU=%s want %s", got, want)
	}
}

func TestBuildAndParseSMSRPData(t *testing.T) {
	tpdu, err := BuildSMSSubmitTPDU("+18005551212", SMSPart{Text: "hello", Encoding: "gsm7"}, 7)
	if err != nil {
		t.Fatalf("BuildSMSSubmitTPDU() error = %v", err)
	}
	rpData, err := BuildSMSRPData(7, "", tpdu)
	if err != nil {
		t.Fatalf("BuildSMSRPData() error = %v", err)
	}
	got := strings.ToUpper(hex.EncodeToString(rpData))
	want := "000700001201070B918100551512F2000005E8329BFD06"
	if got != want {
		t.Fatalf("RP-DATA=%s want %s", got, want)
	}
	rpMR, parsedTPDU, err := ParseSMSRPData(rpData)
	if err != nil {
		t.Fatalf("ParseSMSRPData() error = %v", err)
	}
	if rpMR != 7 || string(parsedTPDU) != string(tpdu) {
		t.Fatalf("rpMR=%d tpdu=%x want %d/%x", rpMR, parsedTPDU, 7, tpdu)
	}
}

func TestBuildSMSSubmitTPDUGSM7WithUDH(t *testing.T) {
	part := SMSPart{PartNo: 1, TotalParts: 2, Text: strings.Repeat("a", 153), Encoding: "gsm7", UDH: concatUDH(2, 1)}
	tpdu, err := BuildSMSSubmitTPDU("+18005551212", part, 1)
	if err != nil {
		t.Fatalf("BuildSMSSubmitTPDU() error = %v", err)
	}
	if tpdu[0] != 0x41 {
		t.Fatalf("first octet=0x%02x want UDHI set", tpdu[0])
	}
	if tpdu[12] != 160 {
		t.Fatalf("UDL=%d want 160 septets", tpdu[12])
	}
	if len(tpdu) != 13+140 {
		t.Fatalf("TPDU length=%d want %d", len(tpdu), 153)
	}
}

func TestParseSMSRPDUAckAndError(t *testing.T) {
	ack, err := ParseSMSRPDU(BuildSMSRPAck(0x22))
	if err != nil {
		t.Fatalf("ParseSMSRPDU(ack) error = %v", err)
	}
	if ack.Kind != SMSRPDUKindAck || ack.MR != 0x22 {
		t.Fatalf("ack=%+v", ack)
	}
	errRPDU, err := ParseSMSRPDU(BuildSMSRPError(0x23, SMSRPCauseTemporaryFailure))
	if err != nil {
		t.Fatalf("ParseSMSRPDU(error) error = %v", err)
	}
	if errRPDU.Kind != SMSRPDUKindError || errRPDU.MR != 0x23 || errRPDU.Cause != int(SMSRPCauseTemporaryFailure) {
		t.Fatalf("error rpdu=%+v", errRPDU)
	}
}

func TestParseSMSDeliverTPDUGSM7(t *testing.T) {
	tpdu := mustHex(t, "0005810180F600006270502143650005E8329BFD06")
	deliver, err := ParseSMSDeliverTPDU(tpdu)
	if err != nil {
		t.Fatalf("ParseSMSDeliverTPDU() error = %v", err)
	}
	if deliver.Sender != "10086" || deliver.Text != "hello" {
		t.Fatalf("deliver=%+v", deliver)
	}
	want := time.Date(2026, 7, 5, 12, 34, 56, 0, time.FixedZone("", 0))
	if !deliver.Timestamp.Equal(want) {
		t.Fatalf("timestamp=%s want %s", deliver.Timestamp, want)
	}
}

func TestParseSMSDeliverTPDUUCS2WithConcatUDH(t *testing.T) {
	tpdu := mustHex(t, "4005810180F6000862705021436500080500037A02014F60")
	deliver, err := ParseSMSDeliverTPDU(tpdu)
	if err != nil {
		t.Fatalf("ParseSMSDeliverTPDU() error = %v", err)
	}
	if deliver.Text != "你" || !deliver.Concat.IsConcat || deliver.Concat.Ref != 0x7a || deliver.Concat.Total != 2 || deliver.Concat.Seq != 1 {
		t.Fatalf("deliver=%+v", deliver)
	}
}

func TestParseSMSStatusReportTPDU(t *testing.T) {
	tpdu := mustHex(t, "02070B918100551512F2627050214365006270502144000000")
	report, err := ParseSMSStatusReportTPDU(tpdu)
	if err != nil {
		t.Fatalf("ParseSMSStatusReportTPDU() error = %v", err)
	}
	if report.Reference != 7 || report.Recipient != "+18005551212" || report.Status != 0 || report.State != "delivered" {
		t.Fatalf("report=%+v", report)
	}
}

func mustHex(tb testing.TB, s string) []byte {
	tb.Helper()
	out, err := hex.DecodeString(s)
	if err != nil {
		tb.Fatalf("DecodeString(%q) error = %v", s, err)
	}
	return out
}
