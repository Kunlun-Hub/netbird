package store

import (
	"context"
	"net/netip"
	"os"
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
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_dns_domain_filter_mode"))
	require.True(t, sqlStore.db.Migrator().HasColumn(&types.Account{}, "settings_extra_flow_dns_domain_filter_list"))
}

func TestEnsureFlowLogStorageDoesNotBackfillFlowSummaries(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	event := &networktraffic.Event{
		ID:        "raw-event-id",
		AccountID: "account-id",
		FlowID:    "flow-id",
		Timestamp: time.Now().UTC(),
		TxPackets: 1,
	}
	require.NoError(t, sqlStore.db.Create(event).Error)

	require.NoError(t, ensureFlowLogStorage(ctx, sqlStore.db))

	var summaryCount int64
	require.NoError(t, sqlStore.db.Model(&networktraffic.FlowSummary{}).Count(&summaryCount).Error)
	require.Equal(t, int64(0), summaryCount)
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

func TestCreateNetworkTrafficEventSkipsUnsupportedDNSLogs(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	events := []*networktraffic.Event{
		{
			ID:           "dns-empty-success",
			AccountID:    "account-id",
			FlowID:       "flow-empty-success",
			Timestamp:    time.Now().UTC(),
			DNSDomain:    "example.com",
			DNSQueryType: "AAAA",
			DNSRCode:     "NOERROR",
		},
		{
			ID:           "dns-blank-answer-success",
			AccountID:    "account-id",
			FlowID:       "flow-blank-answer-success",
			Timestamp:    time.Now().UTC(),
			DNSDomain:    "example.com",
			DNSQueryType: "A",
			DNSAnswers:   []string{" "},
			DNSRCode:     "NOERROR",
		},
		{
			ID:           "dns-mx-answer",
			AccountID:    "account-id",
			FlowID:       "flow-mx-answer",
			Timestamp:    time.Now().UTC(),
			DNSDomain:    "example.com",
			DNSQueryType: "MX",
			DNSAnswers:   []string{"mail.example.com"},
			DNSRCode:     "NOERROR",
		},
		{
			ID:           "dns-txt-failed",
			AccountID:    "account-id",
			FlowID:       "flow-txt-failed",
			Timestamp:    time.Now().UTC(),
			DNSDomain:    "missing.example.com",
			DNSQueryType: "TXT",
			DNSRCode:     "NXDOMAIN",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	var eventCount int64
	require.NoError(t, sqlStore.db.Model(&networktraffic.Event{}).Count(&eventCount).Error)
	require.Equal(t, int64(0), eventCount)

	var summaryCount int64
	require.NoError(t, sqlStore.db.Model(&networktraffic.FlowSummary{}).Count(&summaryCount).Error)
	require.Equal(t, int64(0), summaryCount)
}

func TestCreateNetworkTrafficEventUpdatesSummaryWithDNSAnswers(t *testing.T) {
	testCreateNetworkTrafficEventUpdatesSummaryWithDNSAnswers(t)
}

func TestPostgresql_CreateNetworkTrafficEventUpdatesSummaryWithDNSAnswers(t *testing.T) {
	previousEngine, hadEngine := os.LookupEnv("NETBIRD_STORE_ENGINE")
	t.Setenv("NETBIRD_STORE_ENGINE", string(types.PostgresStoreEngine))
	t.Cleanup(func() {
		if hadEngine {
			require.NoError(t, os.Setenv("NETBIRD_STORE_ENGINE", previousEngine))
			return
		}
		require.NoError(t, os.Unsetenv("NETBIRD_STORE_ENGINE"))
	})

	testCreateNetworkTrafficEventUpdatesSummaryWithDNSAnswers(t)
}

func testCreateNetworkTrafficEventUpdatesSummaryWithDNSAnswers(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	flowID := "flow-id"
	now := time.Now().UTC()

	require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, &networktraffic.Event{
		ID:                 "initial-event",
		AccountID:          accountID,
		FlowID:             flowID,
		Timestamp:          now,
		Protocol:           17,
		SourceAddress:      "100.80.1.1:52000",
		DestinationAddress: "100.80.1.53:53",
		RxPackets:          1,
	}))

	require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, &networktraffic.Event{
		ID:                 "dns-event",
		AccountID:          accountID,
		FlowID:             flowID,
		Timestamp:          now.Add(time.Second),
		Protocol:           17,
		SourceAddress:      "100.80.1.1:52000",
		DestinationAddress: "100.80.1.53:53",
		DNSDomain:          "example.com",
		DNSQueryType:       "A",
		DNSAnswers:         []string{"93.184.216.34"},
		DNSRCode:           "NOERROR",
	}))

	var summary networktraffic.FlowSummary
	require.NoError(t, sqlStore.db.
		Where("account_id = ? AND flow_id = ?", accountID, flowID).
		First(&summary).Error)
	require.Equal(t, 2, summary.EventCount)
	require.Equal(t, "example.com", summary.DNSDomain)
	require.Equal(t, "A", summary.DNSQueryType)
	require.Equal(t, []string{"93.184.216.34"}, summary.DNSAnswers)
	require.Equal(t, "NOERROR", summary.DNSRCode)
}

