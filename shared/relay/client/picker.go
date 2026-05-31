package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	auth "github.com/netbirdio/netbird/shared/relay/auth/hmac"
)

const (
	maxConcurrentServers     = 7
	defaultConnectionTimeout = 30 * time.Second
)

type connResult struct {
	RelayClient *Client
	Url         string
	Err         error
}

type ServerPicker struct {
	TokenStore        *auth.TokenStore
	ServerURLs        atomic.Value
	ServerWeights     atomic.Value
	PeerID            string
	MTU               uint16
	ConnectionTimeout time.Duration
	CooldownDuration  time.Duration

	cooldownMu sync.Mutex
	cooldowns  map[string]time.Time
}

func (sp *ServerPicker) PickServer(parentCtx context.Context) (*Client, error) {
	ctx, cancel := context.WithTimeout(parentCtx, sp.ConnectionTimeout)
	defer cancel()

	serverURLs := sp.ServerURLs.Load().([]string)
	serverURLs = sp.availableServerURLs(serverURLs, time.Now())
	totalServers := len(serverURLs)
	if totalServers == 0 {
		return nil, errors.New("failed to connect to any relay server: all attempts failed")
	}

	connResultChan := make(chan connResult, totalServers)
	concurrentLimiter := make(chan struct{}, maxConcurrentServers)
	startedServers := 0
	connectionCancels := make(map[string]context.CancelFunc, totalServers)

	startConnection := func(url string) {
		concurrentLimiter <- struct{}{}
		startedServers++
		connectionCtx, connectionCancel := context.WithCancel(parentCtx)
		connectionCancels[url] = connectionCancel
		go func(url string) {
			defer func() {
				<-concurrentLimiter
			}()
			sp.startConnection(connectionCtx, connResultChan, url)
		}(url)
	}

	cancelConnectionsExcept := func(selectedURL string) {
		for url, cancelConnection := range connectionCancels {
			if url == selectedURL {
				continue
			}
			cancelConnection()
		}
	}

	log.Debugf("pick server from list: %v", serverURLs)
	startedUpTo := sp.startNextPriorityGroup(serverURLs, startedServers, startConnection)

	receivedResults := 0
	for receivedResults < startedServers || startedUpTo < totalServers {
		select {
		case cr := <-connResultChan:
			receivedResults++
			if cr.Err == nil {
				log.Infof("chosen home Relay server: %s", cr.Url)
				sp.clearServerFailure(cr.Url)
				cancelConnectionsExcept(cr.Url)
				go sp.drainConnResults(connResultChan, receivedResults, startedServers)
				return cr.RelayClient, nil
			}

			log.Tracef("failed to connect to Relay server: %s: %v", cr.Url, cr.Err)
			sp.markServerFailure(cr.Url, time.Now(), cr.Err)
			if receivedResults == startedServers && startedUpTo < totalServers {
				startedUpTo = sp.startNextPriorityGroup(serverURLs, startedUpTo, startConnection)
			}
		case <-ctx.Done():
			cancelConnectionsExcept("")
			return nil, fmt.Errorf("failed to connect to any relay server: %w", ctx.Err())
		}
	}

	cancelConnectionsExcept("")
	return nil, errors.New("failed to connect to any relay server: all attempts failed")
}

func (sp *ServerPicker) availableServerURLs(serverURLs []string, now time.Time) []string {
	if sp.CooldownDuration <= 0 || len(serverURLs) == 0 {
		return serverURLs
	}

	sp.cooldownMu.Lock()
	defer sp.cooldownMu.Unlock()

	if len(sp.cooldowns) == 0 {
		return serverURLs
	}

	available := make([]string, 0, len(serverURLs))
	skipped := make([]string, 0)
	for _, relayURL := range serverURLs {
		cooldownUntil, ok := sp.cooldowns[relayURL]
		if !ok {
			available = append(available, relayURL)
			continue
		}
		if !now.Before(cooldownUntil) {
			delete(sp.cooldowns, relayURL)
			available = append(available, relayURL)
			continue
		}
		skipped = append(skipped, relayURL)
	}

	if len(available) == 0 {
		log.WithField("cooldown_servers", skipped).Warn("all Relay servers are in cooldown, trying all servers")
		return serverURLs
	}

	if len(skipped) > 0 {
		log.WithField("cooldown_servers", skipped).Debug("skipping Relay servers in cooldown")
	}
	return available
}

