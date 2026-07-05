package ikev2

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidInformational = errors.New("invalid ikev2 informational exchange")

type InformationalConfig struct {
	Transport     InitTransport
	Init          InitResult
	Keys          IKEKeys
	MessageID     uint32
	FromResponder bool
	Payloads      []Payload
	Random        io.Reader
	IV            []byte
}

type InformationalResult struct {
	RequestBytes  []byte
	ResponseBytes []byte
	ResponseInner []Payload
	NextMessageID uint32
}

func RunInformationalExchange(ctx context.Context, cfg InformationalConfig) (InformationalResult, error) {
	if cfg.Transport == nil {
		return InformationalResult{}, fmt.Errorf("%w: transport is nil", ErrInvalidInformational)
	}
	keys := cfg.Keys
	if keys.Profile.RequiredLength() == 0 {
		keys = cfg.Init.Keys
	}
	if err := validateKeySet(keys); err != nil {
		return InformationalResult{}, err
	}
	if cfg.Init.InitiatorSPI == 0 || cfg.Init.ResponderSPI == 0 {
		return InformationalResult{}, fmt.Errorf("%w: missing IKE SPIs", ErrInvalidInformational)
	}
	iv, err := informationalIV(cfg.Random, keys.Profile, cfg.IV)
	if err != nil {
		return InformationalResult{}, err
	}
	requestFromInitiator := !cfg.FromResponder
	_, reqBytes, err := BuildInformationalRequestFrom(cfg.Init, keys, cfg.MessageID, requestFromInitiator, cfg.Payloads, iv)
	if err != nil {
		return InformationalResult{}, err
	}
	respBytes, err := cfg.Transport.ExchangeIKE(ctx, reqBytes)
	if err != nil {
		return InformationalResult{}, err
	}
	_, inner, err := ParseInformationalResponseFrom(respBytes, cfg.Init, keys, cfg.MessageID, !requestFromInitiator)
	if err != nil {
		return InformationalResult{}, err
	}
	return InformationalResult{
		RequestBytes:  append([]byte(nil), reqBytes...),
		ResponseBytes: append([]byte(nil), respBytes...),
		ResponseInner: clonePayloads(inner),
		NextMessageID: cfg.MessageID + 1,
	}, nil
}

func RunLivenessCheck(ctx context.Context, cfg InformationalConfig) (InformationalResult, error) {
	cfg.Payloads = nil
	return RunInformationalExchange(ctx, cfg)
}

func BuildInformationalRequest(init InitResult, keys IKEKeys, messageID uint32, inner []Payload, iv []byte) (Message, []byte, error) {
	return BuildInformationalRequestFrom(init, keys, messageID, true, inner, iv)
}

func BuildInformationalResponse(init InitResult, keys IKEKeys, messageID uint32, inner []Payload, iv []byte) (Message, []byte, error) {
	return BuildInformationalResponseFrom(init, keys, messageID, false, inner, iv)
}

func BuildInformationalRequestFrom(init InitResult, keys IKEKeys, messageID uint32, fromInitiator bool, inner []Payload, iv []byte) (Message, []byte, error) {
	return ProtectMessage(informationalHeader(init, messageID, fromInitiator, false), keys, fromInitiator, inner, iv)
}

func BuildInformationalResponseFrom(init InitResult, keys IKEKeys, messageID uint32, fromInitiator bool, inner []Payload, iv []byte) (Message, []byte, error) {
	return ProtectMessage(informationalHeader(init, messageID, fromInitiator, true), keys, fromInitiator, inner, iv)
}

func ParseInformationalRequest(raw []byte, init InitResult, keys IKEKeys, messageID uint32) (Message, []Payload, error) {
	return ParseInformationalRequestFrom(raw, init, keys, messageID, true)
}

func ParseInformationalResponse(raw []byte, init InitResult, keys IKEKeys, messageID uint32) (Message, []Payload, error) {
	return ParseInformationalResponseFrom(raw, init, keys, messageID, false)
}

func ParseInformationalRequestFrom(raw []byte, init InitResult, keys IKEKeys, messageID uint32, fromInitiator bool) (Message, []Payload, error) {
	msg, inner, err := UnprotectMessage(raw, keys, fromInitiator)
	if err != nil {
		return Message{}, nil, err
	}
	if err := validateInformationalHeader(msg.Header, init, messageID, fromInitiator, false); err != nil {
		return Message{}, nil, err
	}
	return msg, inner, nil
}

func ParseInformationalResponseFrom(raw []byte, init InitResult, keys IKEKeys, messageID uint32, fromInitiator bool) (Message, []Payload, error) {
	msg, inner, err := UnprotectMessage(raw, keys, fromInitiator)
	if err != nil {
		return Message{}, nil, err
	}
	if err := validateInformationalHeader(msg.Header, init, messageID, fromInitiator, true); err != nil {
		return Message{}, nil, err
	}
	return msg, inner, nil
}

func informationalHeader(init InitResult, messageID uint32, fromInitiator bool, response bool) Header {
	flags := uint8(0)
	if fromInitiator {
		flags |= FlagInitiator
	}
	if response {
		flags |= FlagResponse
	}
	return Header{
		InitiatorSPI: init.InitiatorSPI,
		ResponderSPI: init.ResponderSPI,
		ExchangeType: ExchangeINFORMATIONAL,
		Flags:        flags,
		MessageID:    messageID,
	}
}

func validateInformationalHeader(h Header, init InitResult, messageID uint32, fromInitiator bool, response bool) error {
	if h.InitiatorSPI != init.InitiatorSPI || h.ResponderSPI != init.ResponderSPI ||
		h.ExchangeType != ExchangeINFORMATIONAL || h.MessageID != messageID {
		return fmt.Errorf("%w: unexpected header", ErrInvalidInformational)
	}
	expectedFlags := uint8(0)
	if fromInitiator {
		expectedFlags |= FlagInitiator
	}
	if response {
		expectedFlags |= FlagResponse
	}
	if h.Flags&(FlagInitiator|FlagResponse) != expectedFlags {
		return fmt.Errorf("%w: unexpected flags", ErrInvalidInformational)
	}
	return nil
}

func informationalIV(random io.Reader, profile KeyMaterialProfile, override []byte) ([]byte, error) {
	if len(override) > 0 {
		if len(override) != profile.EncryptionBlockSize {
			return nil, fmt.Errorf("%w: IV length %d != %d", ErrInvalidInformational, len(override), profile.EncryptionBlockSize)
		}
		return append([]byte(nil), override...), nil
	}
	return RandomIV(random, profile)
}
