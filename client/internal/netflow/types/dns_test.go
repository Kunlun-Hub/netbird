package types

import (
	"encoding/binary"
	"testing"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/require"
)

func TestParseDNSInfoQuestion(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)

	payload, err := msg.Pack()
	require.NoError(t, err)

	info := ParseDNSInfo(payload)
	require.NotNil(t, info)
	require.Equal(t, "example.com", info.Domain)
	require.Equal(t, "A", info.QueryType)
	require.Equal(t, "NOERROR", info.RCode)
	require.Empty(t, info.Answers)
}

func TestParseDNSInfoAnswer(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeA)
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   "example.com.",
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    60,
		},
		A: []byte{93, 184, 216, 34},
	})

	payload, err := msg.Pack()
	require.NoError(t, err)

	info := ParseDNSInfo(payload)
	require.NotNil(t, info)
	require.Equal(t, "example.com", info.Domain)
	require.Equal(t, "A", info.QueryType)
	require.Equal(t, "NOERROR", info.RCode)
	require.Equal(t, []string{"93.184.216.34"}, info.Answers)
}

func TestParseDNSInfoCNAMEAnswer(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("www.example.com.", dns.TypeCNAME)
	msg.Answer = append(msg.Answer, &dns.CNAME{
		Hdr: dns.RR_Header{
			Name:   "www.example.com.",
			Rrtype: dns.TypeCNAME,
			Class:  dns.ClassINET,
			Ttl:    60,
		},
		Target: "example.com.",
	})

	payload, err := msg.Pack()
	require.NoError(t, err)

	info := ParseDNSInfo(payload)
	require.NotNil(t, info)
	require.Equal(t, "www.example.com", info.Domain)
	require.Equal(t, "CNAME", info.QueryType)
	require.Equal(t, "NOERROR", info.RCode)
	require.Equal(t, []string{"example.com"}, info.Answers)
}

func TestParseTCPDNSInfo(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeAAAA)

	dnsPayload, err := msg.Pack()
	require.NoError(t, err)

	payload := make([]byte, 2+len(dnsPayload))
	binary.BigEndian.PutUint16(payload[:2], uint16(len(dnsPayload)))
	copy(payload[2:], dnsPayload)

	info := ParseTCPDNSInfo(payload)
	require.NotNil(t, info)
	require.Equal(t, "example.com", info.Domain)
	require.Equal(t, "AAAA", info.QueryType)
}

func TestParseTCPDNSInfoRejectsPartialPayload(t *testing.T) {
	payload := []byte{0, 10, 1, 2, 3}
	require.Nil(t, ParseTCPDNSInfo(payload))
}

func TestAppendTCPDNSInfoReassemblesSegmentedPayload(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("split.example.com.", dns.TypeTXT)

	dnsPayload, err := msg.Pack()
	require.NoError(t, err)

	payload := make([]byte, 2+len(dnsPayload))
	binary.BigEndian.PutUint16(payload[:2], uint16(len(dnsPayload)))
	copy(payload[2:], dnsPayload)

	info, buffer := AppendTCPDNSInfo(nil, payload[:5])
	require.Nil(t, info)
	require.NotEmpty(t, buffer)

	info, buffer = AppendTCPDNSInfo(buffer, payload[5:])
	require.NotNil(t, info)
	require.Empty(t, buffer)
	require.Equal(t, "split.example.com", info.Domain)
	require.Equal(t, "TXT", info.QueryType)
}

func TestMergeDNSInfoPreservesQueryAndAddsAnswers(t *testing.T) {
	merged := MergeDNSInfo(
		&DNSInfo{Domain: "example.com", QueryType: "A"},
		&DNSInfo{RCode: "NOERROR", Answers: []string{"93.184.216.34", "93.184.216.34"}},
	)

	require.Equal(t, "example.com", merged.Domain)
	require.Equal(t, "A", merged.QueryType)
	require.Equal(t, "NOERROR", merged.RCode)
	require.Equal(t, []string{"93.184.216.34"}, merged.Answers)
}
