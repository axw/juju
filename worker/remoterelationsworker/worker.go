// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package remoterelationsworker provides a worker that manages the exchange
// of relation settings between environments.
package remoterelationsworker

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"launchpad.net/tomb"

	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state/multiwatcher"
	statewatcher "github.com/juju/juju/state/watcher"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.remoterelationsworker")

// Config encapsulates the configuration for the worker.
type Config struct {
	RemoteServicesAccessor RemoteServicesAccessor
	ServiceChangePublisher ServiceChangePublisher
}

func (cfg Config) Validate() error {
	if cfg.RemoteServicesAccessor == nil {
		return errors.NotValidf("nil RemoteServicesAccessor")
	}
	if cfg.ServiceChangePublisher == nil {
		return errors.NotValidf("nil ServiceChangePublisher")
	}
	return nil
}

// RemoteServicesAccessor is an interface that provides a means of watching
// the lifecycle states of remote services known to the local environment.
type RemoteServicesAccessor interface {
	// ExportEntities allocates unique, remote entity IDs for the
	// given entities in the local environment.
	ExportEntities([]names.Tag) ([]params.RemoteEntityIdResult, error)

	// PublishLocalRelationChange publishes local relation changes to the
	// environment hosting the remote service involved in the relation.
	PublishLocalRelationChange(params.RemoteRelationChange) error

	// RelationUnitSettings returns the relation unit settings for the
	// given relation units in the local environment.
	RelationUnitSettings([]params.RelationUnit) ([]params.SettingsResult, error)

	// RemoteRelations returns information about the cross-model relations
	// with the specified keys in the local environment.
	RemoteRelations(keys []string) ([]params.RemoteRelationResult, error)

	// RemoteServices returns the current state of the remote services with
	// the specified names in the local environment.
	RemoteServices(names []string) ([]params.RemoteServiceResult, error)

	// WatchRemoteServices watches for addition, removal and lifecycle
	// changes to remote services known to the local environment.
	WatchRemoteServices() (watcher.StringsWatcher, error)

	// WatchServiceRelations watches for changes to relations in the
	// local environment involving the service with the given name.
	WatchServiceRelations(service string) (watcher.StringsWatcher, error)

	// WatchLocalRelationUnits watches for changes to units of the
	// local service involved in the relation with the specified relation
	// key.
	WatchLocalRelationUnits(relationKey string) (watcher.RelationUnitsWatcher, error)
}

// ServiceChangePublisher is an interface that provides a means of publishing
// changes to cross-model relations,and the local services involved in them,
// to the other side of the relation.
type ServiceChangePublisher interface {
	// PublishServiceChange publishes changes to a local service that is
	// involved in one or more cross-model relations.
	PublishServiceChange(params.RemoteServiceChange) error
}

type publishFunc func(params.RemoteRelationChange) error

func NewWorker(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Annotate(err, "validating config")
	}
	worker := worker.NewStringsWorker(&remoteServicesHandler{
		config:  config,
		workers: make(map[string]worker.Worker),
	})
	return worker, nil
}

type remoteServicesHandler struct {
	config  Config
	workers map[string]worker.Worker
}

func (h *remoteServicesHandler) SetUp() (watcher.StringsWatcher, error) {
	return h.config.RemoteServicesAccessor.WatchRemoteServices()
}

func (h *remoteServicesHandler) TearDown() error {
	for _, w := range h.workers {
		w.Kill()
	}
	var firstErr error
	var firstErrServiceId string
	var errCount int
	for serviceId, w := range h.workers {
		err := w.Wait()
		if err != nil {
			if firstErr == nil {
				firstErr = err
				firstErrServiceId = serviceId
			}
			errCount++
		}
	}
	if firstErr != nil {
		return errors.Annotatef(
			firstErr, "stopping relations watcher for remote service %q (%d more error(s))",
			firstErrServiceId, errCount-1,
		)
	}
	return nil
}

