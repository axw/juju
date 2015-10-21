// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"io"

	"gopkg.in/juju/charm.v6-unstable"
	"gopkg.in/mgo.v2"

	"github.com/juju/names"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/leadership"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state/toolstorage"
	"github.com/juju/juju/tools"
	"github.com/juju/juju/version"
)

type State interface {
	io.Closer

	ActionState
	AnnotationState
	BlockState
	CharmState
	EnvironmentState
	EnvironmentUserState
	LeadershipState
	MachineState
	MetricState
	NetworkState
	RelationState
	RestoreState
	ServerState
	ServiceState
	UnitState
	UpgradeState

	// Miscellaneous helpers.

	EntityFinder

	// Things that really shouldn't be on State.

	MongoConnectionInfo() *mongo.MongoInfo
	MongoSession() *mgo.Session
	ToolsStorage() (toolstorage.StorageCloser, error)
}

type ActionState interface {
	ActionByTag(names.ActionTag) (*Action, error)
	FindActionTagsByPrefix(string) []names.ActionTag
}

type AnnotationState interface {
	Annotation(GlobalEntity, string) (string, error)
	Annotations(GlobalEntity) (map[string]string, error)
	SetAnnotations(GlobalEntity, map[string]string) error
}

type BlockState interface {
	AllBlocks() ([]Block, error)
	GetBlockForType(BlockType) (Block, bool, error)
	SwitchBlockOff(BlockType) error
	SwitchBlockOn(BlockType, string) error
}

type CharmState interface {
	AddStoreCharmPlaceholder(*charm.URL) error
	AllCharms() ([]*Charm, error)
	Charm(*charm.URL) (*Charm, error)
	LatestPlaceholderCharm(*charm.URL) (*Charm, error)
	PrepareStoreCharmUpload(*charm.URL) (*Charm, error)
	UpdateUploadedCharm(charm.Charm, *charm.URL, string, string) (*Charm, error)
}

type EnvironmentState interface {
	Environment() (*Environment, error)
	EnvironConfig() (*config.Config, error)
	EnvironConstraints() (constraints.Value, error)
	EnvironTag() names.EnvironTag
	EnvironUUID() string
	ForEnviron(names.EnvironTag) (State, error)
	SetEnvironAgentVersion(version.Number) error
	SetEnvironConstraints(constraints.Value) error
	// TODO(axw) rename
	RemoveAllEnvironDocs() error
	UpdateEnvironConfig(map[string]interface{}, []string, ValidateConfigFunc) error
	Watch() *Multiwatcher
}

type EnvironmentUserState interface {
	AddEnvironmentUser(names.UserTag, names.UserTag, string) (*EnvironmentUser, error)
	RemoveEnvironmentUser(names.UserTag) error
}

type LeadershipState interface {
	LeadershipClaimer() leadership.Claimer
	LeadershipChecker() leadership.Checker
}

type MachineState interface {
	AddOneMachine(MachineTemplate) (*Machine, error)
	AddMachineInsideMachine(MachineTemplate, string, instance.ContainerType) (*Machine, error)
	AddMachineInsideNewMachine(MachineTemplate, MachineTemplate, instance.ContainerType) (*Machine, error)
	AllMachines() ([]*Machine, error)
	Machine(name string) (*Machine, error)
}

type MetricState interface {
	CountOfSentMetrics() (int, error)
	CountOfUnsentMetrics() (int, error)
	MetricsManager() (*MetricsManager, error)
	MetricsToSend(int) ([]*MetricBatch, error)
	SetMetricBatchesSent([]string) error
}

type NetworkState interface {
	AllNetworks() ([]*Network, error)
	DeadIPAddresses() ([]*IPAddress, error)
	IPAddress(string) (*IPAddress, error)
	WatchIPAddresses() StringsWatcher
}

type RelationState interface {
	AddRelation(...Endpoint) (*Relation, error)
	AllRelations() ([]*Relation, error)
	EndpointsRelation(...Endpoint) (*Relation, error)
	InferEndpoints(...string) ([]Endpoint, error)
}

type RestoreState interface {
	// TODO(axw) rename
	RestoreInfoSetter() (*RestoreInfo, error)
}

type ServerState interface {
	APIHostPorts() ([][]network.HostPort, error)
	Cleanup() error
	EnsureAvailability(int, constraints.Value, string, []string) (StateServersChanges, error)
	IsStateServer() bool
	SetAPIHostPorts([][]network.HostPort) error
	SetStateServingInfo(StateServingInfo) error
	StateServerInfo() (*StateServerInfo, error)
	StateServingInfo() (StateServingInfo, error)
	WatchCleanups() NotifyWatcher
	WatchStateServerInfo() NotifyWatcher
}

