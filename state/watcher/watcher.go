// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// The watcher package provides an interface for observing changes
// to arbitrary MongoDB documents that are maintained via the
// mgo/txn transaction package.
package watcher

import (
	"container/list"
	"fmt"
	"strings"
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"gopkg.in/juju/worker.v1"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/tomb.v1"

	"github.com/juju/juju/mongo"
	jworker "github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.state.watcher")

// A Watcher can watch any number of collections and documents for changes.
type Watcher struct {
	tomb         tomb.Tomb
	iteratorFunc func() mongo.Iterator
	log          *mgo.Collection

	collections []*collectionInfo

	// request is used to deliver requests from the public API into
	// the the goroutine loop.
	request chan interface{}
}

type collectionInfo struct {
	name       string
	version    int64
	documentsL *list.List
	documentsM map[interface{}]*documentInfo
	watches    []*collWatchInfo
}

type documentInfo struct {
	e       *list.Element
	id      interface{}
	version int64
	// revno holds the current txn-revno values for the document.
	// Documents that have been deleted will have revno -1.
	revno   int64
	watches []docWatchInfo
}

// A Change holds information about a document change.
type Change struct {
	// C and Id hold the collection name and document _id field value.
	C  string
	Id interface{}

	// Revno is the latest known value for the document's txn-revno
	// field, or -1 if the document was deleted.
	Revno int64
}

type watchKey struct {
	c  string
	id interface{} // nil when watching collection
}

func (k watchKey) String() string {
	coll := "collection " + k.c
	if k.id == nil {
		return coll
	}
	return fmt.Sprintf("document %v in %s", k.id, coll)
}

// match returns whether the receiving watch key,
// which may refer to a particular item or
// an entire collection, matches k1, which refers
// to a particular item.
func (k watchKey) match(k1 watchKey) bool {
	if k.c != k1.c {
		return false
	}
	if k.id == nil {
		// k refers to entire collection
		return true
	}
	return k.id == k1.id
}

type docWatchInfo struct {
	ch    chan<- int64
	revno int64
}

type collWatchInfo struct {
	ch      chan<- Change
	next    *list.Element
	version int64
	waiting bool
}

// Period is the delay between each sync.
// It must not be changed when any watchers are active.
var Period time.Duration = 5 * time.Second

// New returns a new Watcher observing the changelog collection,
// which must be a capped collection maintained by mgo/txn.
func New(changelog *mgo.Collection) *Watcher {
	return newWatcher(changelog, nil)
}

func newWatcher(changelog *mgo.Collection, iteratorFunc func() mongo.Iterator) *Watcher {
	w := &Watcher{
		log:          changelog,
		iteratorFunc: iteratorFunc,
		request:      make(chan interface{}),
	}
	if w.iteratorFunc == nil {
		w.iteratorFunc = w.iter
	}
	go func() {
		err := w.loop()
		cause := errors.Cause(err)
		// tomb expects ErrDying or ErrStillAlive as
		// exact values, so we need to log and unwrap
		// the error first.
		if err != nil && cause != tomb.ErrDying {
			logger.Infof("watcher loop failed: %v", err)
		}
		w.tomb.Kill(cause)
		w.tomb.Done()
	}()
	return w
}

// NewDead returns a new watcher that is already dead
// and always returns the given error from its Err method.
func NewDead(err error) *Watcher {
	var w Watcher
	w.tomb.Kill(errors.Trace(err))
	w.tomb.Done()
	return &w
}

// Kill is part of the worker.Worker interface.
func (w *Watcher) Kill() {
	w.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *Watcher) Wait() error {
	return w.tomb.Wait()
}

// Stop stops all the watcher activities.
func (w *Watcher) Stop() error {
	return worker.Stop(w)
}

// Dead returns a channel that is closed when the watcher has stopped.
func (w *Watcher) Dead() <-chan struct{} {
	return w.tomb.Dead()
}

// Err returns the error with which the watcher stopped.
// It returns nil if the watcher stopped cleanly, tomb.ErrStillAlive
// if the watcher is still running properly, or the respective error
// if the watcher is terminating or has terminated with an error.
func (w *Watcher) Err() error {
	return w.tomb.Err()
}

