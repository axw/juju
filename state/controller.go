// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"github.com/juju/errors"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/controller"
)

const (
	// controllerInheritedSettingsGlobalKey is the key for default settings shared across models.
	controllerInheritedSettingsGlobalKey = "controllerInheritedSettings"

	// controllerConfigGlobalKey is the key for the controller config doc.
	controllerConfigGlobalKey = "controllerConfig"
)

type controllerConfigDoc struct {
	APIPort   int    `bson:"api-port"`
	StatePort int    `bson:"state-port"`
	CACert    string `bson:"ca-cert"`
	//CAPrivateKey         string `bson:"ca-private-key"`
	IdentityURL          string `bson:"identity-url,omitempty"`
	IdentityPublicKey    string `bson:"identity-public-key,omitempty"`
	SetNumaControlPolicy bool   `bson:"set-numa-control-policy"`
}

func createControllerConfigOp(cfg controller.Config) txn.Op {
	return txn.Op{
		C:  controllersC,
		Id: controllerConfigGlobalKey,
		Insert: &controllerConfigDoc{
			APIPort:              cfg.APIPort,
			StatePort:            cfg.StatePort,
			CACert:               cfg.CACert,
			IdentityURL:          cfg.IdentityURL,
			IdentityPublicKey:    cfg.IdentityPublicKey,
			SetNumaControlPolicy: cfg.SetNumaControlPolicy,
		},
	}
}

// ControllerConfig returns the config values for the controller.
func (st *State) ControllerConfig() (controller.Config, error) {
	coll, cleanup := st.getCollection(controllersC)
	defer cleanup()

	var doc controllerConfigDoc
	if err := coll.FindId(controllerConfigGlobalKey).One(&doc); err != nil {
		return controller.Config{}, errors.Annotate(err, "reading controller config")
	}
	config := controller.Config{
		APIPort:              doc.APIPort,
		StatePort:            doc.StatePort,
		UUID:                 st.controllerTag.Id(),
		CACert:               doc.CACert,
		IdentityURL:          doc.IdentityURL,
		IdentityPublicKey:    doc.IdentityPublicKey,
		SetNumaControlPolicy: doc.SetNumaControlPolicy,
	}
	return config, config.Validate()
}