type ServiceState interface {
	AddService(string, string, *Charm, []string, map[string]StorageConstraints) (*Service, error)
	AllServices() ([]*Service, error)
	Service(name string) (*Service, error)
}

type UnitState interface {
	AssignUnit(*Unit, AssignmentPolicy) error
	Unit(name string) (*Unit, error)
}

type UpgradeState interface {
	AbortCurrentUpgrade() error
}

// EntityFinder is implemented by *State. See State.FindEntity
// for documentation on the method.
type EntityFinder interface {
	FindEntity(tag names.Tag) (Entity, error)
}

// Entity represents any entity that can be returned
// by State.FindEntity. All entities have a tag.
type Entity interface {
	Tag() names.Tag
}

// EntityWithService is implemented by Units it is intended
// for anything that can return its Service.
type EntityWithService interface {
	Service() (*Service, error)
}

// Lifer represents an entity with a life.
type Lifer interface {
	Life() Life
}

// LifeBinder represents an entity whose lifespan is bindable
// to that of another entity.
type LifeBinder interface {
	Lifer

	// LifeBinding either returns the tag of an entity to which this
	// entity's lifespan is bound; the result may be nil, indicating
	// that the entity's lifespan is not bound to anything.
	//
	// The types of tags that may be returned are depdendent on the
	// entity type. For example, a Volume may be bound to a Filesystem,
	// but a Filesystem may not be bound to a Filesystem.
	LifeBinding() names.Tag
}

// AgentTooler is implemented by entities
// that have associated agent tools.
type AgentTooler interface {
	AgentTools() (*tools.Tools, error)
	SetAgentVersion(version.Binary) error
}

// EnsureDeader with an EnsureDead method.
type EnsureDeader interface {
	EnsureDead() error
}

// Remover represents entities with a Remove method.
type Remover interface {
	Remove() error
}

// Authenticator represents entites capable of handling password
// authentication.
type Authenticator interface {
	Refresh() error
	SetPassword(pass string) error
	PasswordValid(pass string) bool
}

// NotifyWatcherFactory represents an entity that
// can be watched.
type NotifyWatcherFactory interface {
	Watch() NotifyWatcher
}

// AgentEntity represents an entity that can
// have an agent responsible for it.
type AgentEntity interface {
	Entity
	Lifer
	Authenticator
	AgentTooler
	StatusSetter
	EnsureDeader
	Remover
	NotifyWatcherFactory
}

// EnvironAccessor defines the methods needed to watch for environment
// config changes, and read the environment config.
type EnvironAccessor interface {
	WatchForEnvironConfigChanges() NotifyWatcher
	EnvironConfig() (*config.Config, error)
}

// UnitsWatcher defines the methods needed to retrieve an entity (a
// machine or a service) and watch its units.
type UnitsWatcher interface {
	Entity
	WatchUnits() StringsWatcher
}

// EnvironMachinesWatcher defines a single method -
// WatchEnvironMachines.
type EnvironMachinesWatcher interface {
	WatchEnvironMachines() StringsWatcher
}

// InstanceIdGetter defines a single method - InstanceId.
type InstanceIdGetter interface {
	InstanceId() (instance.Id, error)
}

// ActionsWatcher defines the methods an entity exposes to watch Actions
// queued up for itself
type ActionsWatcher interface {
	Entity
	WatchActionNotifications() StringsWatcher
}

// ActionReceiver describes Entities that can have Actions queued for
// them, and that can get ActionRelated information about those actions.
// TODO(jcw4) consider implementing separate Actor classes for this
// interface; for example UnitActor that implements this interface, and
// takes a Unit and performs all these actions.
type ActionReceiver interface {
	Entity

	// AddAction queues an action with the given name and payload for this
	// ActionReceiver.
	AddAction(name string, payload map[string]interface{}) (*Action, error)

	// CancelAction removes a pending Action from the queue for this
	// ActionReceiver and marks it as cancelled.
	CancelAction(action *Action) (*Action, error)

	// WatchActionNotifications returns a StringsWatcher that will notify
	// on changes to the queued actions for this ActionReceiver.
	WatchActionNotifications() StringsWatcher

	// Actions returns the list of Actions queued and completed for this
	// ActionReceiver.
	Actions() ([]*Action, error)

	// CompletedActions returns the list of Actions completed for this
	// ActionReceiver.
	CompletedActions() ([]*Action, error)

	// PendingActions returns the list of Actions queued for this
	// ActionReceiver.
	PendingActions() ([]*Action, error)

	// RunningActions returns the list of Actions currently running for
	// this ActionReceiver.
	RunningActions() ([]*Action, error)
}

// GlobalEntity specifies entity.
type GlobalEntity interface {
	globalKey() string
	Tag() names.Tag
}