type reqWatchColl struct {
	c    string
	info *collWatchInfo
}

type reqWatchCollNext struct {
	c    string
	info *collWatchInfo
}

type reqUnwatchColl struct {
	c    string
	info *collWatchInfo
}

type reqWatchDoc struct {
	key  watchKey
	info docWatchInfo
}

// Watch starts watching the given collection and document id.
// An event will be sent onto ch whenever a matching document's txn-revno
// field is observed to change after a transaction is applied. The revno
// parameter holds the currently known revision number for the document.
// Non-existent documents are represented by a -1 revno.
func (w *Watcher) Watch(collection string, id interface{}, revno int64, ch chan<- Change) worker.Worker {
	if id == nil {
		panic("watcher: cannot watch a document with nil id")
	}
	dcw := &docChangeWorker{w: w, key: watchKey{collection, id}, out: ch}
	go func() {
		defer dcw.tomb.Done()
		dcw.tomb.Kill(dcw.loop(revno))
	}()
	return dcw
}

type docChangeWorker struct {
	tomb tomb.Tomb
	w    *Watcher
	key  watchKey
	out  chan<- Change
}

func (w *docChangeWorker) loop(initRevno int64) error {
	resp := make(chan int64, 1)
	req := reqWatchDoc{w.key, docWatchInfo{resp, initRevno}}
	reqch := w.w.request
	change := Change{C: w.key.c, Id: w.key.id}
	var out chan<- Change
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case <-w.w.tomb.Dying():
			return errors.New("Watcher was stopped")
		case reqch <- req:
			reqch = nil
		case revno := <-resp:
			req.info.revno = revno
			change.Revno = revno
			out = w.out
		case out <- change:
			out = nil
			reqch = w.w.request
		}
	}
}

// Kill is part of the worker.Worker interface.
func (w *docChangeWorker) Kill() {
	w.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *docChangeWorker) Wait() error {
	return w.tomb.Wait()
}

// WatchCollection starts watching the given collection.
// An event will be sent onto ch whenever the txn-revno field is observed
// to change after a transaction is applied for any document in the collection.
func (w *Watcher) WatchCollection(collection string, ch chan<- Change) worker.Worker {
	return w.WatchCollectionWithFilter(collection, ch, nil)
}

// WatchCollectionWithFilter starts watching the given collection.
// An event will be sent onto ch whenever the txn-revno field is observed
// to change after a transaction is applied for any document in the collection, so long as the
// specified filter function returns true when called with the document id value.
func (w *Watcher) WatchCollectionWithFilter(collection string, ch chan<- Change, filter func(interface{}) bool) worker.Worker {
	ccw := &collChangeWorker{w: w, c: collection, filter: filter, out: ch}
	go func() {
		defer ccw.tomb.Done()
		ccw.tomb.Kill(ccw.loop())
	}()
	return ccw
}

type collChangeWorker struct {
	tomb   tomb.Tomb
	w      *Watcher
	c      string
	filter func(interface{}) bool
	out    chan<- Change
}

func (w *collChangeWorker) loop() error {
	// Algorithm:
	//  1. register collection watcher
	//  2. until dying, request changes
	//  3. unregister collection watcher

	// Register the watcher.
	respch := make(chan Change, 1)
	defer close(respch)
	info := &collWatchInfo{ch: respch, version: -1}
	select {
	case <-w.tomb.Dying():
		return tomb.ErrDying
	case <-w.w.tomb.Dying():
		return errors.New("Watcher was stopped")
	case w.w.request <- reqWatchColl{w.c, info}:
	}

	filter := w.filter
	if filter == nil {
		filter = func(interface{}) bool {
			return true
		}
	}

	// Request changes until this worker or the watcher is stopped.
	reqch := w.w.request
	var change Change
	var out chan<- Change
	for {
		select {
		case <-w.tomb.Dying():
			// Unregister the watcher.
			select {
			case w.w.request <- reqUnwatchColl{w.c, info}:
			case <-w.w.tomb.Dying():
				return errors.New("Watcher was stopped")
			}
			return tomb.ErrDying
		case <-w.w.tomb.Dying():
			return errors.New("Watcher was stopped")
		case reqch <- reqWatchCollNext{w.c, info}:
			reqch = nil
		case change = <-respch:
			if !filter(change.Id) {
				reqch = w.w.request
				continue
			}
			out = w.out
		case out <- change:
			out = nil
			reqch = w.w.request
		}
	}
}

