package voicehost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

var ErrRTPRelayConfig = errors.New("invalid rtp relay config")

type RTPRelayConfig struct {
	ClientListenIP      string
	ClientAdvertiseIP   string
	ClientPort          int
	ClientRTCPPort      int
	IMSListenIP         string
	IMSAdvertiseIP      string
	IMSPort             int
	IMSRTCPPort         int
	BufferSize          int
	Transforms          RTPRelayTransforms
	RTCPFeedbackHandler RTCPFeedbackHandler
}

type RTPRelayTransform func([]byte) ([]byte, error)

type RTPRelayTransforms struct {
	ClientToIMSRTP  RTPRelayTransform
	IMSToClientRTP  RTPRelayTransform
	ClientToIMSRTCP RTPRelayTransform
	IMSToClientRTCP RTPRelayTransform
}

type RTPRelayStats struct {
	ClientToIMSPackets                   uint64
	IMSToClientPackets                   uint64
	ClientToIMSBytes                     uint64
	IMSToClientBytes                     uint64
	ClientToIMSRTPPackets                uint64
	IMSToClientRTPPackets                uint64
	ClientToIMSRTCPPackets               uint64
	IMSToClientRTCPPackets               uint64
	ClientToIMSRTPBytes                  uint64
	IMSToClientRTPBytes                  uint64
	ClientToIMSRTCPBytes                 uint64
	IMSToClientRTCPBytes                 uint64
	ClientToIMSRTPDrops                  uint64
	IMSToClientRTPDrops                  uint64
	ClientToIMSRTCPDrops                 uint64
	IMSToClientRTCPDrops                 uint64
	RTCPFeedbackPackets                  uint64
	RTCPFeedbackParseErrors              uint64
	RTCPSenderReports                    uint64
	RTCPReceiverReports                  uint64
	RTCPPictureLossIndications           uint64
	RTCPFullIntraRequests                uint64
	RTCPRapidResynchronizationRequests   uint64
	RTCPTransportLayerNacks              uint64
	RTCPReceiverEstimatedMaximumBitrates uint64
	RTCPTransportLayerCongestionControls uint64
	RTCPSliceLossIndications             uint64
	RTCPExtendedReports                  uint64
	RTCPSourceDescriptions               uint64
	RTCPGoodbyes                         uint64
	RTCPApplicationDefined               uint64
	RTCPUnknownPackets                   uint64
}

type RTPRelaySession struct {
	clientConn     *net.UDPConn
	imsConn        *net.UDPConn
	clientRTCPConn *net.UDPConn
	imsRTCPConn    *net.UDPConn

	clientTarget     *net.UDPAddr
	clientRTCPTarget *net.UDPAddr

	mu            sync.RWMutex
	imsTarget     *net.UDPAddr
	imsRTCPTarget *net.UDPAddr
	closed        bool

	clientAdvertiseIP string
	imsAdvertiseIP    string
	bufferSize        int

	cancel context.CancelFunc
	wg     sync.WaitGroup

	clientToIMSRTPPackets                atomic.Uint64
	imsToClientRTPPackets                atomic.Uint64
	clientToIMSRTCPPackets               atomic.Uint64
	imsToClientRTCPPackets               atomic.Uint64
	clientToIMSRTPBytes                  atomic.Uint64
	imsToClientRTPBytes                  atomic.Uint64
	clientToIMSRTCPBytes                 atomic.Uint64
	imsToClientRTCPBytes                 atomic.Uint64
	clientToIMSRTPDrops                  atomic.Uint64
	imsToClientRTPDrops                  atomic.Uint64
	clientToIMSRTCPDrops                 atomic.Uint64
	imsToClientRTCPDrops                 atomic.Uint64
	transforms                           RTPRelayTransforms
	rtcpFeedbackHandler                  RTCPFeedbackHandler
	rtcpFeedbackPackets                  atomic.Uint64
	rtcpFeedbackParseErrors              atomic.Uint64
	rtcpSenderReports                    atomic.Uint64
	rtcpReceiverReports                  atomic.Uint64
	rtcpPictureLossIndications           atomic.Uint64
	rtcpFullIntraRequests                atomic.Uint64
	rtcpRapidResynchronizationRequests   atomic.Uint64
	rtcpTransportLayerNacks              atomic.Uint64
	rtcpReceiverEstimatedMaximumBitrates atomic.Uint64
	rtcpTransportLayerCongestionControls atomic.Uint64
	rtcpSliceLossIndications             atomic.Uint64
	rtcpExtendedReports                  atomic.Uint64
	rtcpSourceDescriptions               atomic.Uint64
	rtcpGoodbyes                         atomic.Uint64
	rtcpApplicationDefined               atomic.Uint64
	rtcpUnknownPackets                   atomic.Uint64
}

