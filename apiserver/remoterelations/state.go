// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/model/crossmodel"
	"github.com/juju/juju/state"
)

// RemoteRelationStateCloser extends RemoteRelationsState with a Close method.
type RemoteRelationsStateCloser interface {
	RemoteRelationsState
	Close() error
}

// RemoteRelationState provides the subset of global state required by the
// remote relations facade.
type RemoteRelationsState interface {
	// TODO
	EnvironUUID() string

	// TODO
	ExportLocalEntity(entity names.Tag) (string, error)

	// ForEnviron returns a RemoteRelationsState for the specified
	// environment.
	ForEnviron(names.EnvironTag) (RemoteRelationsStateCloser, error)

	// KeyRelation returns the existing relation with the given key (which can
	// be derived unambiguously from the relation's endpoints).
	KeyRelation(string) (Relation, error)

	// Relation returns the existing relation with the given id.
	Relation(int) (Relation, error)

	// RemoteService returns a remote service by name.
	RemoteService(string) (RemoteService, error)

	// Service returns a local service by name.
	Service(string) (Service, error)

	// ServiceDirectory returns the local service directory for the
	// controller.
	ServiceDirectory() crossmodel.ServiceDirectory

	// OfferedServices returns the offered services for the environment.
	OfferedServices() crossmodel.OfferedServices

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

	// Endpoints returns the endpoints that constitute the relation.
	Endpoints() []state.Endpoint

	// Id returns the integer internal relation key.
	Id() int

	// Life returns the relation's current life state.
	Life() state.Life

	// RemoteUnit returns a RelationUnit for the remote service unit
	// with the supplied ID.
	RemoteUnit(unitId string) (RelationUnit, error)

	// Unit returns a RelationUnit for the unit with the supplied ID.
	Unit(unitId string) (RelationUnit, error)

	// WatchEndpointUnits returns a watcher that notifies of changes to
	// the units of the specified service in the relation.
	WatchUnits(serviceName string) (state.RelationUnitsWatcher, error)
}

// RelationUnit provides access to the settings of a single unit in a relation,
// and methods for modifying the unit's involvement in the relation.
type RelationUnit interface {
	// EnterScope ensures that the unit has entered its scope in the
	// relation. When the unit has already entered its scope, EnterScope
	// will report success but make no changes to state.
	EnterScope(settings map[string]interface{}) error

	// InScope returns whether the relation unit has entered scope and
	// not left it.
	InScope() (bool, error)

	// LeaveScope signals that the unit has left its scope in the relation.
	// After the unit has left its relation scope, it is no longer a member
	// of the relation; if the relation is dying when its last member unit
	// leaves, it is removed immediately. It is not an error to leave a
	// scope that the unit is not, or never was, a member of.
	LeaveScope() error

	// ReplaceSettings replaces the relation unit's settings within the
	// relation.
	ReplaceSettings(map[string]interface{}) error

	// Settings returns the relation unit's settings within the relation.
	Settings() (map[string]interface{}, error)
}

// RemoteService represents the state of a service hosted in an external
// (remote) environment.
type RemoteService interface {
	Life() state.Life

	// Destroy ensures that the service and all its relations will be
	// removed at some point; if no relation involving the service has
	// any units in scope, they are all removed immediately.
	Destroy() error

	// Name returns the name of the remote service.
	Name() string

	// URL returns the remote service URL, at which it is offered.
	URL() (string, bool)
}

// Service represents the state of a service hosted in the local environment.
type Service interface {
	// Life returns the lifecycle state of the service.
	Life() state.Life

	// Watch returns a NotifyWatcher that notifies of changes to
	// the service.
	Watch() state.NotifyWatcher

	// WatchRelations returns a StringsWatcher that notifies of changes to
	// the lifecycles of relations involving the service.
	WatchRelations() state.StringsWatcher
}

type stateShim struct {
	*state.State
}

func (st stateShim) ExportLocalEntity(entity names.Tag) (string, error) {
	r := st.State.RemoteEntities()
	return r.ExportLocalEntity(entity)
}

func (st stateShim) ForEnviron(tag names.EnvironTag) (RemoteRelationsStateCloser, error) {
	other, err := st.State.ForEnviron(tag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return stateShim{other}, nil
}

func (st stateShim) KeyRelation(key string) (Relation, error) {
	r, err := st.State.KeyRelation(key)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return relationShim{r, st.State}, nil
}

func (st stateShim) OfferedServices() crossmodel.OfferedServices {
	return state.NewOfferedServices(st.State)
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

func (st stateShim) Service(name string) (Service, error) {
	s, err := st.State.Service(name)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return serviceShim{s}, nil
}

func (st stateShim) ServiceDirectory() crossmodel.ServiceDirectory {
	return state.NewServiceDirectory(st.State)
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

type serviceShim struct {
	*state.Service
}
