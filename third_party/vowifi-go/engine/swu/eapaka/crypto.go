package eapaka

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"

	"github.com/iniwex5/vowifi-go/engine/sim"
)

const (
	KeyLengthKEncr        = 16
	KeyLengthKAut         = 16
	KeyLengthAKAPrimeKAut = 32
	KeyLengthKRe          = 32
	KeyLengthMSK          = 64
	KeyLengthEMSK         = 64
)

const AKAPrimeKDFDefault uint16 = 1

var (
	ErrInvalidAKAChallenge  = errors.New("invalid eap-aka challenge")
	ErrInvalidEncryptedData = errors.New("invalid eap-aka encrypted data")
	ErrInvalidMAC           = errors.New("invalid eap-aka mac")
	ErrInvalidKeyMaterial   = errors.New("invalid eap-aka key material")
	ErrInvalidReauth        = errors.New("invalid eap-aka reauthentication")
	ErrUnsupportedKDF       = errors.New("unsupported eap-aka prime kdf")
)

type Keys struct {
	MK      []byte
	KEncr   []byte
	KAut    []byte
	KRe     []byte
	MSK     []byte
	EMSK    []byte
	CKPrime []byte
	IKPrime []byte
}

func DeriveKeys(identity string, aka sim.AKAResult) (Keys, error) {
	if len(aka.IK) == 0 || len(aka.CK) == 0 {
		return Keys{}, fmt.Errorf("%w: IK/CK is empty", ErrInvalidKeyMaterial)
	}
	mkInput := make([]byte, 0, len(identity)+len(aka.IK)+len(aka.CK))
	mkInput = append(mkInput, []byte(identity)...)
	mkInput = append(mkInput, aka.IK...)
	mkInput = append(mkInput, aka.CK...)
	mkSum := sha1.Sum(mkInput)
	stream := fips1862PRF(mkSum[:], KeyLengthKEncr+KeyLengthKAut+KeyLengthMSK+KeyLengthEMSK)
	return Keys{
		MK:    append([]byte(nil), mkSum[:]...),
		KEncr: append([]byte(nil), stream[:KeyLengthKEncr]...),
		KAut:  append([]byte(nil), stream[KeyLengthKEncr:KeyLengthKEncr+KeyLengthKAut]...),
		MSK:   append([]byte(nil), stream[KeyLengthKEncr+KeyLengthKAut:KeyLengthKEncr+KeyLengthKAut+KeyLengthMSK]...),
		EMSK:  append([]byte(nil), stream[KeyLengthKEncr+KeyLengthKAut+KeyLengthMSK:]...),
	}, nil
}

func DeriveAKAPrimeKeys(identity, networkName string, autn16 []byte, aka sim.AKAResult) (Keys, error) {
	if identity == "" {
		return Keys{}, fmt.Errorf("%w: identity is empty", ErrInvalidKeyMaterial)
	}
	if networkName == "" {
		return Keys{}, fmt.Errorf("%w: network name is empty", ErrInvalidKeyMaterial)
	}
	if len(autn16) < 6 {
		return Keys{}, fmt.Errorf("%w: AUTN length %d", ErrInvalidKeyMaterial, len(autn16))
	}
	if len(aka.IK) == 0 || len(aka.CK) == 0 {
		return Keys{}, fmt.Errorf("%w: IK/CK is empty", ErrInvalidKeyMaterial)
	}
	if len(networkName) > 0xffff {
		return Keys{}, fmt.Errorf("%w: network name too long", ErrInvalidKeyMaterial)
	}
	ckPrimeIKPrime := deriveAKAPrimeCKIK([]byte(networkName), autn16[:6], aka)
	ckPrime := append([]byte(nil), ckPrimeIKPrime[:16]...)
	ikPrime := append([]byte(nil), ckPrimeIKPrime[16:]...)
	key := make([]byte, 0, len(ikPrime)+len(ckPrime))
	key = append(key, ikPrime...)
	key = append(key, ckPrime...)
	seed := make([]byte, 0, len("EAP-AKA'")+len(identity))
	seed = append(seed, []byte("EAP-AKA'")...)
	seed = append(seed, []byte(identity)...)
	stream := prfPrimeSHA256(key, seed, KeyLengthKEncr+KeyLengthAKAPrimeKAut+KeyLengthKRe+KeyLengthMSK+KeyLengthEMSK)
	offset := 0
	out := Keys{
		MK:      append([]byte(nil), stream...),
		KEncr:   append([]byte(nil), stream[offset:offset+KeyLengthKEncr]...),
		CKPrime: ckPrime,
		IKPrime: ikPrime,
	}
	offset += KeyLengthKEncr
	out.KAut = append([]byte(nil), stream[offset:offset+KeyLengthAKAPrimeKAut]...)
	offset += KeyLengthAKAPrimeKAut
	out.KRe = append([]byte(nil), stream[offset:offset+KeyLengthKRe]...)
	offset += KeyLengthKRe
	out.MSK = append([]byte(nil), stream[offset:offset+KeyLengthMSK]...)
	offset += KeyLengthMSK
	out.EMSK = append([]byte(nil), stream[offset:offset+KeyLengthEMSK]...)
	return out, nil
}

