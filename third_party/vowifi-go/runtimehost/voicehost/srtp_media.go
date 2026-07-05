package voicehost

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/pion/srtp/v3"
)

var ErrSRTPMediaConfig = errors.New("invalid srtp media config")

type SRTPProtectionProfile string

const (
	SRTPProfileAes128CmHmacSha1_80 SRTPProtectionProfile = "AES_CM_128_HMAC_SHA1_80"
	SRTPProfileAes128CmHmacSha1_32 SRTPProtectionProfile = "AES_CM_128_HMAC_SHA1_32"
	SRTPProfileAes256CmHmacSha1_80 SRTPProtectionProfile = "AES_CM_256_HMAC_SHA1_80"
	SRTPProfileAes256CmHmacSha1_32 SRTPProtectionProfile = "AES_CM_256_HMAC_SHA1_32"
	SRTPProfileAeadAes128Gcm       SRTPProtectionProfile = "AEAD_AES_128_GCM"
	SRTPProfileAeadAes256Gcm       SRTPProtectionProfile = "AEAD_AES_256_GCM"
)

type SRTPKeys struct {
	MasterKey  []byte
	MasterSalt []byte
}

type SRTPMediaConfig struct {
	Profile             SRTPProtectionProfile
	ClientKeys          SRTPKeys
	IMSKeys             SRTPKeys
	ReplayWindowSize    uint
	RTCPFeedbackHandler RTCPFeedbackHandler
}

type SRTPMediaSession struct {
	mu sync.Mutex

	clientProtect       *srtp.Context
	clientUnprotect     *srtp.Context
	imsProtect          *srtp.Context
	imsUnprotect        *srtp.Context
	rtcpFeedbackHandler RTCPFeedbackHandler
}

func NewSRTPMediaSession(cfg SRTPMediaConfig) (*SRTPMediaSession, error) {
	profile, err := srtpProtectionProfile(cfg.Profile)
	if err != nil {
		return nil, err
	}
	if err := validateSRTPKeys(profile, cfg.ClientKeys, "client"); err != nil {
		return nil, err
	}
	if err := validateSRTPKeys(profile, cfg.IMSKeys, "ims"); err != nil {
		return nil, err
	}
	window := cfg.ReplayWindowSize
	if window == 0 {
		window = 64
	}
	clientProtect, err := srtp.CreateContext(cfg.ClientKeys.MasterKey, cfg.ClientKeys.MasterSalt, profile)
	if err != nil {
		return nil, fmt.Errorf("%w: client protect: %v", ErrSRTPMediaConfig, err)
	}
	clientUnprotect, err := srtp.CreateContext(cfg.ClientKeys.MasterKey, cfg.ClientKeys.MasterSalt, profile, srtp.SRTPReplayProtection(window), srtp.SRTCPReplayProtection(window))
	if err != nil {
		return nil, fmt.Errorf("%w: client unprotect: %v", ErrSRTPMediaConfig, err)
	}
	imsProtect, err := srtp.CreateContext(cfg.IMSKeys.MasterKey, cfg.IMSKeys.MasterSalt, profile)
	if err != nil {
		return nil, fmt.Errorf("%w: ims protect: %v", ErrSRTPMediaConfig, err)
	}
	imsUnprotect, err := srtp.CreateContext(cfg.IMSKeys.MasterKey, cfg.IMSKeys.MasterSalt, profile, srtp.SRTPReplayProtection(window), srtp.SRTCPReplayProtection(window))
	if err != nil {
		return nil, fmt.Errorf("%w: ims unprotect: %v", ErrSRTPMediaConfig, err)
	}
	return &SRTPMediaSession{
		clientProtect:       clientProtect,
		clientUnprotect:     clientUnprotect,
		imsProtect:          imsProtect,
		imsUnprotect:        imsUnprotect,
		rtcpFeedbackHandler: cfg.RTCPFeedbackHandler,
	}, nil
}

func (s *SRTPMediaSession) ProtectClientRTP(packet []byte) ([]byte, error) {
	if s == nil || s.clientProtect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientProtect.EncryptRTP(nil, packet, nil)
}

func (s *SRTPMediaSession) UnprotectClientRTP(packet []byte) ([]byte, error) {
	if s == nil || s.clientUnprotect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientUnprotect.DecryptRTP(nil, packet, nil)
}

func (s *SRTPMediaSession) ProtectIMSRTP(packet []byte) ([]byte, error) {
	if s == nil || s.imsProtect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imsProtect.EncryptRTP(nil, packet, nil)
}

func (s *SRTPMediaSession) UnprotectIMSRTP(packet []byte) ([]byte, error) {
	if s == nil || s.imsUnprotect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imsUnprotect.DecryptRTP(nil, packet, nil)
}

func (s *SRTPMediaSession) ProtectClientRTCP(packet []byte) ([]byte, error) {
	if s == nil || s.clientProtect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientProtect.EncryptRTCP(nil, packet, nil)
}

func (s *SRTPMediaSession) UnprotectClientRTCP(packet []byte) ([]byte, error) {
	if s == nil || s.clientUnprotect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientUnprotect.DecryptRTCP(nil, packet, nil)
}