func NewRTPRelaySession(ctx context.Context, cfg RTPRelayConfig, clientTarget SDPInfo) (*RTPRelaySession, error) {
	s, err := newRTPRelaySession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := s.SetClientRemote(clientTarget); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func NewRTPRelaySessionForIMSRemote(ctx context.Context, cfg RTPRelayConfig, imsTarget SDPInfo) (*RTPRelaySession, error) {
	s, err := newRTPRelaySession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := s.SetIMSRemote(imsTarget); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func newRTPRelaySession(ctx context.Context, cfg RTPRelayConfig) (*RTPRelaySession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	clientListenIP := firstVoiceNonEmpty(cfg.ClientListenIP, "0.0.0.0")
	imsListenIP := firstVoiceNonEmpty(cfg.IMSListenIP, clientListenIP)
	clientConn, err := listenUDP(clientListenIP, cfg.ClientPort)
	if err != nil {
		return nil, err
	}
	imsConn, err := listenUDP(imsListenIP, cfg.IMSPort)
	if err != nil {
		_ = clientConn.Close()
		return nil, err
	}
	clientRTCPConn, err := listenUDP(clientListenIP, cfg.ClientRTCPPort)
	if err != nil {
		_ = clientConn.Close()
		_ = imsConn.Close()
		return nil, err
	}
	imsRTCPConn, err := listenUDP(imsListenIP, cfg.IMSRTCPPort)
	if err != nil {
		_ = clientConn.Close()
		_ = imsConn.Close()
		_ = clientRTCPConn.Close()
		return nil, err
	}
	childCtx, cancel := context.WithCancel(ctx)
	s := &RTPRelaySession{
		clientConn:          clientConn,
		imsConn:             imsConn,
		clientRTCPConn:      clientRTCPConn,
		imsRTCPConn:         imsRTCPConn,
		clientAdvertiseIP:   advertiseIP(cfg.ClientAdvertiseIP, clientListenIP),
		imsAdvertiseIP:      advertiseIP(cfg.IMSAdvertiseIP, imsListenIP),
		bufferSize:          cfg.BufferSize,
		cancel:              cancel,
		transforms:          cfg.Transforms,
		rtcpFeedbackHandler: cfg.RTCPFeedbackHandler,
	}
	if s.bufferSize <= 0 {
		s.bufferSize = 2048
	}
	s.wg.Add(4)
	go s.forwardLoop(childCtx, s.clientConn, s.imsConn, s.currentIMSTarget, &s.clientToIMSRTPPackets, &s.clientToIMSRTPBytes, &s.clientToIMSRTPDrops, s.transforms.ClientToIMSRTP, "")
	go s.forwardLoop(childCtx, s.imsConn, s.clientConn, s.currentClientTarget, &s.imsToClientRTPPackets, &s.imsToClientRTPBytes, &s.imsToClientRTPDrops, s.transforms.IMSToClientRTP, "")
	go s.forwardLoop(childCtx, s.clientRTCPConn, s.imsRTCPConn, s.currentIMSRTCPTarget, &s.clientToIMSRTCPPackets, &s.clientToIMSRTCPBytes, &s.clientToIMSRTCPDrops, s.transforms.ClientToIMSRTCP, RTCPFeedbackClientToIMS)
	go s.forwardLoop(childCtx, s.imsRTCPConn, s.clientRTCPConn, s.currentClientRTCPTarget, &s.imsToClientRTCPPackets, &s.imsToClientRTCPBytes, &s.imsToClientRTCPDrops, s.transforms.IMSToClientRTCP, RTCPFeedbackIMSToClient)
	return s, nil
}

func (s *RTPRelaySession) IMSOfferSDP(clientOffer SDPInfo) []byte {
	info := clientOffer
	info.ConnectionIP = s.imsAdvertiseIP
	info.MediaPort = s.imsPort()
	info.RTCPPort = s.imsRTCPPort()
	return BuildSDPAnswer(info)
}

func (s *RTPRelaySession) ClientAnswerSDP(imsAnswer SDPInfo) []byte {
	info := imsAnswer
	info.ConnectionIP = s.clientAdvertiseIP
	info.MediaPort = s.clientPort()
	info.RTCPPort = s.clientRTCPPort()
	return BuildSDPAnswer(info)
}

func (s *RTPRelaySession) SetIMSRemote(info SDPInfo) error {
	if s == nil {
		return nil
	}
	addr, rtcpAddr, err := resolveSDPEndpoint(info, "IMS")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.imsTarget = addr
	s.imsRTCPTarget = rtcpAddr
	s.mu.Unlock()
	return nil
}

func (s *RTPRelaySession) SetClientRemote(info SDPInfo) error {
	if s == nil {
		return nil
	}
	addr, rtcpAddr, err := resolveSDPEndpoint(info, "client")
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.clientTarget = addr
	s.clientRTCPTarget = rtcpAddr
	s.mu.Unlock()
	return nil
}

func (s *RTPRelaySession) ClientEndpoint() SDPInfo {
	if s == nil {
		return SDPInfo{}
	}
	return SDPInfo{ConnectionIP: s.clientAdvertiseIP, MediaPort: s.clientPort(), RTCPIP: s.clientAdvertiseIP, RTCPPort: s.clientRTCPPort()}
}

func (s *RTPRelaySession) IMSEndpoint() SDPInfo {
	if s == nil {
		return SDPInfo{}
	}
	return SDPInfo{ConnectionIP: s.imsAdvertiseIP, MediaPort: s.imsPort(), RTCPIP: s.imsAdvertiseIP, RTCPPort: s.imsRTCPPort()}
}

func (s *RTPRelaySession) Stats() RTPRelayStats {
	if s == nil {
		return RTPRelayStats{}
	}
	rtpOutPackets := s.clientToIMSRTPPackets.Load()
	rtpInPackets := s.imsToClientRTPPackets.Load()
	rtpOutBytes := s.clientToIMSRTPBytes.Load()
	rtpInBytes := s.imsToClientRTPBytes.Load()
	return RTPRelayStats{
		ClientToIMSPackets:                   rtpOutPackets,
		IMSToClientPackets:                   rtpInPackets,
		ClientToIMSBytes:                     rtpOutBytes,
		IMSToClientBytes:                     rtpInBytes,
		ClientToIMSRTPPackets:                rtpOutPackets,
		IMSToClientRTPPackets:                rtpInPackets,
		ClientToIMSRTCPPackets:               s.clientToIMSRTCPPackets.Load(),
		IMSToClientRTCPPackets:               s.imsToClientRTCPPackets.Load(),
		ClientToIMSRTPBytes:                  rtpOutBytes,
		IMSToClientRTPBytes:                  rtpInBytes,
		ClientToIMSRTCPBytes:                 s.clientToIMSRTCPBytes.Load(),
		IMSToClientRTCPBytes:                 s.imsToClientRTCPBytes.Load(),
		ClientToIMSRTPDrops:                  s.clientToIMSRTPDrops.Load(),
		IMSToClientRTPDrops:                  s.imsToClientRTPDrops.Load(),
		ClientToIMSRTCPDrops:                 s.clientToIMSRTCPDrops.Load(),
		IMSToClientRTCPDrops:                 s.imsToClientRTCPDrops.Load(),
		RTCPFeedbackPackets:                  s.rtcpFeedbackPackets.Load(),
		RTCPFeedbackParseErrors:              s.rtcpFeedbackParseErrors.Load(),
		RTCPSenderReports:                    s.rtcpSenderReports.Load(),
		RTCPReceiverReports:                  s.rtcpReceiverReports.Load(),
		RTCPPictureLossIndications:           s.rtcpPictureLossIndications.Load(),
		RTCPFullIntraRequests:                s.rtcpFullIntraRequests.Load(),
		RTCPRapidResynchronizationRequests:   s.rtcpRapidResynchronizationRequests.Load(),
		RTCPTransportLayerNacks:              s.rtcpTransportLayerNacks.Load(),
		RTCPReceiverEstimatedMaximumBitrates: s.rtcpReceiverEstimatedMaximumBitrates.Load(),
		RTCPTransportLayerCongestionControls: s.rtcpTransportLayerCongestionControls.Load(),
		RTCPSliceLossIndications:             s.rtcpSliceLossIndications.Load(),
		RTCPExtendedReports:                  s.rtcpExtendedReports.Load(),
		RTCPSourceDescriptions:               s.rtcpSourceDescriptions.Load(),
		RTCPGoodbyes:                         s.rtcpGoodbyes.Load(),
		RTCPApplicationDefined:               s.rtcpApplicationDefined.Load(),
		RTCPUnknownPackets:                   s.rtcpUnknownPackets.Load(),
	}
}

func (s *RTPRelaySession) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	var err error
	if s.clientConn != nil {
		err = errors.Join(err, s.clientConn.Close())
	}
	if s.imsConn != nil {
		err = errors.Join(err, s.imsConn.Close())
	}
	if s.clientRTCPConn != nil {
		err = errors.Join(err, s.clientRTCPConn.Close())
	}
	if s.imsRTCPConn != nil {
		err = errors.Join(err, s.imsRTCPConn.Close())
	}
	s.wg.Wait()
	return err
}

func (s *RTPRelaySession) forwardLoop(ctx context.Context, src, out *net.UDPConn, target func() *net.UDPAddr, packets, bytes, drops *atomic.Uint64, transform RTPRelayTransform, rtcpDirection RTCPFeedbackDirection) {
	defer s.wg.Done()
	buf := make([]byte, s.bufferSize)
	for {
		n, _, err := src.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		dst := target()
		if dst == nil {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		if transform != nil {
			transformed, err := transform(packet)
			if err != nil {
				drops.Add(1)
				continue
			}
			packet = transformed
		}
		if rtcpDirection != "" && transform == nil {
			s.inspectRTCPFeedback(rtcpDirection, packet)
		}
		if _, err := out.WriteToUDP(packet, dst); err != nil {
			drops.Add(1)
			continue
		}
		packets.Add(1)
		bytes.Add(uint64(len(packet)))
	}
}

func (s *RTPRelaySession) inspectRTCPFeedback(direction RTCPFeedbackDirection, packet []byte) {
	if s == nil {
		return
	}
	summary, err := InspectRTCPFeedback(direction, packet, s.rtcpFeedbackHandler)
	if err != nil {
		s.rtcpFeedbackParseErrors.Add(1)
		return
	}
	s.recordRTCPFeedbackSummary(summary)
}

func (s *RTPRelaySession) recordRTCPFeedbackSummary(summary RTCPFeedbackSummary) {
	if s == nil {
		return
	}
	s.rtcpFeedbackPackets.Add(summary.Packets)
	s.rtcpSenderReports.Add(summary.SenderReports)
	s.rtcpReceiverReports.Add(summary.ReceiverReports)
	s.rtcpPictureLossIndications.Add(summary.PictureLossIndications)
	s.rtcpFullIntraRequests.Add(summary.FullIntraRequests)
	s.rtcpRapidResynchronizationRequests.Add(summary.RapidResynchronizationRequests)
	s.rtcpTransportLayerNacks.Add(summary.TransportLayerNacks)
	s.rtcpReceiverEstimatedMaximumBitrates.Add(summary.ReceiverEstimatedMaximumBitrates)
	s.rtcpTransportLayerCongestionControls.Add(summary.TransportLayerCongestionControls)
	s.rtcpSliceLossIndications.Add(summary.SliceLossIndications)
	s.rtcpExtendedReports.Add(summary.ExtendedReports)
	s.rtcpSourceDescriptions.Add(summary.SourceDescriptions)
	s.rtcpGoodbyes.Add(summary.Goodbyes)
	s.rtcpApplicationDefined.Add(summary.ApplicationDefined)
	s.rtcpUnknownPackets.Add(summary.UnknownPackets)
}

func (s *RTPRelaySession) currentIMSTarget() *net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.imsTarget == nil {
		return nil
	}
	cp := *s.imsTarget
	return &cp
}

func (s *RTPRelaySession) currentIMSRTCPTarget() *net.UDPAddr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.imsRTCPTarget == nil {
		return nil
	}
	cp := *s.imsRTCPTarget
	return &cp
}

