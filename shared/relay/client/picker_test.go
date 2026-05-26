package client

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServerPicker_UnavailableServers(t *testing.T) {
	timeout := 5 * time.Second
	sp := ServerPicker{
		TokenStore:        nil,
		PeerID:            "test",
		ConnectionTimeout: timeout,
	}
	sp.ServerURLs.Store([]string{"rel://dummy1", "rel://dummy2"})

	ctx, cancel := context.WithTimeout(context.Background(), timeout+1)
	defer cancel()

	go func() {
		_, err := sp.PickServer(ctx)
		if err == nil {
			t.Error(err)
		}
		cancel()
	}()

	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Errorf("PickServer() took too long to complete")
	}
}

func TestServerPicker_StartNextPriorityGroupStartsSameWeightOnly(t *testing.T) {
	sp := ServerPicker{}
	sp.ServerWeights.Store(map[string]int{
		"relay-a": 40,
		"relay-b": 40,
		"relay-c": 30,
	})

	var started []string
	next := sp.startNextPriorityGroup([]string{"relay-a", "relay-b", "relay-c"}, 0, func(url string) {
		started = append(started, url)
	})

	if next != 2 {
		t.Fatalf("next start index = %d, want 2", next)
	}
	if len(started) != 2 || started[0] != "relay-a" || started[1] != "relay-b" {
		t.Fatalf("started = %v, want relay-a and relay-b", started)
	}
}
