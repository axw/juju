// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package lease

import (
	"time"

	"launchpad.net/tomb"

	"github.com/juju/errors"
	"github.com/juju/loggo"
)

const (
	// There are no blocking calls, so this can be long. We just don't
	// want goroutines to hang around indefinitely, so notifications
	// will time out after this value.
	notificationTimeout = 1 * time.Minute

	// This is a useful thing to know in several contexts.
	maxDuration = time.Duration(1<<63 - 1)
)

var (
	LeaseClaimDeniedErr = errors.New("lease claim denied")
	NotLeaseOwnerErr    = errors.Unauthorizedf("caller did not own lease for namespace")
	logger              = loggo.GetLogger("juju.lease")
)

type leasePersistor interface {
	WriteToken(string, Token) error
	RemoveToken(id string) error
	PersistedTokens() ([]Token, error)
}

func NewManager(persistor leasePersistor, stop <-chan struct{}) *leaseManager {
	m := &leaseManager{
		leasePersistor:   persistor,
		claimLease:       make(chan claimLeaseMsg),
		releaseLease:     make(chan releaseLeaseMsg),
		leaseReleasedSub: make(chan leaseReleasedMsg),
		copyOfTokens:     make(chan copyTokensMsg),
	}
	go func() {
		defer m.tomb.Done()
		m.tomb.Kill(m.workerLoop(stop))
	}()
	return m
}

func (m *leaseManager) Kill() {
	m.tomb.Kill(nil)
}

func (m *leaseManager) Wait() error {
	return m.tomb.Wait()
}

// Token represents a lease claim.
type Token struct {
	Namespace, Id string
	Expiration    time.Time
}

//
// Messages for channels.
//

type claimLeaseMsg struct {
	Token    Token
	Response chan<- Token
}
type releaseLeaseMsg struct {
	Token    Token
	Response chan<- error
}
type leaseReleasedMsg struct {
	Watcher      chan<- struct{}
	ForNamespace string
}
type copyTokensMsg struct {
	Response chan<- []Token
}

type leaseManager struct {
	tomb             tomb.Tomb
	leasePersistor   leasePersistor
	claimLease       chan claimLeaseMsg
	releaseLease     chan releaseLeaseMsg
	leaseReleasedSub chan leaseReleasedMsg
	copyOfTokens     chan copyTokensMsg
}

// CopyOfLeaseTokens returns a copy of the lease tokens currently held
// by the manager.
func (m *leaseManager) CopyOfLeaseTokens() []Token {
	ch := make(chan []Token)
	for {
		select {
		case <-m.tomb.Dying():
			return nil
		case m.copyOfTokens <- copyTokensMsg{ch}:
		case result := <-ch:
			return result
		}
	}
}

// RetrieveLease returns the lease token currently stored for the
// given namespace.
func (m *leaseManager) RetrieveLease(namespace string) Token {
	for _, tok := range m.CopyOfLeaseTokens() {
		if tok.Namespace != namespace {
			continue
		}
		return tok
	}
	return Token{}
}

// Claimlease claims a lease for the given duration for the given
// namespace and id. If the lease is already owned, a
// LeaseClaimDeniedErr will be returned. Either way the current lease
// owner's ID will be returned.
func (m *leaseManager) ClaimLease(namespace, id string, forDur time.Duration) (leaseOwnerId string, err error) {

	ch := make(chan Token)
	token := Token{namespace, id, time.Now().Add(forDur)}
	message := claimLeaseMsg{token, ch}
	for {
		select {
		case <-m.tomb.Dying():
			return "", tomb.ErrDying
		case m.claimLease <- message:
		case activeClaim := <-ch:
			leaseOwnerId = activeClaim.Id
			if id != leaseOwnerId {
				err = LeaseClaimDeniedErr
			}
			return leaseOwnerId, err
		}
	}
}

// ReleaseLease releases the lease held for namespace by id.
func (m *leaseManager) ReleaseLease(namespace, id string) (err error) {

	ch := make(chan error)
	token := Token{Namespace: namespace, Id: id}
	message := releaseLeaseMsg{token, ch}
	for {
		select {
		case <-m.tomb.Dying():
			return tomb.ErrDying
		case m.releaseLease <- message:
		case err = <-ch:
			break
		}
	}

	if err != nil {
		err = errors.Annotatef(err, `could not release lease for namespace %q, id %q`, namespace, id)

		// Log errors so that we're aware they're happening, but don't
		// burden the caller with dealing with an error if it's
		// essential a no-op.
		if errors.IsUnauthorized(err) {
			logger.Warningf(err.Error())
			return nil
		}
		return err
	}

	return nil
}

// LeaseReleasedNotifier returns a channel a caller can block on to be
// notified of when a lease is released for namespace. This channel is
// reusable, but will be closed if it does not respond within
// "notificationTimeout".
func (m *leaseManager) LeaseReleasedNotifier(namespace string) (notifier <-chan struct{}, err error) {
	watcher := make(chan struct{})
	select {
	case <-m.tomb.Dying():
		close(watcher)
		return nil, tomb.ErrDying
	case m.leaseReleasedSub <- leaseReleasedMsg{watcher, namespace}:
	}
	return watcher, nil
}

