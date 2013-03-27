package state

import (
	"errors"
	"fmt"
	"labix.org/v2/mgo"
	"labix.org/v2/mgo/bson"
	"labix.org/v2/mgo/txn"
	"launchpad.net/juju-core/charm"
	"launchpad.net/juju-core/constraints"
	"launchpad.net/juju-core/trivial"
	"sort"
	"strconv"
)

// Service represents the state of a service.
type Service struct {
	st  *State
	doc serviceDoc
	annotator
}

// serviceDoc represents the internal state of a service in MongoDB.
type serviceDoc struct {
	Name          string `bson:"_id"`
	Series        string
	Subordinate   bool
	CharmURL      *charm.URL
	ForceCharm    bool
	Life          Life
	UnitSeq       int
	UnitCount     int
	RelationCount int
	Exposed       bool
	TxnRevno      int64 `bson:"txn-revno"`
}

func newService(st *State, doc *serviceDoc) *Service {
	svc := &Service{
		st:  st,
		doc: *doc,
	}
	svc.annotator = annotator{
		globalKey:  svc.globalKey(),
		entityName: svc.EntityName(),
		st:         st,
	}
	return svc
}

// Name returns the service name.
func (s *Service) Name() string {
	return s.doc.Name
}

// EntityName returns a name identifying the service that is safe to use
// as a file name.  The returned name will be different from other
// EntityName values returned by any other entities from the same state.
func (s *Service) EntityName() string {
	return "service-" + s.Name()
}

// globalKey returns the global database key for the service.
func (s *Service) globalKey() string {
	return "s#" + s.doc.Name
}

func serviceSettingsKey(serviceName string, curl *charm.URL) string {
	return fmt.Sprintf("s#%s#%s", serviceName, curl)
}

// settingsKey returns the charm-version-specific settings collection
// key for the service.
func (s *Service) settingsKey() string {
	return serviceSettingsKey(s.doc.Name, s.doc.CharmURL)
}

// Life returns whether the service is Alive, Dying or Dead.
func (s *Service) Life() Life {
	return s.doc.Life
}

var errRefresh = errors.New("cannot determine relation destruction operations; please refresh the service")

// Destroy ensures that the service and all its relations will be removed at
// some point; if the service has no units, and no relation involving the
// service has any units in scope, they are all removed immediately.
func (s *Service) Destroy() (err error) {
	defer trivial.ErrorContextf(&err, "cannot destroy service %q", s)
	defer func() {
		if err == nil {
			// This is a white lie; the document might actually be removed.
			s.doc.Life = Dying
		}
	}()
	svc := &Service{st: s.st, doc: s.doc}
	for i := 0; i < 5; i++ {
		ops, err := svc.destroyOps()
		switch {
		case err == errRefresh:
		case err == errAlreadyDying:
			return nil
		case err != nil:
			return err
		default:
			if err := svc.st.runner.Run(ops, "", nil); err != txn.ErrAborted {
				return err
			}
		}
		if err := svc.Refresh(); IsNotFound(err) {
			return nil
		} else if err != nil {
			return err
		}
	}
	return ErrExcessiveContention
}