func (h *remoteServicesHandler) Handle(serviceIds []string) error {
	// Fetch the current state of each of the remote services that have changed.
	results, err := h.config.RemoteServicesAccessor.RemoteServices(serviceIds)
	if err != nil {
		return errors.Annotate(err, "querying remote services")
	}

	// TODO(axw) bulk methods for starting watchers?
	for i, result := range results {
		name := serviceIds[i]
		if result.Error != nil {
			if params.IsCodeNotFound(result.Error) {
				w, ok := h.workers[name]
				if ok {
					w.Kill()
					if err := w.Wait(); err != nil {
						return errors.Annotate(err, "stopping worker")
					}
				}
				continue
			}
			return errors.Annotatef(err, "querying remote service %q", name)
		}
		if _, ok := h.workers[name]; ok {
			// We don't react to service Dying/Dead at the moment.
			// If the worker is running, then there's nothing left
			// to do.
			continue
		}
		relationsWatcher, err := h.config.RemoteServicesAccessor.WatchServiceRelations(name)
		if errors.IsNotFound(err) {
			w, ok := h.workers[name]
			if ok {
				w.Kill()
				if err := w.Wait(); err != nil {
					return errors.Annotate(err, "stopping worker")
				}
			}
			continue
		} else if err != nil {
			return errors.Annotatef(err, "watching relations for remote service %q", name)
		}
		h.workers[name] = newRemoteServiceWorker(
			relationsWatcher,
			h.config.RemoteServicesAccessor.ExportEntities,
			h.config.RemoteServicesAccessor.RemoteRelations,
			h.config.RemoteServicesAccessor.WatchLocalRelationUnits,
			h.config.RemoteServicesAccessor.RelationUnitSettings,
			h.config.RemoteServicesAccessor.PublishLocalRelationChange,
		)
	}
	return nil
}

type remoteServiceWorker struct {
	tomb                    tomb.Tomb
	relationsWatcher        watcher.StringsWatcher
	exportEntities          func([]names.Tag) ([]params.RemoteEntityIdResult, error)
	remoteRelations         func([]string) ([]params.RemoteRelationResult, error)
	watchLocalRelationUnits func(string) (watcher.RelationUnitsWatcher, error)
	relationUnitSettings    func([]params.RelationUnit) ([]params.SettingsResult, error)
	publish                 publishFunc
	relationUnitsChanges    chan relationUnitsChange
}

func newRemoteServiceWorker(
	relationsWatcher watcher.StringsWatcher,
	exportEntities func([]names.Tag) ([]params.RemoteEntityIdResult, error),
	remoteRelations func([]string) ([]params.RemoteRelationResult, error),
	watchLocalRelationUnits func(string) (watcher.RelationUnitsWatcher, error),
	relationUnitSettings func([]params.RelationUnit) ([]params.SettingsResult, error),
	publish publishFunc,
) worker.Worker {
	worker := &remoteServiceWorker{
		relationsWatcher:        relationsWatcher,
		exportEntities:          exportEntities,
		remoteRelations:         remoteRelations,
		watchLocalRelationUnits: watchLocalRelationUnits,
		relationUnitSettings:    relationUnitSettings,
		publish:                 publish,
		relationUnitsChanges:    make(chan relationUnitsChange, 1),
	}
	go func() {
		defer worker.tomb.Done()
		defer close(worker.relationUnitsChanges)
		defer statewatcher.Stop(worker.relationsWatcher, &worker.tomb)
		worker.tomb.Kill(worker.loop())
	}()
	return worker
}

func (w *remoteServiceWorker) Kill() {
	w.tomb.Kill(nil)
}

func (w *remoteServiceWorker) Wait() error {
	return w.tomb.Wait()
}

