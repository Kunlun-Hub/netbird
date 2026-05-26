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
	dnsEvents := []*types.EventFields{
		{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 53},
		{Protocol: types.TCP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 53},
		{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: nbdns.ForwarderClientPort},
		{Protocol: types.TCP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: nbdns.ForwarderServerPort},
		{Protocol: types.UDP, SourceIP: netip.MustParseAddr("100.80.1.1"), DestIP: netip.MustParseAddr("100.80.1.2"), DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "A"}},
	}

	logger := &Logger{}
	for _, event := range dnsEvents {
		require.False(t, logger.shouldStore(&types.Event{EventFields: *event}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
	}

	logger.UpdateConfig(true, false)
	for _, event := range dnsEvents {
		require.True(t, logger.shouldStore(&types.Event{EventFields: *event}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
	}

	require.False(t, (&Logger{}).shouldStore(&types.Event{EventFields: types.EventFields{Protocol: types.TCP, DestPort: 443}}, peer.RouteLookupResult{}, peer.RouteLookupResult{}, false))
}

func TestShouldStoreZeroTrustNetworkFlows(t *testing.T) {
	logger := New(nil, netip.MustParsePrefix("100.80.73.73/10"), netip.Prefix{})

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
