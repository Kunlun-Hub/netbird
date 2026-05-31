package client

import (
	"context"
	"errors"
	"slices"
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

func TestServerPicker_AvailableServerURLsSkipsCooldown(t *testing.T) {
	now := time.Unix(100, 0)
	sp := ServerPicker{CooldownDuration: time.Minute}
	serverURLs := []string{"relay-a", "relay-b"}

	sp.markServerFailure("relay-a", now, errors.New("dial failed"))

	got := sp.availableServerURLs(serverURLs, now.Add(10*time.Second))
	if want := []string{"relay-b"}; !slices.Equal(got, want) {
		t.Fatalf("availableServerURLs() = %v, want %v", got, want)
	}

	got = sp.availableServerURLs(serverURLs, now.Add(time.Minute+time.Nanosecond))
	if !slices.Equal(got, serverURLs) {
		t.Fatalf("availableServerURLs() after cooldown = %v, want %v", got, serverURLs)
	}
}

func TestServerPicker_AvailableServerURLsFallsBackWhenAllCooldown(t *testing.T) {
	now := time.Unix(100, 0)
	sp := ServerPicker{CooldownDuration: time.Minute}
	serverURLs := []string{"relay-a", "relay-b"}

	sp.markServerFailure("relay-a", now, errors.New("dial failed"))
	sp.markServerFailure("relay-b", now, errors.New("dial failed"))

	got := sp.availableServerURLs(serverURLs, now.Add(10*time.Second))
	if !slices.Equal(got, serverURLs) {
		t.Fatalf("availableServerURLs() = %v, want fallback to %v", got, serverURLs)
	}
}

func TestServerPicker_ClearServerFailure(t *testing.T) {
	now := time.Unix(100, 0)
	sp := ServerPicker{CooldownDuration: time.Minute}
	serverURLs := []string{"relay-a", "relay-b"}

	sp.markServerFailure("relay-a", now, errors.New("dial failed"))
	sp.clearServerFailure("relay-a")

	got := sp.availableServerURLs(serverURLs, now.Add(10*time.Second))
	if !slices.Equal(got, serverURLs) {
		t.Fatalf("availableServerURLs() = %v, want %v", got, serverURLs)
	}
}

func TestServerPicker_DrainConnResultsDoesNotCooldownAfterSuccess(t *testing.T) {
	sp := ServerPicker{CooldownDuration: time.Minute}
	serverURLs := []string{"relay-a", "relay-b"}
	resultChan := make(chan connResult, 2)
	resultChan <- connResult{Url: "relay-a", Err: errors.New("connection canceled")}

	sp.drainConnResults(resultChan, 1, 2)

	got := sp.availableServerURLs(serverURLs, time.Now())
	if !slices.Equal(got, serverURLs) {
		t.Fatalf("availableServerURLs() = %v, want %v", got, serverURLs)
	}
}