func (w *remoteServiceWorker) loop() error {
	relations := make(map[string]*relation)
	defer func() {
		for _, r := range relations {
			statewatcher.Stop(r.ruw, &w.tomb)
		}
	}()

	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case change, ok := <-w.relationsWatcher.Changes():
			if !ok {
				return statewatcher.EnsureErr(w.relationsWatcher)
			}
			// TODO(axw) change this to fetch the *local* relation
			// state, and then export the relation IDs separately..
			results, err := w.remoteRelations(change)
			if err != nil {
				return errors.Annotate(err, "querying relations")
			}
			for i, result := range results {
				key := change[i]
				if err := w.relationChanged(key, result, relations); err != nil {
					if err == tomb.ErrDying {
						return err
					}
					return errors.Annotatef(err, "handling change for relation %q", key)
				}
			}

		case change := <-w.relationUnitsChanges:
			r := relations[change.relationTag.Id()]
			r.ChangedUnits = change.changed
			r.DepartedUnits = change.departed
			if err := w.publish(r.RemoteRelationChange); err != nil {
				return errors.Annotate(err, "publishing change to offering environment")
			}
			r.Initial = false
		}
	}
}

func (w *remoteServiceWorker) relationChanged(
	key string, result params.RemoteRelationResult, relations map[string]*relation,
) error {
	if result.Error != nil {
		if params.IsCodeNotFound(result.Error) {
			// TODO(axw) we need to guarantee that the relation has
			// unregistered with the remote side before it is
			// removed from state. We probably need to stop
			// automatically removing relations on destruction,
			// and have this worker responsible for removing them
			// once they're Dead and unregistered.
			relation, ok := relations[key]
			if ok {
				if err := relation.ruw.Stop(); err != nil {
					return err
				}
				delete(relations, key)
			}
			return nil
		}
		return result.Error
	}

	if r := relations[key]; r != nil {
		r.Life = result.Result.Life
		if r.Life == params.Dead {
			if r != nil {
				r.Life = result.Result.Life
				if err := r.ruw.Stop(); err != nil {
					return err
				}
				r.ruw = nil
			}
		}
		return nil
	}

	if result.Result.Life != params.Dead {
		r := &relation{}
		lruw, err := w.watchLocalRelationUnits(key)
		if err != nil {
			return errors.Trace(err)
		}
		ruw := newRelationUnitsWatcher(
			names.NewRelationTag(key),
			lruw,
			w.exportEntities,
			w.relationUnitSettings,
			w.relationUnitsChanges,
		)
		r.Id = result.Result.Id
		r.Life = result.Result.Life
		r.Initial = true
		r.ruw = ruw
		relations[key] = r
	}
	return nil
}

type relation struct {
	params.RemoteRelationChange
	ruw *relationUnitsWatcher
}

// relationUnitsWatcher wraps a watcher.RelationUnitsWatcher to convert
// changes for inclusion in params.RemoteRelationChanges, exporting
// relation units to remote environments.
type relationUnitsWatcher struct {
	tomb                 tomb.Tomb
	relationTag          names.RelationTag
	ruw                  watcher.RelationUnitsWatcher
	remoteIds            map[string]params.RemoteEntityId
	exportEntities       func([]names.Tag) ([]params.RemoteEntityIdResult, error)
	relationUnitSettings func([]params.RelationUnit) ([]params.SettingsResult, error)
	out                  chan<- relationUnitsChange
}

func newRelationUnitsWatcher(
	relationTag names.RelationTag,
	ruw watcher.RelationUnitsWatcher,
	exportEntities func([]names.Tag) ([]params.RemoteEntityIdResult, error),
	relationUnitSettings func([]params.RelationUnit) ([]params.SettingsResult, error),
	out chan<- relationUnitsChange,
) *relationUnitsWatcher {
	w := &relationUnitsWatcher{
		relationTag:          relationTag,
		ruw:                  ruw,
		remoteIds:            make(map[string]params.RemoteEntityId),
		exportEntities:       exportEntities,
		relationUnitSettings: relationUnitSettings,
		out:                  make(chan relationUnitsChange, 1),
	}
	go func() {
		defer w.tomb.Done()
		defer statewatcher.Stop(ruw, &w.tomb)
		w.tomb.Kill(w.loop())
	}()
	return w
}

