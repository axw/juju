// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/utils/set"
	"launchpad.net/tomb"

	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/model/crossmodel"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/multiwatcher"
	"github.com/juju/juju/state/watcher"
)

// watchRemoteServiceLocalController returns a service watcher that receives
// changes made to the remote service with the specified URL.
//
// TODO(axw) this should be extracted into an interface, so we can later drop
// in an alternative for non-local URLs.
func (api *RemoteRelationsAPI) watchRemoteServiceLocalController(
	serviceURL string,
) (_ *remoteServiceWatcher, resultErr error) {
	// Look up the offer in the service directory so we can identify the UUID
	// of the offering environment.
	//
	// TODO(axw) ideally we'd use crossmodel.ServiceOfferForURL, but the API it
	// expects does not match ServiceDirectory. Why does the API client have a
	// different interface to crossmodel.ServiceDirectory?
	localServiceDirectory := api.st.ServiceDirectory()
	serviceOffers, err := localServiceDirectory.ListOffers(crossmodel.ServiceOfferFilter{
		ServiceOffer: crossmodel.ServiceOffer{ServiceURL: serviceURL},
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	if len(serviceOffers) == 0 {
		return nil, errors.NotFoundf("service offer for %q", serviceURL)
	}
	otherState, err := api.st.ForEnviron(names.NewEnvironTag(
		serviceOffers[0].SourceEnvUUID,
	))
	if err != nil {
		return nil, errors.Trace(err)
	}
	defer func() {
		if resultErr == nil {
			// otherState is now owned by the returned
			// watcher, which will take care of closing.
			return
		}
		defer otherState.Close()
	}()

	// The offered service may have a different name to what it's called in
	// the consuming environment, and in the directory. The offer records
	// the service's name in the remote environment.
	offeredServices, err := otherState.OfferedServices().ListOffers(
		crossmodel.OfferedServiceFilter{
			ServiceURL: serviceURL,
		},
	)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if len(offeredServices) == 0 {
		return nil, errors.NotFoundf("offered service for %q", serviceURL)
	}
	serviceName := offeredServices[0].ServiceName
	service, err := otherState.Service(serviceName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	remoteServiceWatcher := service.Watch()
	relationsWatcher := service.WatchRelations()
	rw := newRemoteRelationsWatcher(otherState, serviceName, false, relationsWatcher)
	return newRemoteServiceWatcher(otherState, serviceName, serviceURL, remoteServiceWatcher, rw), nil
}

type remoteServiceWatcher struct {
	tomb                 tomb.Tomb
	st                   RemoteRelationsStateCloser
	serviceName          string
	serviceURL           string
	remoteServiceWatcher state.NotifyWatcher
	relationsWatcher     *remoteRelationsWatcher
	out                  chan params.RemoteServiceChange
}

func newRemoteServiceWatcher(
	st RemoteRelationsStateCloser,
	serviceName string,
	serviceURL string,
	sw state.NotifyWatcher,
	rw *remoteRelationsWatcher,
) *remoteServiceWatcher {
	w := &remoteServiceWatcher{
		st:                   st,
		serviceName:          serviceName,
		serviceURL:           serviceURL,
		remoteServiceWatcher: sw,
		relationsWatcher:     rw,
		out:                  make(chan params.RemoteServiceChange),
	}
	go func() {
		defer w.tomb.Done()
		defer close(w.out)
		defer func() { w.tomb.Kill(w.st.Close()) }()
		defer watcher.Stop(sw, &w.tomb)
		defer watcher.Stop(rw, &w.tomb)
		w.tomb.Kill(w.loop())
	}()
	return w
}

func (w *remoteServiceWatcher) loop() error {
	var out chan<- params.RemoteServiceChange
	value := params.RemoteServiceChange{
		ServiceURL: w.serviceURL,
	}
	var seenServiceChange, seenRelationsChange bool
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying

		case _, ok := <-w.remoteServiceWatcher.Changes():
			if !ok {
				return watcher.EnsureErr(w.remoteServiceWatcher)
			}
			seenServiceChange = true
			var life params.Life
			service, err := w.st.Service(w.serviceName)
			if errors.IsNotFound(err) {
				// Service has been removed. Just say it's
				// Dead, because we don't have any other
				// way of saying it's removed. The consumer
				// will still remove it after destroying.
				life = params.Dead
			} else if err != nil {
				return errors.Trace(err)
			} else {
				life = params.Life(service.Life().String())
			}
			var changed bool
			if life != value.Life {
				value.Life = life
				changed = true
			}
			// Only send changes when there is a service change
			// that we are interested in.
			if changed && seenRelationsChange {
				out = w.out
			}

		case change, ok := <-w.relationsWatcher.Changes():
			if !ok {
				return watcher.EnsureErr(w.relationsWatcher)
			}
			seenRelationsChange = true
			value.Relations = change
			if seenServiceChange {
				out = w.out
			}

		case out <- value:
			out = nil
			value = params.RemoteServiceChange{
				ServiceURL: value.ServiceURL,
				Life:       value.Life,
			}
		}
	}
}

func (w *remoteServiceWatcher) Changes() <-chan params.RemoteServiceChange {
	return w.out
}

func (w *remoteServiceWatcher) Err() error {
	return w.tomb.Err()
}

func (w *remoteServiceWatcher) Stop() error {
	w.tomb.Kill(nil)
	return w.tomb.Wait()
}

// remoteRelationsWatcher watches the relations of a service, and the
// units for each of those relations. Depending on configuration, the
// reported units will be for the service specified, or for the
// counterpart service.
type remoteRelationsWatcher struct {
	tomb                  tomb.Tomb
	st                    RemoteRelationsState
	serviceName           string
	counterpartUnits      bool
	relationsWatcher      state.StringsWatcher
	relationUnitsChanges  chan relationUnitsChange
	relationUnitsWatchers map[string]*relationWatcher
	relations             map[string]relationInfo
	out                   chan params.RemoteRelationsChange
}

func newRemoteRelationsWatcher(
	st RemoteRelationsState,
	serviceName string,
	counterpartUnits bool,
	rw state.StringsWatcher,
) *remoteRelationsWatcher {
	w := &remoteRelationsWatcher{
		st:                    st,
		serviceName:           serviceName,
		counterpartUnits:      counterpartUnits,
		relationsWatcher:      rw,
		relationUnitsChanges:  make(chan relationUnitsChange),
		relationUnitsWatchers: make(map[string]*relationWatcher),
		relations:             make(map[string]relationInfo),
		out:                   make(chan params.RemoteRelationsChange),
	}
	go func() {
		defer w.tomb.Done()
		defer close(w.out)
		defer close(w.relationUnitsChanges)
		defer watcher.Stop(rw, &w.tomb)
		defer func() {
			for _, ruw := range w.relationUnitsWatchers {
				watcher.Stop(ruw, &w.tomb)
			}
		}()
		w.tomb.Kill(w.loop())
	}()
	return w
}

func (w *remoteRelationsWatcher) loop() error {
	var out chan<- params.RemoteRelationsChange
	var value params.RemoteRelationsChange
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying

		case change, ok := <-w.relationsWatcher.Changes():
			if !ok {
				return watcher.EnsureErr(w.relationsWatcher)
			}
			for _, relationKey := range change {
				relation, err := w.st.KeyRelation(relationKey)
				if errors.IsNotFound(err) {
					r, ok := w.relations[relationKey]
					if !ok {
						// Relation was not previously known, so
						// don't report it as removed.
						continue
					}
					delete(w.relations, relationKey)
					relationId := r.relationId

					// Relation has been removed, so stop and remove its
					// relation units watcher, and then add the relation
					// ID to the removed relations list.
					watcher, ok := w.relationUnitsWatchers[relationKey]
					if ok {
						if err := watcher.Stop(); err != nil {
							return errors.Trace(err)
						}
						delete(w.relationUnitsWatchers, relationKey)
					}
					value.RemovedRelations = append(
						value.RemovedRelations, relationId,
					)
					continue
				} else if err != nil {
					return errors.Trace(err)
				}

				relationId := relation.Id()
				relationChange, _ := getRelationChange(&value, relationId)
				relationChange.Life = params.Life(relation.Life().String())
				w.relations[relationKey] = relationInfo{relationId, relationChange.Life}
				if _, ok := w.relationUnitsWatchers[relationKey]; !ok {
					// Start a relation units watcher, wait for the initial
					// value before informing the client of the relation.
					var ruw state.RelationUnitsWatcher
					if w.counterpartUnits {
						ruw, err = relation.WatchCounterpartEndpointUnits(w.serviceName)
					} else {
						ruw, err = relation.WatchUnits(w.serviceName)
					}
					if err != nil {
						return errors.Trace(err)
					}
					var knownUnits set.Strings
					select {
					case <-w.tomb.Dying():
						return tomb.ErrDying
					case change, ok := <-ruw.Changes():
						if !ok {
							return watcher.EnsureErr(ruw)
						}
						ru := relationUnitsChange{
							relationKey: relationKey,
						}
						knownUnits = make(set.Strings)
						if err := updateRelationUnits(
							w.st, relation, knownUnits, change, &ru,
						); err != nil {
							watcher.Stop(ruw, &w.tomb)
							return errors.Trace(err)
						}
						w.updateRelationUnits(ru, &value)
					}
					w.relationUnitsWatchers[relationKey] = newRelationWatcher(
						w.st, relation, relationKey, knownUnits,
						ruw, w.relationUnitsChanges,
					)
				}
			}
			out = w.out

		case change := <-w.relationUnitsChanges:
			w.updateRelationUnits(change, &value)
			out = w.out

		case out <- value:
			out = nil
			value = params.RemoteRelationsChange{}
		}
	}
}

