package swu

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

func TestPacketPumpForwardsBothDirections(t *testing.T) {
	device := newPumpFakeDevice()
	session := newPumpFakeSession()
	pump, err := NewPacketPump(PacketPumpConfig{Session: session, Device: device})
	if err != nil {
		t.Fatalf("NewPacketPump() error = %v", err)
	}
	if err := pump.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	outbound := []byte{0x45, 0, 0, 0x14, 0xaa}
	device.reads <- outbound
	if got := readPumpBytes(t, session.sent); !bytes.Equal(got, outbound) {
		t.Fatalf("sent=%x, want %x", got, outbound)
	}

	inbound := []byte{0x60, 0, 0, 0, 0xbb}
	session.reads <- PacketTunnelPacket{Payload: inbound}
	if got := readPumpBytes(t, device.writes); !bytes.Equal(got, inbound) {
		t.Fatalf("written=%x, want %x", got, inbound)
	}

	if err := pump.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	stats, err := pump.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if stats.DeviceToESPPackets != 1 || stats.DeviceToESPBytes != uint64(len(outbound)) ||
		stats.ESPToDevicePackets != 1 || stats.ESPToDeviceBytes != uint64(len(inbound)) {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestPacketPumpReportsSendErrors(t *testing.T) {
	device := newPumpFakeDevice()
	sendErr := errors.New("send failed")
	session := newPumpFakeSession()
	session.sendErr = sendErr
	var gotDirection PacketPumpDirection
	pump, err := NewPacketPump(PacketPumpConfig{
		Session: session,
		Device:  device,
		OnError: func(direction PacketPumpDirection, err error) {
			gotDirection = direction
		},
	})
	if err != nil {
		t.Fatalf("NewPacketPump() error = %v", err)
	}
	if err := pump.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	device.reads <- []byte{0x45, 0, 0, 0x14}
	stats, err := pump.Wait()
	if !errors.Is(err, sendErr) {
		t.Fatalf("Wait() err=%v, want sendErr", err)
	}
	if gotDirection != PacketPumpDeviceToESP {
		t.Fatalf("direction=%s, want %s", gotDirection, PacketPumpDeviceToESP)
	}
	if stats.ESPSendErrors != 1 || stats.DeviceToESPPackets != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestPacketPumpReportsDeviceWriteErrors(t *testing.T) {
	device := newPumpFakeDevice()
	writeErr := errors.New("write failed")
	device.writeErr = writeErr
	session := newPumpFakeSession()
	pump, err := NewPacketPump(PacketPumpConfig{Session: session, Device: device})
	if err != nil {
		t.Fatalf("NewPacketPump() error = %v", err)
	}
	if err := pump.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	session.reads <- PacketTunnelPacket{Payload: []byte{0x60, 0, 0, 0}}
	stats, err := pump.Wait()
	if !errors.Is(err, writeErr) {
		t.Fatalf("Wait() err=%v, want writeErr", err)
	}
	if stats.DeviceWriteErrors != 1 || stats.ESPToDevicePackets != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestPacketPumpWaitBeforeStartReturnsError(t *testing.T) {
	pump, err := NewPacketPump(PacketPumpConfig{Session: newPumpFakeSession(), Device: newPumpFakeDevice()})
	if err != nil {
		t.Fatalf("NewPacketPump() error = %v", err)
	}
	if _, err := pump.Wait(); !errors.Is(err, ErrInvalidPacketPump) {
		t.Fatalf("Wait() err=%v, want ErrInvalidPacketPump", err)
	}
	if err := pump.Close(context.Background()); err != nil {
		t.Fatalf("Close(before start) error = %v", err)
	}
}

func readPumpBytes(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	select {
	case packet := <-ch:
		return packet
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for packet")
		return nil
	}
}

type pumpFakeDevice struct {
	reads    chan []byte
	writes   chan []byte
	writeErr error
	close    sync.Once
	closed   chan struct{}
}

func newPumpFakeDevice() *pumpFakeDevice {
	return &pumpFakeDevice{
		reads:  make(chan []byte, 4),
		writes: make(chan []byte, 4),
		closed: make(chan struct{}),
	}
}

func (d *pumpFakeDevice) ReadInnerPacket(ctx context.Context) ([]byte, error) {
	select {
	case packet := <-d.reads:
		return append([]byte(nil), packet...), nil
	case <-d.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *pumpFakeDevice) WriteInnerPacket(ctx context.Context, packet []byte) error {
	if d.writeErr != nil {
		return d.writeErr
	}
	select {
	case d.writes <- append([]byte(nil), packet...):
		return nil
	case <-d.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *pumpFakeDevice) Close(ctx context.Context) error {
	d.close.Do(func() { close(d.closed) })
	return nil
}

type pumpFakeSession struct {
	sent    chan []byte
	reads   chan PacketTunnelPacket
	sendErr error
	readErr error
	close   sync.Once
	closed  chan struct{}
}

func newPumpFakeSession() *pumpFakeSession {
	return &pumpFakeSession{
		sent:   make(chan []byte, 4),
		reads:  make(chan PacketTunnelPacket, 4),
		closed: make(chan struct{}),
	}
}

func (s *pumpFakeSession) Result() TunnelResult {
	return TunnelResult{Ready: true, IKEEstablished: true, IPsecEstablished: true}
}

func (s *pumpFakeSession) MOBIKE(ctx context.Context, req MOBIKERequest) (MOBIKEResult, error) {
	return MOBIKEResult{}, nil
}

func (s *pumpFakeSession) Close(ctx context.Context) error {
	s.close.Do(func() { close(s.closed) })
	return nil
}

func (s *pumpFakeSession) SendInnerPacket(ctx context.Context, packet []byte) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	select {
	case s.sent <- append([]byte(nil), packet...):
		return nil
	case <-s.closed:
		return ErrPacketTunnelClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *pumpFakeSession) SendInnerPacketWithNextHeader(ctx context.Context, nextHeader uint8, packet []byte) error {
	return s.SendInnerPacket(ctx, packet)
}

func (s *pumpFakeSession) ReceiveESPPacket(ctx context.Context, packet []byte) (PacketTunnelPacket, error) {
	return PacketTunnelPacket{Payload: append([]byte(nil), packet...)}, nil
}

func (s *pumpFakeSession) ReadInnerPacket(ctx context.Context) (PacketTunnelPacket, error) {
	if s.readErr != nil {
		return PacketTunnelPacket{}, s.readErr
	}
	select {
	case packet := <-s.reads:
		packet.Payload = append([]byte(nil), packet.Payload...)
		return packet, nil
	case <-s.closed:
		return PacketTunnelPacket{}, ErrPacketTunnelClosed
	case <-ctx.Done():
		return PacketTunnelPacket{}, ctx.Err()
	}
}

func (s *pumpFakeSession) PacketStats() PacketTunnelStats {
	return PacketTunnelStats{}
}
