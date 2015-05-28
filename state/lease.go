// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/names"
	jujutxn "github.com/juju/txn"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/lease"
)

/*
type leaseEntity struct {
	LastUpdate time.Time
	lease.Token
}

// NewLeasePersistor returns a new LeasePersistor. It should be passed
// functions it can use to run transactions and get collections.
func NewLeasePersistor(
	collectionName string,
	runTransaction func([]txn.Op) error,
	getCollection func(string) (_ stateCollection, closer func()),
) *LeasePersistor {
	return &LeasePersistor{
		collectionName: collectionName,
		runTransaction: runTransaction,
		getCollection:  getCollection,
	}
}

// LeasePersistor represents logic which can persist lease tokens to a
// data store.
type LeasePersistor struct {
	collectionName string
	runTransaction func([]txn.Op) error
	getCollection  func(string) (_ stateCollection, closer func())
}
*/

/*
// WriteToken writes the given token to the data store with the given
// ID.
func (p *LeasePersistor) WriteToken(id string, tok lease.Token) error {

	entity := leaseEntity{time.Now(), tok}

	// Write's should always overwrite anything that's there. The
	// business-logic of managing leases is handled elsewhere.
	ops := []txn.Op{
		// First remove anything that's there.
		{
			C:      p.collectionName,
			Id:     id,
			Remove: true,
		},
		// Then insert the token.
		{
			Assert: txn.DocMissing,
			C:      p.collectionName,
			Id:     id,
			Insert: entity,
		},
	}

	if err := p.runTransaction(ops); err != nil {
		return errors.Annotatef(err, `could not add token "%s" to data-store`, tok.Id)
	}

	return nil
}

// RemoveToken removes the lease token with the given ID from the data
// store.
func (p *LeasePersistor) RemoveToken(id string) error {

	ops := []txn.Op{{C: p.collectionName, Id: id, Remove: true}}
	if err := p.runTransaction(ops); err != nil {
		return errors.Annotatef(err, `could not remove token "%s"`, id)
	}

	return nil
}

// PersistedTokens retrieves all tokens currently persisted.
func (p *LeasePersistor) PersistedTokens() (tokens []lease.Token, _ error) {

	collection, closer := p.getCollection(p.collectionName)
	defer closer()

	// Pipeline entities into tokens.
	var query bson.D
	iter := collection.Find(query).Iter()
	defer iter.Close()

	var doc leaseEntity
	for iter.Next(&doc) {
		tokens = append(tokens, doc.Token)
	}

	if err := iter.Err(); err != nil {
		return nil, errors.Annotate(err, "could not retrieve all persisted tokens")
	}

	return tokens, nil
}
*/

type LeasePersistor struct {
	collectionName string
	runTransaction func([]txn.Op) error
	run            func(jujutxn.TransactionSource) error
	getCollection  func(string) (_ stateCollection, closer func())
	docID          func(string) string

	// localWriter is the tag used to write to the database.
	localWriter names.Tag

	// clockDelta contains a pessimistic clock delta for each state
	// database writer. This will initially contain only the local
	// machine, whose delta is zero.
	//
	// When a lease document for a writer without a delta is seen, the
	// lease time is compared to the current time of this machine to
	// compute the delta. The times of leases are adjusted by the delta
	// before returning to the caller. This will effectively increase
	// the lease time by the time difference between when the lease was
	// entered into state and the reader first observed it.
	clockDelta map[names.Tag]time.Duration
}

type leaseDoc struct {
	Namespace string        `bson:"_id"`
	EnvUUID   string        `bson:"env-uuid"`
	Writer    string        `bson:"writer"`
	Id        string        `bson:"id"`
	Start     time.Time     `bson:"start"`
	Duration  time.Duration `bson:"duration"`
}

