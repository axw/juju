// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package featuretests

import (
	"time"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/charm.v6-unstable"

	"github.com/juju/juju/api/remoterelations"
	"github.com/juju/juju/apiserver"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/instance"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/model/crossmodel"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
	statetesting "github.com/juju/juju/state/testing"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/testing/factory"
)

// TODO(axw) this suite should be re-written as end-to-end tests using the
// remote relations worker when it is ready.

type remoteRelationsSuite struct {
	jujutesting.JujuConnSuite
	client *remoterelations.State
}

func (s *remoteRelationsSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
	conn, _ := s.OpenAPIAsNewMachine(c, state.JobManageEnviron)
	s.client = remoterelations.NewState(conn)
}

func (s *remoteRelationsSuite) TestWatchRemoteServices(c *gc.C) {
	_, err := s.State.AddRemoteService("mysql", "local:/u/me/mysql", nil)
	c.Assert(err, jc.ErrorIsNil)

	w, err := s.client.WatchRemoteServices()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(w, gc.NotNil)
	defer statetesting.AssertStop(c, w)

	wc := statetesting.NewStringsWatcherC(c, s.BackingState, w)
	wc.AssertChangeInSingleEvent("mysql")
	wc.AssertNoChange()

	_, err = s.State.AddRemoteService("db2", "local:/u/ibm/db2", nil)
	c.Assert(err, jc.ErrorIsNil)
	wc.AssertChangeInSingleEvent("db2")
	wc.AssertNoChange()
}

func (s *remoteRelationsSuite) TestWatchServiceRelations(c *gc.C) {
	// Add a remote service, and watch it. It should initially have no
	// relations.
	_, err := s.State.AddRemoteService("mysql", "local:/u/me/mysql", []charm.Relation{{
		Interface: "mysql",
		Name:      "db",
		Role:      charm.RoleProvider,
		Scope:     charm.ScopeGlobal,
	}})
	c.Assert(err, jc.ErrorIsNil)
	w, err := s.client.WatchServiceRelations("mysql")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(w, gc.NotNil)
	defer statetesting.AssertStop(c, w)
	assertServiceRelationsChange(c, s.BackingState, w, params.ServiceRelationsChange{})
	assertNoServiceRelationsChange(c, s.BackingState, w)

	// Add the relation, and expect a watcher change.
	wordpress := s.Factory.MakeService(c, &factory.ServiceParams{
		Charm: s.Factory.MakeCharm(c, &factory.CharmParams{
			Name: "wordpress",
		}),
	})
	eps, err := s.State.InferEndpoints("wordpress", "mysql")
	c.Assert(err, jc.ErrorIsNil)
	rel, err := s.State.AddRelation(eps[0], eps[1])
	c.Assert(err, jc.ErrorIsNil)

	expect := params.ServiceRelationsChange{
		ChangedRelations: []params.RelationChange{{
			RelationId: rel.Id(),
			Life:       params.Alive,
		}},
	}
	assertServiceRelationsChange(c, s.BackingState, w, expect)
	assertNoServiceRelationsChange(c, s.BackingState, w)

	// Add a unit of wordpress, expect a change.
	settings := map[string]interface{}{"key": "value"}
	wordpress0, err := wordpress.AddUnit()
	c.Assert(err, jc.ErrorIsNil)
	ru, err := rel.Unit(wordpress0)
	c.Assert(err, jc.ErrorIsNil)
	err = ru.EnterScope(settings)
	c.Assert(err, jc.ErrorIsNil)
	expect.ChangedRelations[rel.Id()] = params.RelationChange{
		Life: params.Alive,
		ChangedUnits: map[string]params.RelationUnitChange{
			wordpress0.Name(): params.RelationUnitChange{
				Settings: settings,
			},
		},
	}
	assertServiceRelationsChange(c, s.BackingState, w, expect)
	assertNoServiceRelationsChange(c, s.BackingState, w)

	// Change the settings, expect a change.
	ruSettings, err := ru.Settings()
	c.Assert(err, jc.ErrorIsNil)
	settings["quay"] = 123
	ruSettings.Update(settings)
	_, err = ruSettings.Write()
	c.Assert(err, jc.ErrorIsNil)
	expect.ChangedRelations[rel.Id()].ChangedUnits[wordpress0.Name()] = params.RelationUnitChange{
		Settings: settings,
	}
	assertServiceRelationsChange(c, s.BackingState, w, expect)
	assertNoServiceRelationsChange(c, s.BackingState, w)
}