func (w *relationUnitsWatcher) Stop() error {
	w.tomb.Kill(nil)
	return w.tomb.Wait()
}

func (w *relationUnitsWatcher) Err() error {
	return w.tomb.Err()
}

func (w *relationUnitsWatcher) loop() error {
	var out chan<- relationUnitsChange
	value := relationUnitsChange{relationTag: w.relationTag}
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case change, ok := <-w.ruw.Changes():
			if !ok {
				return statewatcher.EnsureErr(w.ruw)
			}
			if err := w.updateRelationUnitsChange(change, &value); err != nil {
				return errors.Trace(err)
			}
			out = w.out
		case out <- value:
			out = nil
			value = relationUnitsChange{relationTag: w.relationTag}
		}
	}
}

func (w *relationUnitsWatcher) updateRelationUnitsChange(
	change multiwatcher.RelationUnitsChange,
	value *relationUnitsChange,
) error {
	if len(change.Changed)+len(change.Departed) == 0 {
		return nil
	}
	changedNames := make([]string, len(change.Changed))
	for name := range change.Changed {
		changedNames = append(changedNames, name)
	}
	remoteIds, err := w.exportUnits(append(changedNames, change.Departed...))
	if err != nil {
		return errors.Annotate(err, "exporting units")
	}
	for i := range change.Departed {
		remoteId := remoteIds[len(changedNames)+i]
		for i, change := range value.changed {
			if change.Id == remoteId {
				value.changed = append(
					value.changed[:i], value.changed[i+1:]...,
				)
				break
			}
		}
		value.departed = append(value.departed, remoteId)
	}
	if len(change.Changed) > 0 {
		relationUnits := make([]params.RelationUnit, len(change.Changed))
		for i, changedName := range changedNames {
			relationUnits[i] = params.RelationUnit{
				Relation: w.relationTag.String(),
				Unit:     changedName,
			}
		}
		results, err := w.relationUnitSettings(relationUnits)
		if err != nil {
			return errors.Annotate(err, "fetching relation units settings")
		}
		for _, result := range results {
			if result.Error != nil {
				return errors.Annotate(err, "fetching relation unit settings")
			}
		}
		for i, result := range results {
			remoteId := remoteIds[i]
			var found bool
			for i, change := range value.changed {
				if change.Id == remoteId {
					change.Settings = result.Settings
					value.changed[i] = change
					found = true
					break
				}
			}
			if !found {
				change := params.RemoteRelationUnitChange{
					remoteId, result.Settings,
				}
				value.changed = append(value.changed, change)
			}
		}
	}
	return nil
}

// TODO(axw) ensure we don't error if two relation units watchers
// (different relations) both attempt to export a unit concurrently.
func (w *relationUnitsWatcher) exportUnits(unitNames []string) ([]params.RemoteEntityId, error) {
	var unexported []names.Tag
	for _, name := range unitNames {
		if _, ok := w.remoteIds[name]; !ok {
			unexported = append(unexported, names.NewUnitTag(name))
		}
	}
	if len(unexported) > 0 {
		results, err := w.exportEntities(unexported)
		if err != nil {
			return nil, errors.Annotate(err, "exporting units")
		}
		for i, result := range results {
			if result.Error != nil {
				return nil, errors.Annotatef(err, "exporting unit %q", unexported[i].Id())
			}
			w.remoteIds[unexported[i].Id()] = *result.Result
		}
	}
	results := make([]params.RemoteEntityId, len(unitNames))
	for i, name := range unitNames {
		results[i] = w.remoteIds[name]
	}
	return results, nil
}

type relationUnitsChange struct {
	relationTag names.RelationTag
	changed     []params.RemoteRelationUnitChange
	departed    []params.RemoteEntityId
}