func (s *SRTPMediaSession) ProtectIMSRTCP(packet []byte) ([]byte, error) {
	if s == nil || s.imsProtect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imsProtect.EncryptRTCP(nil, packet, nil)
}

func (s *SRTPMediaSession) UnprotectIMSRTCP(packet []byte) ([]byte, error) {
	if s == nil || s.imsUnprotect == nil {
		return nil, ErrSRTPMediaConfig
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.imsUnprotect.DecryptRTCP(nil, packet, nil)
}

func (s *SRTPMediaSession) RelayTransforms() RTPRelayTransforms {
	if s == nil {
		return RTPRelayTransforms{}
	}
	return s.RelayTransformsWithRTCPFeedback(s.rtcpFeedbackHandler)
}

func (s *SRTPMediaSession) RelayTransformsWithRTCPFeedback(handler RTCPFeedbackHandler) RTPRelayTransforms {
	if s == nil {
		return RTPRelayTransforms{}
	}
	return RTPRelayTransforms{
		ClientToIMSRTP: s.ClientToIMSRTP,
		IMSToClientRTP: s.IMSToClientRTP,
		ClientToIMSRTCP: func(packet []byte) ([]byte, error) {
			return s.clientToIMSRTCP(packet, handler)
		},
		IMSToClientRTCP: func(packet []byte) ([]byte, error) {
			return s.imsToClientRTCP(packet, handler)
		},
	}
}

func (s *SRTPMediaSession) ClientToIMSRTP(packet []byte) ([]byte, error) {
	plain, err := s.UnprotectClientRTP(packet)
	if err != nil {
		return nil, err
	}
	return s.ProtectIMSRTP(plain)
}

func (s *SRTPMediaSession) IMSToClientRTP(packet []byte) ([]byte, error) {
	plain, err := s.UnprotectIMSRTP(packet)
	if err != nil {
		return nil, err
	}
	return s.ProtectClientRTP(plain)
}

func (s *SRTPMediaSession) ClientToIMSRTCP(packet []byte) ([]byte, error) {
	return s.clientToIMSRTCP(packet, s.rtcpFeedbackHandler)
}

func (s *SRTPMediaSession) clientToIMSRTCP(packet []byte, handler RTCPFeedbackHandler) ([]byte, error) {
	plain, err := s.UnprotectClientRTCP(packet)
	if err != nil {
		return nil, err
	}
	_, _ = InspectRTCPFeedback(RTCPFeedbackClientToIMS, plain, handler)
	return s.ProtectIMSRTCP(plain)
}

func (s *SRTPMediaSession) IMSToClientRTCP(packet []byte) ([]byte, error) {
	return s.imsToClientRTCP(packet, s.rtcpFeedbackHandler)
}

func (s *SRTPMediaSession) imsToClientRTCP(packet []byte, handler RTCPFeedbackHandler) ([]byte, error) {
	plain, err := s.UnprotectIMSRTCP(packet)
	if err != nil {
		return nil, err
	}
	_, _ = InspectRTCPFeedback(RTCPFeedbackIMSToClient, plain, handler)
	return s.ProtectClientRTCP(plain)
}

func srtpProtectionProfile(profile SRTPProtectionProfile) (srtp.ProtectionProfile, error) {
	switch SRTPProtectionProfile(strings.ToUpper(strings.TrimSpace(string(profile)))) {
	case "", SRTPProfileAes128CmHmacSha1_80:
		return srtp.ProtectionProfileAes128CmHmacSha1_80, nil
	case SRTPProfileAes128CmHmacSha1_32:
		return srtp.ProtectionProfileAes128CmHmacSha1_32, nil
	case SRTPProfileAes256CmHmacSha1_80:
		return srtp.ProtectionProfileAes256CmHmacSha1_80, nil
	case SRTPProfileAes256CmHmacSha1_32:
		return srtp.ProtectionProfileAes256CmHmacSha1_32, nil
	case SRTPProfileAeadAes128Gcm:
		return srtp.ProtectionProfileAeadAes128Gcm, nil
	case SRTPProfileAeadAes256Gcm:
		return srtp.ProtectionProfileAeadAes256Gcm, nil
	default:
		return 0, fmt.Errorf("%w: unsupported profile %q", ErrSRTPMediaConfig, profile)
	}
}

func validateSRTPKeys(profile srtp.ProtectionProfile, keys SRTPKeys, label string) error {
	keyLen, err := profile.KeyLen()
	if err != nil {
		return fmt.Errorf("%w: %s key length: %v", ErrSRTPMediaConfig, label, err)
	}
	saltLen, err := profile.SaltLen()
	if err != nil {
		return fmt.Errorf("%w: %s salt length: %v", ErrSRTPMediaConfig, label, err)
	}
	if len(keys.MasterKey) != keyLen {
		return fmt.Errorf("%w: %s master key length %d != %d", ErrSRTPMediaConfig, label, len(keys.MasterKey), keyLen)
	}
	if len(keys.MasterSalt) != saltLen {
		return fmt.Errorf("%w: %s master salt length %d != %d", ErrSRTPMediaConfig, label, len(keys.MasterSalt), saltLen)
	}
	return nil
}