// destroyOps returns the operations required to destroy the service. If it
// returns errRefresh, the service should be refreshed and the destruction
// operations recalculated.
func (s *Service) destroyOps() ([]txn.Op, error) {
	if s.doc.Life == Dying {
		return nil, errAlreadyDying
	}
	rels, err := s.Relations()
	if err != nil {
		return nil, err
	}
	if len(rels) != s.doc.RelationCount {
		// This is just an early bail out. The relations obtained may still
		// be wrong, but that situation will be caught by a combination of
		// asserts on relationcount and on each known relation, below.
		return nil, errRefresh
	}
	var ops []txn.Op
	removeCount := 0
	for _, rel := range rels {
		relOps, isRemove, err := rel.destroyOps(s.doc.Name)
		if err == errAlreadyDying {
			relOps = []txn.Op{{
				C:      s.st.relations.Name,
				Id:     rel.doc.Key,
				Assert: D{{"life", Dying}},
			}}
		} else if err != nil {
			return nil, err
		}
		if isRemove {
			removeCount++
		}
		ops = append(ops, relOps...)
	}
	// If the service has no units, and all its known relations will be
	// removed, the service can also be removed.
	if s.doc.UnitCount == 0 && s.doc.RelationCount == removeCount {
		hasLastRefs := D{{"life", Alive}, {"unitcount", 0}, {"relationcount", removeCount}}
		return append(ops, s.removeOps(hasLastRefs)...), nil
	}
	// In all other cases, service removal will be handled as a consequence
	// of the removal of the last unit or relation referencing it. If any
	// relations have been removed, they'll be caught by the operations
	// collected above; but if any has been added, we need to abort and add
	// a destroy op for that relation too. In combination, it's enough to
	// check for count equality: an add/remove will not touch the count, but
	// will be caught by virtue of being a remove.
	notLastRefs := D{
		{"life", Alive},
		{"relationcount", s.doc.RelationCount},
	}
	// With respect to unit count, a changing value doesn't matter, so long
	// as the count's equality with zero does not change, because all we care
	// about is that *some* unit is, or is not, keeping the service from
	// being removed: the difference between 1 unit and 1000 is irrelevant.
	if s.doc.UnitCount > 0 {
		notLastRefs = append(notLastRefs, D{{"unitcount", D{{"$gt", 0}}}}...)
	} else {
		notLastRefs = append(notLastRefs, D{{"unitcount", 0}}...)
	}
	update := D{{"$set", D{{"life", Dying}}}}
	if removeCount != 0 {
		decref := D{{"$inc", D{{"relationcount", -removeCount}}}}
		update = append(update, decref...)
	}
	return append(ops, txn.Op{
		C:      s.st.services.Name,
		Id:     s.doc.Name,
		Assert: notLastRefs,
		Update: update,
	}), nil
}

// removeOps returns the operations required to remove the service. Supplied
// asserts will be included in the operation on the service document.
func (s *Service) removeOps(asserts D) []txn.Op {
	ops := []txn.Op{{
		C:      s.st.services.Name,
		Id:     s.doc.Name,
		Assert: asserts,
		Remove: true,
	}, {
		C:      s.st.constraints.Name,
		Id:     s.globalKey(),
		Remove: true,
	}, {
		C:      s.st.settingsrefs.Name,
		Id:     s.settingsKey(),
		Remove: true,
	}, {
		C:      s.st.settings.Name,
		Id:     s.settingsKey(),
		Remove: true,
	}}
	return append(ops, annotationRemoveOp(s.st, s.globalKey()))
}

// IsExposed returns whether this service is exposed. The explicitly open
// ports (with open-port) for exposed services may be accessed from machines
// outside of the local deployment network. See SetExposed and ClearExposed.
func (s *Service) IsExposed() bool {
	return s.doc.Exposed
}

// SetExposed marks the service as exposed.
// See ClearExposed and IsExposed.
func (s *Service) SetExposed() error {
	return s.setExposed(true)
}

// ClearExposed removes the exposed flag from the service.
// See SetExposed and IsExposed.
func (s *Service) ClearExposed() error {
	return s.setExposed(false)
}

func (s *Service) setExposed(exposed bool) (err error) {
	ops := []txn.Op{{
		C:      s.st.services.Name,
		Id:     s.doc.Name,
		Assert: isAliveDoc,
		Update: D{{"$set", D{{"exposed", exposed}}}},
	}}
	if err := s.st.runner.Run(ops, "", nil); err != nil {
		return fmt.Errorf("cannot set exposed flag for service %q to %v: %v", s, exposed, onAbort(err, errNotAlive))
	}
	s.doc.Exposed = exposed
	return nil
}

// Charm returns the service's charm and whether units should upgrade to that
// charm even if they are in an error state.
func (s *Service) Charm() (ch *Charm, force bool, err error) {
	ch, err = s.st.Charm(s.doc.CharmURL)
	if err != nil {
		return nil, false, err
	}
	return ch, s.doc.ForceCharm, nil
}

// CharmURL returns the service's charm URL, and whether units should upgrade
// to the charm with that URL even if they are in an error state.
func (s *Service) CharmURL() (curl *charm.URL, force bool) {
	return s.doc.CharmURL, s.doc.ForceCharm
}

