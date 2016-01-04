// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package params

import (
	"github.com/juju/juju/state/multiwatcher"
	"gopkg.in/juju/charm.v6-unstable"
)

// OfferFilterParams contains filters used to query service offers
// from one or more directories.
type OfferFilterParams struct {
	Filters []OfferFilters `json:"filters"`
}

// EndpointFilterAttributes is used to filter offers matching the
// specified endpoint criteria.
type EndpointFilterAttributes struct {
	Role      charm.RelationRole `json:"role"`
	Interface string             `json:"interface"`
	Name      string             `json:"name"`
}

// OfferFilters is used to query offers in a service directory.
// Offers matching any of the filters are returned.
type OfferFilters struct {
	Directory string
	Filters   []OfferFilter
}

// OfferFilter is used to query offers in a service directory.
type OfferFilter struct {
	ServiceURL         string                     `json:"serviceurl"`
	SourceLabel        string                     `json:"sourcelabel"`
	SourceEnvUUIDTag   string                     `json:"sourceuuid"`
	ServiceName        string                     `json:"servicename"`
	ServiceDescription string                     `json:"servicedescription"`
	ServiceUser        string                     `json:"serviceuser"`
	Endpoints          []EndpointFilterAttributes `json:"endpoints"`
	AllowedUserTags    []string                   `json:"allowedusers"`
}

// ServiceOffer represents a service offering from an external environment.
type ServiceOffer struct {
	ServiceURL         string           `json:"serviceurl"`
	SourceEnvironTag   string           `json:"sourceenviron"`
	SourceLabel        string           `json:"sourcelabel"`
	ServiceName        string           `json:"servicename"`
	ServiceDescription string           `json:"servicedescription"`
	Endpoints          []RemoteEndpoint `json:"endpoints"`
}

// AddServiceOffers is used when adding offers to a service directory.
type AddServiceOffers struct {
	Offers []AddServiceOffer
}

// AddServiceOffer represents a service offering from an external environment.
type AddServiceOffer struct {
	ServiceOffer
	// UserTags are those who can consume the offer.
	UserTags []string `json:"users"`
}

// ServiceOfferResults is a result of listing service offers.
type ServiceOfferResults struct {
	Offers []ServiceOffer
	Error  *Error
}

// RemoteEndpoint represents a remote service endpoint.
type RemoteEndpoint struct {
	Name      string              `json:"name"`
	Role      charm.RelationRole  `json:"role"`
	Interface string              `json:"interface"`
	Limit     int                 `json:"limit"`
	Scope     charm.RelationScope `json:"scope"`
}

// ServiceOfferParams is used to offer remote service.
type ServiceOfferParams struct {
	// ServiceURL may contain user supplied service url.
	ServiceURL string `json:"serviceurl,omitempty"`

	// ServiceName contains name of service being offered.
	ServiceName string `json:"servicename"`

	// Description is description for the offered service.
	// For now, this defaults to description provided in the charm or
	// is supplied by the user.
	ServiceDescription string `json:"servicedescription"`

	// Endpoints contains offered service endpoints.
	Endpoints []string `json:"endpoints"`

	// AllowedUserTags contains tags of users that are allowed to use this offered service.
	AllowedUserTags []string `json:"allowedusers"`
}

// ServiceOffersParams contains a collection of offers to allow adding offers in bulk.
type ServiceOffersParams struct {
	Offers []ServiceOfferParams `json:"offers"`
}

// FindServiceOffersResults is a result of finding remote service offers.
type FindServiceOffersResults struct {
	// Results contains service offers matching each filter.
	Results []ServiceOfferResults `json:"results"`
}

// ServiceOfferResult is a result of listing a remote service offer.
type ServiceOfferResult struct {
	// Result contains service offer information.
	Result ServiceOffer `json:"result"`

	// Error contains related error.
	Error *Error `json:"error,omitempty"`
}

// ServiceOffersResults is a result of listing remote service offers.
type ServiceOffersResults struct {
	// Result contains collection of remote service results.
	Results []ServiceOfferResult `json:"results,omitempty"`
}

// ServiceURLs is a filter used to select remote services via show call.
type ServiceURLs struct {
	// URLs contains collection of urls for services that are to be shown.
	ServiceUrls []string `json:"serviceurls,omitempty"`
}