// Kill is part of the worker.Worker interface.
func (w *collChangeWorker) Kill() {
	w.tomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *collChangeWorker) Wait() error {
	return w.tomb.Wait()
}

// StartSync forces the watcher to load new events from the database.
func (w *Watcher) StartSync() {
	// TODO(axw) get rid of this
}

// loop implements the main watcher loop.
func (w *Watcher) loop() error {
	changes := make(chan Change)
	syncDone := make(chan struct{})
	syncAbort := make(chan struct{})
	syncErr := make(chan error, 1)
	go func() {
		defer close(syncDone)
		syncErr <- w.syncLoop(changes, syncAbort)
	}()
	defer func() {
		close(syncAbort)
		<-syncDone
	}()

	for {
		select {
		case <-w.tomb.Dying():
			return errors.Trace(tomb.ErrDying)
		case req := <-w.request:
			w.handle(req)
		case change := <-changes:
			w.handleChange(change)
		case err := <-syncErr:
			// If the txn log collection overflows from underneath us,
			// the easiest cause of action to recover is to cause the
			// agen tto restart.
			if errors.Cause(err) == cappedPositionLostError {
				// Ideally we'd not import the worker package but that's
				// where all the errors are defined.
				return jworker.ErrRestartAgent
			}
			return errors.Trace(err)
		}
	}
}

func (w *Watcher) handleChange(change Change) {
	collInfo, docInfo := w.document(change.C, change.Id)
	if docInfo.revno == change.Revno {
		// Non-mutating txns do not increment revno.
		return
	}
	collInfo.version++
	docInfo.version = collInfo.version
	docInfo.revno = change.Revno
	if docInfo.e == nil {
		docInfo.e = collInfo.documentsL.PushBack(docInfo)
	} else if next := docInfo.e.Next(); next != nil {
		// The document has been updated, so
		// move it to the back and redirect
		// any collection watchers that were
		// going to report it next.
		for _, info := range collInfo.watches {
			if info.next == docInfo.e {
				info.next = next
			}
		}
		collInfo.documentsL.MoveToBack(docInfo.e)
	}
	for _, info := range collInfo.watches {
		if info.waiting {
			info.waiting = false
			info.version = docInfo.version
			info.ch <- change
		}
	}
	for i := 0; i < len(docInfo.watches); i++ {
		dw := docInfo.watches[i]
		if change.Revno > dw.revno || change.Revno == -1 && dw.revno >= 0 {
			fmt.Println("~~~", docInfo.id, change.Id)
			docInfo.watches[i].ch <- change.Revno
			docInfo.watches[i] = docInfo.watches[len(docInfo.watches)-1]
			docInfo.watches = docInfo.watches[:len(docInfo.watches)-1]
		}
	}
}

// handle deals with requests delivered by the public API
// onto the background watcher goroutine.
func (w *Watcher) handle(req interface{}) {
	logger.Tracef("got request: %#v", req)
	switch r := req.(type) {
	case reqWatchColl:
		collInfo := w.collection(r.c)
		r.info.version = collInfo.version
		collInfo.watches = append(collInfo.watches, r.info)
	case reqWatchCollNext:
		collInfo := w.collection(r.c)
		next := r.info.next
		if next == nil {
			next = collInfo.documentsL.Back()
			if next == nil || next.Value.(*documentInfo).version <= r.info.version {
				r.info.waiting = true
				return
			}
			prev := next.Prev()
			for prev != nil && prev.Value.(*documentInfo).version > r.info.version {
				next = prev
				prev = prev.Prev()
			}
		}
		docInfo := next.Value.(*documentInfo)
		r.info.next = next.Next()
		r.info.version = docInfo.version
		r.info.waiting = false
		r.info.ch <- Change{C: r.c, Id: docInfo.id, Revno: docInfo.revno}
	case reqUnwatchColl:
		collInfo := w.collection(r.c)
		for i, info := range collInfo.watches {
			if info == r.info {
				collInfo.watches[i] = collInfo.watches[len(collInfo.watches)-1]
				collInfo.watches = collInfo.watches[:len(collInfo.watches)-1]
				break
			}
		}
	case reqWatchDoc:
		_, docInfo := w.document(r.key.c, r.key.id)
		revno := docInfo.revno
		if revno > r.info.revno || revno == -1 && r.info.revno >= 0 {
			r.info.ch <- revno
		} else {
			docInfo.watches = append(docInfo.watches, r.info)
		}
	default:
		panic(fmt.Errorf("unknown request: %T", req))
	}
}

