package simauth

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"

	swusim "github.com/iniwex5/vowifi-go/engine/sim"
)

type fakeTransport struct {
	calls     []string
	responses []string
	err       error
}

func (f *fakeTransport) OpenLogicalChannel(aid string) (int, error) { return 1, nil }
func (f *fakeTransport) CloseLogicalChannel(channel int) error      { return nil }
func (f *fakeTransport) TransmitAPDU(channel int, hexAPDU string) (string, error) {
	f.calls = append(f.calls, hexAPDU)
	if f.err != nil {
		return "", f.err
	}
	if len(f.responses) == 0 {
		return "9000", nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func TestParseTLVListAndFCP(t *testing.T) {
	fcp := []byte{0x62, 0x0B, 0x80, 0x02, 0x00, 0x20, 0x82, 0x05, 0x21, 0x00, 0x00, 0x10, 0x02}
	if got := FileSizeFromFCP(fcp); got != 32 {
		t.Fatalf("FileSizeFromFCP() = %d, want 32", got)
	}
	recordLen, recordCount := RecordInfoFromFCP(fcp)
	if recordLen != 16 || recordCount != 2 {
		t.Fatalf("RecordInfoFromFCP() = %d/%d, want 16/2", recordLen, recordCount)
	}
	highTag := []byte{0x5F, 0x20, 0x03, 'b', 'o', 'a'}
	if v, ok := FindTLV(highTag, 0x5F20); !ok || string(v) != "boa" {
		t.Fatalf("FindTLV(high tag) = %q/%v, want boa/true", string(v), ok)
	}
}

func TestTransmitHandlesRetryLengthAndGetResponse(t *testing.T) {
	retry := &fakeTransport{responses: []string{"6C03", "0102039000"}}
	resp, err := Transmit(retry, 1, []byte{0x00, 0xB0, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatalf("Transmit(6C) error = %v", err)
	}
	if !reflect.DeepEqual(resp.Body, []byte{1, 2, 3}) {
		t.Fatalf("retry body = % X", resp.Body)
	}
	if !reflect.DeepEqual(retry.calls, []string{"00B0000000", "00B0000003"}) {
		t.Fatalf("retry calls = %#v", retry.calls)
	}

	getResponse := &fakeTransport{responses: []string{"AA6102", "BBCC9000"}}
	resp, err = Transmit(getResponse, 1, []byte{0x00, 0xA4, 0x00, 0x04, 0x02, 0x6F, 0x02})
	if err != nil {
		t.Fatalf("Transmit(61) error = %v", err)
	}
	if !reflect.DeepEqual(resp.Body, []byte{0xAA, 0xBB, 0xCC}) {
		t.Fatalf("get-response body = % X", resp.Body)
	}
	if !reflect.DeepEqual(getResponse.calls, []string{"00A40004026F02", "00C0000002"}) {
		t.Fatalf("get-response calls = %#v", getResponse.calls)
	}
}

func TestReadTransparentAndLinearFixedEF(t *testing.T) {
	ft := &fakeTransport{responses: []string{
		"6204800200039000",
		"0102039000",
		"6207820521000005029000",
		"11223344559000",
		"66778899AA9000",
	}}
	data, _, err := ReadTransparentEF(ft, 1, 0x6F02)
	if err != nil {
		t.Fatalf("ReadTransparentEF() error = %v", err)
	}
	if !reflect.DeepEqual(data, []byte{1, 2, 3}) {
		t.Fatalf("transparent data = % X", data)
	}
	records, _, err := ReadLinearFixedEF(ft, 1, 0x6F04, 16)
	if err != nil {
		t.Fatalf("ReadLinearFixedEF() error = %v", err)
	}
	if len(records) != 2 || hex.EncodeToString(records[0]) != "1122334455" || hex.EncodeToString(records[1]) != "66778899aa" {
		t.Fatalf("records = % X", records)
	}
	wantCalls := []string{"00A40004026F02", "00B0000003", "00A40004026F04", "00B2010405", "00B2020405"}
	if !reflect.DeepEqual(ft.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", ft.calls, wantCalls)
	}
}

func TestBuildAndParseUSIMAuth(t *testing.T) {
	rand16 := make([]byte, 16)
	autn16 := make([]byte, 16)
	apdu, err := BuildUSIMAuthAPDU(rand16, autn16, true)
	if err != nil {
		t.Fatalf("BuildUSIMAuthAPDU() error = %v", err)
	}
	if got, want := hex.EncodeToString(apdu[:5]), "0088008122"; got != want {
		t.Fatalf("APDU prefix = %s, want %s", got, want)
	}
	if apdu[len(apdu)-1] != 0x00 {
		t.Fatalf("APDU Le = 0x%02X, want 0", apdu[len(apdu)-1])
	}

	body := append([]byte{0xDB, 0x02, 0x11, 0x22, 0x10}, bytesFrom(0x01, 16)...)
	body = append(body, 0x10)
	body = append(body, bytesFrom(0x21, 16)...)
	res, err := ParseUSIMAuthResponse(body, 0x90, 0x00)
	if err != nil {
		t.Fatalf("ParseUSIMAuthResponse() error = %v", err)
	}
	if len(res.RES) != 2 || len(res.CK) != 16 || len(res.IK) != 16 {
		t.Fatalf("AKA lengths = RES %d CK %d IK %d", len(res.RES), len(res.CK), len(res.IK))
	}

	res, err = ParseUSIMAuthResponse([]byte{0xDC, 0x02, 0xAA, 0xBB}, 0x90, 0x00)
	if !errors.Is(err, swusim.ErrSyncFailure) || hex.EncodeToString(res.AUTS) != "aabb" {
		t.Fatalf("sync failure = %+v err=%v, want AUTS and ErrSyncFailure", res, err)
	}

	_, err = ParseUSIMAuthResponse([]byte{0xDD, 0x00}, 0x90, 0x00)
	if !errors.Is(err, swusim.ErrAuthFailure) {
		t.Fatalf("auth failure err=%v, want ErrAuthFailure", err)
	}
}

func bytesFrom(start byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = start + byte(i)
	}
	return out
}
