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
	wgIfaceIP          netip.Addr
	wgIfaceIPv6        netip.Addr
	wgIfaceNet         netip.Prefix
	wgIfaceNetV6       netip.Prefix
	dnsCollection      atomic.Bool
	exitNodeCollection atomic.Bool
	Store              types.Store
	syslogSender       *syslog.Sender

	localStorageEnabled   atomic.Bool
	localStoragePath      string
	localStorageMaxSizeMB int
	localStorageMaxFiles  int

	syslogEnabled  atomic.Bool
	syslogServer   string
	syslogProtocol string
	syslogFacility string
	syslogTag      string
}

func New(statusRecorder *peer.Status, wgIfaceIPNet, wgIfaceIPNetV6 netip.Prefix) *Logger {
	logger := &Logger{
		statusRecorder: statusRecorder,
		wgIfaceNet:     wgIfaceIPNet,
		Store:          store.NewFileStore("", 100, 10),
		wgIfaceNetV6:   wgIfaceIPNetV6,
	}
	if wgIfaceIPNet.IsValid() {
		logger.wgIfaceIP = wgIfaceIPNet.Addr()
	}
	if wgIfaceIPNetV6.IsValid() {
		logger.wgIfaceIPv6 = wgIfaceIPNetV6.Addr()
	}
	return logger
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
		if current, ok := l.Store.(*store.File); !ok || !current.Matches(l.localStoragePath, maxSize, maxFiles) {
			l.replaceStore(store.NewFileStore(l.localStoragePath, maxSize, maxFiles))
		}
	} else {
		if current, ok := l.Store.(*store.File); !ok || !current.Matches("", 100, 10) {
			l.replaceStore(store.NewFileStore("", 100, 10))
		}
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

func (l *Logger) replaceStore(next types.Store) {
	if next == nil || l.Store == next {
		return
	}

	if l.Store != nil {
		for _, event := range l.Store.GetEvents() {
			next.StoreEvent(event)
		}
		l.Store.Close()
	}

	l.Store = next
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
			var srcRoute peer.RouteLookupResult
			var destRoute peer.RouteLookupResult

			if !l.isOverlayIP(event.SourceIP) {
				srcRoute = l.statusRecorder.CheckRoutesDetailed(event.SourceIP)
				event.SourceResourceID = []byte(srcRoute.ResourceID)
				isSrcExitNode = srcRoute.IsExitNode
			}

			if !l.isOverlayIP(event.DestIP) {
				destRoute = l.statusRecorder.CheckRoutesDetailed(event.DestIP)
				event.DestResourceID = []byte(destRoute.ResourceID)
				isDestExitNode = destRoute.IsExitNode
			}

			if l.shouldStore(&event, srcRoute, destRoute, isSrcExitNode || isDestExitNode) {
				l.Store.StoreEvent(&event)

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

func (l *Logger) isOverlayIP(ip netip.Addr) bool {
	return l.wgIfaceNet.Contains(ip) || (l.wgIfaceNetV6.IsValid() && l.wgIfaceNetV6.Contains(ip))
}

func (l *Logger) shouldStore(event *types.Event, srcRoute, destRoute peer.RouteLookupResult, isExitNode bool) bool {
	if isDNSEvent(&event.EventFields) {
		return l.dnsCollection.Load() && !isNoiseAddress(event.SourceIP) && !isNoiseAddress(event.DestIP)
	}

	// check dns collection
	if !l.dnsCollection.Load() {
		if event.DNSInfo != nil {
			return false
		}
		if (event.Protocol == types.UDP || event.Protocol == types.TCP) &&
			(event.DestPort == 53 || event.DestPort == dns.ForwarderClientPort || event.DestPort == dns.ForwarderServerPort) {
			return false
		}
	}

	// check exit node collection
	if !l.exitNodeCollection.Load() && isExitNode {
		return false
	}

	return l.isZeroTrustFlow(event, srcRoute, destRoute)
}

func (l *Logger) isZeroTrustFlow(event *types.Event, srcRoute, destRoute peer.RouteLookupResult) bool {
	if event.SourceIP == event.DestIP {
		return false
	}
	if isNoiseAddress(event.SourceIP) || isNoiseAddress(event.DestIP) {
		return false
	}

	sourceOverlay := l.isOverlayIP(event.SourceIP)
	destOverlay := l.isOverlayIP(event.DestIP)
	sourceResource := srcRoute.ResourceID != ""
	destResource := destRoute.ResourceID != ""

	if sourceOverlay && destOverlay {
		return true
	}
	if sourceOverlay && destResource {
		if destRoute.Kind == peer.RouteLookupLocal && l.isLocalOverlayIP(event.SourceIP) {
			return false
		}
		return destRoute.Kind == peer.RouteLookupLocal || destRoute.Kind == peer.RouteLookupRemote || destRoute.Kind == peer.RouteLookupResolved
	}
	if destOverlay && sourceResource {
		return srcRoute.Kind == peer.RouteLookupLocal
	}
	return false
}

func (l *Logger) isLocalOverlayIP(ip netip.Addr) bool {
	return (l.wgIfaceIP.IsValid() && ip == l.wgIfaceIP) || (l.wgIfaceIPv6.IsValid() && ip == l.wgIfaceIPv6)
}

func isNoiseAddress(ip netip.Addr) bool {
	return !ip.IsValid() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLoopback() || ip.IsInterfaceLocalMulticast()
}

func isDNSEvent(event *types.EventFields) bool {
	if event.DNSInfo != nil {
		return true
	}
	if event.Protocol != types.UDP && event.Protocol != types.TCP {
		return false
	}
	return event.DestPort == 53 ||
		event.SourcePort == 53 ||
		event.DestPort == dns.ForwarderClientPort ||
		event.SourcePort == dns.ForwarderClientPort ||
		event.DestPort == dns.ForwarderServerPort ||
		event.SourcePort == dns.ForwarderServerPort
}
