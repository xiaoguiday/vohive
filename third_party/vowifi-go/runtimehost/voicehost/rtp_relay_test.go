package voicehost

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/rtcp"
)

func TestRTPRelaySessionForwardsBidirectionalPackets(t *testing.T) {
	clientPeer := listenTestUDP(t)
	defer clientPeer.Close()
	clientRTCPPeer := listenTestUDP(t)
	defer clientRTCPPeer.Close()
	imsPeer := listenTestUDP(t)
	defer imsPeer.Close()
	imsRTCPPeer := listenTestUDP(t)
	defer imsRTCPPeer.Close()

	clientAddr := clientPeer.LocalAddr().(*net.UDPAddr)
	clientRTCPAddr := clientRTCPPeer.LocalAddr().(*net.UDPAddr)
	imsAddr := imsPeer.LocalAddr().(*net.UDPAddr)
	imsRTCPAddr := imsRTCPPeer.LocalAddr().(*net.UDPAddr)
	relay, err := NewRTPRelaySession(context.Background(), RTPRelayConfig{
		ClientListenIP:    "127.0.0.1",
		ClientAdvertiseIP: "127.0.0.1",
		IMSListenIP:       "127.0.0.1",
		IMSAdvertiseIP:    "127.0.0.1",
	}, SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: clientAddr.Port, RTCPPort: clientRTCPAddr.Port})
	if err != nil {
		t.Fatalf("NewRTPRelaySession() error = %v", err)
	}
	defer relay.Close()
	if err := relay.SetIMSRemote(SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: imsAddr.Port, RTCPPort: imsRTCPAddr.Port}); err != nil {
		t.Fatalf("SetIMSRemote() error = %v", err)
	}

	clientEndpoint := udpAddrFromSDP(t, relay.ClientEndpoint())
	clientRTCPEndpoint := udpRTCPAddrFromSDP(t, relay.ClientEndpoint())
	imsEndpoint := udpAddrFromSDP(t, relay.IMSEndpoint())
	imsRTCPEndpoint := udpRTCPAddrFromSDP(t, relay.IMSEndpoint())

	if _, err := clientPeer.WriteToUDP([]byte{0x11, 0x22, 0x33}, clientEndpoint); err != nil {
		t.Fatalf("client WriteToUDP() error = %v", err)
	}
	got, from := readTestUDP(t, imsPeer)
	if string(got) != string([]byte{0x11, 0x22, 0x33}) {
		t.Fatalf("IMS got=%x", got)
	}
	if from.Port != imsEndpoint.Port {
		t.Fatalf("IMS packet source port=%d, want relay IMS port %d", from.Port, imsEndpoint.Port)
	}

	if _, err := imsPeer.WriteToUDP([]byte{0x44, 0x55}, imsEndpoint); err != nil {
		t.Fatalf("ims WriteToUDP() error = %v", err)
	}
	got, from = readTestUDP(t, clientPeer)
	if string(got) != string([]byte{0x44, 0x55}) {
		t.Fatalf("client got=%x", got)
	}
	if from.Port != clientEndpoint.Port {
		t.Fatalf("client packet source port=%d, want relay client port %d", from.Port, clientEndpoint.Port)
	}

	if _, err := clientRTCPPeer.WriteToUDP([]byte{0x81, 0xc9}, clientRTCPEndpoint); err != nil {
		t.Fatalf("client RTCP WriteToUDP() error = %v", err)
	}
	got, from = readTestUDP(t, imsRTCPPeer)
	if string(got) != string([]byte{0x81, 0xc9}) {
		t.Fatalf("IMS RTCP got=%x", got)
	}
	if from.Port != imsRTCPEndpoint.Port {
		t.Fatalf("IMS RTCP packet source port=%d, want relay IMS RTCP port %d", from.Port, imsRTCPEndpoint.Port)
	}

	if _, err := imsRTCPPeer.WriteToUDP([]byte{0x82, 0xca, 0x00}, imsRTCPEndpoint); err != nil {
		t.Fatalf("IMS RTCP WriteToUDP() error = %v", err)
	}
	got, from = readTestUDP(t, clientRTCPPeer)
	if string(got) != string([]byte{0x82, 0xca, 0x00}) {
		t.Fatalf("client RTCP got=%x", got)
	}
	if from.Port != clientRTCPEndpoint.Port {
		t.Fatalf("client RTCP packet source port=%d, want relay client RTCP port %d", from.Port, clientRTCPEndpoint.Port)
	}

	stats := relay.Stats()
	if stats.ClientToIMSRTPPackets != 1 || stats.IMSToClientRTPPackets != 1 || stats.ClientToIMSRTCPPackets != 1 || stats.IMSToClientRTCPPackets != 1 {
		t.Fatalf("stats packets=%+v", stats)
	}
	if stats.ClientToIMSRTPBytes != 3 || stats.IMSToClientRTPBytes != 2 || stats.ClientToIMSRTCPBytes != 2 || stats.IMSToClientRTCPBytes != 3 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestRTPRelaySessionRewritesSDP(t *testing.T) {
	clientPeer := listenTestUDP(t)
	defer clientPeer.Close()
	clientAddr := clientPeer.LocalAddr().(*net.UDPAddr)
	relay, err := NewRTPRelaySession(context.Background(), RTPRelayConfig{
		ClientListenIP:    "127.0.0.1",
		ClientAdvertiseIP: "198.51.100.10",
		IMSListenIP:       "127.0.0.1",
		IMSAdvertiseIP:    "203.0.113.10",
	}, SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: clientAddr.Port, RTCPPort: clientAddr.Port + 1, Payloads: []int{0, 101}, Direction: "sendrecv"})
	if err != nil {
		t.Fatalf("NewRTPRelaySession() error = %v", err)
	}
	defer relay.Close()

	offer, err := ParseSDP(relay.IMSOfferSDP(SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: clientAddr.Port, Payloads: []int{0, 101}}))
	if err != nil {
		t.Fatalf("ParseSDP(offer) error = %v", err)
	}
	if offer.ConnectionIP != "203.0.113.10" || offer.MediaPort != relay.IMSEndpoint().MediaPort || offer.RTCPPort != relay.IMSEndpoint().RTCPPort {
		t.Fatalf("offer=%+v relayIMS=%+v", offer, relay.IMSEndpoint())
	}
	answer, err := ParseSDP(relay.ClientAnswerSDP(SDPInfo{ConnectionIP: "192.0.2.20", MediaPort: 49170, RTCPPort: 49171, Payloads: []int{0}}))
	if err != nil {
		t.Fatalf("ParseSDP(answer) error = %v", err)
	}
	if answer.ConnectionIP != "198.51.100.10" || answer.MediaPort != relay.ClientEndpoint().MediaPort || answer.RTCPPort != relay.ClientEndpoint().RTCPPort {
		t.Fatalf("answer=%+v relayClient=%+v", answer, relay.ClientEndpoint())
	}
}

