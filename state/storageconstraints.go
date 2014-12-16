// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"fmt"

	"github.com/juju/errors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/storage"
)

// storageConstraintsDoc is the mongodb representation of a storage.Constraints.
type storageConstraintsDoc struct {
	EnvUUID string `bson:"env-uuid"`
	// TODO(axw) clone structure
	Constraints storage.Constraints
}

func (doc storageConstraintsDoc) value() storage.Constraints {
	return doc.Constraints
}

func newStorageConstraintsDoc(st *State, cons storage.Constraints) storageConstraintsDoc {
	return storageConstraintsDoc{
		EnvUUID:     st.EnvironUUID(),
		Constraints: cons,
	}
}

func createStorageConstraintsOp(st *State, id string, cons storage.Constraints) txn.Op {
	return txn.Op{
		C:      storageConstraintsC,
		Id:     st.docID(id),
		Assert: txn.DocMissing,
		Insert: newStorageConstraintsDoc(st, cons),
	}
}

func setStorageConstraintsOp(st *State, id string, cons storage.Constraints) txn.Op {
	return txn.Op{
		C:      storageConstraintsC,
		Id:     st.docID(id),
		Assert: txn.DocExists,
		Update: bson.D{{"$set", newStorageConstraintsDoc(st, cons)}},
	}
}

func removeStorageConstraintsOp(st *State, id string) txn.Op {
	return txn.Op{
		C:      storageConstraintsC,
		Id:     st.docID(id),
		Remove: true,
	}
}

func readStorageConstraints(st *State, id string) (storage.Constraints, error) {
	storageConstraintsCollection, closer := st.getCollection(storageConstraintsC)
	defer closer()

	doc := storageConstraintsDoc{}
	if err := storageConstraintsCollection.FindId(id).One(&doc); err == mgo.ErrNotFound {
		return storage.Constraints{}, errors.NotFoundf("storage constraints")
	} else if err != nil {
		return storage.Constraints{}, err
	}
	return doc.value(), nil
}

func writeStorageConstraints(st *State, id string, cons storage.Constraints) error {
	ops := []txn.Op{setStorageConstraintsOp(st, id, cons)}
	if err := st.runTransaction(ops); err != nil {
		return fmt.Errorf("cannot set storage constraints: %v", err)
	}
	return nil
}