// Endpoints returns the service's currently available relation endpoints.
func (s *Service) Endpoints() (eps []Endpoint, err error) {
	ch, _, err := s.Charm()
	if err != nil {
		return nil, err
	}
	collect := func(role RelationRole, rels map[string]charm.Relation) {
		for name, rel := range rels {
			eps = append(eps, Endpoint{
				ServiceName:   s.doc.Name,
				Interface:     rel.Interface,
				RelationName:  name,
				RelationRole:  role,
				RelationScope: rel.Scope,
			})
		}
	}
	meta := ch.Meta()
	collect(RolePeer, meta.Peers)
	collect(RoleProvider, meta.Provides)
	collect(RoleRequirer, meta.Requires)
	collect(RoleProvider, map[string]charm.Relation{
		"juju-info": {
			Interface: "juju-info",
			Scope:     charm.ScopeGlobal,
		},
	})
	sort.Sort(epSlice(eps))
	return eps, nil
}

// Endpoint returns the relation endpoint with the supplied name, if it exists.
func (s *Service) Endpoint(relationName string) (Endpoint, error) {
	eps, err := s.Endpoints()
	if err != nil {
		return Endpoint{}, err
	}
	for _, ep := range eps {
		if ep.RelationName == relationName {
			return ep, nil
		}
	}
	return Endpoint{}, fmt.Errorf("service %q has no %q relation", s, relationName)
}

// convertConfig takes the given charm's config and converts the
// current service's charm config to the new one (if possible,
// otherwise returns an error). It also returns an assert op to
// ensure the old settings haven't changed in the meantime.
func (s *Service) convertConfig(ch *Charm) (map[string]interface{}, txn.Op, error) {
	orig, err := s.Config()
	if err != nil {
		return nil, txn.Op{}, err
	}
	newcfg, err := ch.Config().Convert(orig.Map())
	if err != nil {
		return nil, txn.Op{}, err
	}
	return newcfg, orig.assertUnchangedOp(), nil
}

// changeCharmOps returns the operations necessary to set a service's
// charm URL to a new value.
func (s *Service) changeCharmOps(ch *Charm, force bool) ([]txn.Op, error) {
	// Build the new service config.
	newcfg, assertOrigSettingsOp, err := s.convertConfig(ch)
	if err != nil {
		return nil, err
	}

	// Create or replace service settings.
	var settingsOp txn.Op
	newkey := serviceSettingsKey(s.doc.Name, ch.URL())
	if count, err := s.st.settings.FindId(newkey).Count(); err != nil {
		return nil, err
	} else if count == 0 {
		// No settings for this key yet, create it.
		settingsOp = createSettingsOp(s.st, newkey, newcfg)
	} else {
		// Settings exist, just replace them with the new ones.
		var err error
		settingsOp, _, err = replaceSettingsOp(s.st, newkey, newcfg)
		if err != nil {
			return nil, err
		}
	}

	// Add or create a reference to the new settings doc.
	incOp, err := settingsIncRefOp(s.st, s.doc.Name, ch.URL(), true)
	if err != nil {
		return nil, err
	}
	// Drop the reference to the old settings doc.
	decOps, err := settingsDecRefOps(s.st, s.doc.Name, s.doc.CharmURL) // current charm
	if err != nil {
		return nil, err
	}

	// Build the transaction.
	differentCharm := D{{"charmurl", D{{"$ne", ch.URL()}}}}
	ops := []txn.Op{
		// Old settings shouldn't change
		assertOrigSettingsOp,
		// Create/replace with new settings.
		settingsOp,
		// Increment the ref count.
		incOp,
		// Update the charm URL and force flag (if relevant).
		{
			C:      s.st.services.Name,
			Id:     s.doc.Name,
			Assert: append(isAliveDoc, differentCharm...),
			Update: D{{"$set", D{{"charmurl", ch.URL()}, {"forcecharm", force}}}},
		},
	}
	// And finally, decrement the old settings.
	return append(ops, decOps...), nil
}

