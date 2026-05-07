package store

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/netbirdio/netbird/client/internal/netflow/types"
)

func TestFileStorePersistsAndDeletesEvents(t *testing.T) {
	dir := t.TempDir()
	firstID := uuid.New()
	secondID := uuid.New()

	store := NewFileStore(dir, 1, 10)
	store.StoreEvent(&types.Event{
		ID: firstID,
		EventFields: types.EventFields{
			FlowID:   uuid.New(),
			Protocol: types.TCP,
		},
	})
	store.StoreEvent(&types.Event{
		ID: secondID,
		EventFields: types.EventFields{
			FlowID:   uuid.New(),
			Protocol: types.UDP,
		},
	})
	store.Close()

	reopened := NewFileStore(dir, 1, 10)
	events := reopened.GetEvents()
	require.Len(t, events, 2)
	require.ElementsMatch(t, []uuid.UUID{firstID, secondID}, eventIDs(events))

	reopened.DeleteEvents([]uuid.UUID{firstID})
	events = reopened.GetEvents()
	require.Len(t, events, 1)
	require.Equal(t, secondID, events[0].ID)
}

func eventIDs(events []*types.Event) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}
