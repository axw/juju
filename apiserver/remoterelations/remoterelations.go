// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
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

func (api *RemoteRelationsAPI) ExportEntities(entities params.Entities) (params.RemoteEntityIdResults, error) {
	results := params.RemoteEntityIdResults{
		Results: make([]params.RemoteEntityIdResult, len(entities.Entities)),
	}
	for i, entity := range entities.Entities {
		tag, err := names.ParseTag(entity.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		token, err := api.st.ExportLocalEntity(tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		results.Results[i].Result = &params.RemoteEntityId{
			EnvUUID: api.st.EnvironUUID(),
			Token:   token,
		}
	}
	return results, nil
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
		/*
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
		*/
		return nil
	}
	handleServiceChange := func(change params.RemoteServiceChange) error {
		/*
			service, err := api.st.RemoteServiceByURL(change.ServiceURL)
			if err != nil {
				return errors.Trace(err)
			}
			// TODO(axw) update service status.
			if change.Life != params.Alive {
				if err := service.Destroy(); err != nil {
					return errors.Trace(err)
				}
			}
		*/
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
func (api *RemoteRelationsAPI) PublishLocalRelationChange(
	changes params.RemoteRelationChanges,
) (params.ErrorResults, error) {
	results := params.ErrorResults{
		Results: make([]params.ErrorResult, len(changes.Changes)),
	}
	for i, change := range changes.Changes {
		if err := api.publishChange(change); err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
	}
	return results, nil
}

func (api *RemoteRelationsAPI) publishChange(change params.RemoteRelationChange) error {
	logger.Debugf("publish change: %+v", change)
	// TODO(axw) actually copy changes across to target env
	return nil
}

func (api *RemoteRelationsAPI) RelationUnitSettings(relationUnits params.RelationUnits) (params.SettingsResults, error) {
	results := params.SettingsResults{
		Results: make([]params.SettingsResult, len(relationUnits.RelationUnits)),
	}
	one := func(ru params.RelationUnit) (params.Settings, error) {
		relationTag, err := names.ParseRelationTag(ru.Relation)
		if err != nil {
			return nil, errors.Trace(err)
		}
		rel, err := api.st.KeyRelation(relationTag.Id())
		if err != nil {
			return nil, errors.Trace(err)
		}
		unitTag, err := names.ParseUnitTag(ru.Unit)
		if err != nil {
			return nil, errors.Trace(err)
		}
		unit, err := rel.Unit(unitTag.Id())
		if err != nil {
			return nil, errors.Trace(err)
		}
		settings, err := unit.Settings()
		if err != nil {
			return nil, errors.Trace(err)
		}
		paramsSettings := make(params.Settings)
		for k, v := range settings {
			vString, ok := v.(string)
			if !ok {
				return nil, errors.Errorf(
					"invalid relation setting %q: expected string, got %T", k, v,
				)
			}
			paramsSettings[k] = vString
		}
		return paramsSettings, nil
	}
	for i, ru := range relationUnits.RelationUnits {
		settings, err := one(ru)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		results.Results[i].Settings = settings
	}
	return results, nil
}

func (api *RemoteRelationsAPI) RemoteRelations(entities params.Entities) (params.RemoteRelationResults, error) {
	results := params.RemoteRelationResults{
		Results: make([]params.RemoteRelationResult, len(entities.Entities)),
	}
	one := func(entity params.Entity) (*params.RemoteRelation, error) {
		tag, err := names.ParseRelationTag(entity.Tag)
		if err != nil {
			return nil, errors.Trace(err)
		}
		rel, err := api.st.KeyRelation(tag.Id())
		if err != nil {
			return nil, errors.Trace(err)
		}
		return &params.RemoteRelation{
			// TODO(axw) remote entity ID
			Life: params.Life(rel.Life().String()),
		}, nil
	}
	for i, entity := range entities.Entities {
		remoteRelation, err := one(entity)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		results.Results[i].Result = remoteRelation
	}
	return results, nil
}

func (api *RemoteRelationsAPI) RemoteServices(entities params.Entities) (params.RemoteServiceResults, error) {
	results := params.RemoteServiceResults{
		Results: make([]params.RemoteServiceResult, len(entities.Entities)),
	}
	one := func(entity params.Entity) (*params.RemoteService, error) {
		tag, err := names.ParseServiceTag(entity.Tag)
		if err != nil {
			return nil, errors.Trace(err)
		}
		svc, err := api.st.RemoteService(tag.Id())
		if err != nil {
			return nil, errors.Trace(err)
		}
		return &params.RemoteService{
			// TODO(axw) remote entity ID
			Life: params.Life(svc.Life().String()),
		}, nil
	}
	for i, entity := range entities.Entities {
		remoteService, err := one(entity)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		results.Results[i].Result = remoteService
	}
	return results, nil
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

// WatchServiceRelations starts a StringsWatcher for watching the relations of
// each specified service in the local environment, and returns the watcher IDs
// and initial values, or an error if the services' relations could not be
// watched.
func (api *RemoteRelationsAPI) WatchServiceRelations(args params.Entities) (params.StringsWatchResults, error) {
	results := params.StringsWatchResults{
		make([]params.StringsWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		serviceTag, err := names.ParseServiceTag(arg.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		serviceName := serviceTag.Id()
		w, err := api.st.WatchRemoteServiceRelations(serviceName)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		changes, ok := <-w.Changes()
		if !ok {
			results.Results[i].Error = common.ServerError(watcher.EnsureErr(w))
			continue
		}
		results.Results[i].StringsWatcherId = api.resources.Register(w)
		results.Results[i].Changes = changes
	}
	return results, nil
}

// WatchLocalRelationUnits starts a RelationUnitsWatcher for watching the local
// relation units involved in each specified relation in the local environment,
// and returns the watcher IDs and initial values, or an error if the relation
// units could not be watched.
func (api *RemoteRelationsAPI) WatchLocalRelationUnits(args params.Entities) (params.RelationUnitsWatchResults, error) {
	results := params.RelationUnitsWatchResults{
		make([]params.RelationUnitsWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		relationTag, err := names.ParseRelationTag(arg.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		w, err := api.watchLocalRelationUnits(relationTag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		changes, ok := <-w.Changes()
		if !ok {
			results.Results[i].Error = common.ServerError(watcher.EnsureErr(w))
			continue
		}
		results.Results[i].RelationUnitsWatcherId = api.resources.Register(w)
		results.Results[i].Changes = changes
	}
	return results, nil
}

func (api *RemoteRelationsAPI) watchLocalRelationUnits(tag names.RelationTag) (state.RelationUnitsWatcher, error) {
	relation, err := api.st.KeyRelation(tag.Id())
	if err != nil {
		return nil, errors.Trace(err)
	}
	for _, ep := range relation.Endpoints() {
		_, err := api.st.Service(ep.ServiceName)
		if errors.IsNotFound(err) {
			// Not found, probably means it's the remote service. Try the next endpoint.
			continue
		} else if err != nil {
			return nil, errors.Trace(err)
		}
		w, err := relation.WatchUnits(ep.ServiceName)
		if err != nil {
			return nil, errors.Trace(err)
		}
		return w, nil
	}
	return nil, errors.NotFoundf("local service for %s", names.ReadableString(tag))
}
