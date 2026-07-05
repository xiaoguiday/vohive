package voicehost

import (
	"net"
	"strconv"
	"strings"
)

func RewriteSDPMediaEndpoint(body []byte, endpoint SDPInfo) []byte {
	if len(body) == 0 || strings.TrimSpace(endpoint.ConnectionIP) == "" || endpoint.MediaPort <= 0 {
		return BuildSDPAnswer(endpoint)
	}
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	ipVersion := "IP4"
	if ip := net.ParseIP(endpoint.ConnectionIP); ip != nil && ip.To4() == nil {
		ipVersion = "IP6"
	}
	rtcpIP := strings.TrimSpace(endpoint.RTCPIP)
	if rtcpIP == "" {
		rtcpIP = endpoint.ConnectionIP
	}
	rtcpIPVersion := "IP4"
	if ip := net.ParseIP(rtcpIP); ip != nil && ip.To4() == nil {
		rtcpIPVersion = "IP6"
	}
	rewroteConnection := false
	rewroteAudio := false
	rewroteRTCP := false
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "c=IN IP"):
			out = append(out, "c=IN "+ipVersion+" "+endpoint.ConnectionIP)
			rewroteConnection = true
		case strings.HasPrefix(line, "m=audio "):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				fields[1] = strconv.Itoa(endpoint.MediaPort)
				line = strings.Join(fields, " ")
				rewroteAudio = true
			}
			out = append(out, line)
		case strings.HasPrefix(line, "a=rtcp:") && endpoint.RTCPPort > 0:
			out = append(out, "a=rtcp:"+strconv.Itoa(endpoint.RTCPPort)+" IN "+rtcpIPVersion+" "+rtcpIP)
			rewroteRTCP = true
		default:
			out = append(out, line)
		}
	}
	if !rewroteAudio {
		return BuildSDPAnswer(endpoint)
	}
	if !rewroteConnection {
		insertAt := len(out)
		for i, line := range out {
			if strings.HasPrefix(line, "m=audio ") {
				insertAt = i
				break
			}
		}
		out = append(out, "")
		copy(out[insertAt+1:], out[insertAt:])
		out[insertAt] = "c=IN " + ipVersion + " " + endpoint.ConnectionIP
	}
	if endpoint.RTCPPort > 0 && !rewroteRTCP {
		insertAt := len(out)
		for i, line := range out {
			if strings.HasPrefix(line, "m=audio ") {
				insertAt = i + 1
				break
			}
		}
		out = append(out, "")
		copy(out[insertAt+1:], out[insertAt:])
		out[insertAt] = "a=rtcp:" + strconv.Itoa(endpoint.RTCPPort) + " IN " + rtcpIPVersion + " " + rtcpIP
	}
	return []byte(strings.Join(out, "\r\n") + "\r\n")
}