func TestRTPRelaySessionAppliesSRTPTransforms(t *testing.T) {
	clientPeer := listenTestUDP(t)
	defer clientPeer.Close()
	clientRTCPPeer := listenTestUDP(t)
	defer clientRTCPPeer.Close()
	imsPeer := listenTestUDP(t)
	defer imsPeer.Close()
	imsRTCPPeer := listenTestUDP(t)
	defer imsRTCPPeer.Close()
	media, err := NewSRTPMediaSession(testSRTPMediaConfig())
	if err != nil {
		t.Fatalf("NewSRTPMediaSession() error = %v", err)
	}
	clientAddr := clientPeer.LocalAddr().(*net.UDPAddr)
	clientRTCPAddr := clientRTCPPeer.LocalAddr().(*net.UDPAddr)
	imsAddr := imsPeer.LocalAddr().(*net.UDPAddr)
	imsRTCPAddr := imsRTCPPeer.LocalAddr().(*net.UDPAddr)
	relay, err := NewRTPRelaySession(context.Background(), RTPRelayConfig{
		ClientListenIP:    "127.0.0.1",
		ClientAdvertiseIP: "127.0.0.1",
		IMSListenIP:       "127.0.0.1",
		IMSAdvertiseIP:    "127.0.0.1",
		Transforms:        media.RelayTransforms(),
	}, SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: clientAddr.Port, RTCPPort: clientRTCPAddr.Port})
	if err != nil {
		t.Fatalf("NewRTPRelaySession() error = %v", err)
	}
	defer relay.Close()
	if err := relay.SetIMSRemote(SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: imsAddr.Port, RTCPPort: imsRTCPAddr.Port}); err != nil {
		t.Fatalf("SetIMSRemote() error = %v", err)
	}
	clientEndpoint := udpAddrFromSDP(t, relay.ClientEndpoint())
	imsEndpoint := udpAddrFromSDP(t, relay.IMSEndpoint())

	clientPlain := testRTPPacket(31, 0x11111111, []byte{0x01, 0x02, 0x03})
	clientProtected, err := media.ProtectClientRTP(clientPlain)
	if err != nil {
		t.Fatalf("ProtectClientRTP() error = %v", err)
	}
	if _, err := clientPeer.WriteToUDP(clientProtected, clientEndpoint); err != nil {
		t.Fatalf("client WriteToUDP() error = %v", err)
	}
	got, _ := readTestUDP(t, imsPeer)
	if bytes.Equal(got, clientPlain) || bytes.Equal(got, clientProtected) {
		t.Fatalf("IMS got untransformed packet=%x", got)
	}
	gotPlain, err := media.UnprotectIMSRTP(got)
	if err != nil {
		t.Fatalf("UnprotectIMSRTP() error = %v", err)
	}
	if !bytes.Equal(gotPlain, clientPlain) {
		t.Fatalf("IMS plain=%x, want %x", gotPlain, clientPlain)
	}

	imsPlain := testRTPPacket(32, 0x22222222, []byte{0x04, 0x05})
	imsProtected, err := media.ProtectIMSRTP(imsPlain)
	if err != nil {
		t.Fatalf("ProtectIMSRTP() error = %v", err)
	}
	if _, err := imsPeer.WriteToUDP(imsProtected, imsEndpoint); err != nil {
		t.Fatalf("ims WriteToUDP() error = %v", err)
	}
	got, _ = readTestUDP(t, clientPeer)
	if bytes.Equal(got, imsPlain) || bytes.Equal(got, imsProtected) {
		t.Fatalf("client got untransformed packet=%x", got)
	}
	gotPlain, err = media.UnprotectClientRTP(got)
	if err != nil {
		t.Fatalf("UnprotectClientRTP() error = %v", err)
	}
	if !bytes.Equal(gotPlain, imsPlain) {
		t.Fatalf("client plain=%x, want %x", gotPlain, imsPlain)
	}
	stats := waitRelayStats(t, relay, func(stats RTPRelayStats) bool {
		return stats.ClientToIMSRTPPackets == 1 && stats.IMSToClientRTPPackets == 1
	})
	if stats.ClientToIMSRTPDrops != 0 || stats.IMSToClientRTPDrops != 0 || stats.ClientToIMSRTPPackets != 1 || stats.IMSToClientRTPPackets != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestRTPRelaySessionReportsRTCPFeedback(t *testing.T) {
	clientPeer := listenTestUDP(t)
	defer clientPeer.Close()
	clientRTCPPeer := listenTestUDP(t)
	defer clientRTCPPeer.Close()
	imsPeer := listenTestUDP(t)
	defer imsPeer.Close()
	imsRTCPPeer := listenTestUDP(t)
	defer imsRTCPPeer.Close()

	events := make(chan RTCPFeedbackEvent, 4)
	clientAddr := clientPeer.LocalAddr().(*net.UDPAddr)
	clientRTCPAddr := clientRTCPPeer.LocalAddr().(*net.UDPAddr)
	imsAddr := imsPeer.LocalAddr().(*net.UDPAddr)
	imsRTCPAddr := imsRTCPPeer.LocalAddr().(*net.UDPAddr)
	relay, err := NewRTPRelaySession(context.Background(), RTPRelayConfig{
		ClientListenIP:    "127.0.0.1",
		ClientAdvertiseIP: "127.0.0.1",
		IMSListenIP:       "127.0.0.1",
		IMSAdvertiseIP:    "127.0.0.1",
		RTCPFeedbackHandler: func(event RTCPFeedbackEvent) {
			events <- event
		},
	}, SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: clientAddr.Port, RTCPPort: clientRTCPAddr.Port})
	if err != nil {
		t.Fatalf("NewRTPRelaySession() error = %v", err)
	}
	defer relay.Close()
	if err := relay.SetIMSRemote(SDPInfo{ConnectionIP: "127.0.0.1", MediaPort: imsAddr.Port, RTCPPort: imsRTCPAddr.Port}); err != nil {
		t.Fatalf("SetIMSRemote() error = %v", err)
	}

	packet, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.PictureLossIndication{SenderSSRC: 0x11111111, MediaSSRC: 0x22222222},
		&rtcp.TransportLayerNack{
			SenderSSRC: 0x11111111,
			MediaSSRC:  0x22222222,
			Nacks:      rtcp.NackPairsFromSequenceNumbers([]uint16{7, 8, 12}),
		},
	})
	if err != nil {
		t.Fatalf("rtcp.Marshal() error = %v", err)
	}
	clientRTCPEndpoint := udpRTCPAddrFromSDP(t, relay.ClientEndpoint())
	if _, err := clientRTCPPeer.WriteToUDP(packet, clientRTCPEndpoint); err != nil {
		t.Fatalf("client RTCP WriteToUDP() error = %v", err)
	}
	got, _ := readTestUDP(t, imsRTCPPeer)
	if !bytes.Equal(got, packet) {
		t.Fatalf("IMS RTCP got=%x, want %x", got, packet)
	}

	first := readRTCPFeedbackEvent(t, events)
	second := readRTCPFeedbackEvent(t, events)
	seen := map[RTCPFeedbackKind]RTCPFeedbackEvent{
		first.Kind:  first,
		second.Kind: second,
	}
	if event, ok := seen[RTCPFeedbackPictureLossIndication]; !ok || event.Direction != RTCPFeedbackClientToIMS || event.MediaSSRC != 0x22222222 {
		t.Fatalf("PLI event=%+v seen=%v", event, ok)
	}
	if event, ok := seen[RTCPFeedbackTransportLayerNack]; !ok || event.NACKCount != 3 {
		t.Fatalf("NACK event=%+v seen=%v", event, ok)
	}
	stats := waitRelayStats(t, relay, func(stats RTPRelayStats) bool {
		return stats.RTCPFeedbackPackets == 2
	})
	if stats.RTCPPictureLossIndications != 1 || stats.RTCPTransportLayerNacks != 1 || stats.RTCPFeedbackParseErrors != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func listenTestUDP(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	return conn
}

func readTestUDP(t *testing.T, conn *net.UDPConn) ([]byte, *net.UDPAddr) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 128)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP() error = %v", err)
	}
	return append([]byte(nil), buf[:n]...), addr
}

func readRTCPFeedbackEvent(t *testing.T, events <-chan RTCPFeedbackEvent) RTCPFeedbackEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for RTCP feedback event")
		return RTCPFeedbackEvent{}
	}
}

func waitRelayStats(t *testing.T, relay *RTPRelaySession, pred func(RTPRelayStats) bool) RTPRelayStats {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		stats := relay.Stats()
		if pred(stats) {
			return stats
		}
		if time.Now().After(deadline) {
			return stats
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func udpAddrFromSDP(t *testing.T, info SDPInfo) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(info.ConnectionIP, strconv.Itoa(info.MediaPort)))
	if err != nil {
		t.Fatalf("ResolveUDPAddr(%+v) error = %v", info, err)
	}
	return addr
}

func udpRTCPAddrFromSDP(t *testing.T, info SDPInfo) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(info.ConnectionIP, strconv.Itoa(info.RTCPPort)))
	if err != nil {
		t.Fatalf("ResolveUDPAddr(%+v RTCP) error = %v", info, err)
	}
	return addr
}