func (sp *ServerPicker) markServerFailure(relayURL string, now time.Time, err error) {
	if sp.CooldownDuration <= 0 || relayURL == "" {
		return
	}

	sp.cooldownMu.Lock()
	if sp.cooldowns == nil {
		sp.cooldowns = make(map[string]time.Time)
	}
	cooldownUntil := now.Add(sp.CooldownDuration)
	sp.cooldowns[relayURL] = cooldownUntil
	sp.cooldownMu.Unlock()

	log.WithFields(log.Fields{
		"relay":          relayURL,
		"cooldown_until": cooldownUntil,
		"cooldown":       sp.CooldownDuration,
	}).WithError(err).Debug("marked Relay server cooldown")
}

func (sp *ServerPicker) clearServerFailure(relayURL string) {
	if sp.CooldownDuration <= 0 || relayURL == "" {
		return
	}

	sp.cooldownMu.Lock()
	delete(sp.cooldowns, relayURL)
	sp.cooldownMu.Unlock()
}

func (sp *ServerPicker) startNextPriorityGroup(serverURLs []string, startAt int, startConnection func(string)) int {
	if startAt >= len(serverURLs) {
		return startAt
	}
	remainingCapacity := maxConcurrentServers
	weight := sp.relayURLWeight(serverURLs[startAt])
	idx := startAt
	for idx < len(serverURLs) && sp.relayURLWeight(serverURLs[idx]) == weight && remainingCapacity > 0 {
		startConnection(serverURLs[idx])
		idx++
		remainingCapacity--
	}
	return idx
}

func (sp *ServerPicker) relayURLWeight(relayURL string) int {
	weights, ok := sp.ServerWeights.Load().(map[string]int)
	if !ok {
		return defaultRelayWeight
	}
	weight := weights[relayURL]
	if weight <= 0 {
		return defaultRelayWeight
	}
	return weight
}

func (sp *ServerPicker) startConnection(ctx context.Context, resultChan chan connResult, url string) {
	log.Infof("try to connecting to relay server: %s", url)
	relayClient := NewClient(url, sp.TokenStore, sp.PeerID, sp.MTU)
	err := relayClient.Connect(ctx)
	resultChan <- connResult{
		RelayClient: relayClient,
		Url:         url,
		Err:         err,
	}
}

func (sp *ServerPicker) processConnResults(resultChan chan connResult, successChan chan connResult) {
	var hasSuccess bool
	for numOfResults := 0; numOfResults < cap(resultChan); numOfResults++ {
		cr := <-resultChan
		if cr.Err != nil {
			log.Tracef("failed to connect to Relay server: %s: %v", cr.Url, cr.Err)
			if !hasSuccess {
				sp.markServerFailure(cr.Url, time.Now(), cr.Err)
			}
			continue
		}
		log.Infof("connected to Relay server: %s", cr.Url)
		sp.clearServerFailure(cr.Url)

		if hasSuccess {
			log.Infof("closing unnecessary Relay connection to: %s", cr.Url)
			if err := cr.RelayClient.Close(); err != nil {
				log.Errorf("failed to close connection to %s: %v", cr.Url, err)
			}
			continue
		}

		hasSuccess = true
		successChan <- cr
	}
	close(successChan)
}

func (sp *ServerPicker) drainConnResults(resultChan <-chan connResult, receivedResults, startedServers int) {
	for ; receivedResults < startedServers; receivedResults++ {
		cr := <-resultChan
		if cr.Err != nil || cr.RelayClient == nil {
			continue
		}
		log.Infof("closing unnecessary Relay connection to: %s", cr.Url)
		if err := cr.RelayClient.Close(); err != nil {
			log.Errorf("failed to close connection to %s: %v", cr.Url, err)
		}
	}
}