func TestGetAccountNetworkTrafficSummaryReturnsBackendPoints(t *testing.T) {
	ctx := context.Background()
	store, cleanup, err := NewTestStoreFromSQL(ctx, "", t.TempDir())
	require.NoError(t, err)
	defer cleanup()

	sqlStore, ok := store.(*SqlStore)
	require.True(t, ok)

	accountID := "account-id"
	startTime := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	endTime := startTime.Add(3 * time.Minute)
	events := []*networktraffic.Event{
		{
			ID:             "first-bucket",
			AccountID:      accountID,
			FlowID:         "flow-a",
			Timestamp:      startTime.Add(10 * time.Second),
			ConnectionType: networktraffic.ConnectionTypeRouted,
			RxBytes:        120,
			TxBytes:        60,
		},
		{
			ID:             "third-bucket",
			AccountID:      accountID,
			FlowID:         "flow-b",
			Timestamp:      startTime.Add(2*time.Minute + 5*time.Second),
			ConnectionType: networktraffic.ConnectionTypeRouted,
			RxBytes:        300,
			TxBytes:        180,
		},
		{
			ID:             "filtered-out",
			AccountID:      accountID,
			FlowID:         "flow-c",
			Timestamp:      startTime.Add(time.Minute),
			ConnectionType: networktraffic.ConnectionTypeP2P,
			RxBytes:        999,
			TxBytes:        999,
		},
		{
			ID:             "end-exclusive",
			AccountID:      accountID,
			FlowID:         "flow-d",
			Timestamp:      endTime,
			ConnectionType: networktraffic.ConnectionTypeRouted,
			RxBytes:        999,
			TxBytes:        999,
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	connectionType := networktraffic.ConnectionTypeRouted
	filter := networktraffic.Filter{
		StartDate:      &startTime,
		EndDate:        &endTime,
		ConnectionType: &connectionType,
	}

	points, err := sqlStore.GetAccountNetworkTrafficSummary(ctx, accountID, filter, 0)
	require.NoError(t, err)
	require.Len(t, points, 3)

	require.Equal(t, startTime, points[0].BucketStart)
	require.Equal(t, startTime.Add(time.Minute), points[0].BucketEnd)
	require.Equal(t, float64(networktraffic.SummaryBucketSeconds), points[0].CoveredSeconds)
	require.Equal(t, int64(120), points[0].RxBytes)
	require.Equal(t, int64(60), points[0].TxBytes)
	require.InDelta(t, 2.0, points[0].DownloadRate, 0.001)
	require.InDelta(t, 1.0, points[0].UploadRate, 0.001)

	require.Equal(t, startTime.Add(time.Minute), points[1].BucketStart)
	require.Equal(t, int64(0), points[1].RxBytes)
	require.Equal(t, int64(0), points[1].TxBytes)
	require.InDelta(t, 0.0, points[1].DownloadRate, 0.001)
	require.InDelta(t, 0.0, points[1].UploadRate, 0.001)

	require.Equal(t, startTime.Add(2*time.Minute), points[2].BucketStart)
	require.Equal(t, int64(300), points[2].RxBytes)
	require.Equal(t, int64(180), points[2].TxBytes)
	require.InDelta(t, 5.0, points[2].DownloadRate, 0.001)
	require.InDelta(t, 3.0, points[2].UploadRate, 0.001)
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

func TestGetAccountNetworkTrafficEventsDNSFilterRequiresDNSInfo(t *testing.T) {
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
			ID:                 "tcp-dns-port-only",
			AccountID:          accountID,
			FlowID:             "flow-tcp-dns-port-only",
			Timestamp:          now,
			Protocol:           6,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:53",
		},
		{
			ID:                 "dns-record",
			AccountID:          accountID,
			FlowID:             "flow-dns-record",
			Timestamp:          now.Add(time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52001",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "A",
			DNSAnswers:         []string{"93.184.216.34"},
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "tcp-web",
			AccountID:          accountID,
			FlowID:             "flow-tcp-web",
			Timestamp:          now.Add(2 * time.Second),
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
	require.Equal(t, "dns-record", result[0].ID)
}

func TestGetAccountNetworkTrafficEventsDNSFilterExcludesNOERRORWithoutAnswers(t *testing.T) {
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
			ID:                 "dns-empty-success",
			AccountID:          accountID,
			FlowID:             "flow-empty-success",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "AAAA",
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "dns-blank-answer-success",
			AccountID:          accountID,
			FlowID:             "flow-blank-answer-success",
			Timestamp:          now.Add(500 * time.Millisecond),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52003",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "A",
			DNSAnswers:         []string{" "},
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "dns-success-answer",
			AccountID:          accountID,
			FlowID:             "flow-success-answer",
			Timestamp:          now.Add(time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52001",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "A",
			DNSAnswers:         []string{"93.184.216.34"},
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "dns-failed",
			AccountID:          accountID,
			FlowID:             "flow-failed",
			Timestamp:          now.Add(2 * time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52002",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "missing.example.com",
			DNSQueryType:       "A",
			DNSRCode:           "NXDOMAIN",
		},
		{
			ID:                 "dns-mx-answer",
			AccountID:          accountID,
			FlowID:             "flow-mx-answer",
			Timestamp:          now.Add(3 * time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52004",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "MX",
			DNSAnswers:         []string{"mail.example.com"},
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "dns-txt-failed",
			AccountID:          accountID,
			FlowID:             "flow-txt-failed",
			Timestamp:          now.Add(4 * time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52005",
			DestinationAddress: "100.80.1.2:53",
			DNSDomain:          "missing.example.com",
			DNSQueryType:       "TXT",
			DNSRCode:           "NXDOMAIN",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.db.Create(event).Error)
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
	require.Equal(t, int64(2), total)
	require.Len(t, result, 2)
	require.Equal(t, "dns-failed", result[0].ID)
	require.Equal(t, "dns-success-answer", result[1].ID)

	requireAnswers := true
	filter.RequireDNSAnswers = &requireAnswers

	result, total, err = sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	require.Equal(t, "dns-success-answer", result[0].ID)
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

func TestGetAccountNetworkTrafficEventsAggregateFlowsIncludesDNSWithoutCounters(t *testing.T) {
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
			ID:                 "dns-flow",
			AccountID:          accountID,
			FlowID:             "flow-dns",
			Timestamp:          now,
			Protocol:           17,
			SourceAddress:      "100.80.1.1:52000",
			DestinationAddress: "100.80.1.53:53",
			DNSDomain:          "example.com",
			DNSQueryType:       "A",
			DNSAnswers:         []string{"93.184.216.34"},
			DNSRCode:           "NOERROR",
		},
		{
			ID:                 "empty-flow",
			AccountID:          accountID,
			FlowID:             "flow-empty",
			Timestamp:          now.Add(time.Second),
			Protocol:           17,
			SourceAddress:      "100.80.1.2:52000",
			DestinationAddress: "100.80.1.3:443",
		},
	}

	for _, event := range events {
		require.NoError(t, sqlStore.CreateNetworkTrafficEvent(ctx, event))
	}

	dnsOnly := true
	aggregateFlows := true
	filter := networktraffic.Filter{
		Page:           1,
		PageSize:       10,
		SortBy:         networktraffic.DefaultSortBy,
		SortOrd:        networktraffic.DefaultSortOrd,
		DNS:            &dnsOnly,
		AggregateFlows: &aggregateFlows,
	}

	result, total, err := sqlStore.GetAccountNetworkTrafficEvents(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	require.Equal(t, "dns-flow", result[0].ID)
	require.Equal(t, "example.com", result[0].DNSDomain)
}

func TestNetworkTrafficGroupsFallbackToRawEventsWhenSummariesAreEmpty(t *testing.T) {
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
			Timestamp:          now.Add(-time.Minute),
			SourceID:           "peer-a",
			SourceType:         networktraffic.EndpointTypePeer,
			SourceName:         "Peer A",
			SourceAddress:      "100.80.1.1:52000",
			DestinationID:      "resource-a",
			DestinationType:    networktraffic.EndpointTypeHostResource,
			DestinationName:    "Resource A",
			DestinationAddress: "192.168.3.10:443",
		},
		{
			ID:                 "flow-a-end",
			AccountID:          accountID,
			FlowID:             "flow-a",
			Timestamp:          now,
			SourceID:           "peer-a",
			SourceType:         networktraffic.EndpointTypePeer,
			SourceName:         "Peer A",
			SourceAddress:      "100.80.1.1:52000",
			DestinationID:      "resource-a",
			DestinationType:    networktraffic.EndpointTypeHostResource,
			DestinationName:    "Resource A",
			DestinationAddress: "192.168.3.10:443",
			TxPackets:          2,
			RxPackets:          3,
		},
		{
			ID:                 "flow-b-end",
			AccountID:          accountID,
			FlowID:             "flow-b",
			Timestamp:          now.Add(-30 * time.Second),
			SourceID:           "peer-b",
			SourceType:         networktraffic.EndpointTypePeer,
			SourceName:         "Peer B",
			SourceAddress:      "100.80.1.2:52000",
			DestinationID:      "resource-b",
			DestinationType:    networktraffic.EndpointTypeHostResource,
			DestinationName:    "Resource B",
			DestinationAddress: "192.168.3.20:443",
			TxPackets:          4,
		},
	}
	for _, event := range events {
		require.NoError(t, sqlStore.db.Create(event).Error)
	}

	filter := networktraffic.Filter{
		Page:     1,
		PageSize: 10,
		SortBy:   networktraffic.DefaultSortBy,
		SortOrd:  networktraffic.DefaultSortOrd,
	}

	groups, total, err := sqlStore.GetAccountNetworkTrafficClientGroups(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, groups, 2)
	require.Equal(t, "peer-a", groups[0].ID)
	require.Equal(t, int64(1), groups[0].FlowCount)
	require.Equal(t, int64(2), groups[0].TxPackets)
	require.Equal(t, int64(3), groups[0].RxPackets)

	clientKey := "peer-a"
	filter.ClientKey = &clientKey
	flowEvents, flowTotal, err := sqlStore.GetAccountNetworkTrafficGroupFlows(ctx, LockingStrengthNone, accountID, filter)
	require.NoError(t, err)
	require.Equal(t, int64(1), flowTotal)
	require.Len(t, flowEvents, 2)
	require.Equal(t, "flow-a", flowEvents[0].FlowID)
	require.Equal(t, "flow-a", flowEvents[1].FlowID)
}