// OfferedService represents attributes for an offered service.
type OfferedService struct {
	ServiceURL  string            `json:"serviceurl"`
	ServiceName string            `json:"servicename"`
	CharmName   string            `json:"charmname"`
	Description string            `json:"description"`
	Registered  bool              `json:"registered"`
	Endpoints   map[string]string `json:"endpoints"`
}

// OfferedServiceResult holds the result of loading an
// offerred service at a URL.
type OfferedServiceResult struct {
	Result OfferedService `json:"result"`
	Error  *Error         `json:"error,omitempty"`
}

// OfferedServiceResults represents the result of a ListOfferedServices call.
type OfferedServiceResults struct {
	Results []OfferedServiceResult
}

// OfferedServiceDetails is a service found during a request to list remote services.
type OfferedServiceDetails struct {
	// ServiceURL may contain user supplied service url.
	ServiceURL string `json:"serviceurl,omitempty"`

	// ServiceName contains name of service being offered.
	ServiceName string `json:"servicename"`

	// CharmName is the charm name of this service.
	CharmName string `json:"charmname"`

	// UsersCount is the count of how many users are connected to this shared service.
	UsersCount int `json:"userscount,omitempty"`

	// Endpoints is a list of charm relations that this remote service offered.
	Endpoints []RemoteEndpoint `json:"endpoints"`
}

// OfferedServiceDetailsResult is a result of listing a remote service.
type OfferedServiceDetailsResult struct {
	// Result contains remote service information.
	Result *OfferedServiceDetails `json:"result,omitempty"`

	// Error contains error related to this item.
	Error *Error `json:"error,omitempty"`
}

// ListOffersFilterResults is a result of listing remote service offers
// for a service directory.
type ListOffersFilterResults struct {
	// Error contains error related to this directory.
	Error *Error `json:"error,omitempty"`

	// Result contains collection of remote service item results for this directory.
	Result []OfferedServiceDetailsResult `json:"result,omitempty"`
}

// ListOffersResults is a result of listing remote service offers
// for service directories.
type ListOffersResults struct {
	// Results contains collection of remote directories results.
	Results []ListOffersFilterResults `json:"results,omitempty"`
}

// OfferedServiceFilters has sets of filters that
// are used by a vendor to query remote services that the vendor has offered.
type OfferedServiceFilters struct {
	Filters []OfferedServiceFilter `json:"filters,omitempty"`
}

// OfferedServiceFilter has a set of filter terms that
// are used by a vendor to query remote services that the vendor has offered.
type OfferedServiceFilter struct {
	FilterTerms []OfferedServiceFilterTerm `json:"filterterms,omitempty"`
}

// OfferedServiceFilterTerm has filter criteria that
// are used by a vendor to query remote services that the vendor has offered.
type OfferedServiceFilterTerm struct {
	// ServiceURL is url for remote service.
	// This may be a part of valid URL.
	ServiceURL string `json:"serviceurl,omitempty"`

	// Endpoint contains endpoint properties for filter.
	Endpoint RemoteEndpoint `json:"endpoint"`

	// CharmName is the charm name of this service.
	CharmName string `json:"charmname,omitempty"`
}

// RemoteEntityId is an identifier for an entity that may be involved in a
// cross-model relation. This object comprises the UUID of the model to
// which the entity belongs, and an opaque token that is unique to that model.
type RemoteEntityId struct {
	EnvUUID string `json:"env-uuid"`
	Token   string `json:"token"`
}

