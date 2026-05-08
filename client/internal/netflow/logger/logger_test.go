package logger_test

import (
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/netbirdio/netbird/client/internal/netflow/logger"
	"github.com/netbirdio/netbird/client/internal/netflow/types"
)

func TestStore(t *testing.T) {
	logger := logger.New(nil, netip.Prefix{}, netip.Prefix{})
	logger.UpdateFlowStorageConfig(true, t.TempDir(), 1, 10, false, "", "", "", "")
	logger.Enable()

	event := types.EventFields{
		FlowID:    uuid.New(),
		Type:      types.TypeStart,
		Direction: types.Ingress,
		Protocol:  6,
	}

	wait := func() { time.Sleep(time.Millisecond) }
	wait()
	logger.StoreEvent(event)
	wait()

	allEvents := logger.GetEvents()
	eventCount := len(allEvents)
	matched := false
	for _, e := range allEvents {
		if e.EventFields.FlowID == event.FlowID {
			matched = true
		}
	}
	if !matched {
		t.Errorf("didn't match any event")
	}

	// test disable
	logger.Close()
	wait()
	logger.StoreEvent(event)
	wait()
	allEvents = logger.GetEvents()
	if len(allEvents) != eventCount {
		t.Errorf("expected %d events, got %d", eventCount, len(allEvents))
	}

	// test re-enable
	logger.Enable()
	wait()
	logger.StoreEvent(event)
	wait()

	allEvents = logger.GetEvents()
	matched = false
	for _, e := range allEvents {
		if e.EventFields.FlowID == event.FlowID {
			matched = true
		}
	}
	if !matched {
		t.Errorf("didn't match any event")
	}
}

func TestLocalStorageRestoresPendingEvents(t *testing.T) {
	dir := t.TempDir()
	wgNet := netip.MustParsePrefix("100.64.0.0/10")
	flowID := uuid.New()

	first := logger.New(nil, wgNet, netip.Prefix{})
	first.UpdateFlowStorageConfig(true, dir, 1, 10, false, "", "", "", "")
	first.Enable()
	time.Sleep(time.Millisecond)

	first.StoreEvent(types.EventFields{
		FlowID:    flowID,
		Type:      types.TypeStart,
		Direction: types.Ingress,
		Protocol:  types.TCP,
		SourceIP:  netip.MustParseAddr("100.64.0.1"),
		DestIP:    netip.MustParseAddr("100.64.0.2"),
	})
	time.Sleep(10 * time.Millisecond)
	first.Close()

	second := logger.New(nil, wgNet, netip.Prefix{})
	second.UpdateFlowStorageConfig(true, dir, 1, 10, false, "", "", "", "")

	allEvents := second.GetEvents()
	matched := false
	for _, e := range allEvents {
		if e.EventFields.FlowID == flowID {
			matched = true
			second.DeleteEvents([]uuid.UUID{e.ID})
		}
	}
	if !matched {
		t.Fatalf("expected pending flow event to be restored from local storage")
	}

	if len(second.GetEvents()) != 0 {
		t.Fatalf("expected restored event to be deleted after ack")
	}
}
