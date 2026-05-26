package store

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	nbdns "github.com/netbirdio/netbird/dns"
	"github.com/netbirdio/netbird/management/internals/modules/networktraffic"
	"github.com/netbirdio/netbird/management/server/types"
)

func TestEnsureFlowLogStorage(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	require.NoError(t, ensureFlowLogStorage(ctx, sqlStore.db))
	require.True(t, sqlStore.db.Migrator().HasTable(&networktraffic.Event{}))
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_enabled"))
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_packet_counter_enabled"))
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_en_collection_enabled"))
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_dns_collection_enabled"))
}

func TestCreateNetworkTrafficEventIgnoresDuplicateID(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	event := &networktraffic.Event{
		ID:        "event-id",
		AccountID: "account-id",
		FlowID:    "flow-id",
		Timestamp: time.Now().UTC(),
	}

	require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))

	var count int64
	require.NoError(t, sqlStore.db.Model(&networktraffic.Event{}).Where("id = ?", event.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestGetAccountNetworkTrafficEventsNetworkOnlyFiltersNoise(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	now := time.Now().UTC()
	events := []*networktraffic.Event{
		{
			ID:                 "peer-to-peer",
			AccountID:          accountID,
			FlowID:             "flow-peer-to-peer",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceID:           "peer-a",
			DestinationID:      "peer-b",
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:443",
		},
		{
			ID:                 "resource",
			AccountID:          accountID,
			FlowID:             "flow-resource",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypeHostResource,
			SourceID:           "peer-a",
			DestinationID:      "resource-a",
			SourceAddress:      "100.80.1.1:52001",
			DestinationAddress: "192.168.3.10:80",
		},
		{
			ID:                 "dns",
			AccountID:          accountID,
			FlowID:             "flow-dns",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceID:           "peer-a",
			DestinationID:      "peer-a",
			SourceAddress:      "100.80.1.1:52002",
			DestinationAddress: "100.80.1.1:53",
			DNSDomain:          "example.com",
		},
		{
			ID:                 "multicast",
			AccountID:          accountID,
			FlowID:             "flow-multicast",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypeUnknown,
			SourceAddress:      "100.80.73.73:52420",
			DestinationAddress: "239.255.255.250:1900",
		},
		{
			ID:                 "unknown-lan",
			AccountID:          accountID,
			FlowID:             "flow-unknown-lan",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypeUnknown,
			SourceAddress:      "100.80.73.73:50482",
			DestinationAddress: "192.168.3.1:80",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	networkOnly := true
	filter := networktraffic.Filter{
		Page:        1,
		PageSize:    10,
		SortBy:      networktraffic.DefaultSortBy,
		SortOrd:     networktraffic.DefaultSortOrd,
		NetworkOnly: &networkOnly,
	}

	result, total, err := sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)

	ids := make([]string, 0, len(result))
	for _, event := range result {
		ids = append(ids, event.ID)
	}
	require.ElementsMatch(t, []string{"peer-to-peer", "resource"}, ids)
}

func TestGetAccountNetworkTrafficEventsDNSFilterIncludesTCP(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	now := time.Now().UTC()
	events := []*networktraffic.Event{
		{
			ID:                 "tcp-dns",
			AccountID:          accountID,
			FlowID:             "flow-tcp-dns",
			Timestamp:          now,
			Protocol:           6,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:53",
		},
		{
			ID:                 "tcp-web",
			AccountID:          accountID,
			FlowID:             "flow-tcp-web",
			Timestamp:          now,
			Protocol:           6,
			SourceAddress:      "100.80.1.1:52001",
			DestinationAddress: "100.80.1.2:443",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	dnsOnly := true
	filter := networktraffic.Filter{
		Page:     1,
		PageSize: 10,
		SortBy:   networktraffic.DefaultSortBy,
		SortOrd:  networktraffic.DefaultSortOrd,
		DNS:      &dnsOnly,
	}

	result, total, err := sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	require.Equal(t, "tcp-dns", result[0].ID)
}

func TestGetAccountNetworkTrafficEventsInternalDNSFiltersByNameserverGroup(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	require.NoError(t, sqlStore.SaveNameServerGroup(ctx, &nbdns.NameServerGroup{
		ID:        "internal-dns",
		AccountID: accountID,
		Enabled:   true,
		Primary:   false,
		Domains:   []string{"cloink.local"},
		NameServers: []nbdns.NameServer{{
			IP:     netip.MustParseAddr("100.80.1.53"),
			NSType: nbdns.UDPNameServerType,
			Port:   53,
		}},
	}))
	require.NoError(t, sqlStore.SaveNameServerGroup(ctx, &nbdns.NameServerGroup{
		ID:        "primary-dns",
		AccountID: accountID,
		Enabled:   true,
		Primary:   true,
		Domains:   []string{"example.com"},
		NameServers: []nbdns.NameServer{{
			IP:     netip.MustParseAddr("8.8.8.8"),
			NSType: nbdns.UDPNameServerType,
			Port:   53,
		}},
	}))

	now := time.Now().UTC()
	events := []*networktraffic.Event{
		{
			ID:                 "internal-match",
			AccountID:          accountID,
			FlowID:             "flow-internal-match",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.53:53",
			DNSDomain:          "api.cloink.local",
			DNSQueryType:       "A",
		},
		{
			ID:                 "wrong-domain",
			AccountID:          accountID,
			FlowID:             "flow-wrong-domain",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52001",
			DestinationAddress: "100.80.1.53:53",
			DNSDomain:          "_spotify-connect._tcp.local",
			DNSQueryType:       "PTR",
		},
		{
			ID:                 "wrong-server",
			AccountID:          accountID,
			FlowID:             "flow-wrong-server",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52002",
			DestinationAddress: "8.8.8.8:53",
			DNSDomain:          "api.cloink.local",
			DNSQueryType:       "A",
		},
		{
			ID:                 "primary-group",
			AccountID:          accountID,
			FlowID:             "flow-primary-group",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52003",
			DestinationAddress: "8.8.8.8:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "A",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	dnsOnly := true
	internalDNS := true
	filter := networktraffic.Filter{
		Page:        1,
		PageSize:    10,
		SortBy:      networktraffic.DefaultSortBy,
		SortOrd:     networktraffic.DefaultSortOrd,
		DNS:         &dnsOnly,
		InternalDNS: &internalDNS,
	}

	result, total, err := sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	require.Equal(t, "internal-match", result[0].ID)
}

func TestGetAccountNetworkTrafficEventsAggregateFlowsPaginatesByFlowID(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	now := time.Now().UTC()
	events := []*networktraffic.Event{
		{
			ID:                 "flow-a-start",
			AccountID:          accountID,
			FlowID:             "flow-a",
			Timestamp:          now.Add(-2 * time.Minute),
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:443",
		},
		{
			ID:                 "flow-a-end",
			AccountID:          accountID,
			FlowID:             "flow-a",
			Timestamp:          now,
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:443",
			TxPackets:          2,
		},
		{
			ID:                 "flow-b-start",
			AccountID:          accountID,
			FlowID:             "flow-b",
			Timestamp:          now.Add(-time.Minute),
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceAddress:      "100.80.1.3:52000",
			DestinationAddress: "100.80.1.4:443",
		},
		{
			ID:                 "flow-b-end",
			AccountID:          accountID,
			FlowID:             "flow-b",
			Timestamp:          now.Add(-30 * time.Second),
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceAddress:      "100.80.1.3:52000",
			DestinationAddress: "100.80.1.4:443",
			TxPackets:          2,
		},
		{
			ID:                 "flow-empty-start",
			AccountID:          accountID,
			FlowID:             "flow-empty",
			Timestamp:          now.Add(time.Minute),
			SourceType:         networktraffic.EndpointTypePeer,
			DestinationType:    networktraffic.EndpointTypePeer,
			SourceAddress:      "100.80.1.5:52000",
			DestinationAddress: "100.80.1.6:443",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	aggregateFlows := true
	filter := networktraffic.Filter{
		Page:           1,
		PageSize:       1,
		SortBy:         networktraffic.DefaultSortBy,
		SortOrd:        networktraffic.DefaultSortOrd,
		AggregateFlows: &aggregateFlows,
	}

	result, total, err := sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, result, 2)
	require.Equal(t, "flow-a", result[0].FlowID)
	require.Equal(t, "flow-a", result[1].FlowID)
}
