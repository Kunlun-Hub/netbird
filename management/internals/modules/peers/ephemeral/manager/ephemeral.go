package manager

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/netbirdio/netbird/management/internals/modules/peers"
	"github.com/netbirdio/netbird/management/internals/modules/peers/ephemeral"
	"github.com/netbirdio/netbird/management/server/activity"
	nbpeer "github.com/netbirdio/netbird/management/server/peer"

	"github.com/netbirdio/netbird/management/server/store"
)

const (
	// cleanupWindow is the time window to wait after nearest peer deadline to start the cleanup procedure.
	cleanupWindow = 1 * time.Minute
)

var (
	timeNow = time.Now
)

type ephemeralPeer struct {
	id        string
	accountID string
	deadline  time.Time
	force     bool
	next      *ephemeralPeer
}

// todo: consider to remove peer from ephemeral list when the peer has been deleted via API. If we do not do it
// in worst case we will get invalid error message in this manager.

// EphemeralManager keep a list of ephemeral peers. After EphemeralLifeTime inactivity the peer will be deleted
// automatically. Inactivity means the peer disconnected from the Management server.
type EphemeralManager struct {
	store        store.Store
	peersManager peers.Manager

	headPeer  *ephemeralPeer
	tailPeer  *ephemeralPeer
	peersLock sync.Mutex
	timer     *time.Timer

	lifeTime      time.Duration
	cleanupWindow time.Duration
}

// NewEphemeralManager instantiate new EphemeralManager
func NewEphemeralManager(store store.Store, peersManager peers.Manager) *EphemeralManager {
	return &EphemeralManager{
		store:        store,
		peersManager: peersManager,

		lifeTime:      ephemeral.EphemeralLifeTime,
		cleanupWindow: cleanupWindow,
	}
}

// LoadInitialPeers load from the database the ephemeral type of peers and schedule a cleanup procedure to the head
// of the linked list (to the most deprecated peer). At the end of cleanup it schedules the next cleanup to the new
// head.
func (e *EphemeralManager) LoadInitialPeers(ctx context.Context) {
	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	e.loadEphemeralPeers(ctx)
	if e.headPeer != nil {
		e.scheduleCleanup(ctx)
	}
}

// Stop timer
func (e *EphemeralManager) Stop() {
	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	if e.timer != nil {
		e.timer.Stop()
	}
}

// OnPeerConnected remove regular ephemeral peers from the linked list because they should only be deleted after
// inactivity. Browser temporary-access peers keep their absolute deadline and are deleted even while connected.
func (e *EphemeralManager) OnPeerConnected(ctx context.Context, peer *nbpeer.Peer) {
	if !peer.Ephemeral {
		return
	}

	if isBrowserTemporaryPeer(peer) {
		log.WithContext(ctx).Tracef("keep browser temporary peer on ephemeral list: %s", peer.ID)
		e.peersLock.Lock()
		defer e.peersLock.Unlock()
		e.trackPeer(peer, e.absoluteDeadline(peer), true)
		e.scheduleCleanup(ctx)
		return
	}

	log.WithContext(ctx).Tracef("remove peer from ephemeral list: %s", peer.ID)

	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	e.removePeer(peer.ID)

	// stop the unnecessary timer
	if e.headPeer == nil && e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	} else {
		e.scheduleCleanup(ctx)
	}
}

// OnPeerDisconnected add the peer to the linked list of ephemeral peers. Because of the peer
// is inactive it will be deleted after the EphemeralLifeTime period.
func (e *EphemeralManager) OnPeerDisconnected(ctx context.Context, peer *nbpeer.Peer) {
	if !peer.Ephemeral {
		return
	}

	log.WithContext(ctx).Tracef("add peer to ephemeral list: %s", peer.ID)

	e.peersLock.Lock()
	defer e.peersLock.Unlock()

	deadline := e.disconnectDeadline()
	force := false
	if isBrowserTemporaryPeer(peer) {
		deadline = e.absoluteDeadline(peer)
		force = true
	}

	e.trackPeer(peer, deadline, force)
	e.scheduleCleanup(ctx)
}

func (e *EphemeralManager) loadEphemeralPeers(ctx context.Context) {
	peers, err := e.store.GetAllEphemeralPeers(ctx, store.LockingStrengthNone)
	if err != nil {
		log.WithContext(ctx).Debugf("failed to load ephemeral peers: %s", err)
		return
	}

	for _, p := range peers {
		if isBrowserTemporaryPeer(p) {
			e.addPeer(p.AccountID, p.ID, e.absoluteDeadline(p), true)
			continue
		}
		e.addPeer(p.AccountID, p.ID, e.disconnectDeadline(), false)
	}

	log.WithContext(ctx).Debugf("loaded ephemeral peer(s): %d", len(peers))
}

