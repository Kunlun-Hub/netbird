package types

import (
	"encoding/binary"
	"strings"

	"github.com/miekg/dns"
)

func ParseDNSInfo(payload []byte) *DNSInfo {
	if len(payload) == 0 {
		return nil
	}

	msg := &dns.Msg{}
	if err := msg.Unpack(payload); err != nil {
		return nil
	}

	info := &DNSInfo{
		RCode: dns.RcodeToString[msg.Rcode],
	}

	if len(msg.Question) > 0 {
		question := msg.Question[0]
		info.Domain = strings.TrimSuffix(question.Name, ".")
		info.QueryType = dns.TypeToString[question.Qtype]
	}

	for _, answer := range msg.Answer {
		if value := answerValue(answer); value != "" {
			info.Answers = append(info.Answers, value)
		}
	}

	if info.Domain == "" && len(info.Answers) == 0 && info.RCode == "" {
		return nil
	}

	return info
}

func ParseTCPDNSInfo(payload []byte) *DNSInfo {
	info, _ := AppendTCPDNSInfo(nil, payload)
	return info
}

func AppendTCPDNSInfo(buffer []byte, payload []byte) (*DNSInfo, []byte) {
	if len(payload) == 0 {
		return nil, buffer
	}

	buffer = append(buffer, payload...)

	var merged *DNSInfo
	for len(buffer) >= 2 {
		msgLength := int(binary.BigEndian.Uint16(buffer[:2]))
		if msgLength <= 0 {
			return merged, nil
		}

		frameLength := 2 + msgLength
		if len(buffer) < frameLength {
			return merged, trimTCPDNSBuffer(buffer)
		}

		merged = MergeDNSInfo(merged, ParseDNSInfo(buffer[2:frameLength]))
		buffer = buffer[frameLength:]
	}

	return merged, buffer
}

func trimTCPDNSBuffer(buffer []byte) []byte {
	const maxDNSFrameLength = 2 + 65535
	if len(buffer) <= maxDNSFrameLength {
		return buffer
	}
	return buffer[len(buffer)-maxDNSFrameLength:]
}

func MergeDNSInfo(current, next *DNSInfo) *DNSInfo {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}

	merged := *current
	if merged.Domain == "" {
		merged.Domain = next.Domain
	}
	if merged.QueryType == "" {
		merged.QueryType = next.QueryType
	}
	if next.RCode != "" {
		merged.RCode = next.RCode
	}

	seen := make(map[string]struct{}, len(merged.Answers)+len(next.Answers))
	answers := make([]string, 0, len(merged.Answers)+len(next.Answers))
	for _, answer := range append(merged.Answers, next.Answers...) {
		if answer == "" {
			continue
		}
		if _, ok := seen[answer]; ok {
			continue
		}
		seen[answer] = struct{}{}
		answers = append(answers, answer)
	}
	merged.Answers = answers

	return &merged
}

func answerValue(rr dns.RR) string {
	switch value := rr.(type) {
	case *dns.A:
		return value.A.String()
	case *dns.AAAA:
		return value.AAAA.String()
	case *dns.CNAME:
		return strings.TrimSuffix(value.Target, ".")
	case *dns.PTR:
		return strings.TrimSuffix(value.Ptr, ".")
	case *dns.MX:
		return strings.TrimSuffix(value.Mx, ".")
	case *dns.NS:
		return strings.TrimSuffix(value.Ns, ".")
	case *dns.TXT:
		return strings.Join(value.Txt, " ")
	case *dns.SRV:
		return strings.TrimSuffix(value.Target, ".")
	default:
		return strings.TrimSpace(rr.String())
	}
}
