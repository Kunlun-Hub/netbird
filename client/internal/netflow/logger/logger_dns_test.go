package logger

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/client/internal/netflow/types"
	nbdns "github.com/netbirdio/netbird/dns"
)

func TestShouldStoreDNSCollection(t *testing.T) {
	dnsEvents := []*types.EventFields{
		{Protocol: types.UDP, DestPort: 53},
		{Protocol: types.TCP, DestPort: 53},
		{Protocol: types.UDP, DestPort: nbdns.ForwarderClientPort},
		{Protocol: types.TCP, DestPort: nbdns.ForwarderServerPort},
		{Protocol: types.UDP, DestPort: 443, DNSInfo: &types.DNSInfo{Domain: "example.com", QueryType: "A"}},
	}

	logger := &Logger{}
	for _, event := range dnsEvents {
		require.False(t, logger.shouldStore(event, false))
	}

	logger.UpdateConfig(true, false)
	for _, event := range dnsEvents {
		require.True(t, logger.shouldStore(event, false))
	}

	require.True(t, (&Logger{}).shouldStore(&types.EventFields{Protocol: types.TCP, DestPort: 443}, false))
}
