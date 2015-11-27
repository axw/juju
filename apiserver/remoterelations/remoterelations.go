// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"github.com/juju/utils/set"
	"launchpad.net/tomb"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/model/crossmodel"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/multiwatcher"
	"github.com/juju/juju/state/watcher"
)

var logger = loggo.GetLogger("juju.apiserver.remoterelations")

func init() {
	common.RegisterStandardFacade("RemoteRelations", 1, NewStateRemoteRelationsAPI)
}

// RemoteRelationsAPI provides access to the Provisioner API facade.
type RemoteRelationsAPI struct {
	st         RemoteRelationsState
	resources  *common.Resources
	authorizer common.Authorizer
}

// NewRemoteRelationsAPI creates a new server-side RemoteRelationsAPI facade
// backed by global state.
func NewStateRemoteRelationsAPI(
	st *state.State,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*RemoteRelationsAPI, error) {
	return NewRemoteRelationsAPI(stateShim{st}, resources, authorizer)
}

// NewRemoteRelationsAPI returns a new server-side RemoteRelationsAPI facade.
func NewRemoteRelationsAPI(
	st RemoteRelationsState,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*RemoteRelationsAPI, error) {
	if !authorizer.AuthEnvironManager() {
		return nil, common.ErrPerm
	}
	return &RemoteRelationsAPI{
		st:         st,
		resources:  resources,
		authorizer: authorizer,
	}, nil
}

// ConsumeRemoteServiceChange consumes remote changes to services into the
// local environment.
func (api *RemoteRelationsAPI) ConsumeRemoteServiceChange(
	changes params.RemoteServiceChanges,
) (params.ErrorResults, error) {
	results := params.ErrorResults{
		Results: make([]params.ErrorResult, len(changes.Changes)),
	}
	handleRemoteRelationsChange := func(change params.RemoteRelationsChange) error {
		// For any relations that have been removed on the offering
		// side, destroy them on the consuming side.
		for _, relId := range change.RemovedRelations {
			rel, err := api.st.Relation(relId)
			if errors.IsNotFound(err) {
				continue
			} else if err != nil {
				return errors.Trace(err)
			}
			if err := rel.Destroy(); err != nil {
				return errors.Trace(err)
			}
			// TODO(axw) remove remote relation units.
		}
		for _, change := range change.ChangedRelations {
			rel, err := api.st.Relation(change.RelationId)
			if err != nil {
				return errors.Trace(err)
			}
			if change.Life != params.Alive {
				if err := rel.Destroy(); err != nil {
					return errors.Trace(err)
				}
			}
			for _, unitId := range change.DepartedUnits {
				ru, err := rel.RemoteUnit(unitId)
				if err != nil {
					return errors.Trace(err)
				}
				if err := ru.LeaveScope(); err != nil {
					return errors.Trace(err)
				}
			}
			for unitId, change := range change.ChangedUnits {
				ru, err := rel.RemoteUnit(unitId)
				if err != nil {
					return errors.Trace(err)
				}
				inScope, err := ru.InScope()
				if err != nil {
					return errors.Trace(err)
				}
				if !inScope {
					err = ru.EnterScope(change.Settings)
				} else {
					err = ru.ReplaceSettings(change.Settings)
				}
				if err != nil {
					return errors.Trace(err)
				}
			}
		}
		return nil
	}
	handleServiceChange := func(change params.RemoteServiceChange) error {
		// TODO(axw) make it easy to look up a RemoteService by URL.

		serviceTag, err := names.ParseServiceTag(change.ServiceTag)
		if err != nil {
			return errors.Trace(err)
		}
		service, err := api.st.RemoteService(serviceTag.Id())
		if err != nil {
			return errors.Trace(err)
		}
		// TODO(axw) update service status.
		if change.Life != params.Alive {
			if err := service.Destroy(); err != nil {
				return errors.Trace(err)
			}
		}
		return handleRemoteRelationsChange(change.Relations)
	}
	for i, change := range changes.Changes {
		if err := handleServiceChange(change); err != nil {
			results.Results[i].Error = common.ServerError(err)
		}
	}
	return results, nil
}

// PublishLocalRelationChange publishes local relations changes to the
// remote side offering those relations.
func (api *RemoteRelationsAPI) PublishLocalRelationsChange(
	changes params.RemoteRelationsChanges,
) (params.ErrorResults, error) {
	return params.ErrorResults{}, errors.NotImplementedf("PublishLocalRelationChange")
}

// WatchRemoteServices starts a strings watcher that notifies of the addition,
// removal, and lifecycle changes of remote services in the environment; and
// returns the watcher ID and initial IDs of remote services, or an error if
// watching failed.
func (api *RemoteRelationsAPI) WatchRemoteServices() (params.StringsWatchResult, error) {
	w := api.st.WatchRemoteServices()
	if changes, ok := <-w.Changes(); ok {
		return params.StringsWatchResult{
			StringsWatcherId: api.resources.Register(w),
			Changes:          changes,
		}, nil
	}
	return params.StringsWatchResult{}, watcher.EnsureErr(w)
}

