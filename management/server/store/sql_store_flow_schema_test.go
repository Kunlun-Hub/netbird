package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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