func (p *LeasePersistor) ClaimLease(namespace, id string, forDur time.Duration) (owner string, err error) {
	doc := &leaseDoc{
		Namespace: namespace,
		Writer:    p.localWriter.String(),
		Id:        id,
		Duration:  forDur,
	}
	buildTxn := func(attempt int) ([]txn.Op, error) {
		doc.Start = time.Now()
		currentDoc, err := p.retrieveLease(namespace)
		if errors.IsNotFound(err) {
			return []txn.Op{{
				C:      p.collectionName,
				Id:     namespace,
				Assert: txn.DocMissing,
				Insert: doc,
			}}, nil
		} else if err != nil {
			return nil, err
		}
		currentToken, err := p.toLeaseToken(currentDoc)
		if err != nil {
			return nil, err
		}
		if currentDoc.Id != id && currentToken.Expiration.After(doc.Start) {
			owner = currentDoc.Id
			return nil, lease.LeaseClaimDeniedErr
		}
		return []txn.Op{{
			C:  p.collectionName,
			Id: namespace,
			Assert: bson.D{
				{"writer", currentDoc.Writer},
				{"id", currentDoc.Id},
				{"start", currentDoc.Start}, // unadjusted
				{"duration", currentDoc.Duration},
			},
			Update: bson.D{{"$set", bson.D{
				{"writer", doc.Writer},
				{"id", doc.Id},
				{"start", doc.Start},
				{"duration", doc.Duration},
			}}},
		}}, nil
	}
	if err := p.run(buildTxn); err != nil {
		return "", err
	}
	return id, nil
}

func (p *LeasePersistor) ReleaseLease(namespace, id string) error {
	if _, err := p.RetrieveLease(namespace); errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}
	return p.runTransaction([]txn.Op{{
		C:      p.collectionName,
		Id:     namespace,
		Assert: bson.D{{"id", id}},
		Remove: true,
	}})
}

func (p *LeasePersistor) RetrieveLease(namespace string) (lease.Token, error) {
	doc, err := p.retrieveLease(namespace)
	if err != nil {
		return lease.Token{}, err
	}
	return p.toLeaseToken(doc)
}

func (p *LeasePersistor) retrieveLease(namespace string) (*leaseDoc, error) {
	coll, closer := p.getCollection(p.collectionName)
	defer closer()

	var doc leaseDoc
	if err := coll.FindId(namespace).One(&doc); err == mgo.ErrNotFound {
		return nil, errors.NotFoundf("token for namespace %q", namespace)
	} else if err != nil {
		return nil, errors.Annotatef(err, "getting token for namespace %q", namespace)
	}
	return &doc, nil
}

func (p *LeasePersistor) toLeaseToken(doc *leaseDoc) (lease.Token, error) {
	writer, err := names.ParseTag(doc.Writer)
	if err != nil {
		return lease.Token{}, err
	}
	delta, ok := p.clockDelta[writer]
	if !ok {
		delta = time.Now().Sub(doc.Start)
		p.clockDelta[writer] = delta
	}
	return lease.Token{
		doc.Namespace,
		doc.Id,
		doc.Start.Add(delta).Add(doc.Duration),
	}, nil
}

func (st *State) WatchLease(namespace string) NotifyWatcher {
	return newEntityWatcher(st, leaseC, st.docID(namespace))
}

// TODO(axw) move to LeasePersistor
func (st *State) LeaseReleasedNotifier(namespace string) <-chan struct{} {
	w := newEntityWatcher(st, leaseC, st.docID(namespace))
	notifier := make(chan struct{}, 1)
	go func() {
		defer close(notifier)
		var delay <-chan time.Time
		for {
			select {
			// TODO tomb.Dying()
			case _, ok := <-w.Changes():
				if !ok {
					logger.Debugf("watcher closed")
					return
				}
			case <-delay:
				delay = nil
			}
			lease, err := st.RetrieveLease(namespace)
			if err != nil {
				logger.Debugf("error retrieving lease: %v", err)
				return
			}
			now := time.Now()
			if !lease.Expiration.After(now) {
				logger.Debugf("notifying caller of lease release")
				notifier <- struct{}{}
				return
			}
			logger.Debugf("waiting for lease update or until %s", lease.Expiration)
			delay = time.After(lease.Expiration.Sub(now))
		}
	}()
	return notifier
}

/*
func (p *LeasePersistor) Watch(namespace string) NotifyWatcher {
}
*/