func (e *EphemeralManager) cleanup(ctx context.Context) {
	log.Tracef("on ephemeral cleanup")
	deletePeers := make(map[string]*ephemeralPeer)

	e.peersLock.Lock()
	now := timeNow()
	for p := e.headPeer; p != nil; p = p.next {
		if now.Before(p.deadline) {
			break
		}

		deletePeers[p.id] = p
		e.headPeer = p.next
		if p.next == nil {
			e.tailPeer = nil
		}
	}

	e.scheduleCleanup(ctx)

	e.peersLock.Unlock()

	regularPeerIDsPerAccount := make(map[string][]string)
	for id, p := range deletePeers {
		if p.force {
			continue
		}
		regularPeerIDsPerAccount[p.accountID] = append(regularPeerIDsPerAccount[p.accountID], id)
	}

	for accountID, peerIDs := range regularPeerIDsPerAccount {
		log.WithContext(ctx).Tracef("cleanup: deleting %d ephemeral peers for account %s", len(peerIDs), accountID)
		err := e.peersManager.DeletePeers(ctx, accountID, peerIDs, activity.SystemInitiator, true)
		if err != nil {
			log.WithContext(ctx).Errorf("failed to delete ephemeral peers: %s", err)
		}
	}

	forcePeerIDsPerAccount := make(map[string][]string)
	for _, p := range deletePeers {
		if !p.force {
			continue
		}
		forcePeerIDsPerAccount[p.accountID] = append(forcePeerIDsPerAccount[p.accountID], p.id)
	}
	for accountID, peerIDs := range forcePeerIDsPerAccount {
		log.WithContext(ctx).Tracef("cleanup: force deleting %d browser temporary peers for account %s", len(peerIDs), accountID)
		err := e.peersManager.DeletePeers(ctx, accountID, peerIDs, activity.SystemInitiator, false)
		if err != nil {
			log.WithContext(ctx).Errorf("failed to force delete browser temporary peers: %s", err)
		}
	}
}

func (e *EphemeralManager) trackPeer(peer *nbpeer.Peer, deadline time.Time, force bool) {
	if existing := e.findPeer(peer.ID); existing != nil {
		force = force || existing.force
		if existing.deadline.Before(deadline) {
			deadline = existing.deadline
		}
		e.removePeer(peer.ID)
	}
	e.addPeer(peer.AccountID, peer.ID, deadline, force)
}

func (e *EphemeralManager) scheduleCleanup(ctx context.Context) {
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if e.headPeer == nil {
		return
	}

	delay := e.headPeer.deadline.Sub(timeNow()) + e.cleanupWindow
	if delay < 0 {
		delay = 0
	}
	e.timer = time.AfterFunc(delay, func() {
		e.cleanup(ctx)
	})
}

func (e *EphemeralManager) addPeer(accountID string, peerID string, deadline time.Time, force bool) {
	ep := &ephemeralPeer{
		id:        peerID,
		accountID: accountID,
		deadline:  deadline,
		force:     force,
	}

	if e.headPeer == nil {
		e.headPeer = ep
		e.tailPeer = ep
		return
	}

	if deadline.Before(e.headPeer.deadline) {
		ep.next = e.headPeer
		e.headPeer = ep
		return
	}

	for p := e.headPeer; p != nil; p = p.next {
		if p.next == nil {
			p.next = ep
			e.tailPeer = ep
			return
		}
		if deadline.Before(p.next.deadline) {
			ep.next = p.next
			p.next = ep
			return
		}
	}
}

func (e *EphemeralManager) removePeer(id string) {
	if e.headPeer == nil {
		return
	}

	if e.headPeer.id == id {
		e.headPeer = e.headPeer.next
		if e.tailPeer.id == id {
			e.tailPeer = nil
		}
		return
	}

	for p := e.headPeer; p.next != nil; p = p.next {
		if p.next.id == id {
			// if we remove the last element from the chain then set the last-1 as tail
			if e.tailPeer.id == id {
				e.tailPeer = p
			}
			p.next = p.next.next
			return
		}
	}
}

func (e *EphemeralManager) findPeer(id string) *ephemeralPeer {
	for p := e.headPeer; p != nil; p = p.next {
		if p.id == id {
			return p
		}
	}
	return nil
}

func (e *EphemeralManager) disconnectDeadline() time.Time {
	return timeNow().Add(e.lifeTime)
}

func (e *EphemeralManager) absoluteDeadline(peer *nbpeer.Peer) time.Time {
	if peer.CreatedAt.IsZero() {
		return e.disconnectDeadline()
	}
	return peer.CreatedAt.Add(e.lifeTime)
}

func isBrowserTemporaryPeer(peer *nbpeer.Peer) bool {
	return peer.Meta.GoOS == "js" && peer.Meta.KernelVersion == "wasm"
}