func BuildChallengeResponse(identity string, request Packet, aka sim.AKAResult) (Packet, Keys, error) {
	return BuildChallengeResponseWithCheckcode(identity, request, aka, nil)
}

func BuildChallengeResponseWithCheckcode(identity string, request Packet, aka sim.AKAResult, identityPackets [][]byte) (Packet, Keys, error) {
	if request.Code != CodeRequest || request.Subtype != SubtypeChallenge {
		return Packet{}, Keys{}, fmt.Errorf("%w: not an AKA challenge", ErrInvalidAKAChallenge)
	}
	if len(aka.RES) == 0 {
		return Packet{}, Keys{}, fmt.Errorf("%w: RES is empty", ErrInvalidKeyMaterial)
	}
	keys, selectedKDF, err := deriveChallengeKeys(identity, request, aka)
	if err != nil {
		return Packet{}, Keys{}, err
	}
	requestRaw, err := request.MarshalBinary()
	if err != nil {
		return Packet{}, Keys{}, err
	}
	if err := verifyChallengeMAC(request.Type, keys.KAut, requestRaw); err != nil {
		return Packet{}, Keys{}, err
	}
	includeCheckcode, err := verifyChallengeCheckcode(request, identityPackets)
	if err != nil {
		return Packet{}, Keys{}, err
	}
	responseAttrs := []Attribute{RESAttribute(aka.RES)}
	if request.Type == TypeAKAPrime {
		responseAttrs = append(responseAttrs, KDFAttribute(selectedKDF))
	}
	if includeCheckcode {
		responseAttrs = append(responseAttrs, CheckcodeAttributeForPackets(identityPackets))
	}
	if _, ok := FindAttribute(request.Attributes, AttributeResultInd); ok {
		responseAttrs = append(responseAttrs, ResultIndAttribute())
	}
	responseAttrs = append(responseAttrs, MACAttribute(nil))
	response := Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       request.Type,
		Subtype:    SubtypeChallenge,
		Attributes: responseAttrs,
	}
	raw, err := response.MarshalBinary()
	if err != nil {
		return Packet{}, Keys{}, err
	}
	mac, err := calculateChallengeMAC(response.Type, keys.KAut, raw)
	if err != nil {
		return Packet{}, Keys{}, err
	}
	response.Attributes[len(response.Attributes)-1] = MACAttribute(mac)
	return response, keys, nil
}

func verifyChallengeCheckcode(request Packet, identityPackets [][]byte) (bool, error) {
	attr, ok := FindAttribute(request.Attributes, AttributeCheckcode)
	if !ok {
		return false, nil
	}
	if err := VerifyCheckcodeAttribute(attr, identityPackets); err != nil {
		return false, err
	}
	return true, nil
}

func BuildAKAPrimeKDFNegotiationResponse(request Packet) (Packet, bool, error) {
	if request.Type != TypeAKAPrime {
		return Packet{}, false, nil
	}
	if request.Code != CodeRequest || request.Subtype != SubtypeChallenge {
		return Packet{}, false, fmt.Errorf("%w: not an AKA' challenge", ErrInvalidAKAChallenge)
	}
	values, err := kdfValues(request.Attributes)
	if err != nil {
		return Packet{}, false, err
	}
	if len(values) == 0 {
		return Packet{}, false, fmt.Errorf("%w: missing AT_KDF", ErrInvalidAKAChallenge)
	}
	if values[0] == AKAPrimeKDFDefault {
		return Packet{}, false, nil
	}
	for _, value := range values[1:] {
		if value == AKAPrimeKDFDefault {
			return Packet{
				Code:       CodeResponse,
				Identifier: request.Identifier,
				Type:       TypeAKAPrime,
				Subtype:    SubtypeChallenge,
				Attributes: []Attribute{KDFAttribute(AKAPrimeKDFDefault)},
			}, true, nil
		}
	}
	return Packet{}, false, fmt.Errorf("%w: offered %v", ErrUnsupportedKDF, values)
}