// SetCharm changes the charm for the service. New units will be started with
// this charm, and existing units will be upgraded to use it. If force is true,
// units will be upgraded even if they are in an error state.
func (s *Service) SetCharm(ch *Charm, force bool) (err error) {
	if ch.Meta().Subordinate != s.doc.Subordinate {
		return fmt.Errorf("cannot change a service's subordinacy")
	}
	if ch.URL().Series != s.doc.Series {
		return fmt.Errorf("cannot change a service's series")
	}
	for i := 0; i < 5; i++ {
		var ops []txn.Op
		// Make sure the service doesn't have this charm already.
		sel := D{{"_id", s.doc.Name}, {"charmurl", ch.URL()}}
		if count, err := s.st.services.Find(sel).Count(); err != nil {
			return err
		} else if count == 1 {
			// Charm URL already set; just update the force flag.
			sameCharm := D{{"charmurl", ch.URL()}}
			ops = []txn.Op{{
				C:      s.st.services.Name,
				Id:     s.doc.Name,
				Assert: append(isAliveDoc, sameCharm...),
				Update: D{{"$set", D{{"forcecharm", force}}}},
			}}
		} else {
			// Change the charm URL.
			ops, err = s.changeCharmOps(ch, force)
			if err != nil {
				return err
			}
		}

		if err := s.st.runner.Run(ops, "", nil); err == nil {
			s.doc.CharmURL = ch.URL()
			s.doc.ForceCharm = force
			return nil
		} else if err != txn.ErrAborted {
			return err
		}

		// If the service is not alive, fail out immediately;
		// otherwise settings data changed underneath us, so retry.
		if alive, err := isAlive(s.st.services, s.doc.Name); err != nil {
			return err
		} else if !alive {
			return fmt.Errorf("service %q is not alive", s.doc.Name)
		}
	}
	return ErrExcessiveContention
}

// String returns the service name.
func (s *Service) String() string {
	return s.doc.Name
}

// Refresh refreshes the contents of the Service from the underlying
// state. It returns an error that satisfies IsNotFound if the service has
// been removed.
func (s *Service) Refresh() error {
	err := s.st.services.FindId(s.doc.Name).One(&s.doc)
	if err == mgo.ErrNotFound {
		return NotFoundf("service %q", s)
	}
	if err != nil {
		return fmt.Errorf("cannot refresh service %q: %v", s, err)
	}
	return nil
}

// newUnitName returns the next unit name.
func (s *Service) newUnitName() (string, error) {
	change := mgo.Change{Update: D{{"$inc", D{{"unitseq", 1}}}}}
	result := serviceDoc{}
	if _, err := s.st.services.Find(D{{"_id", s.doc.Name}}).Apply(change, &result); err == mgo.ErrNotFound {
		return "", NotFoundf("service %q", s)
	} else if err != nil {
		return "", fmt.Errorf("cannot increment unit sequence: %v", err)
	}
	name := s.doc.Name + "/" + strconv.Itoa(result.UnitSeq)
	return name, nil
}

// addUnitOps returns a unique name for a new unit, and a list of txn operations
// necessary to create that unit. The principalName param must be non-empty if
// and only if s is a subordinate service. Only one subordinate of a given
// service will be assigned to a given principal.
func (s *Service) addUnitOps(principalName string) (string, []txn.Op, error) {
	if s.doc.Subordinate && principalName == "" {
		return "", nil, fmt.Errorf("service is a subordinate")
	} else if s.doc.Subordinate && principalName != "" {
		return "", nil, fmt.Errorf("service is not a subordinate")
	}
	name, err := s.newUnitName()
	if err != nil {
		return "", nil, err
	}
	udoc := &unitDoc{
		Name:      name,
		Service:   s.doc.Name,
		Series:    s.doc.Series,
		Life:      Alive,
		Status:    UnitPending,
		Principal: principalName,
	}
	ops := []txn.Op{{
		C:      s.st.units.Name,
		Id:     name,
		Assert: txn.DocMissing,
		Insert: udoc,
	}, {
		C:      s.st.services.Name,
		Id:     s.doc.Name,
		Assert: isAliveDoc,
		Update: D{{"$inc", D{{"unitcount", 1}}}},
	}}
	if principalName != "" {
		ops = append(ops, txn.Op{
			C:  s.st.units.Name,
			Id: principalName,
			Assert: append(isAliveDoc, bson.DocElem{
				"subordinates", D{{"$not", bson.RegEx{Pattern: "^" + s.doc.Name + "/"}}},
			}),
			Update: D{{"$addToSet", D{{"subordinates", name}}}},
		})
	}
	return name, ops, nil
}