func (w *remoteRelationsWatcher) updateRelationUnits(change relationUnitsChange, value *params.RemoteRelationsChange) {
	relationInfo, ok := w.relations[change.relationKey]
	r, ok := getRelationChange(value, relationInfo.relationId)
	if !ok {
		r.Life = relationInfo.life
	}
	if r.ChangedUnits == nil && len(change.changedUnits) > 0 {
		r.ChangedUnits = make(map[string]params.RemoteRelationUnitChange)
	}
	for unitId, unitChange := range change.changedUnits {
		r.ChangedUnits[unitId] = unitChange
	}
	if r.ChangedUnits != nil {
		for _, unitId := range change.departedUnits {
			delete(r.ChangedUnits, unitId)
		}
	}
	r.DepartedUnits = append(r.DepartedUnits, change.departedUnits...)
}

func getRelationChange(value *params.RemoteRelationsChange, relationId int) (*params.RemoteRelationChange, bool) {
	for i, r := range value.ChangedRelations {
		if r.RelationId == relationId {
			return &value.ChangedRelations[i], true
		}
	}
	value.ChangedRelations = append(
		value.ChangedRelations, params.RemoteRelationChange{RelationId: relationId},
	)
	return &value.ChangedRelations[len(value.ChangedRelations)-1], false
}