// WatchRemoteService returns a remote service watcher that delivers
// changes to the remote service in the offering environment. This
// includes status, lifecycle and relation changes.
func (api *RemoteRelationsAPI) WatchRemoteService(
	args params.Entities,
) (params.RemoteServiceWatchResults, error) {
	results := params.ServiceWatchResults{
		make([]params.ServiceWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		serviceTag, err := names.ParseServiceTag(arg.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		w, err := api.watchRemoteService(serviceTag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		change, ok := <-w.Changes()
		if !ok {
			results.Results[i].Error = common.ServerError(watcher.EnsureErr(w))
			continue
		}
		results.Results[i].ServiceWatcherId = api.resources.Register(w)
		results.Results[i].Change = &change
	}
	return results, nil
}

func (api *RemoteRelationsAPI) watchRemoteService(
	serviceTag names.ServiceTag,
) (_ *serviceWatcher, resultErr error) {
	// TODO(axw) when we want to support cross-model relations involving
	// non-local directories, we'll need to subscribe to some sort of
	// message bus which will receive the changes from remote controllers.
	// For now we only handle local.
	serviceName := serviceTag.Id()
	remoteService, err := api.st.RemoteService(serviceName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	serviceURL := remoteService.URL()
	directoryName, err := crossmodel.ServiceDirectoryForURL(serviceURL)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if directoryName != "local" {
		return nil, errors.NotSupportedf("non-local service URL %q", serviceURL)
	}

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
	serviceName = offeredServices[0].ServiceName
	service, err := otherState.Service(serviceName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	serviceWatcher := service.Watch()
	relationsWatcher := service.WatchRelations()
	rw := newRemoteRelationsWatcher(otherState, serviceName, false, relationsWatcher)
	return newServiceWatcher(otherState, serviceName, serviceWatcher, rw), nil
}

// WatchRemoteRelations starts a RemoteRelationsWatcher for each specified
// remote service, and returns the watcher IDs and initial values, or an error
// if the remote services could not be watched.
func (api *RemoteRelationsAPI) WatchRemoteRelations(
	args params.Entities,
) (params.RemoteRelationsWatchResults, error) {
	results := params.RemoteRelationsWatchResults{
		make([]params.RemoteRelationsWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		serviceTag, err := names.ParseServiceTag(arg.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		w, err := api.watchRemoteRelations(serviceTag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		changes, ok := <-w.Changes()
		if !ok {
			results.Results[i].Error = common.ServerError(watcher.EnsureErr(w))
			continue
		}
		results.Results[i].RemoteRelationsWatcherId = api.resources.Register(w)
		results.Results[i].Changes = &changes
	}
	return results, nil
}

func (api *RemoteRelationsAPI) watchRemoteRelations(serviceTag names.ServiceTag) (*remoteRelationsWatcher, error) {
	serviceName := serviceTag.Id()
	relationsWatcher, err := api.st.WatchRemoteRemoteRelations(serviceName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return newRemoteRelationsWatcher(api.st, serviceName, true, relationsWatcher), nil
}

type serviceWatcher struct {
	tomb             tomb.Tomb
	st               RemoteRelationsStateCloser
	serviceName      string
	serviceWatcher   state.NotifyWatcher
	relationsWatcher *remoteRelationsWatcher
	out              chan params.RemoteServiceChange
}

func newServiceWatcher(
	st RemoteRelationsStateCloser,
	serviceName string,
	sw state.NotifyWatcher,
	rw *remoteRelationsWatcher,
) *serviceWatcher {
	w := &serviceWatcher{
		st:               st,
		serviceName:      serviceName,
		serviceWatcher:   sw,
		relationsWatcher: rw,
		out:              make(chan params.RemoteServiceChange),
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

func (w *serviceWatcher) loop() error {
	var out chan<- params.RemoteServiceChange
	value := params.RemoteServiceChange{
		ServiceTag: names.NewServiceTag(w.serviceName).String(),
	}
	var seenServiceChange, seenRelationsChange bool
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying

		case _, ok := <-w.serviceWatcher.Changes():
			if !ok {
				return watcher.EnsureErr(w.serviceWatcher)
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
				ServiceTag: value.ServiceTag,
				Life:       value.Life,
			}
		}
	}
}

func (w *serviceWatcher) Changes() <-chan params.RemoteServiceChange {
	return w.out
}

func (w *serviceWatcher) Err() error {
	return w.tomb.Err()
}

func (w *serviceWatcher) Stop() error {
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
		r.ChangedUnits = make(map[string]params.RelationUnitChange)
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

func getRelationChange(value *params.RemoteRelationsChange, relationId int) (*params.RelationChange, bool) {
	for i, r := range value.ChangedRelations {
		if r.RelationId == relationId {
			return &value.ChangedRelations[i], true
		}
	}
	value.ChangedRelations = append(
		value.ChangedRelations, params.RelationChange{RelationId: relationId},
	)
	return &value.ChangedRelations[len(value.ChangedRelations)-1], false
}

func (w *remoteRelationsWatcher) updateRelation(change params.RelationChange, value *params.RemoteRelationsChange) {
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
		value.changedUnits = make(map[string]params.RelationUnitChange)
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
		value.changedUnits[unitId] = params.RelationUnitChange{settings}
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