// AddUnit adds a new principal unit to the service.
func (s *Service) AddUnit() (unit *Unit, err error) {
	defer trivial.ErrorContextf(&err, "cannot add unit to service %q", s)
	name, ops, err := s.addUnitOps("")
	if err != nil {
		return nil, err
	}
	if err := s.st.runner.Run(ops, "", nil); err == txn.ErrAborted {
		if alive, err := isAlive(s.st.services, s.doc.Name); err != nil {
			return nil, err
		} else if !alive {
			return nil, fmt.Errorf("service is not alive")
		}
		return nil, fmt.Errorf("inconsistent state")
	} else if err != nil {
		return nil, err
	}
	return s.Unit(name)
}

var ErrExcessiveContention = errors.New("state changing too quickly; try again soon")

func (s *Service) removeUnitOps(u *Unit) ([]txn.Op, error) {
	var ops []txn.Op
	if u.doc.Principal != "" {
		ops = append(ops, txn.Op{
			C:      s.st.units.Name,
			Id:     u.doc.Principal,
			Assert: txn.DocExists,
			Update: D{{"$pull", D{{"subordinates", u.doc.Name}}}},
		})
	} else if u.doc.MachineId != "" {
		ops = append(ops, txn.Op{
			C:      s.st.machines.Name,
			Id:     u.doc.MachineId,
			Assert: txn.DocExists,
			Update: D{{"$pull", D{{"principals", u.doc.Name}}}},
		})
	}
	ops = append(ops, txn.Op{
		C:      s.st.units.Name,
		Id:     u.doc.Name,
		Assert: txn.DocExists,
		Remove: true,
	})
	if u.doc.CharmURL != nil {
		decOps, err := settingsDecRefOps(s.st, s.doc.Name, u.doc.CharmURL)
		if err != nil {
			return nil, err
		}
		ops = append(ops, decOps...)
	}
	if s.doc.Life == Dying && s.doc.RelationCount == 0 && s.doc.UnitCount == 1 {
		hasLastRef := D{{"life", Dying}, {"relationcount", 0}, {"unitcount", 1}}
		return append(ops, s.removeOps(hasLastRef)...), nil
	}
	svcOp := txn.Op{
		C:      s.st.services.Name,
		Id:     s.doc.Name,
		Update: D{{"$inc", D{{"unitcount", -1}}}},
	}
	if s.doc.Life == Alive {
		svcOp.Assert = D{{"life", Alive}, {"unitcount", D{{"$gt", 0}}}}
	} else {
		svcOp.Assert = D{{"life", Dying}, {"unitcount", D{{"$gt", 1}}}}
	}
	return append(ops, svcOp, annotationRemoveOp(s.st, u.globalKey())), nil
}

// Unit returns the service's unit with name.
func (s *Service) Unit(name string) (*Unit, error) {
	if !IsUnitName(name) {
		return nil, fmt.Errorf("%q is not a valid unit name", name)
	}
	udoc := &unitDoc{}
	sel := D{{"_id", name}, {"service", s.doc.Name}}
	if err := s.st.units.Find(sel).One(udoc); err != nil {
		return nil, fmt.Errorf("cannot get unit %q from service %q: %v", name, s.doc.Name, err)
	}
	return newUnit(s.st, udoc), nil
}

// AllUnits returns all units of the service.
func (s *Service) AllUnits() (units []*Unit, err error) {
	docs := []unitDoc{}
	err = s.st.units.Find(D{{"service", s.doc.Name}}).All(&docs)
	if err != nil {
		return nil, fmt.Errorf("cannot get all units from service %q: %v", s, err)
	}
	for i := range docs {
		units = append(units, newUnit(s.st, &docs[i]))
	}
	return units, nil
}

