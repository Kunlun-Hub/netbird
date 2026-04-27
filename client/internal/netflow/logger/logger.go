package logger

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/client/internal/netflow/store"
	"github.com/netbirdio/netbird/client/internal/netflow/syslog"
	"github.com/netbirdio/netbird/client/internal/netflow/types"
	"github.com/netbirdio/netbird/client/internal/peer"
	"github.com/netbirdio/netbird/dns"
)

type rcvChan chan *types.EventFields
type Logger struct {
	mux                sync.Mutex
	enabled            atomic.Bool
	rcvChan            atomic.Pointer[rcvChan]
	cancel             context.CancelFunc
	statusRecorder     *peer.Status
	wgIfaceNet         netip.Prefix
	dnsCollection      atomic.Bool
	exitNodeCollection atomic.Bool
	Store              types.Store
	fileStore          types.Store
	syslogSender       *syslog.Sender

	localStorageEnabled atomic.Bool
	localStoragePath    string
	localStorageMaxSizeMB int
	localStorageMaxFiles  int

	syslogEnabled atomic.Bool
	syslogServer  string
	syslogProtocol string
	syslogFacility string
	syslogTag      string
}

func New(statusRecorder *peer.Status, wgIfaceIPNet netip.Prefix) *Logger {
	return &Logger{
		statusRecorder: statusRecorder,
		wgIfaceNet:     wgIfaceIPNet,
		Store:          store.NewMemoryStore(),
	}
}

func (l *Logger) UpdateFlowStorageConfig(
	localStorageEnabled bool,
	localStoragePath string,
	localStorageMaxSizeMB int,
	localStorageMaxFiles int,
	syslogEnabled bool,
	syslogServer string,
	syslogProtocol string,
	syslogFacility string,
	syslogTag string,
) {
	l.mux.Lock()
	defer l.mux.Unlock()

	l.localStorageEnabled.Store(localStorageEnabled)
	l.localStoragePath = localStoragePath
	l.localStorageMaxSizeMB = localStorageMaxSizeMB
	l.localStorageMaxFiles = localStorageMaxFiles

	l.syslogEnabled.Store(syslogEnabled)
	l.syslogServer = syslogServer
	l.syslogProtocol = syslogProtocol
	l.syslogFacility = syslogFacility
	l.syslogTag = syslogTag

	l.refreshStorage()
}

func (l *Logger) refreshStorage() {
	if l.localStorageEnabled.Load() {
		maxSize := l.localStorageMaxSizeMB
		if maxSize <= 0 {
			maxSize = 100
		}
		maxFiles := l.localStorageMaxFiles
		if maxFiles <= 0 {
			maxFiles = 10
		}
		l.fileStore = store.NewFileStore(l.localStoragePath, maxSize, maxFiles)
	} else if l.fileStore != nil {
		l.fileStore.Close()
		l.fileStore = nil
	}

	if l.syslogEnabled.Load() && l.syslogServer != "" {
		l.syslogSender = syslog.NewSender(l.syslogProtocol, l.syslogServer, l.syslogTag, l.syslogFacility)
		if err := l.syslogSender.Enable(); err != nil {
			log.Errorf("failed to enable syslog sender: %v", err)
		}
	} else if l.syslogSender != nil {
		l.syslogSender.Close()
		l.syslogSender = nil
	}
}

func (l *Logger) StoreEvent(flowEvent types.EventFields) {
	if !l.enabled.Load() {
		return
	}

	c := l.rcvChan.Load()
	if c == nil {
		return
	}

	select {
	case *c <- &flowEvent:
	default:
		// todo: we should collect or log on this
	}
}

func (l *Logger) Enable() {
	go l.startReceiver()
}

func (l *Logger) startReceiver() {
	if l.enabled.Load() {
		return
	}

	l.mux.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.mux.Unlock()

	c := make(rcvChan, 100)
	l.rcvChan.Store(&c)
	l.enabled.Store(true)

	for {
		select {
		case <-ctx.Done():
			log.Info("flow Memory store receiver stopped")
			return
		case eventFields := <-c:
			id := uuid.New()
			event := types.Event{
				ID:          id,
				EventFields: *eventFields,
				Timestamp:   time.Now().UTC(),
			}

			var isSrcExitNode bool
			var isDestExitNode bool

			if !l.wgIfaceNet.Contains(event.SourceIP) {
				event.SourceResourceID, isSrcExitNode = l.statusRecorder.CheckRoutes(event.SourceIP)
			}

			if !l.wgIfaceNet.Contains(event.DestIP) {
				event.DestResourceID, isDestExitNode = l.statusRecorder.CheckRoutes(event.DestIP)
			}

			if l.shouldStore(eventFields, isSrcExitNode || isDestExitNode) {
				l.Store.StoreEvent(&event)

				if l.localStorageEnabled.Load() && l.fileStore != nil {
					l.fileStore.StoreEvent(&event)
				}

				if l.syslogEnabled.Load() && l.syslogSender != nil {
					if err := l.syslogSender.Send(&event); err != nil {
						log.Debugf("failed to send event to syslog: %v", err)
					}
				}
			}
		}
	}
}

func (l *Logger) Close() {
	l.stop()
	l.Store.Close()
	if l.fileStore != nil {
		l.fileStore.Close()
	}
	if l.syslogSender != nil {
		l.syslogSender.Close()
	}
}

func (l *Logger) stop() {
	if !l.enabled.Load() {
		return
	}

	l.enabled.Store(false)
	l.mux.Lock()
	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.rcvChan.Store(nil)
	l.mux.Unlock()
}

func (l *Logger) GetEvents() []*types.Event {
	return l.Store.GetEvents()
}

func (l *Logger) DeleteEvents(ids []uuid.UUID) {
	l.Store.DeleteEvents(ids)
}

func (l *Logger) UpdateConfig(dnsCollection, exitNodeCollection bool) {
	l.dnsCollection.Store(dnsCollection)
	l.exitNodeCollection.Store(exitNodeCollection)
}

func (l *Logger) shouldStore(event *types.EventFields, isExitNode bool) bool {
	// check dns collection
	if !l.dnsCollection.Load() && event.Protocol == types.UDP &&
		(event.DestPort == 53 || event.DestPort == dns.ForwarderClientPort || event.DestPort == dns.ForwarderServerPort) {
		return false
	}

	// check exit node collection
	if !l.exitNodeCollection.Load() && isExitNode {
		return false
	}

	return true
}
