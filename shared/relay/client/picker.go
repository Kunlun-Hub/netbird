package client

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	auth "github.com/netbirdio/netbird/shared/relay/auth/hmac"
)

const (
	maxConcurrentServers     = 7
	defaultConnectionTimeout = 30 * time.Second
	preferredConnectionDelay = 1500 * time.Millisecond
)

type connResult struct {
	RelayClient *Client
	Url         string
	Err         error
}

type ServerPicker struct {
	TokenStore        *auth.TokenStore
	ServerURLs        atomic.Value
	PeerID            string
	MTU               uint16
	ConnectionTimeout time.Duration
}

func (sp *ServerPicker) PickServer(parentCtx context.Context) (*Client, error) {
	ctx, cancel := context.WithTimeout(parentCtx, sp.ConnectionTimeout)
	defer cancel()

	serverURLs := sp.ServerURLs.Load().([]string)
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

	startFallbacks := func() {
		for _, url := range serverURLs[1:] {
			startConnection(url)
		}
	}

	log.Debugf("pick server from list: %v", serverURLs)
	startConnection(serverURLs[0])

	fallbacksStarted := totalServers == 1
	preferredTimer := time.NewTimer(preferredConnectionDelay)
	defer preferredTimer.Stop()
	if fallbacksStarted {
		preferredTimer.Stop()
	}

	receivedResults := 0
	for receivedResults < startedServers || !fallbacksStarted {
		select {
		case cr := <-connResultChan:
			receivedResults++
			if cr.Err == nil {
				log.Infof("chosen home Relay server: %s", cr.Url)
				cancelConnectionsExcept(cr.Url)
				go sp.drainConnResults(connResultChan, receivedResults, startedServers)
				return cr.RelayClient, nil
			}

			log.Tracef("failed to connect to Relay server: %s: %v", cr.Url, cr.Err)
			if !fallbacksStarted {
				fallbacksStarted = true
				preferredTimer.Stop()
				startFallbacks()
			}
		case <-preferredTimer.C:
			if !fallbacksStarted {
				log.Tracef("preferred Relay server did not connect within %s, trying fallback servers", preferredConnectionDelay)
				fallbacksStarted = true
				startFallbacks()
			}
		case <-ctx.Done():
			cancelConnectionsExcept("")
			return nil, fmt.Errorf("failed to connect to any relay server: %w", ctx.Err())
		}
	}

	cancelConnectionsExcept("")
	return nil, errors.New("failed to connect to any relay server: all attempts failed")
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
			continue
		}
		log.Infof("connected to Relay server: %s", cr.Url)

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