func BuildAuthenticationRejectResponse(request Packet) (Packet, error) {
	if request.Code != CodeRequest || request.Subtype != SubtypeChallenge {
		return Packet{}, fmt.Errorf("%w: not an AKA challenge", ErrInvalidAKAChallenge)
	}
	if !isAKAType(request.Type) {
		return Packet{}, fmt.Errorf("%w: EAP type %d", ErrInvalidAKAChallenge, request.Type)
	}
	eapType := request.Type
	if eapType == 0 {
		eapType = TypeAKA
	}
	return Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       eapType,
		Subtype:    SubtypeAuthenticationReject,
	}, nil
}

func BuildNotificationResponse(request Packet) (Packet, bool, error) {
	return buildNotificationResponse(request, nil, false)
}

func BuildAuthenticatedNotificationResponse(request Packet, kAut []byte) (Packet, bool, error) {
	return buildNotificationResponse(request, kAut, true)
}

func BuildClientErrorResponse(request Packet, code uint16) (Packet, error) {
	if request.Code != CodeRequest {
		return Packet{}, fmt.Errorf("%w: not an EAP-AKA request", ErrInvalidAKAChallenge)
	}
	if !isAKAType(request.Type) {
		return Packet{}, fmt.Errorf("%w: EAP type %d", ErrInvalidAKAChallenge, request.Type)
	}
	eapType := request.Type
	if eapType == 0 {
		eapType = TypeAKA
	}
	return Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       eapType,
		Subtype:    SubtypeClientError,
		Attributes: []Attribute{ClientErrorCodeAttribute(code)},
	}, nil
}

func BuildSynchronizationFailureResponse(request Packet, auts []byte) (Packet, error) {
	if request.Code != CodeRequest || request.Subtype != SubtypeChallenge {
		return Packet{}, fmt.Errorf("%w: not an AKA challenge", ErrInvalidAKAChallenge)
	}
	if len(auts) == 0 {
		return Packet{}, fmt.Errorf("%w: AUTS is empty", ErrInvalidAKAChallenge)
	}
	attrs := []Attribute{AUTSAttribute(auts)}
	if request.Type == TypeAKAPrime {
		attrs = append(attrs, challengeKDFAttributes(request.Attributes)...)
	}
	return Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       request.Type,
		Subtype:    SubtypeSynchronizationFailure,
		Attributes: attrs,
	}, nil
}

func EncryptAttributes(kEncr, iv []byte, attrs []Attribute) (Attribute, error) {
	block, err := encryptedDataBlock(kEncr, iv)
	if err != nil {
		return Attribute{}, err
	}
	plaintext, err := MarshalAttributes(attrs)
	if err != nil {
		return Attribute{}, err
	}
	if rem := len(plaintext) % aes.BlockSize; rem != 0 {
		padding, err := encryptedPaddingAttribute(aes.BlockSize - rem)
		if err != nil {
			return Attribute{}, err
		}
		rawPadding, err := padding.MarshalBinary()
		if err != nil {
			return Attribute{}, err
		}
		plaintext = append(plaintext, rawPadding...)
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
	return EncrDataAttribute(ciphertext), nil
}

func DecryptAttributes(kEncr, iv []byte, encrypted Attribute) ([]Attribute, error) {
	if encrypted.Type != AttributeEncrData {
		return nil, fmt.Errorf("%w: attribute type %d", ErrInvalidEncryptedData, encrypted.Type)
	}
	block, err := encryptedDataBlock(kEncr, iv)
	if err != nil {
		return nil, err
	}
	ciphertext, err := encrypted.EncrDataValue()
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("%w: ciphertext length %d", ErrInvalidEncryptedData, len(ciphertext))
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	attrs, err := ParseAttributes(plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEncryptedData, err)
	}
	return stripEncryptedPadding(attrs)
}

func DecryptEncryptedAttributes(kEncr []byte, ivAttr, encrypted Attribute) ([]Attribute, error) {
	iv, err := ivAttr.IVValue()
	if err != nil {
		return nil, err
	}
	return DecryptAttributes(kEncr, iv, encrypted)
}

func DecryptChallengeEncryptedAttributes(request Packet, keys Keys) ([]Attribute, bool, error) {
	return DecryptPacketEncryptedAttributes(request, keys)
}

func DecryptPacketEncryptedAttributes(request Packet, keys Keys) ([]Attribute, bool, error) {
	ivAttr, hasIV := FindAttribute(request.Attributes, AttributeIV)
	encryptedAttr, hasEncrypted := FindAttribute(request.Attributes, AttributeEncrData)
	if !hasIV && !hasEncrypted {
		return nil, false, nil
	}
	if !hasIV || !hasEncrypted {
		return nil, true, fmt.Errorf("%w: incomplete AT_IV/AT_ENCR_DATA pair", ErrInvalidEncryptedData)
	}
	attrs, err := DecryptEncryptedAttributes(keys.KEncr, ivAttr, encryptedAttr)
	if err != nil {
		return nil, true, err
	}
	return attrs, true, nil
}