func (w *remoteRelationsWatcher) updateRelation(change params.RemoteRelationChange, value *params.RemoteRelationsChange) {
	for i, r := range value.ChangedRelations {
		if r.RelationId == change.RelationId {
			value.ChangedRelations[i] = change
			return
		}
	}
}

func (w *remoteRelationsWatcher) Changes() <-chan params.RemoteRelationsChange {
	return w.out
}

func (w *remoteRelationsWatcher) Err() error {
	return w.tomb.Err()
}

func (w *remoteRelationsWatcher) Stop() error {
	w.tomb.Kill(nil)
	return w.tomb.Wait()
}

// relationWatcher watches the counterpart endpoint units for a relation.
type relationWatcher struct {
	tomb        tomb.Tomb
	st          RemoteRelationsState
	relation    Relation
	relationKey string
	knownUnits  set.Strings
	watcher     state.RelationUnitsWatcher
	out         chan<- relationUnitsChange
}

func newRelationWatcher(
	st RemoteRelationsState,
	relation Relation,
	relationKey string,
	knownUnits set.Strings,
	ruw state.RelationUnitsWatcher,
	out chan<- relationUnitsChange,
) *relationWatcher {
	w := &relationWatcher{
		st:          st,
		relation:    relation,
		relationKey: relationKey,
		knownUnits:  knownUnits,
		watcher:     ruw,
		out:         out,
	}
	go func() {
		defer w.tomb.Done()
		defer watcher.Stop(ruw, &w.tomb)
		w.tomb.Kill(w.loop())
	}()
	return w
}