// workerLoop serializes all requests into a single thread.
func (m *leaseManager) workerLoop(stop <-chan struct{}) error {
	// TODO(fwereade): this method never returns any errors after it's
	// entered the loop. This is bad; it may poison its cache and continue
	// to operate, serving unhelpful results.

	// These data-structures are local to ensure they're only utilized
	// within this thread-safe context.

	releaseSubs := make(map[string][]chan<- struct{}, 0)

	// Pull everything off our data-store & check for expirations.
	leaseCache, err := populateTokenCache(m.leasePersistor)
	if err != nil {
		return err
	}
	nextExpiration := m.expireLeases(leaseCache, releaseSubs)

	for {
		select {
		case <-stop:
			return nil
		case claim := <-m.claimLease:
			lease := claimLease(leaseCache, claim.Token)
			if lease.Id == claim.Token.Id {
				// TODO(fwereade): we should *definitely* not be ignoring this error.
				m.leasePersistor.WriteToken(lease.Namespace, lease)
				if lease.Expiration.Before(nextExpiration) {
					nextExpiration = lease.Expiration
				}
			}
			claim.Response <- lease
		case release := <-m.releaseLease:
			// Unwind our layers from most volatile to least.
			err := releaseLease(leaseCache, release.Token)
			if err == nil {
				namespace := release.Token.Namespace
				err = m.leasePersistor.RemoveToken(namespace)
				// TODO(fwereade): if the above error is non-nil, we should
				// not be continuing as if nothing had happened.
				notifyOfRelease(releaseSubs[namespace], namespace)
			}
			release.Response <- err
		case subscription := <-m.leaseReleasedSub:
			subscribe(releaseSubs, subscription)
		case msg := <-m.copyOfTokens:
			// create a copy of the lease cache for use by code
			// external to our thread-safe context.
			msg.Response <- copyTokens(leaseCache)
		case <-time.After(nextExpiration.Sub(time.Now())):
			nextExpiration = m.expireLeases(leaseCache, releaseSubs)
		}
	}
}

func (m *leaseManager) expireLeases(
	cache map[string]Token,
	subscribers map[string][]chan<- struct{},
) time.Time {

	// Having just looped through all the leases we're holding, we can
	// inform the caller of when the next expiration will occur.
	nextExpiration := time.Now().Add(maxDuration)

	for _, token := range cache {

		if token.Expiration.After(time.Now()) {
			// For the tokens that aren't expiring yet, find the
			// minimum time we should wait before cleaning up again.
			if nextExpiration.After(token.Expiration) {
				nextExpiration = token.Expiration
				logger.Debugf("Setting next expiration to %s", nextExpiration)
			}
			continue
		}

		logger.Infof(`Lease for namespace %q has expired.`, token.Namespace)
		if err := releaseLease(cache, token); err != nil {
			// TODO(fwereade): we should certainly be returning the error and
			// killing the main loop.
			logger.Errorf("Failed to release expired lease for namespace %q: %v", token.Namespace, err)
		} else {
			notifyOfRelease(subscribers[token.Namespace], token.Namespace)
		}
	}

	return nextExpiration
}

func copyTokens(cache map[string]Token) (copy []Token) {
	for _, t := range cache {
		copy = append(copy, t)
	}
	return copy
}

func claimLease(cache map[string]Token, claim Token) Token {
	if active, ok := cache[claim.Namespace]; ok && active.Id != claim.Id {
		return active
	}
	cache[claim.Namespace] = claim
	logger.Infof(`%q obtained lease for %q`, claim.Id, claim.Namespace)
	return claim
}

func releaseLease(cache map[string]Token, claim Token) error {
	if active, ok := cache[claim.Namespace]; !ok || active.Id != claim.Id {
		return NotLeaseOwnerErr
	}
	delete(cache, claim.Namespace)
	logger.Infof(`%q released lease for namespace %q`, claim.Id, claim.Namespace)
	return nil
}

func subscribe(subMap map[string][]chan<- struct{}, subscription leaseReleasedMsg) {
	subList := subMap[subscription.ForNamespace]
	subList = append(subList, subscription.Watcher)
	subMap[subscription.ForNamespace] = subList
}

func notifyOfRelease(subscribers []chan<- struct{}, namespace string) {
	logger.Infof(`Notifying namespace %q subscribers that its lease has been released.`, namespace)
	for _, subscriber := range subscribers {
		// Spin off into go-routine so we don't rely on listeners to
		// not block.
		go func(subscriber chan<- struct{}) {
			select {
			case subscriber <- struct{}{}:
			case <-time.After(notificationTimeout):
				// TODO(kate): Remove this bad-citizen from the
				// notifier's list.
				logger.Warningf("A notification timed out after %s.", notificationTimeout)
			}
		}(subscriber)
	}
}

func populateTokenCache(persistor leasePersistor) (map[string]Token, error) {

	tokens, err := persistor.PersistedTokens()
	if err != nil {
		return nil, err
	}

	cache := make(map[string]Token)
	for _, tok := range tokens {
		cache[tok.Namespace] = tok
	}

	return cache, nil
}