func ParseReauthenticationRequest(request Packet, keys Keys) (ReauthenticationRequest, error) {
	if request.Code != CodeRequest || request.Subtype != SubtypeReauthentication {
		return ReauthenticationRequest{}, fmt.Errorf("%w: not an AKA reauthentication request", ErrInvalidReauth)
	}
	if !isAKAType(request.Type) {
		return ReauthenticationRequest{}, fmt.Errorf("%w: EAP type %d", ErrInvalidReauth, request.Type)
	}
	if err := validateReauthenticationMethod(request.Type, keys); err != nil {
		return ReauthenticationRequest{}, err
	}
	raw, err := request.MarshalBinary()
	if err != nil {
		return ReauthenticationRequest{}, err
	}
	if err := verifyChallengeMAC(request.Type, keys.KAut, raw); err != nil {
		return ReauthenticationRequest{}, err
	}
	attrs, ok, err := DecryptPacketEncryptedAttributes(request, keys)
	if err != nil {
		return ReauthenticationRequest{}, err
	}
	if !ok {
		return ReauthenticationRequest{}, fmt.Errorf("%w: missing AT_IV/AT_ENCR_DATA", ErrInvalidReauth)
	}
	counterAttr, ok := FindAttribute(attrs, AttributeCounter)
	if !ok {
		return ReauthenticationRequest{}, fmt.Errorf("%w: missing AT_COUNTER", ErrInvalidReauth)
	}
	counter, err := counterAttr.CounterValue()
	if err != nil {
		return ReauthenticationRequest{}, err
	}
	nonceAttr, ok := FindAttribute(attrs, AttributeNonceS)
	if !ok {
		return ReauthenticationRequest{}, fmt.Errorf("%w: missing AT_NONCE_S", ErrInvalidReauth)
	}
	nonceS, err := nonceAttr.NonceSValue()
	if err != nil {
		return ReauthenticationRequest{}, err
	}
	state, err := IdentityStateFromAttributes(attrs)
	if err != nil {
		return ReauthenticationRequest{}, err
	}
	return ReauthenticationRequest{
		Counter:             counter,
		NonceS:              nonceS,
		IdentityState:       state,
		EncryptedAttributes: cloneAttributes(attrs),
	}, nil
}

func DeriveReauthenticationKeys(identity string, previous Keys, counter uint16, nonceS []byte) (Keys, error) {
	if identity == "" {
		return Keys{}, fmt.Errorf("%w: identity is empty", ErrInvalidKeyMaterial)
	}
	if len(previous.KEncr) == 0 || len(previous.KAut) == 0 {
		return Keys{}, fmt.Errorf("%w: TEKs are empty", ErrInvalidKeyMaterial)
	}
	if len(nonceS) != 16 {
		return Keys{}, fmt.Errorf("%w: NONCE_S length %d", ErrInvalidReauth, len(nonceS))
	}
	var counterBytes [2]byte
	binary.BigEndian.PutUint16(counterBytes[:], counter)

	if len(previous.KRe) > 0 {
		if len(previous.KRe) != KeyLengthKRe {
			return Keys{}, fmt.Errorf("%w: K_re length %d", ErrInvalidKeyMaterial, len(previous.KRe))
		}
		seed := make([]byte, 0, len("EAP-AKA' re-auth")+len(identity)+2+len(nonceS))
		seed = append(seed, []byte("EAP-AKA' re-auth")...)
		seed = append(seed, []byte(identity)...)
		seed = append(seed, counterBytes[:]...)
		seed = append(seed, nonceS...)
		stream := prfPrimeSHA256(previous.KRe, seed, KeyLengthMSK+KeyLengthEMSK)
		return reauthenticationKeys(previous, stream, stream), nil
	}

	if len(previous.MK) == 0 {
		return Keys{}, fmt.Errorf("%w: MK is empty", ErrInvalidKeyMaterial)
	}
	seedInput := make([]byte, 0, len(identity)+2+len(nonceS)+len(previous.MK))
	seedInput = append(seedInput, []byte(identity)...)
	seedInput = append(seedInput, counterBytes[:]...)
	seedInput = append(seedInput, nonceS...)
	seedInput = append(seedInput, previous.MK...)
	seed := sha1.Sum(seedInput)
	stream := fips1862PRF(seed[:], KeyLengthMSK+KeyLengthEMSK)
	return reauthenticationKeys(previous, previous.MK, stream), nil
}

