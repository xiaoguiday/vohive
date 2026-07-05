package simauth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	USIMAIDPrefix = "A0000000871002"
	ISIMAIDPrefix = "A0000000871004"
)

type LogicalChannelTransport interface {
	OpenLogicalChannel(aid string) (int, error)
	CloseLogicalChannel(channel int) error
	TransmitAPDU(channel int, hexAPDU string) (string, error)
}

type LogicalChannelAIDResolver interface {
	ResolveLogicalChannelAID(app string, fallbackAID string) (resolvedAID string, source string, err error)
}

type Response struct {
	Body []byte
	SW1  byte
	SW2  byte
}

func (r Response) Success() bool {
	return r.SW1 == 0x90 && r.SW2 == 0x00
}

func (r Response) Status() uint16 {
	return uint16(r.SW1)<<8 | uint16(r.SW2)
}

func (r Response) StatusString() string {
	return fmt.Sprintf("%02X%02X", r.SW1, r.SW2)
}

func ResolveAID(t LogicalChannelTransport, app, fallbackAID, expectedPrefix string) (string, string, error) {
	fallback := strings.ToUpper(strings.TrimSpace(fallbackAID))
	want := strings.ToUpper(strings.TrimSpace(expectedPrefix))
	if resolver, ok := t.(LogicalChannelAIDResolver); ok {
		aid, source, err := resolver.ResolveLogicalChannelAID(app, fallback)
		if err == nil {
			aid = strings.ToUpper(strings.TrimSpace(aid))
			if strings.HasPrefix(aid, want) && len(aid) > len(want) {
				if strings.TrimSpace(source) == "" {
					source = "resolver"
				}
				return aid, source, nil
			}
			return "", "resolver_invalid", fmt.Errorf("%s AID does not match %s: %s", app, want, aid)
		}
	}
	if fallback == "" {
		return "", "missing", fmt.Errorf("%s AID is empty", app)
	}
	return fallback, "fallback", nil
}

func SelectFileAPDU(fid uint16) []byte {
	return []byte{0x00, 0xA4, 0x00, 0x04, 0x02, byte(fid >> 8), byte(fid)}
}

func ReadBinaryAPDU(offset, le int) []byte {
	if le <= 0 || le > 256 {
		le = 256
	}
	leByte := byte(le)
	if le == 256 {
		leByte = 0x00
	}
	return []byte{0x00, 0xB0, byte(offset >> 8), byte(offset), leByte}
}

func ReadRecordAPDU(record, le int) []byte {
	if le <= 0 || le > 256 {
		le = 256
	}
	leByte := byte(le)
	if le == 256 {
		leByte = 0x00
	}
	return []byte{0x00, 0xB2, byte(record), 0x04, leByte}
}

func Transmit(t LogicalChannelTransport, channel int, cmd []byte) (Response, error) {
	resp, err := transmitOnce(t, channel, cmd)
	if err != nil {
		return Response{}, err
	}
	if resp.SW1 == 0x6C {
		retry := append([]byte(nil), cmd...)
		if len(retry) >= 5 {
			retry[len(retry)-1] = resp.SW2
		} else {
			retry = append(retry, resp.SW2)
		}
		resp, err = transmitOnce(t, channel, retry)
		if err != nil {
			return Response{}, err
		}
	}
	if resp.SW1 == 0x61 {
		le := int(resp.SW2)
		if le == 0 {
			le = 256
		}
		getResp, err := transmitOnce(t, channel, []byte{0x00, 0xC0, 0x00, 0x00, byte(le)})
		if err != nil {
			return Response{}, err
		}
		getResp.Body = append(append([]byte(nil), resp.Body...), getResp.Body...)
		return getResp, nil
	}
	return resp, nil
}

func transmitOnce(t LogicalChannelTransport, channel int, cmd []byte) (Response, error) {
	if t == nil {
		return Response{}, errors.New("nil logical channel transport")
	}
	out, err := t.TransmitAPDU(channel, strings.ToUpper(hex.EncodeToString(cmd)))
	if err != nil {
		return Response{}, err
	}
	raw, err := hex.DecodeString(strings.TrimSpace(out))
	if err != nil {
		return Response{}, fmt.Errorf("decode APDU response: %w", err)
	}
	if len(raw) < 2 {
		return Response{}, fmt.Errorf("APDU response too short: %d", len(raw))
	}
	return Response{
		Body: append([]byte(nil), raw[:len(raw)-2]...),
		SW1:  raw[len(raw)-2],
		SW2:  raw[len(raw)-1],
	}, nil
}

func SelectFile(t LogicalChannelTransport, channel int, fid uint16) (Response, error) {
	resp, err := Transmit(t, channel, SelectFileAPDU(fid))
	if err != nil {
		return Response{}, err
	}
	if !resp.Success() {
		return resp, fmt.Errorf("SELECT %04X failed: SW=%s", fid, resp.StatusString())
	}
	return resp, nil
}

func ReadTransparentEF(t LogicalChannelTransport, channel int, fid uint16) ([]byte, Response, error) {
	selectResp, err := SelectFile(t, channel, fid)
	if err != nil {
		return nil, selectResp, err
	}
	size := FileSizeFromFCP(selectResp.Body)
	if size <= 0 {
		size = 256
	}
	var out []byte
	for offset := 0; offset < size; {
		chunk := size - offset
		if chunk > 256 {
			chunk = 256
		}
		resp, err := Transmit(t, channel, ReadBinaryAPDU(offset, chunk))
		if err != nil {
			return nil, resp, err
		}
		if !resp.Success() {
			return nil, resp, fmt.Errorf("READ BINARY %04X offset=%d failed: SW=%s", fid, offset, resp.StatusString())
		}
		out = append(out, resp.Body...)
		if len(resp.Body) == 0 || size == 256 && len(resp.Body) < chunk {
			break
		}
		offset += len(resp.Body)
	}
	return out, selectResp, nil
}

func ReadLinearFixedEF(t LogicalChannelTransport, channel int, fid uint16, maxRecords int) ([][]byte, Response, error) {
	selectResp, err := SelectFile(t, channel, fid)
	if err != nil {
		return nil, selectResp, err
	}
	if maxRecords <= 0 {
		maxRecords = 16
	}
	recordLen, recordCount := RecordInfoFromFCP(selectResp.Body)
	if recordCount > 0 && recordCount < maxRecords {
		maxRecords = recordCount
	}
	if recordLen <= 0 {
		recordLen = 256
	}
	var records [][]byte
	for rec := 1; rec <= maxRecords; rec++ {
		resp, err := Transmit(t, channel, ReadRecordAPDU(rec, recordLen))
		if err != nil {
			return nil, resp, err
		}
		if isRecordNotFound(resp.SW1, resp.SW2) {
			break
		}
		if !resp.Success() {
			return nil, resp, fmt.Errorf("READ RECORD %04X #%d failed: SW=%s", fid, rec, resp.StatusString())
		}
		records = append(records, append([]byte(nil), resp.Body...))
	}
	return records, selectResp, nil
}

func isRecordNotFound(sw1, sw2 byte) bool {
	return (sw1 == 0x6A && (sw2 == 0x82 || sw2 == 0x83)) ||
		(sw2 == 0x6A && (sw1 == 0x82 || sw1 == 0x83))
}