type RemoteEntityIdResult struct {
	Result *RemoteEntityId `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type RemoteEntityIdResults struct {
	Results []RemoteEntityIdResult `json:"results,omitempty"`
}

// RemoteRelation describes the current state of a cross-model relation from
// the perspective of the local environment.
type RemoteRelation struct {
	Id   RemoteEntityId `json:"id"`
	Life Life           `json:"life"`
}

type RemoteRelationResult struct {
	Result *RemoteRelation `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

type RemoteRelationResults struct {
	Results []RemoteRelationResult `json:"results,omitempty"`
}

// RemoteService describes the current state of a service involved in a cross-
// model relation, from the perspective of the local environment.
type RemoteService struct {
	Id   RemoteEntityId `json:"id"`
	Life Life           `json:"life"`
}

type RemoteServiceResult struct {
	Result *RemoteService `json:"result,omitempty"`
	Error  *Error         `json:"error,omitempty"`
}

type RemoteServiceResults struct {
	Results []RemoteServiceResult `json:"results,omitempty"`
}

// RelationUnitsWatchResult holds a RelationUnitsWatcher id, changes
// and an error (if any).
type RelationUnitsWatchResult struct {
	RelationUnitsWatcherId string                           `json:"id"`
	Changes                multiwatcher.RelationUnitsChange `json:"changes"`
	Error                  *Error                           `json:"error,omitempty"`
}

// RelationUnitsWatchResults holds the results for any API call which ends up
// returning a list of RelationUnitsWatchers.
type RelationUnitsWatchResults struct {
	Results []RelationUnitsWatchResult `json:"results,omitempty"`
}

// RemoteRelationsWatchResult holds a RemoteRelationsWatcher id,
// changes and an error (if any).
type RemoteRelationsWatchResult struct {
	RemoteRelationsWatcherId string                 `json:"id"`
	Changes                  *RemoteRelationsChange `json:"changes,omitempty"`
	Error                    *Error                 `json:"error,omitempty"`
}

// RemoteRelationsWatchResults holds the results for any API call which ends up
// returning a list of RemoteRelationsWatchers.
type RemoteRelationsWatchResults struct {
	Results []RemoteRelationsWatchResult `json:"results"`
}

// ServiceWatchResult holds a ServiceWatcher id, changes and an error (if any).
type RemoteServiceWatchResult struct {
	RemoteServiceWatcherId string               `json:"id"`
	Change                 *RemoteServiceChange `json:"change,omitempty"`
	Error                  *Error               `json:"error,omitempty"`
}

// RemoteServiceWatchResults holds the results for any API call which ends
// up returning a list of RemoteServiceWatchers.
type RemoteServiceWatchResults struct {
	Results []RemoteServiceWatchResult `json:"results,omitempty"`
}

// RemoteServiceChange describes changes to a remote service.
type RemoteServiceChange struct {
	Id        RemoteEntityId        `json:"id"`
	Life      Life                  `json:"life"`
	Relations RemoteRelationsChange `json:"relations"`
	// TODO(axw) status
}

// RemoteServiceChanges describes a set of changes to services.
type RemoteServiceChanges struct {
	Changes []RemoteServiceChange `json:"changes,omitempty"`
}

// RemoteRelationsChange describes changes to the relations that a remote
// service is involved in.
type RemoteRelationsChange struct {
	// Initial indicates whether or not this is an initial, complete
	// representation of all relations involving a service. If Initial
	// is true, then RemovedRelations will be empty.
	Initial bool `json:"initial"`

	// ChangedRelations contains relation changes.
	ChangedRelations []RemoteRelationChange `json:"changed,omitempty"`

	// RemovedRelations contains the IDs corresponding to
	// relations removed since the last change.
	RemovedRelations []RemoteEntityId `json:"removed,omitempty"`
}

// RemoteRelationsChanges holds a set of RemoteRelationsChange structures.
type RemoteRelationsChanges struct {
	Changes []RemoteRelationsChange `json:"changes,omitempty"`
}

// RelationChange describes changes to a relation.
type RemoteRelationChange struct {
	// Id uniquely identifies the cross-model relation.
	Id RemoteEntityId `json:"id"`

	// Life is the current lifecycle state of the relation.
	Life Life `json:"life"`

	// Initial indicates whether or not this is an initial, complete
	// representation of all relation units involved in a relation.
	// If Initial is true, then DepartedUnits will be empty.
	Initial bool `json:"initial"`

	// ChangedUnits contains relation unit changes.
	ChangedUnits []RemoteRelationUnitChange `json:"changedunits,omitempty"`

	// DepartedUnits contains the IDs identifying units that
	// have departed the relation since the last change.
	DepartedUnits []RemoteEntityId `json:"departedunits,omitempty"`
}

// RemoteRelationChanges holds a set of RemoteRelationChange structures.
type RemoteRelationChanges struct {
	Changes []RemoteRelationChange `json:"changes,omitempty"`
}

// RemoteRelationUnitChange describes a relation unit change.
type RemoteRelationUnitChange struct {
	// Id uniquely identifies the remote unit.
	Id RemoteEntityId `json:"id"`

	// Settings is the current settings for the relation unit.
	Settings map[string]string `json:"settings,omitempty"`
}
