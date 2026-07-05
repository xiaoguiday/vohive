package voiceclient

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestWireSIPFlowReusesUDPFlowForRegisterAndDialog(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	defer pc.Close()

	type seenRequest struct {
		addr string
		wire string
	}
	seen := make(chan []seenRequest, 1)
	go func() {
		var requests []seenRequest
		buf := make([]byte, 65535)
		for i := 0; i < 2; i++ {
			_ = pc.SetReadDeadline(time.Now().Add(time.Second))
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				seen <- append(requests, seenRequest{wire: "read error: " + err.Error()})
				return
			}
			requests = append(requests, seenRequest{
				addr: addr.String(),
				wire: string(append([]byte(nil), buf[:n]...)),
			})
			_, _ = pc.WriteTo([]byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n"), addr)
		}
		seen <- requests
	}()

	flow := &WireSIPFlow{Network: "udp", ServerAddr: pc.LocalAddr().String(), Timeout: time.Second}
	defer flow.Close()
	resp, err := flow.RoundTripRegister(context.Background(), RegisterMessage{
		URI: "sip:ims.example",
		Headers: map[string]string{
			"To":           "<sip:user@example>",
			"From":         "<sip:user@example>;tag=t",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-register",
			"CSeq":         "1 REGISTER",
			"Max-Forwards": "70",
		},
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRegister() response=%+v err=%v", resp, err)
	}
	resp, err = flow.RoundTripRequest(context.Background(), SIPRequestMessage{
		Method: "MESSAGE",
		URI:    "sip:+18005551212@example",
		Headers: map[string]string{
			"To":           "<sip:+18005551212@example>",
			"From":         "<sip:user@example>;tag=sms",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-message",
			"CSeq":         "1 MESSAGE",
			"Max-Forwards": "70",
		},
		Body: []byte("hello"),
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRequest() response=%+v err=%v", resp, err)
	}
	requests := <-seen
	if len(requests) != 2 {
		t.Fatalf("requests=%d %+v", len(requests), requests)
	}
	if requests[0].addr == "" || requests[0].addr != requests[1].addr {
		t.Fatalf("flow used different local addresses: %+v", requests)
	}
	if !strings.Contains(requests[0].wire, "REGISTER sip:ims.example SIP/2.0") || !strings.Contains(requests[1].wire, "MESSAGE sip:+18005551212@example SIP/2.0") {
		t.Fatalf("unexpected wires: %+v", requests)
	}
}

func TestWireSIPFlowSendsCRLFKeepaliveOnEstablishedUDPFlow(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	defer pc.Close()

	seen := make(chan []string, 1)
	go func() {
		var requests []string
		buf := make([]byte, 65535)
		_ = pc.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			seen <- []string{"read register error: " + err.Error()}
			return
		}
		requests = append(requests, string(append([]byte(nil), buf[:n]...)), addr.String())
		_, _ = pc.WriteTo([]byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n"), addr)
		_ = pc.SetReadDeadline(time.Now().Add(time.Second))
		n, keepaliveAddr, err := pc.ReadFrom(buf)
		if err != nil {
			seen <- append(requests, "read keepalive error: "+err.Error())
			return
		}
		requests = append(requests, string(append([]byte(nil), buf[:n]...)), keepaliveAddr.String())
		seen <- requests
	}()

	flow := &WireSIPFlow{Network: "udp", ServerAddr: pc.LocalAddr().String(), Timeout: time.Second}
	defer flow.Close()
	resp, err := flow.RoundTripRegister(context.Background(), RegisterMessage{
		URI: "sip:ims.example",
		Headers: map[string]string{
			"To":           "<sip:user@example>",
			"From":         "<sip:user@example>;tag=t",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-keepalive",
			"CSeq":         "1 REGISTER",
			"Max-Forwards": "70",
		},
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRegister() response=%+v err=%v", resp, err)
	}
	if err := flow.SendCRLFKeepalive(context.Background()); err != nil {
		t.Fatalf("SendCRLFKeepalive() error = %v", err)
	}
	requests := <-seen
	if len(requests) != 4 {
		t.Fatalf("requests=%d %+v", len(requests), requests)
	}
	if requests[2] != "\r\n\r\n" || requests[1] != requests[3] {
		t.Fatalf("keepalive=%q addrs=%q/%q", requests[2], requests[1], requests[3])
	}
}

func TestWireSIPFlowUsesResolverForRegisterTarget(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket() error = %v", err)
	}
	defer pc.Close()

	seen := make(chan string, 1)
	go func() {
		buf := make([]byte, 65535)
		_ = pc.SetReadDeadline(time.Now().Add(time.Second))
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			seen <- "read error: " + err.Error()
			return
		}
		seen <- string(append([]byte(nil), buf[:n]...))
		_, _ = pc.WriteTo([]byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n"), addr)
	}()

	var resolvedNetwork, resolvedURI string
	flow := &WireSIPFlow{
		Network: "udp",
		Resolver: SIPServerResolverFunc(func(ctx context.Context, network, uri string) (string, error) {
			resolvedNetwork = network
			resolvedURI = uri
			return pc.LocalAddr().String(), nil
		}),
		Timeout: time.Second,
	}
	defer flow.Close()
	resp, err := flow.RoundTripRegister(context.Background(), RegisterMessage{
		URI: "sip:ims.example",
		Headers: map[string]string{
			"To":           "<sip:user@example>",
			"From":         "<sip:user@example>;tag=t",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-resolver",
			"CSeq":         "1 REGISTER",
			"Max-Forwards": "70",
		},
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRegister() response=%+v err=%v", resp, err)
	}
	if resolvedNetwork != "udp" || resolvedURI != "sip:ims.example" {
		t.Fatalf("resolver saw network=%q uri=%q", resolvedNetwork, resolvedURI)
	}
	if wire := <-seen; !strings.Contains(wire, "REGISTER sip:ims.example SIP/2.0") {
		t.Fatalf("wire=%q", wire)
	}
}

func TestWireSIPFlowFailsOverAndReusesResolvedTarget(t *testing.T) {
	dead, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket(dead) error = %v", err)
	}
	defer dead.Close()
	live, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket(live) error = %v", err)
	}
	defer live.Close()

	deadSeen := make(chan string, 1)
	go func() {
		buf := make([]byte, 65535)
		_ = dead.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := dead.ReadFrom(buf)
		if err != nil {
			deadSeen <- "read error: " + err.Error()
			return
		}
		deadSeen <- string(append([]byte(nil), buf[:n]...))
	}()
	type seenRequest struct {
		addr string
		wire string
	}
	liveSeen := make(chan []seenRequest, 1)
	go func() {
		var requests []seenRequest
		buf := make([]byte, 65535)
		for i := 0; i < 2; i++ {
			_ = live.SetReadDeadline(time.Now().Add(time.Second))
			n, addr, err := live.ReadFrom(buf)
			if err != nil {
				liveSeen <- append(requests, seenRequest{wire: "read error: " + err.Error()})
				return
			}
			requests = append(requests, seenRequest{
				addr: addr.String(),
				wire: string(append([]byte(nil), buf[:n]...)),
			})
			_, _ = live.WriteTo([]byte("SIP/2.0 200 OK\r\nContent-Length: 0\r\n\r\n"), addr)
		}
		liveSeen <- requests
	}()

	resolverCalls := 0
	flow := &WireSIPFlow{
		Network: "udp",
		Resolver: SIPServerCandidateResolverFunc(func(ctx context.Context, network, uri string) ([]string, error) {
			resolverCalls++
			if network != "udp" || uri != "sip:ims.example" {
				t.Fatalf("resolver network=%q uri=%q", network, uri)
			}
			return []string{dead.LocalAddr().String(), live.LocalAddr().String()}, nil
		}),
		Timeout:               80 * time.Millisecond,
		RetransmitInterval:    20 * time.Millisecond,
		MaxRetransmitInterval: 20 * time.Millisecond,
		MaxRetransmits:        1,
	}
	defer flow.Close()
	resp, err := flow.RoundTripRegister(context.Background(), RegisterMessage{
		URI: "sip:ims.example",
		Headers: map[string]string{
			"To":           "<sip:user@example>",
			"From":         "<sip:user@example>;tag=t",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-failover-register",
			"CSeq":         "1 REGISTER",
			"Max-Forwards": "70",
		},
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRegister() response=%+v err=%v", resp, err)
	}
	resp, err = flow.RoundTripRequest(context.Background(), SIPRequestMessage{
		Method: "MESSAGE",
		URI:    "sip:+18005551212@example",
		Headers: map[string]string{
			"To":           "<sip:+18005551212@example>",
			"From":         "<sip:user@example>;tag=sms",
			"Contact":      "<sip:user@192.0.2.10:5060>",
			"Call-ID":      "flow-failover-message",
			"CSeq":         "1 MESSAGE",
			"Max-Forwards": "70",
		},
		Body: []byte("hello"),
	})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("RoundTripRequest() response=%+v err=%v", resp, err)
	}
	if wire := <-deadSeen; !strings.Contains(wire, "REGISTER sip:ims.example") {
		t.Fatalf("dead target wire=%q", wire)
	}
	requests := <-liveSeen
	if len(requests) != 2 {
		t.Fatalf("live requests=%d %+v", len(requests), requests)
	}
	if requests[0].addr == "" || requests[0].addr != requests[1].addr {
		t.Fatalf("flow did not reuse live target/local address: %+v", requests)
	}
	if !strings.Contains(requests[0].wire, "REGISTER sip:ims.example") ||
		!strings.Contains(requests[1].wire, "MESSAGE sip:+18005551212@example") {
		t.Fatalf("live wires=%+v", requests)
	}
	if resolverCalls != 1 {
		t.Fatalf("resolver calls=%d, want 1", resolverCalls)
	}
}