func (w *Watcher) collection(name string) *collectionInfo {
	for _, c := range w.collections {
		if c.name == name {
			return c
		}
	}
	c := &collectionInfo{
		name:       name,
		documentsL: list.New(),
		documentsM: make(map[interface{}]*documentInfo),
	}
	w.collections = append(w.collections, c)
	return c
}

func (w *Watcher) document(collection string, id interface{}) (*collectionInfo, *documentInfo) {
	coll := w.collection(collection)
	if d, ok := coll.documentsM[id]; ok {
		return coll, d
	}
	d := &documentInfo{
		id:    id,
		revno: -2, // -2 means never observed
	}
	coll.documentsM[id] = d
	return coll, d
}

func (w *Watcher) iter() mongo.Iterator {
	return w.log.Find(nil).Tail(-1)
}

var cappedPositionLostError = errors.New("capped position lost")

// sync updates the watcher knowledge from the database, and
// queues events to observing channels.
func (w *Watcher) syncLoop(changes chan<- Change, abort <-chan struct{}) error {
	newIter := func() mongo.Iterator {
		iter := w.iteratorFunc()
		go func() {
			<-abort
			iter.Close()
		}()
		return iter
	}
	iter := newIter()
	var entry bson.D
mainloop:
	for {
		if !iter.Next(&entry) {
			if iter.Err() == nil {
				// This happens when the collection is empty to
				// begin with, even though we tail with no
				// timeout. The only solution is to sleep and
				// restart the iterator until the collection is
				// non-empty.
				iter.Close()
				select {
				case <-abort:
					return errors.New("aborted")
				case <-time.After(10 * time.Millisecond):
				}
				iter = newIter()
				continue
			}
			// An error occurred.
			break
		}
		if len(entry) == 0 {
			logger.Tracef("got empty changelog document")
			continue
		}
		logger.Tracef("got changelog document: %#v", entry)
		for _, c := range entry[1:] {
			// See txn's Runner.ChangeLog for the structure of log entries.
			var d, r []interface{}
			dr, _ := c.Value.(bson.D)
			for _, item := range dr {
				switch item.Name {
				case "d":
					d, _ = item.Value.([]interface{})
				case "r":
					r, _ = item.Value.([]interface{})
				}
			}
			if len(d) == 0 || len(d) != len(r) {
				logger.Warningf("changelog has invalid collection document: %#v", c)
				continue
			}
			for i, id := range d {
				revno, ok := r[i].(int64)
				if !ok {
					logger.Warningf("changelog has revno with type %T: %#v", r[i], r[i])
					continue
				}
				if revno < 0 {
					revno = -1
				}
				select {
				case <-abort:
					break mainloop
				case changes <- Change{C: c.Name, Id: id, Revno: revno}:
				}
			}
		}
	}
	if err := iter.Close(); err != nil {
		if qerr, ok := err.(*mgo.QueryError); ok {
			// CappedPositionLost is code 136.
			// Just in case that changes for some reason, we'll also check the error message.
			if qerr.Code == 136 || strings.Contains(qerr.Message, "CappedPositionLost") {
				logger.Warningf("watcher iterator failed due to txn log collection overflow")
				err = cappedPositionLostError
			}
		}
		return errors.Annotate(err, "watcher iteration error")
	}
	return nil
}