func (s *RTPRelaySession) currentClientTarget() *net.UDPAddr {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.clientTarget == nil {
		return nil
	}
	cp := *s.clientTarget
	return &cp
}

func (s *RTPRelaySession) currentClientRTCPTarget() *net.UDPAddr {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.clientRTCPTarget == nil {
		return nil
	}
	cp := *s.clientRTCPTarget
	return &cp
}

func (s *RTPRelaySession) clientPort() int {
	if s == nil || s.clientConn == nil {
		return 0
	}
	return udpLocalPort(s.clientConn)
}

func (s *RTPRelaySession) clientRTCPPort() int {
	if s == nil || s.clientRTCPConn == nil {
		return 0
	}
	return udpLocalPort(s.clientRTCPConn)
}

func (s *RTPRelaySession) imsPort() int {
	if s == nil || s.imsConn == nil {
		return 0
	}
	return udpLocalPort(s.imsConn)
}

func (s *RTPRelaySession) imsRTCPPort() int {
	if s == nil || s.imsRTCPConn == nil {
		return 0
	}
	return udpLocalPort(s.imsRTCPConn)
}

func listenUDP(host string, port int) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(host), strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	return net.ListenUDP("udp", addr)
}

func udpLocalPort(conn *net.UDPConn) int {
	if conn == nil {
		return 0
	}
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

func resolveSDPEndpoint(info SDPInfo, label string) (*net.UDPAddr, *net.UDPAddr, error) {
	if strings.TrimSpace(info.ConnectionIP) == "" || info.MediaPort <= 0 {
		return nil, nil, fmt.Errorf("%w: %s media target is incomplete", ErrRTPRelayConfig, label)
	}
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(info.ConnectionIP), strconv.Itoa(info.MediaPort)))
	if err != nil {
		return nil, nil, err
	}
	rtcpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(defaultRTCPIP(info), strconv.Itoa(defaultRTCPPort(info))))
	if err != nil {
		return nil, nil, err
	}
	return addr, rtcpAddr, nil
}

func defaultRTCPPort(info SDPInfo) int {
	if info.RTCPPort > 0 {
		return info.RTCPPort
	}
	if info.MediaPort > 0 {
		return info.MediaPort + 1
	}
	return 0
}

func defaultRTCPIP(info SDPInfo) string {
	if strings.TrimSpace(info.RTCPIP) != "" {
		return strings.TrimSpace(info.RTCPIP)
	}
	return strings.TrimSpace(info.ConnectionIP)
}

func advertiseIP(explicit, listenIP string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	ip := strings.TrimSpace(listenIP)
	if ip == "" || ip == "0.0.0.0" || ip == "::" {
		return "127.0.0.1"
	}
	return ip
}