func BuildReauthenticationResponse(identity string, request Packet, keys Keys, iv []byte) (Packet, Keys, error) {
	parsed, err := ParseReauthenticationRequest(request, keys)
	if err != nil {
		return Packet{}, Keys{}, err
	}
	nextKeys, err := DeriveReauthenticationKeys(identity, keys, parsed.Counter, parsed.NonceS)
	if err != nil {
		return Packet{}, Keys{}, err
	}
	response, err := buildReauthenticationResponsePacket(request, keys, iv, parsed, []Attribute{
		CounterAttribute(parsed.Counter),
	})
	if err != nil {
		return Packet{}, Keys{}, err
	}
	return response, nextKeys, nil
}

func BuildReauthenticationCounterTooSmallResponse(request Packet, keys Keys, iv []byte) (Packet, error) {
	parsed, err := ParseReauthenticationRequest(request, keys)
	if err != nil {
		return Packet{}, err
	}
	return buildReauthenticationResponsePacket(request, keys, iv, parsed, []Attribute{
		CounterTooSmallAttribute(),
		CounterAttribute(parsed.Counter),
	})
}

func buildReauthenticationResponsePacket(request Packet, keys Keys, iv []byte, parsed ReauthenticationRequest, encryptedAttrs []Attribute) (Packet, error) {
	encrypted, err := EncryptAttributes(keys.KEncr, iv, encryptedAttrs)
	if err != nil {
		return Packet{}, err
	}
	attrs := []Attribute{IVAttribute(iv), encrypted}
	if _, ok := FindAttribute(request.Attributes, AttributeResultInd); ok {
		attrs = append(attrs, ResultIndAttribute())
	}
	attrs = append(attrs, MACAttribute(nil))
	eapType := request.Type
	if eapType == 0 {
		eapType = TypeAKA
	}
	response := Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       eapType,
		Subtype:    SubtypeReauthentication,
		Attributes: attrs,
	}
	raw, err := response.MarshalBinary()
	if err != nil {
		return Packet{}, err
	}
	mac, err := calculatePacketMAC(response.Type, keys.KAut, raw, parsed.NonceS)
	if err != nil {
		return Packet{}, err
	}
	response.Attributes[len(response.Attributes)-1] = MACAttribute(mac)
	return response, nil
}

func ChallengeRANDAndAUTN(request Packet) (rand16, autn16 []byte, err error) {
	randAttr, ok := FindAttribute(request.Attributes, AttributeRAND)
	if !ok {
		return nil, nil, fmt.Errorf("%w: missing AT_RAND", ErrInvalidAKAChallenge)
	}
	rands, err := randAttr.RANDValues()
	if err != nil {
		return nil, nil, err
	}
	if len(rands) != 1 {
		return nil, nil, fmt.Errorf("%w: RAND count %d", ErrInvalidAKAChallenge, len(rands))
	}
	autnAttr, ok := FindAttribute(request.Attributes, AttributeAUTN)
	if !ok {
		return nil, nil, fmt.Errorf("%w: missing AT_AUTN", ErrInvalidAKAChallenge)
	}
	autn, err := autnAttr.AUTNValue()
	if err != nil {
		return nil, nil, err
	}
	return rands[0], autn, nil
}

func buildNotificationResponse(request Packet, kAut []byte, authenticated bool) (Packet, bool, error) {
	if request.Subtype != SubtypeNotification {
		return Packet{}, false, nil
	}
	if request.Code != CodeRequest {
		return Packet{}, true, fmt.Errorf("%w: not an EAP-AKA notification request", ErrInvalidAKAChallenge)
	}
	if !isAKAType(request.Type) {
		return Packet{}, true, fmt.Errorf("%w: EAP type %d", ErrInvalidAKAChallenge, request.Type)
	}
	attr, ok := FindAttribute(request.Attributes, AttributeNotification)
	if !ok {
		return Packet{}, true, fmt.Errorf("%w: missing AT_NOTIFICATION", ErrInvalidAKAChallenge)
	}
	code, err := attr.NotificationValue()
	if err != nil {
		return Packet{}, true, err
	}
	var attrs []Attribute
	if code&NotificationPBit == 0 {
		if !authenticated {
			return Packet{}, true, fmt.Errorf("%w: notification requires K_aut", ErrInvalidKeyMaterial)
		}
		raw, err := request.MarshalBinary()
		if err != nil {
			return Packet{}, true, err
		}
		if err := verifyChallengeMAC(request.Type, kAut, raw); err != nil {
			return Packet{}, true, err
		}
		attrs = []Attribute{MACAttribute(nil)}
	}
	eapType := request.Type
	if eapType == 0 {
		eapType = TypeAKA
	}
	response := Packet{
		Code:       CodeResponse,
		Identifier: request.Identifier,
		Type:       eapType,
		Subtype:    SubtypeNotification,
		Attributes: attrs,
	}
	if len(attrs) == 0 {
		return response, true, nil
	}
	raw, err := response.MarshalBinary()
	if err != nil {
		return Packet{}, true, err
	}
	mac, err := calculateChallengeMAC(response.Type, kAut, raw)
	if err != nil {
		return Packet{}, true, err
	}
	response.Attributes[0] = MACAttribute(mac)
	return response, true, nil
}