func (w *relationWatcher) loop() error {
	value := relationUnitsChange{relationKey: w.relationKey}
	var out chan<- relationUnitsChange
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying

		case change, ok := <-w.watcher.Changes():
			if !ok {
				return watcher.EnsureErr(w.watcher)
			}
			if err := w.update(change, &value); err != nil {
				return errors.Trace(err)
			}
			out = w.out

		case out <- value:
			out = nil
			value = relationUnitsChange{relationKey: w.relationKey}
		}
	}
}

func (w *relationWatcher) update(change multiwatcher.RelationUnitsChange, value *relationUnitsChange) error {
	return updateRelationUnits(w.st, w.relation, w.knownUnits, change, value)
}

// updateRelationUnits updates a relationUnitsChange structure with the a
// multiwatcher.RelationUnitsChange.
func updateRelationUnits(
	st RemoteRelationsState,
	relation Relation,
	knownUnits set.Strings,
	change multiwatcher.RelationUnitsChange,
	value *relationUnitsChange,
) error {
	if value.changedUnits == nil && len(change.Changed) > 0 {
		value.changedUnits = make(map[string]params.RemoteRelationUnitChange)
	}
	if value.changedUnits != nil {
		for _, unitId := range change.Departed {
			delete(value.changedUnits, unitId)
		}
	}
	for _, unitId := range change.Departed {
		if knownUnits == nil || !knownUnits.Contains(unitId) {
			// Unit hasn't previously been seen. This could happen
			// if the unit is removed between the watcher firing
			// when it was present and reading the unit's settings.
			continue
		}
		knownUnits.Remove(unitId)
		value.departedUnits = append(value.departedUnits, unitId)
	}

	// Fetch settings for each changed relation unit.
	for unitId := range change.Changed {
		ru, err := relation.Unit(unitId)
		if errors.IsNotFound(err) {
			// Relation unit removed between watcher firing and
			// reading the unit's settings.
			continue
		} else if err != nil {
			return errors.Trace(err)
		}
		settings, err := ru.Settings()
		if err != nil {
			return errors.Trace(err)
		}
		// TODO(axw) replace the value of settings where the value
		// is the private-address of the unit.
		value.changedUnits[unitId] = params.RemoteRelationUnitChange{settings}
		if knownUnits != nil {
			knownUnits.Add(unitId)
		}
	}
	return nil
}

func (w *relationWatcher) Stop() error {
	w.tomb.Kill(nil)
	return w.tomb.Wait()
}

func (w *relationWatcher) Err() error {
	return w.tomb.Err()
}

type relationInfo struct {
	relationId int
	life       params.Life
}

type relationUnitsChange struct {
	relationKey   string
	changedUnits  map[string]params.RemoteRelationUnitChange
	departedUnits []string
}
