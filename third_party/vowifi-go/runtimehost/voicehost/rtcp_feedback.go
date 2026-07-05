package voicehost

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pion/rtcp"
)

type RTCPFeedbackDirection string

const (
	RTCPFeedbackClientToIMS RTCPFeedbackDirection = "client_to_ims"
	RTCPFeedbackIMSToClient RTCPFeedbackDirection = "ims_to_client"
)

type RTCPFeedbackKind string

const (
	RTCPFeedbackSenderReport                    RTCPFeedbackKind = "sender_report"
	RTCPFeedbackReceiverReport                  RTCPFeedbackKind = "receiver_report"
	RTCPFeedbackPictureLossIndication           RTCPFeedbackKind = "picture_loss_indication"
	RTCPFeedbackFullIntraRequest                RTCPFeedbackKind = "full_intra_request"
	RTCPFeedbackRapidResynchronizationRequest   RTCPFeedbackKind = "rapid_resynchronization_request"
	RTCPFeedbackTransportLayerNack              RTCPFeedbackKind = "transport_layer_nack"
	RTCPFeedbackReceiverEstimatedMaximumBitrate RTCPFeedbackKind = "receiver_estimated_maximum_bitrate"
	RTCPFeedbackTransportLayerCongestionControl RTCPFeedbackKind = "transport_layer_congestion_control"
	RTCPFeedbackSliceLossIndication             RTCPFeedbackKind = "slice_loss_indication"
	RTCPFeedbackExtendedReport                  RTCPFeedbackKind = "extended_report"
	RTCPFeedbackSourceDescription               RTCPFeedbackKind = "source_description"
	RTCPFeedbackGoodbye                         RTCPFeedbackKind = "goodbye"
	RTCPFeedbackApplicationDefined              RTCPFeedbackKind = "application_defined"
	RTCPFeedbackUnknown                         RTCPFeedbackKind = "unknown"
)

type RTCPFeedbackHandler func(RTCPFeedbackEvent)

type RTCPFeedbackEvent struct {
	Direction        RTCPFeedbackDirection
	Kind             RTCPFeedbackKind
	PacketType       string
	SenderSSRC       uint32
	MediaSSRC        uint32
	SSRC             uint32
	DestinationSSRCs []uint32
	ReportCount      int
	NACKCount        int
	FIRCount         int
	SLICount         int
	REMBBitrate      float64
	REMBSSRCs        []uint32
	TransportCCCount int
	Packet           rtcp.Packet
}

type RTCPFeedbackSummary struct {
	Packets                          uint64
	SenderReports                    uint64
	ReceiverReports                  uint64
	PictureLossIndications           uint64
	FullIntraRequests                uint64
	RapidResynchronizationRequests   uint64
	TransportLayerNacks              uint64
	ReceiverEstimatedMaximumBitrates uint64
	TransportLayerCongestionControls uint64
	SliceLossIndications             uint64
	ExtendedReports                  uint64
	SourceDescriptions               uint64
	Goodbyes                         uint64
	ApplicationDefined               uint64
	UnknownPackets                   uint64
}

func InspectRTCPFeedback(direction RTCPFeedbackDirection, packet []byte, handler RTCPFeedbackHandler) (RTCPFeedbackSummary, error) {
	var summary RTCPFeedbackSummary
	packets, err := rtcp.Unmarshal(packet)
	if err != nil {
		return summary, err
	}
	for _, packet := range packets {
		for _, event := range rtcpFeedbackEvents(direction, packet) {
			summary.add(event.Kind)
			emitRTCPFeedback(handler, event)
		}
	}
	return summary, nil
}

