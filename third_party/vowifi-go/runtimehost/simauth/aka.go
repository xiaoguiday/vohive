package simauth

import (
	"errors"
	"fmt"
	"strings"

	swusim "github.com/iniwex5/vowifi-go/engine/sim"
)

const (
	AKAAppPreferenceUSIM       = "usim"
	AKAAppPreferenceAuto       = "auto"
	AKAAppPreferenceISIM       = "isim"
	AKAAppPreferenceISIMStrict = "isim_strict"
)

type AKAResult = swusim.AKAResult

type AKAProvider struct {
	Transport LogicalChannelTransport
}

func NewAKAProvider(t LogicalChannelTransport) *AKAProvider {
	return &AKAProvider{Transport: t}
}

func (p *AKAProvider) CalculateAKA(rand16, autn16 []byte) (AKAResult, error) {
	return p.CalculateAKAWithPreference(rand16, autn16, AKAAppPreferenceUSIM)
}

func (p *AKAProvider) CalculateISIMAKA(rand16, autn16 []byte) (AKAResult, error) {
	return p.CalculateAKAWithPreference(rand16, autn16, AKAAppPreferenceISIMStrict)
}

func (p *AKAProvider) CalculateAKAWithPreference(rand16, autn16 []byte, preference string) (AKAResult, error) {
	if p == nil || p.Transport == nil {
		return AKAResult{}, errors.New("nil AKA transport")
	}
	pref := strings.ToLower(strings.TrimSpace(preference))
	if pref == "" {
		pref = AKAAppPreferenceUSIM
	}
	switch pref {
	case AKAAppPreferenceAuto, AKAAppPreferenceISIM, AKAAppPreferenceISIMStrict:
		if res, err := p.calculateAKAOnApp("isim", ISIMAIDPrefix, ISIMAIDPrefix, rand16, autn16); err == nil {
			return res, nil
		} else if pref == AKAAppPreferenceISIMStrict {
			return AKAResult{}, err
		}
		return p.calculateAKAOnApp("usim", USIMAIDPrefix, USIMAIDPrefix, rand16, autn16)
	default:
		return p.calculateAKAOnApp("usim", USIMAIDPrefix, USIMAIDPrefix, rand16, autn16)
	}
}

func (p *AKAProvider) calculateAKAOnApp(app, fallbackAID, expectedPrefix string, rand16, autn16 []byte) (AKAResult, error) {
	aid, source, err := ResolveAID(p.Transport, app, fallbackAID, expectedPrefix)
	if err != nil {
		return AKAResult{}, err
	}
	ch, err := p.Transport.OpenLogicalChannel(aid)
	if err != nil {
		return AKAResult{}, fmt.Errorf("open %s logical channel (%s): %w", strings.ToUpper(app), source, err)
	}
	defer func() { _ = p.Transport.CloseLogicalChannel(ch) }()

	apdu, err := BuildUSIMAuthAPDU(rand16, autn16, false)
	if err != nil {
		return AKAResult{}, err
	}
	resp, err := Transmit(p.Transport, ch, apdu)
	if err == nil && resp.Success() {
		return ParseUSIMAuthResponse(resp.Body, resp.SW1, resp.SW2)
	}

	apdu, buildErr := BuildUSIMAuthAPDU(rand16, autn16, true)
	if buildErr != nil {
		return AKAResult{}, buildErr
	}
	resp2, err2 := Transmit(p.Transport, ch, apdu)
	if err2 != nil {
		if err != nil {
			return AKAResult{}, fmt.Errorf("%s AKA failed: first=%v second=%v", strings.ToUpper(app), err, err2)
		}
		return AKAResult{}, err2
	}
	return ParseUSIMAuthResponse(resp2.Body, resp2.SW1, resp2.SW2)
}

func BuildUSIMAuthAPDU(rand16, autn16 []byte, includeLe bool) ([]byte, error) {
	if len(rand16) != 16 {
		return nil, fmt.Errorf("RAND length must be 16 bytes: %d", len(rand16))
	}
	if len(autn16) != 16 {
		return nil, fmt.Errorf("AUTN length must be 16 bytes: %d", len(autn16))
	}
	authData := make([]byte, 0, 1+16+1+16)
	authData = append(authData, 0x10)
	authData = append(authData, rand16...)
	authData = append(authData, 0x10)
	authData = append(authData, autn16...)

	apdu := make([]byte, 0, 5+len(authData)+1)
	apdu = append(apdu, 0x00, 0x88, 0x00, 0x81, byte(len(authData)))
	apdu = append(apdu, authData...)
	if includeLe {
		apdu = append(apdu, 0x00)
	}
	return apdu, nil
}

func ParseUSIMAuthResponse(body []byte, sw1, sw2 byte) (AKAResult, error) {
	if sw1 != 0x90 || sw2 != 0x00 {
		return AKAResult{}, fmt.Errorf("APDU status is not 9000: %02X%02X", sw1, sw2)
	}
	if len(body) < 2 {
		return AKAResult{}, fmt.Errorf("AKA response body too short: %d", len(body))
	}

	switch body[0] {
	case 0xDB:
		if out, ok := parseUSIMAuthDB(body); ok {
			return out, nil
		}
		if data, err := parseSimpleTLVData(body); err == nil {
			if out, ok := parseUSIMAuthDB(append([]byte{0xDB}, data...)); ok {
				return out, nil
			}
		}
		return AKAResult{}, errors.New("parse AKA success response failed")
	case 0xDC:
		data, err := parseSimpleTLVData(body)
		if err != nil {
			return AKAResult{}, err
		}
		return AKAResult{AUTS: append([]byte(nil), data...)}, swusim.ErrSyncFailure
	case 0xDD:
		return AKAResult{}, swusim.ErrAuthFailure
	default:
		return AKAResult{}, fmt.Errorf("unknown AKA response tag: 0x%02X", body[0])
	}
}

func parseSimpleTLVData(body []byte) ([]byte, error) {
	if len(body) < 2 {
		return nil, errors.New("response body too short")
	}
	l := int(body[1])
	if len(body) < 2+l {
		return nil, fmt.Errorf("response length mismatch: need=%d have=%d", 2+l, len(body))
	}
	return append([]byte(nil), body[2:2+l]...), nil
}

func parseUSIMAuthDB(body []byte) (AKAResult, bool) {
	if len(body) < 2 || body[0] != 0xDB {
		return AKAResult{}, false
	}
	pos := 1
	resLen := int(body[pos])
	pos++
	if resLen <= 0 || len(body) < pos+resLen+1 {
		return AKAResult{}, false
	}
	res := append([]byte(nil), body[pos:pos+resLen]...)
	pos += resLen

	remain := len(body) - pos
	if remain == 32 {
		return AKAResult{
			RES: res,
			CK:  append([]byte(nil), body[pos:pos+16]...),
			IK:  append([]byte(nil), body[pos+16:pos+32]...),
		}, true
	}

	ckLen := int(body[pos])
	pos++
	if ckLen <= 0 || len(body) < pos+ckLen+1 {
		return AKAResult{}, false
	}
	ck := append([]byte(nil), body[pos:pos+ckLen]...)
	pos += ckLen

	ikLen := int(body[pos])
	pos++
	if ikLen <= 0 || len(body) < pos+ikLen {
		return AKAResult{}, false
	}
	ik := append([]byte(nil), body[pos:pos+ikLen]...)
	return AKAResult{RES: res, CK: ck, IK: ik}, true
}