func (s *remoteRelationsSuite) TestWatchRemoteService(c *gc.C) {
	const serviceURL = "local:/u/me/mariadb"
	mariadbEndpoints := []charm.Relation{{
		Interface: "mysql",
		Name:      "db",
		Role:      charm.RoleProvider,
		Scope:     charm.ScopeGlobal,
	}}
	wordpressEndpoints := []charm.Relation{{
		Interface: "mysql",
		Name:      "db",
		Role:      charm.RoleRequirer,
		Scope:     charm.ScopeGlobal,
	}}

	// Remote environment offers "mysql" as "mariadb".
	configAttrs := testing.Attrs(s.Environ.Config().AllAttrs())
	remoteEnv := s.Factory.MakeEnvironment(c, &factory.EnvParams{
		ConfigAttrs: configAttrs.Delete(
			"name", "uuid", "type", "state-port", "api-port",
		),
	})
	defer remoteEnv.Close()
	remoteOffers := state.NewOfferedServices(remoteEnv)
	remoteStateFactory := factory.NewFactory(remoteEnv)
	mysql := remoteStateFactory.MakeService(c, &factory.ServiceParams{
		Name: "mysql",
		Charm: remoteStateFactory.MakeCharm(c,
			&factory.CharmParams{Name: "mysql"},
		),
	})
	err := remoteOffers.AddOffer(crossmodel.OfferedService{
		ServiceName: "mysql", // internal name
		ServiceURL:  serviceURL,
		Endpoints: map[string]string{
			"server": "db",
		},
	})
	c.Assert(err, jc.ErrorIsNil)

	// Local environment consumes offer, calls it "remote-mariadb". This
	// triggers the creation of a remote service in the remote environment
	// for the consumer's service that will be involved in the relation.
	directory := state.NewServiceDirectory(s.State)
	err = directory.AddOffer(crossmodel.ServiceOffer{
		ServiceURL:         serviceURL,
		ServiceName:        "remote-mariadb",
		ServiceDescription: "just mariadb, honest",
		SourceEnvUUID:      remoteEnv.EnvironUUID(),
		Endpoints:          mariadbEndpoints,
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = s.State.AddRemoteService("remote-mariadb", serviceURL, mariadbEndpoints)
	c.Assert(err, jc.ErrorIsNil)
	_, err = remoteEnv.AddRemoteService("mysql-client", "", wordpressEndpoints)
	c.Assert(err, jc.ErrorIsNil)

	// Add unit to remote service, but don't enter
	// it into the relation's scope yet.
	mysql0, err := mysql.AddUnit()
	c.Assert(err, jc.ErrorIsNil)
	err = mysql0.AssignToNewMachine()
	c.Assert(err, jc.ErrorIsNil)
	mid, err := mysql0.AssignedMachineId()
	c.Assert(err, jc.ErrorIsNil)
	m, err := remoteEnv.Machine(mid)
	c.Assert(err, jc.ErrorIsNil)
	err = m.SetInstanceInfo("inst-id", "nonce", &instance.HardwareCharacteristics{}, nil, nil, nil, nil)
	c.Assert(err, jc.ErrorIsNil)
	const privateAddress = "10.1.2.3"
	const publicAddress = "162.213.33.122"
	err = m.SetMachineAddresses(network.NewAddresses(privateAddress, publicAddress)...)
	c.Assert(err, jc.ErrorIsNil)
	settings := map[string]interface{}{
		"host": privateAddress,
	}

	// TODO(axw) the assertService... functions sync remoteEnv,
	// but that doesn't actually speed anything up. This is because
	// the apiserver facade will get a new State object, which has
	// its own watcher loop, unaffected by the syncing.
	w, err := s.client.WatchRemoteService("remote-mariadb")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(w, gc.NotNil)
	defer statetesting.AssertStop(c, w)
	assertServiceChange(c, remoteEnv, w, params.ServiceChange{
		// TODO(axw) service should be "service-remote-mariadb"
		ServiceTag: "service-mysql",
		Life:       params.Alive,
	})
	assertNoServiceChange(c, remoteEnv, w)

	// Enter the remote unit into a relation, and it should
	// be observed in the consuming side.
	eps, err := remoteEnv.InferEndpoints("mysql", "mysql-client")
	c.Assert(err, jc.ErrorIsNil)
	rel, err := remoteEnv.AddRelation(eps[0], eps[1])
	c.Assert(err, jc.ErrorIsNil)
	ru, err := rel.Unit(mysql0)
	c.Assert(err, jc.ErrorIsNil)
	err = ru.EnterScope(settings)
	c.Assert(err, jc.ErrorIsNil)
	assertServiceChange(c, remoteEnv, w, params.ServiceChange{
		// TODO(axw) service should be "service-remote-mariadb"
		ServiceTag: "service-mysql",
		Life:       params.Alive,
		Relations: params.ServiceRelationsChange{
			ChangedRelations: []params.RelationChange{{
				// TODO(axw) create additonal relations in the
				// consuming environment to ensure there's a
				// translation of relation IDs.
				RelationId: 0,
				Life:       params.Alive,
				ChangedUnits: map[string]params.RelationUnitChange{
					// TODO(axw) unit names should be translated.
					"mysql/0": params.RelationUnitChange{
						// TODO(axw) private address should
						// be translated to public address.
						Settings: map[string]interface{}{
							"host": privateAddress,
						},
					},
				},
			}},
		},
	})
	assertNoServiceChange(c, remoteEnv, w)
}

func assertServiceRelationsChange(
	c *gc.C,
	ss statetesting.SyncStarter,
	w apiserver.ServiceRelationsWatcher,
	expect params.ServiceRelationsChange,
) {
	ss.StartSync()
	select {
	case change, ok := <-w.Changes():
		c.Assert(ok, jc.IsTrue)
		c.Assert(change, jc.DeepEquals, expect)
	case <-time.After(testing.LongWait):
		c.Errorf("timed out waiting for service relations change")
	}
}

func assertNoServiceRelationsChange(c *gc.C, ss statetesting.SyncStarter, w apiserver.ServiceRelationsWatcher) {
	ss.StartSync()
	select {
	case change, ok := <-w.Changes():
		c.Errorf("unexpected change from service relations watcher: %v, %v", change, ok)
	case <-time.After(testing.ShortWait):
	}
}

func assertServiceChange(
	c *gc.C,
	ss statetesting.SyncStarter,
	w apiserver.ServiceWatcher,
	expect params.ServiceChange,
) {
	ss.StartSync()
	select {
	case change, ok := <-w.Changes():
		c.Assert(ok, jc.IsTrue)
		c.Assert(change, jc.DeepEquals, expect)
	case <-time.After(testing.LongWait):
		c.Errorf("timed out waiting for service change")
	}
}

func assertNoServiceChange(c *gc.C, ss statetesting.SyncStarter, w apiserver.ServiceWatcher) {
	ss.StartSync()
	select {
	case change, ok := <-w.Changes():
		c.Errorf("unexpected change from service watcher: %v, %v", change, ok)
	case <-time.After(testing.ShortWait):
	}
}
