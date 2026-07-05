package voiceclient

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

type SIPResponse = RegisterResponse

type SIPRequestTransport interface {
	RoundTripRequest(context.Context, SIPRequestMessage) (SIPResponse, error)
	WriteRequest(context.Context, SIPRequestMessage) error
}

type ProvisionalResponseHandler func(context.Context, SIPRequestMessage, SIPResponse) error

type SIPInviteTransport interface {
	RoundTripInvite(context.Context, SIPRequestMessage, ProvisionalResponseHandler) (SIPResponse, error)
}

type WireSIPTransport struct {
	Network               string
	ServerAddr            string
	LocalAddr             string
	Resolver              SIPServerResolver
	Timeout               time.Duration
	RetransmitInterval    time.Duration
	MaxRetransmitInterval time.Duration
	MaxRetransmits        int
}

func (t WireSIPTransport) RoundTripRequest(ctx context.Context, msg SIPRequestMessage) (SIPResponse, error) {
	return t.roundTripRequest(ctx, msg, nil)
}

func (t WireSIPTransport) RoundTripInvite(ctx context.Context, msg SIPRequestMessage, onProvisional ProvisionalResponseHandler) (SIPResponse, error) {
	return t.roundTripRequest(ctx, msg, onProvisional)
}

func (t WireSIPTransport) roundTripRequest(ctx context.Context, msg SIPRequestMessage, onProvisional ProvisionalResponseHandler) (SIPResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	network := strings.ToLower(strings.TrimSpace(t.Network))
	if network == "" {
		network = "udp"
	}
	targets, err := sipTargetsForRequest(ctx, t.Resolver, network, t.ServerAddr, msg.URI)
	if err != nil {
		return SIPResponse{}, err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var lastErr error
	for _, target := range targets {
		resp, err := t.roundTripTarget(ctx, network, target, timeout, msg, onProvisional)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return SIPResponse{}, ctx.Err()
		}
		lastErr = err
		if !isSIPRetryableTransportError(err) {
			break
		}
	}
	if lastErr != nil {
		return SIPResponse{}, lastErr
	}
	return SIPResponse{}, errSIPDNSResolverEmpty()
}

func (t WireSIPTransport) roundTripTarget(ctx context.Context, network, target string, timeout time.Duration, msg SIPRequestMessage, onProvisional ProvisionalResponseHandler) (SIPResponse, error) {
	conn, err := t.dialTarget(ctx, network, target, timeout)
	if err != nil {
		return SIPResponse{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return SIPResponse{}, err
	}
	attempt := msg
	ensureSIPRequestVia(&attempt, transportName(network), conn.LocalAddr())
	wire, err := buildSIPRequestWire(attempt, transportName(network), conn.LocalAddr())
	if err != nil {
		return SIPResponse{}, err
	}
	if _, err := conn.Write(wire); err != nil {
		return SIPResponse{}, err
	}
	if strings.HasPrefix(network, "tcp") {
		reader := bufio.NewReader(conn)
		return readFinalSIPResponse(ctx, reader, attempt, onProvisional)
	}
	buf := make([]byte, 65535)
	interval := sipRetransmitInterval(timeout, t.RetransmitInterval)
	maxInterval := sipMaxRetransmitInterval(timeout, t.MaxRetransmitInterval)
	deadline := time.Now().Add(timeout)
	retransmits := 0
	gotResponse := false
	retransmitExhausted := false
	for {
		readInterval := interval
		if gotResponse || retransmitExhausted {
			readInterval = time.Until(deadline)
		}
		if err := conn.SetReadDeadline(nextSIPReadDeadline(deadline, readInterval)); err != nil {
			return SIPResponse{}, err
		}
		n, err := conn.Read(buf)
		if err != nil {
			if ctx.Err() != nil {
				return SIPResponse{}, ctx.Err()
			}
			if !isSIPTimeout(err) || !time.Now().Before(deadline) {
				return SIPResponse{}, err
			}
			if !gotResponse && !retransmitExhausted && shouldSIPRetransmit(retransmits, t.MaxRetransmits) {
				if _, writeErr := conn.Write(wire); writeErr != nil {
					return SIPResponse{}, writeErr
				}
				retransmits++
				interval = nextSIPRetransmitInterval(interval, maxInterval)
				continue
			}
			if !gotResponse {
				retransmitExhausted = true
				continue
			}
			return SIPResponse{}, err
		}
		if !isSIPResponseWire(buf[:n]) {
			continue
		}
		resp, err := ParseSIPResponse(buf[:n])
		if err != nil {
			return SIPResponse{}, err
		}
		if !isProvisionalResponse(resp.StatusCode, attempt.Method) {
			return resp, nil
		}
		if onProvisional != nil {
			if err := onProvisional(ctx, attempt, resp); err != nil {
				return SIPResponse{}, err
			}
		}
		gotResponse = true
	}
}

func (t WireSIPTransport) WriteRequest(ctx context.Context, msg SIPRequestMessage) error {
	if ctx == nil {
		ctx = context.Background()
	}
	network := strings.ToLower(strings.TrimSpace(t.Network))
	if network == "" {
		network = "udp"
	}
	targets, err := sipTargetsForRequest(ctx, t.Resolver, network, t.ServerAddr, msg.URI)
	if err != nil {
		return err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	var lastErr error
	for _, target := range targets {
		err := t.writeTarget(ctx, network, target, timeout, msg)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
		if !isSIPRetryableTransportError(err) {
			break
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return errSIPDNSResolverEmpty()
}

func (t WireSIPTransport) writeTarget(ctx context.Context, network, target string, timeout time.Duration, msg SIPRequestMessage) error {
	conn, err := t.dialTarget(ctx, network, target, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	attempt := msg
	ensureSIPRequestVia(&attempt, transportName(network), conn.LocalAddr())
	wire, err := buildSIPRequestWire(attempt, transportName(network), conn.LocalAddr())
	if err != nil {
		return err
	}
	_, err = conn.Write(wire)
	return err
}

func (t WireSIPTransport) dialTarget(ctx context.Context, network, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := dialSIPConn(ctx, network, target, t.LocalAddr, timeout)
	if err != nil {
		if strings.HasPrefix(strings.ToLower(network), "udp") || strings.HasPrefix(strings.ToLower(network), "tcp") {
			return nil, err
		}
		return nil, fmt.Errorf("unsupported SIP network %q", network)
	}
	return conn, nil
}

func readFinalSIPResponse(ctx context.Context, reader *bufio.Reader, msg SIPRequestMessage, onProvisional ProvisionalResponseHandler) (SIPResponse, error) {
	for {
		raw, err := readSIPStreamMessage(reader)
		if err != nil {
			return SIPResponse{}, err
		}
		resp, err := ParseSIPResponse(raw)
		if err != nil {
			return SIPResponse{}, err
		}
		if !isProvisionalResponse(resp.StatusCode, msg.Method) {
			return resp, nil
		}
		if onProvisional != nil {
			if err := onProvisional(ctx, msg, resp); err != nil {
				return SIPResponse{}, err
			}
		}
	}
}

func isProvisionalResponse(code int, method string) bool {
	return strings.EqualFold(strings.TrimSpace(method), "INVITE") && code >= 100 && code < 200
}

func transportName(network string) string {
	if strings.HasPrefix(strings.ToLower(network), "tcp") {
		return "TCP"
	}
	return "UDP"
}
