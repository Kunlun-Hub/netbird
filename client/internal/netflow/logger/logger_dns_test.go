package logger

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/client/internal/netflow/types"
	"github.com/netbirdio/netbird/client/internal/peer"
	nbdns "github.com/netbirdio/netbird/dns"
	"github.com/netbirdio/netbird/route"
)

func TestShouldStoreDNSCollection(t *testing.T) {
	type testCase struct {
		name                       string
		fields                     types.EventFields
		shouldStoreWhenDNSDisabled bool
		shouldStoreWhenDNSEnabled  bool
	}

	cases := []testCase{
		{
			name:   "dns port only udp",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 53},
		},
		{
			name:   "dns port only tcp",
			fields: types.EventFields{Protocol: types.TCP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 53},
		},
		{
			name:   "forwarder client port only",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: nbdns.ForwarderClientPort},
		},
		{
			name:   "forwarder server port only",
			fields: types.EventFields{Protocol: types.TCP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: nbdns.ForwarderServerPort},
		},
		{
			name:   "NOERROR without answers",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "A", RCode: "NOERROR"}},
		},
		{
			name:   "NOERROR with blank answer",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "A", RCode: "NOERROR", Answers: []string{" "}}},
		},
		{
			name:                      "NOERROR with A answer",
			fields:                    types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "A", RCode: "NOERROR", Answers: []string{"93.184.216.34"}}},
			shouldStoreWhenDNSEnabled: true,
		},
		{
			name:                      "NXDOMAIN for A",
			fields:                    types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "missing.example.com", QueryType: "A", RCode: "NXDOMAIN"}},
			shouldStoreWhenDNSEnabled: true,
		},
		{
			name:   "NOERROR with MX answer",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "MX", RCode: "NOERROR", Answers: []string{"mail.example.com"}}},
		},
		{
			name:   "NXDOMAIN for TXT",
			fields: types.EventFields{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "missing.example.com", QueryType: "TXT", RCode: "NXDOMAIN"}},
		},
	}

	logger := &Logger{}
	for _, tc := range cases {
		require.Equal(t, tc.shouldStoreWhenDNSDisabled, logger.shouldStore(&types.Event{EventFields: tc.fields}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false), tc.name)
	}

	logger.UpdateConfig(false, true, false, "", nil)
	for _, tc := range cases {
		require.Equal(t, tc.shouldStoreWhenDNSEnabled, logger.shouldStore(&types.Event{EventFields: tc.fields}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false), tc.name)
	}

	require.False(t, (&Logger{}).shouldStore(&types.Event{EventFields: types.EventFields{Protocol: types.TCP, DestPort: 443}}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
}

func TestShouldStoreDNSCollectionWithDomainFilters(t *testing.T) {
	event := &types.Event{
		EventFields: types.EventFields{
			Protocol: types.UDP,
			SourceIP: netip.MustParseAddr("100.80.1.1"),
			DestIP:   netip.MustParseAddr("100.80.1.2"),
			DestPort: 443,
			DNSInfo: &types.DNSInfo{
				Domain:    "api.baidu.com",
				QueryType: "A",
				RCode:     "NOERROR",
				Answers:   []string{"1.1.1.1"},
			},
		},
	}

	logger := &Logger{}
	logger.UpdateConfig(false, true, false, types.DNSDomainFilterModeAllow, []string{"*.baidu.com"})
	require.True(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	logger.UpdateConfig(false, true, false, types.DNSDomainFilterModeAllow, []string{"baidu.com"})
	require.False(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	logger.UpdateConfig(false, true, false, types.DNSDomainFilterModeExclude, []string{"*.baidu.com"})
	require.False(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	logger.UpdateConfig(false, true, false, types.DNSDomainFilterModeExclude, []string{"example.com"})
	require.True(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
}

func TestZeroDNSCounters(t *testing.T) {
	event := &types.EventFields{
		RxPackets: 4,
		TxPackets: 5,
		RxBytes:   1200,
		TxBytes:   2400,
		DNSInfo:   &types.DNSInfo{Domain: "example.com", QueryType: "A"},
	}

	zeroDNSCounters(event)

	require.Zero(t, event.RxPackets)
	require.Zero(t, event.TxPackets)
	require.Zero(t, event.RxBytes)
	require.Zero(t, event.TxBytes)
}

func TestShouldStoreZeroTrustNetworkFlows(t *testing.T) {
	logger := New(nil, netip.MustParsePrefix("100.80.73.73/10"), netip.Prefix{})
	logger.UpdateConfig(true, false, false, "", nil)

	require.True(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.TCP,
		SourceIP: netip.MustParseAddr("100.80.1.1"),
		DestIP:   netip.MustParseAddr("100.80.1.2"),
		DestPort: 443,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	require.True(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.TCP,
		SourceIP: netip.MustParseAddr("100.80.1.1"),
		DestIP:   netip.MustParseAddr("192.168.3.10"),
		DestPort: 80,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{
		ResourceID: route.ResID("resource-a"),
		Kind:       peer.RouteLookupRemote,
	}, false))

	require.True(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.TCP,
		SourceIP: netip.MustParseAddr("100.80.1.2"),
		DestIP:   netip.MustParseAddr("192.168.3.10"),
		DestPort: 80,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{
		ResourceID: route.ResID("resource-a"),
		Kind:       peer.RouteLookupLocal,
	}, false))

	require.False(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.UDP,
		SourceIP: netip.MustParseAddr("100.80.1.1"),
		DestIP:   netip.MustParseAddr("100.80.1.1"),
		DestPort: 53,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	require.False(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.UDP,
		SourceIP: netip.MustParseAddr("100.80.73.73"),
		DestIP:   netip.MustParseAddr("239.255.255.250"),
		DestPort: 1900,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	require.False(t, logger.shouldStore(&types.Event{EventFields: types.EventFields{
		Protocol: types.TCP,
		SourceIP: netip.MustParseAddr("100.80.73.73"),
		DestIP:   netip.MustParseAddr("192.168.3.1"),
		DestPort: 80,
	}}, peer.RouteLookupResult{}, peer.RouteLookupResult{
		ResourceID: route.ResID("local-lan"),
		Kind:       peer.RouteLookupLocal,
	}, false))
}

func TestShouldStoreTrafficCollection(t *testing.T) {
	logger := New(nil, netip.MustParsePrefix("100.80.73.73/10"), netip.Prefix{})
	event := &types.Event{EventFields: types.EventFields{
		Protocol: types.TCP,
		SourceIP: netip.MustParseAddr("100.80.1.1"),
		DestIP:   netip.MustParseAddr("100.80.1.2"),
		DestPort: 443,
	}}

	logger.UpdateConfig(false, true, false, "", nil)
	require.False(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))

	logger.UpdateConfig(true, false, false, "", nil)
	require.True(t, logger.shouldStore(event, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
}