func rtcpFeedbackEvents(direction RTCPFeedbackDirection, packet rtcp.Packet) []RTCPFeedbackEvent {
	if packet == nil {
		return nil
	}
	if compound, ok := packet.(*rtcp.CompoundPacket); ok && compound != nil {
		var events []RTCPFeedbackEvent
		for _, inner := range *compound {
			events = append(events, rtcpFeedbackEvents(direction, inner)...)
		}
		return events
	}
	event := RTCPFeedbackEvent{
		Direction:        direction,
		Kind:             RTCPFeedbackUnknown,
		PacketType:       rtcpPacketType(packet),
		DestinationSSRCs: append([]uint32(nil), packet.DestinationSSRC()...),
		Packet:           packet,
	}
	switch p := packet.(type) {
	case *rtcp.SenderReport:
		event.Kind = RTCPFeedbackSenderReport
		event.SSRC = p.SSRC
		event.ReportCount = len(p.Reports)
	case *rtcp.ReceiverReport:
		event.Kind = RTCPFeedbackReceiverReport
		event.SSRC = p.SSRC
		event.ReportCount = len(p.Reports)
	case *rtcp.PictureLossIndication:
		event.Kind = RTCPFeedbackPictureLossIndication
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
	case *rtcp.FullIntraRequest:
		event.Kind = RTCPFeedbackFullIntraRequest
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
		event.FIRCount = len(p.FIR)
	case *rtcp.RapidResynchronizationRequest:
		event.Kind = RTCPFeedbackRapidResynchronizationRequest
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
	case *rtcp.TransportLayerNack:
		event.Kind = RTCPFeedbackTransportLayerNack
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
		for _, nack := range p.Nacks {
			event.NACKCount += len(nack.PacketList())
		}
	case *rtcp.ReceiverEstimatedMaximumBitrate:
		event.Kind = RTCPFeedbackReceiverEstimatedMaximumBitrate
		event.SenderSSRC = p.SenderSSRC
		event.REMBBitrate = float64(p.Bitrate)
		event.REMBSSRCs = append([]uint32(nil), p.SSRCs...)
	case *rtcp.TransportLayerCC:
		event.Kind = RTCPFeedbackTransportLayerCongestionControl
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
		event.TransportCCCount = int(p.PacketStatusCount)
	case *rtcp.SliceLossIndication:
		event.Kind = RTCPFeedbackSliceLossIndication
		event.SenderSSRC = p.SenderSSRC
		event.MediaSSRC = p.MediaSSRC
		event.SLICount = len(p.SLI)
	case *rtcp.ExtendedReport:
		event.Kind = RTCPFeedbackExtendedReport
	case *rtcp.SourceDescription:
		event.Kind = RTCPFeedbackSourceDescription
	case *rtcp.Goodbye:
		event.Kind = RTCPFeedbackGoodbye
	case *rtcp.ApplicationDefined:
		event.Kind = RTCPFeedbackApplicationDefined
		event.SSRC = p.SSRC
	case *rtcp.RawPacket:
		event.Kind = RTCPFeedbackUnknown
	}
	return []RTCPFeedbackEvent{event}
}

func (s *RTCPFeedbackSummary) add(kind RTCPFeedbackKind) {
	if s == nil {
		return
	}
	s.Packets++
	switch kind {
	case RTCPFeedbackSenderReport:
		s.SenderReports++
	case RTCPFeedbackReceiverReport:
		s.ReceiverReports++
	case RTCPFeedbackPictureLossIndication:
		s.PictureLossIndications++
	case RTCPFeedbackFullIntraRequest:
		s.FullIntraRequests++
	case RTCPFeedbackRapidResynchronizationRequest:
		s.RapidResynchronizationRequests++
	case RTCPFeedbackTransportLayerNack:
		s.TransportLayerNacks++
	case RTCPFeedbackReceiverEstimatedMaximumBitrate:
		s.ReceiverEstimatedMaximumBitrates++
	case RTCPFeedbackTransportLayerCongestionControl:
		s.TransportLayerCongestionControls++
	case RTCPFeedbackSliceLossIndication:
		s.SliceLossIndications++
	case RTCPFeedbackExtendedReport:
		s.ExtendedReports++
	case RTCPFeedbackSourceDescription:
		s.SourceDescriptions++
	case RTCPFeedbackGoodbye:
		s.Goodbyes++
	case RTCPFeedbackApplicationDefined:
		s.ApplicationDefined++
	default:
		s.UnknownPackets++
	}
}

func emitRTCPFeedback(handler RTCPFeedbackHandler, event RTCPFeedbackEvent) {
	if handler == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	handler(event)
}

type rtcpHeaderer interface {
	Header() rtcp.Header
}

func rtcpPacketType(packet rtcp.Packet) string {
	if packet == nil {
		return ""
	}
	if headerer, ok := packet.(rtcpHeaderer); ok {
		header := headerer.Header()
		return strconv.Itoa(int(header.Type))
	}
	name := fmt.Sprintf("%T", packet)
	name = strings.TrimPrefix(name, "*")
	name = strings.TrimPrefix(name, "rtcp.")
	return name
}
