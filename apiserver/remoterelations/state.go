// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/juju/state"
)

// RemoteRelationState provides the subset of global state required by the
// remote relations facade.
type RemoteRelationsState interface {
	// KeyRelation returns the existing relation with the given key (which can
	// be derived unambiguously from the relation's endpoints).
	KeyRelation(string) (Relation, error)

	// Relation returns the existing relation with the given id.
	Relation(int) (Relation, error)

	// RemoteService returns a remote service by name.
	RemoteService(string) (RemoteService, error)

	// WatchRemoteServices returns a StringsWatcher that notifies of changes to
	// the lifecycles of the remote services in the environment.
	WatchRemoteServices() state.StringsWatcher

	// WatchRemoteServiceRelations returns a StringsWatcher that notifies of
	// changes to the lifecycles of relations involving the specified remote
	// service.
	WatchRemoteServiceRelations(serviceName string) (state.StringsWatcher, error)
}

// Relation provides access a relation in global state.
type Relation interface {
	// Destroy ensures that the relation will be removed at some point; if
	// no units are currently in scope, it will be removed immediately.
	Destroy() error

	// Id returns the integer internal relation key.
	Id() int

	// Life returns the relation's current life state.
	Life() state.Life

	// RemoteUnit returns a RelationUnit for the remote service unit
	// with the supplied ID.
	RemoteUnit(unitId string) (RelationUnit, error)

	// Unit returns a RelationUnit for the unit with the supplied ID.
	Unit(unitId string) (RelationUnit, error)

	// WatchCounterpartEndpointUnits returns a watcher that notifies of
	// changes to the units with the endpoint counterpart to the specified
	// service.
	WatchCounterpartEndpointUnits(serviceName string) (state.RelationUnitsWatcher, error)
}

// RelationUnit provides access to the settings of a single unit in a relation,
// and methods for modifying the unit's involvement in the relation.
type RelationUnit interface {
	EnterScope(settings map[string]interface{}) error
	InScope() (bool, error)
	LeaveScope() error
	ReplaceSettings(map[string]interface{}) error
	Settings() (map[string]interface{}, error)
}

// RemoteService represents the state of a service hosted in an external
// (remote) environment.
type RemoteService interface {
	Name() string
	URL() string
}

type stateShim struct {
	*state.State
}

func (st stateShim) KeyRelation(key string) (Relation, error) {
	r, err := st.State.KeyRelation(key)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return relationShim{r, st.State}, nil
}

func (st stateShim) Relation(id int) (Relation, error) {
	r, err := st.State.Relation(id)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return relationShim{r, st.State}, nil
}

func (st stateShim) RemoteService(name string) (RemoteService, error) {
	s, err := st.State.RemoteService(name)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return remoteServiceShim{s}, nil
}

func (st stateShim) WatchRemoteServiceRelations(serviceName string) (state.StringsWatcher, error) {
	s, err := st.State.RemoteService(serviceName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return s.WatchRelations(), nil
}

type relationShim struct {
	*state.Relation
	st *state.State
}

func (r relationShim) RemoteUnit(unitId string) (RelationUnit, error) {
	ru, err := r.Relation.RemoteUnit(unitId)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return relationUnitShim{ru}, nil
}

func (r relationShim) Unit(unitId string) (RelationUnit, error) {
	unit, err := r.st.Unit(unitId)
	if err != nil {
		return nil, errors.Trace(err)
	}
	ru, err := r.Relation.Unit(unit)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return relationUnitShim{ru}, nil
}

type relationUnitShim struct {
	*state.RelationUnit
}

func (r relationUnitShim) ReplaceSettings(s map[string]interface{}) error {
	settings, err := r.RelationUnit.Settings()
	if err != nil {
		return errors.Trace(err)
	}
	settings.Update(s)
	for _, key := range settings.Keys() {
		if _, ok := s[key]; ok {
			continue
		}
		settings.Delete(key)
	}
	_, err = settings.Write()
	return errors.Trace(err)
}

func (r relationUnitShim) Settings() (map[string]interface{}, error) {
	settings, err := r.RelationUnit.Settings()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return settings.Map(), nil
}

type remoteServiceShim struct {
	*state.RemoteService
}