// Relations returns a Relation for every relation the service is in.
func (s *Service) Relations() (relations []*Relation, err error) {
	defer trivial.ErrorContextf(&err, "can't get relations for service %q", s)
	docs := []relationDoc{}
	err = s.st.relations.Find(D{{"endpoints.servicename", s.doc.Name}}).All(&docs)
	if err != nil {
		return nil, err
	}
	for _, v := range docs {
		relations = append(relations, newRelation(s.st, &v))
	}
	return relations, nil
}

// Config returns the configuration node for the service.
func (s *Service) Config() (config *Settings, err error) {
	config, err = readSettings(s.st, s.settingsKey())
	if err != nil {
		return nil, fmt.Errorf("cannot get configuration of service %q: %v", s, err)
	}
	return config, nil
}

var ErrSubordinateConstraints = errors.New("constraints do not apply to subordinate services")

// Constraints returns the current service constraints.
func (s *Service) Constraints() (constraints.Value, error) {
	if s.doc.Subordinate {
		return constraints.Value{}, ErrSubordinateConstraints
	}
	return readConstraints(s.st, s.globalKey())
}

// SetConstraints replaces the current service constraints.
func (s *Service) SetConstraints(cons constraints.Value) error {
	if s.doc.Subordinate {
		return ErrSubordinateConstraints
	}
	return writeConstraints(s.st, s.globalKey(), cons)
}

// settingsIncRefOp returns an operation that increments the ref count
// of the service settings identified by serviceName and curl. If
// canCreate is false, a missing document will be treated as an error;
// otherwise, it will be created with a ref count of 1.
func settingsIncRefOp(st *State, serviceName string, curl *charm.URL, canCreate bool) (txn.Op, error) {
	key := serviceSettingsKey(serviceName, curl)
	if count, err := st.settingsrefs.FindId(key).Count(); err != nil {
		return txn.Op{}, err
	} else if count == 0 {
		if !canCreate {
			return txn.Op{}, NotFoundf("service settings")
		}
		return txn.Op{
			C:      st.settingsrefs.Name,
			Id:     key,
			Assert: txn.DocMissing,
			Insert: settingsRefsDoc{1},
		}, nil
	}
	return txn.Op{
		C:      st.settingsrefs.Name,
		Id:     key,
		Assert: txn.DocExists,
		Update: D{{"$inc", D{{"refcount", 1}}}},
	}, nil
}

// settingsDecRefOps returns a list of operations that decrement the
// ref count of the service settings identified by serviceName and
// curl. If the ref count is set to zero, the appropriate setting and
// ref count documents will both be deleted.
func settingsDecRefOps(st *State, serviceName string, curl *charm.URL) ([]txn.Op, error) {
	key := serviceSettingsKey(serviceName, curl)
	var doc settingsRefsDoc
	if err := st.settingsrefs.FindId(key).One(&doc); err != nil {
		return nil, err
	}
	if doc.RefCount == 1 {
		return []txn.Op{{
			C:      st.settingsrefs.Name,
			Id:     key,
			Assert: D{{"refcount", 1}},
			Remove: true,
		}, {
			C:      st.settings.Name,
			Id:     key,
			Remove: true,
		}}, nil
	}
	return []txn.Op{{
		C:      st.settingsrefs.Name,
		Id:     key,
		Assert: D{{"refcount", D{{"$gt", 1}}}},
		Update: D{{"$inc", D{{"refcount", -1}}}},
	}}, nil
}

// settingsRefsDoc holds the number of units and services using the
// settings document identified by the document's id. Every time a
// service upgrades its charm the settings doc ref count for the new
// charm url is incremented, and the old settings is ref count is
// decremented. When a unit upgrades to the new charm, the old service
// settings ref count is decremented and the ref count of the new
// charm settings is incremented. The last unit upgrading to the new
// charm is responsible for deleting the old charm's settings doc.
//
// Note: We're not using the settingsDoc for this because changing
// just the ref count is not considered a change worth reporting
// to watchers and firing config-changed hooks.
//
// There is and implicit _id field here, which mongo creates, which is
// always the same as the settingsDoc's id.
type settingsRefsDoc struct {
	RefCount int
}