func MACAttribute(mac []byte) Attribute {
	value := make([]byte, 16)
	copy(value, mac)
	return FixedAttribute(AttributeMAC, value)
}

func CalculateMAC(kAut, packet, extra []byte) ([]byte, error) {
	return calculateMAC(kAut, packet, extra, sha1.New)
}

func CalculateAKAPrimeMAC(kAut, packet, extra []byte) ([]byte, error) {
	return calculateMAC(kAut, packet, extra, sha256.New)
}

func calculateMAC(kAut, packet, extra []byte, h func() hash.Hash) ([]byte, error) {
	if len(kAut) == 0 {
		return nil, fmt.Errorf("%w: K_aut is empty", ErrInvalidKeyMaterial)
	}
	zeroed, err := packetWithZeroedMAC(packet)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(h, kAut)
	_, _ = mac.Write(zeroed)
	_, _ = mac.Write(extra)
	sum := mac.Sum(nil)
	return append([]byte(nil), sum[:16]...), nil
}

func VerifyMAC(kAut, packet, extra []byte) error {
	actual, err := packetMAC(packet)
	if err != nil {
		return err
	}
	expected, err := CalculateMAC(kAut, packet, extra)
	if err != nil {
		return err
	}
	if !hmac.Equal(actual, expected) {
		return fmt.Errorf("%w: AT_MAC mismatch", ErrInvalidMAC)
	}
	return nil
}

func VerifyAKAPrimeMAC(kAut, packet, extra []byte) error {
	actual, err := packetMAC(packet)
	if err != nil {
		return err
	}
	expected, err := CalculateAKAPrimeMAC(kAut, packet, extra)
	if err != nil {
		return err
	}
	if !hmac.Equal(actual, expected) {
		return fmt.Errorf("%w: AT_MAC mismatch", ErrInvalidMAC)
	}
	return nil
}

func packetMAC(packet []byte) ([]byte, error) {
	offset, length, err := findMACAttribute(packet)
	if err != nil {
		return nil, err
	}
	if length != 20 {
		return nil, fmt.Errorf("%w: AT_MAC length %d", ErrInvalidMAC, length)
	}
	return append([]byte(nil), packet[offset+4:offset+20]...), nil
}

func packetWithZeroedMAC(packet []byte) ([]byte, error) {
	offset, length, err := findMACAttribute(packet)
	if err != nil {
		return nil, err
	}
	out := append([]byte(nil), packet...)
	for i := offset + 2; i < offset+length; i++ {
		out[i] = 0
	}
	return out, nil
}

func findMACAttribute(packet []byte) (offset int, length int, err error) {
	if len(packet) < 8 {
		return 0, 0, ErrInvalidPacket
	}
	packetLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if packetLen < 8 || packetLen > len(packet) {
		return 0, 0, ErrInvalidPacket
	}
	for offset := 8; offset < packetLen; {
		if packetLen-offset < 4 {
			return 0, 0, ErrInvalidAttribute
		}
		length := int(packet[offset+1]) * 4
		if length < 4 || offset+length > packetLen {
			return 0, 0, ErrInvalidAttribute
		}
		if packet[offset] == AttributeMAC {
			return offset, length, nil
		}
		offset += length
	}
	return 0, 0, fmt.Errorf("%w: missing AT_MAC", ErrInvalidMAC)
}

func deriveChallengeKeys(identity string, request Packet, aka sim.AKAResult) (Keys, uint16, error) {
	switch request.Type {
	case 0, TypeAKA:
		keys, err := DeriveKeys(identity, aka)
		return keys, 0, err
	case TypeAKAPrime:
		kdf, err := firstKDFValue(request.Attributes)
		if err != nil {
			return Keys{}, 0, err
		}
		if kdf != AKAPrimeKDFDefault {
			return Keys{}, 0, fmt.Errorf("%w: %d", ErrUnsupportedKDF, kdf)
		}
		networkName, err := challengeKDFInput(request.Attributes)
		if err != nil {
			return Keys{}, 0, err
		}
		_, autn16, err := ChallengeRANDAndAUTN(request)
		if err != nil {
			return Keys{}, 0, err
		}
		keys, err := DeriveAKAPrimeKeys(identity, networkName, autn16, aka)
		return keys, kdf, err
	default:
		return Keys{}, 0, fmt.Errorf("%w: EAP type %d", ErrInvalidAKAChallenge, request.Type)
	}
}

func isAKAType(eapType uint8) bool {
	return eapType == 0 || eapType == TypeAKA || eapType == TypeAKAPrime
}

func firstKDFValue(attrs []Attribute) (uint16, error) {
	values, err := kdfValues(attrs)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 0, fmt.Errorf("%w: missing AT_KDF", ErrInvalidAKAChallenge)
	}
	return values[0], nil
}

func kdfValues(attrs []Attribute) ([]uint16, error) {
	var values []uint16
	for _, attr := range attrs {
		if attr.Type != AttributeKDF {
			continue
		}
		value, err := attr.KDFValue()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func challengeKDFInput(attrs []Attribute) (string, error) {
	attr, ok := FindAttribute(attrs, AttributeKDFInput)
	if !ok {
		return "", fmt.Errorf("%w: missing AT_KDF_INPUT", ErrInvalidAKAChallenge)
	}
	value, err := attr.KDFInputValue()
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%w: empty AT_KDF_INPUT", ErrInvalidAKAChallenge)
	}
	return value, nil
}

func challengeKDFAttributes(attrs []Attribute) []Attribute {
	var out []Attribute
	for _, attr := range attrs {
		if attr.Type == AttributeKDF {
			out = append(out, Attribute{Type: attr.Type, Data: append([]byte(nil), attr.Data...)})
		}
	}
	return out
}

func cloneAttributes(attrs []Attribute) []Attribute {
	out := make([]Attribute, len(attrs))
	for i, attr := range attrs {
		out[i] = Attribute{Type: attr.Type, Data: append([]byte(nil), attr.Data...)}
	}
	return out
}

func validateReauthenticationMethod(eapType uint8, keys Keys) error {
	switch eapType {
	case TypeAKAPrime:
		if len(keys.KRe) != KeyLengthKRe {
			return fmt.Errorf("%w: K_re length %d", ErrInvalidKeyMaterial, len(keys.KRe))
		}
		if len(keys.KAut) != KeyLengthAKAPrimeKAut {
			return fmt.Errorf("%w: AKA' K_aut length %d", ErrInvalidKeyMaterial, len(keys.KAut))
		}
	case 0, TypeAKA:
		if len(keys.KRe) > 0 {
			return fmt.Errorf("%w: EAP-AKA' key material used with EAP-AKA", ErrInvalidReauth)
		}
		if len(keys.KAut) != 0 && len(keys.KAut) != KeyLengthKAut {
			return fmt.Errorf("%w: K_aut length %d", ErrInvalidKeyMaterial, len(keys.KAut))
		}
	}
	return nil
}

func reauthenticationKeys(previous Keys, mk, stream []byte) Keys {
	return Keys{
		MK:      append([]byte(nil), mk...),
		KEncr:   append([]byte(nil), previous.KEncr...),
		KAut:    append([]byte(nil), previous.KAut...),
		KRe:     append([]byte(nil), previous.KRe...),
		CKPrime: append([]byte(nil), previous.CKPrime...),
		IKPrime: append([]byte(nil), previous.IKPrime...),
		MSK:     append([]byte(nil), stream[:KeyLengthMSK]...),
		EMSK:    append([]byte(nil), stream[KeyLengthMSK:KeyLengthMSK+KeyLengthEMSK]...),
	}
}

func encryptedDataBlock(kEncr, iv []byte) (cipher.Block, error) {
	if len(kEncr) != aes.BlockSize {
		return nil, fmt.Errorf("%w: K_encr length %d", ErrInvalidKeyMaterial, len(kEncr))
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("%w: IV length %d", ErrInvalidEncryptedData, len(iv))
	}
	return aes.NewCipher(kEncr)
}

func encryptedPaddingAttribute(totalLength int) (Attribute, error) {
	switch totalLength {
	case 4, 8, 12:
		return Attribute{Type: AttributePadding, Data: make([]byte, totalLength-2)}, nil
	default:
		return Attribute{}, fmt.Errorf("%w: padding length %d", ErrInvalidEncryptedData, totalLength)
	}
}

func stripEncryptedPadding(attrs []Attribute) ([]Attribute, error) {
	for i, attr := range attrs {
		if attr.Type != AttributePadding {
			continue
		}
		if i != len(attrs)-1 {
			return nil, fmt.Errorf("%w: AT_PADDING is not last", ErrInvalidEncryptedData)
		}
		totalLength := len(attr.Data) + 2
		if totalLength != 4 && totalLength != 8 && totalLength != 12 {
			return nil, fmt.Errorf("%w: padding length %d", ErrInvalidEncryptedData, totalLength)
		}
		for _, b := range attr.Data {
			if b != 0 {
				return nil, fmt.Errorf("%w: non-zero padding", ErrInvalidEncryptedData)
			}
		}
		return attrs[:i], nil
	}
	return attrs, nil
}

func verifyChallengeMAC(eapType uint8, kAut, raw []byte) error {
	return verifyPacketMAC(eapType, kAut, raw, nil)
}

func calculateChallengeMAC(eapType uint8, kAut, raw []byte) ([]byte, error) {
	return calculatePacketMAC(eapType, kAut, raw, nil)
}

func verifyPacketMAC(eapType uint8, kAut, raw, extra []byte) error {
	if eapType == TypeAKAPrime {
		return VerifyAKAPrimeMAC(kAut, raw, extra)
	}
	return VerifyMAC(kAut, raw, extra)
}

func calculatePacketMAC(eapType uint8, kAut, raw, extra []byte) ([]byte, error) {
	if eapType == TypeAKAPrime {
		return CalculateAKAPrimeMAC(kAut, raw, extra)
	}
	return CalculateMAC(kAut, raw, extra)
}

func deriveAKAPrimeCKIK(networkName, sqnXorAK []byte, aka sim.AKAResult) []byte {
	key := make([]byte, 0, len(aka.CK)+len(aka.IK))
	key = append(key, aka.CK...)
	key = append(key, aka.IK...)
	input := kdfInput(0x20, networkName, sqnXorAK)
	sum := hmac.New(sha256.New, key)
	_, _ = sum.Write(input)
	return sum.Sum(nil)
}

func kdfInput(fc byte, params ...[]byte) []byte {
	out := []byte{fc}
	var length [2]byte
	for _, param := range params {
		out = append(out, param...)
		binary.BigEndian.PutUint16(length[:], uint16(len(param)))
		out = append(out, length[:]...)
	}
	return out
}

func prfPrimeSHA256(key, seed []byte, length int) []byte {
	var out []byte
	var prev []byte
	counter := byte(1)
	for len(out) < length {
		mac := hmac.New(sha256.New, key)
		if len(prev) > 0 {
			_, _ = mac.Write(prev)
		}
		_, _ = mac.Write(seed)
		_, _ = mac.Write([]byte{counter})
		prev = mac.Sum(nil)
		out = append(out, prev...)
		counter++
	}
	return out[:length]
}

func fips1862PRF(seed []byte, length int) []byte {
	xkey := make([]byte, 20)
	copy(xkey, seed)
	var out []byte
	for len(out) < length {
		for i := 0; i < 2 && len(out) < length; i++ {
			w := fips1862G(xkey)
			out = append(out, w...)
			xkey = add160(xkey, w, 1)
		}
	}
	return out[:length]
}

func fips1862G(xval []byte) []byte {
	var block [64]byte
	copy(block[:20], xval)
	h0, h1, h2, h3, h4 := uint32(0x67452301), uint32(0xEFCDAB89), uint32(0x98BADCFE), uint32(0x10325476), uint32(0xC3D2E1F0)
	var w [80]uint32
	for i := 0; i < 16; i++ {
		w[i] = binary.BigEndian.Uint32(block[i*4 : i*4+4])
	}
	for i := 16; i < 80; i++ {
		w[i] = bitsRotateLeft32(w[i-3]^w[i-8]^w[i-14]^w[i-16], 1)
	}
	a, b, c, d, e := h0, h1, h2, h3, h4
	for i := 0; i < 80; i++ {
		var f, k uint32
		switch {
		case i < 20:
			f = (b & c) | ((^b) & d)
			k = 0x5A827999
		case i < 40:
			f = b ^ c ^ d
			k = 0x6ED9EBA1
		case i < 60:
			f = (b & c) | (b & d) | (c & d)
			k = 0x8F1BBCDC
		default:
			f = b ^ c ^ d
			k = 0xCA62C1D6
		}
		temp := bitsRotateLeft32(a, 5) + f + e + k + w[i]
		e = d
		d = c
		c = bitsRotateLeft32(b, 30)
		b = a
		a = temp
	}
	h0 += a
	h1 += b
	h2 += c
	h3 += d
	h4 += e
	out := make([]byte, 20)
	binary.BigEndian.PutUint32(out[0:4], h0)
	binary.BigEndian.PutUint32(out[4:8], h1)
	binary.BigEndian.PutUint32(out[8:12], h2)
	binary.BigEndian.PutUint32(out[12:16], h3)
	binary.BigEndian.PutUint32(out[16:20], h4)
	return out
}

func add160(a, b []byte, carry uint16) []byte {
	out := make([]byte, 20)
	for i := 19; i >= 0; i-- {
		sum := uint16(a[i]) + uint16(b[i]) + carry
		out[i] = byte(sum)
		carry = sum >> 8
	}
	return out
}

func bitsRotateLeft32(v uint32, n uint) uint32 {
	return (v << n) | (v >> (32 - n))
}
