// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"github.com/juju/errors"
	"github.com/juju/names"
	jujutxn "github.com/juju/txn"
	"github.com/juju/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"
)

// remoteEntityDoc represents the internal state of a remote entity in
// MongoDB. Remote entities may be exported local entities, or imported
// remote entities.
type remoteEntityDoc struct {
	DocID   string `bson:"_id"`
	EnvUUID string `bson:"env-uuid"`

	SourceEnvUUID string `bson:"source-env-uuid"`
	EntityTag     string `bson:"entity"`
	Token         string `bson:"token"`
}

type tokenDoc struct {
	Token   string `bson:"_id"`
	EnvUUID string `bson:"env-uuid"`
}

type remoteEntities struct {
	st *State
}

func newRemoteEntities(st *State) *remoteEntities {
	return &remoteEntities{st}
}

// ExportLocalEntity adds an entity to the remote entities collection,
// returning an opaque token that uniquely identifies the entity within
// the environment.
//
// It is an error to export an entity twice.
func (r *remoteEntities) ExportLocalEntity(entity names.Tag) (string, error) {
	var token string
	sourceEnv := r.st.EnvironTag()
	ops := func(attempt int) ([]txn.Op, error) {
		// The entity must not already be exported.
		_, err := r.GetRemoteEntity(sourceEnv, token)
		if err == nil {
			return nil, errors.AlreadyExistsf(
				"token for %s",
				names.ReadableString(entity),
			)
		} else if !errors.IsNotFound(err) {
			return nil, errors.Trace(err)
		}

		// Generate a unique token within the environment.
		uuid, err := utils.NewUUID()
		if err != nil {
			return nil, errors.Trace(err)
		}
		token = uuid.String()
		exists, err := r.tokenExists(token)
		if err != nil {
			return nil, errors.Trace(err)
		}
		if exists {
			return nil, jujutxn.ErrTransientFailure
		}

		return []txn.Op{{
			C:      tokensC,
			Assert: txn.DocMissing,
			Insert: &tokenDoc{Token: token},
		}, {
			C:      remoteEntitiesC,
			Assert: txn.DocMissing,
			Insert: &remoteEntityDoc{
				DocID:         r.docID(sourceEnv, entity),
				SourceEnvUUID: sourceEnv.Id(),
				EntityTag:     entity.String(),
				Token:         token,
			},
		}}, nil
	}
	if err := r.st.run(ops); err != nil {
		return "", errors.Trace(err)
	}
	return token, nil
}

// ImportRemoteEntity adds an entity to the remote entities collection
// with the specified opaque token.
//
// This method assumes that the provided token is unique within the
// source environment, and does not perform any uniqueness checks on it.
func (r *remoteEntities) ImportRemoteEntity(
	sourceEnv names.EnvironTag, entity names.Tag, token string,
) error {
	ops := r.importRemoteEntityOps(sourceEnv, entity, token)
	err := r.st.runTransaction(ops)
	if err == txn.ErrAborted {
		return errors.AlreadyExistsf(
			"reference to %s in %s",
			names.ReadableString(entity),
			names.ReadableString(sourceEnv),
		)
	}
	if err != nil {
		return errors.Annotatef(
			err, "recording reference to %s in %s",
			names.ReadableString(entity),
			names.ReadableString(sourceEnv),
		)
	}
	return nil
}

func (r *remoteEntities) importRemoteEntityOps(
	sourceEnv names.EnvironTag, entity names.Tag, token string,
) []txn.Op {
	return []txn.Op{{
		C:      remoteEntitiesC,
		Id:     r.docID(sourceEnv, entity),
		Assert: txn.DocMissing,
		Insert: &remoteEntityDoc{
			SourceEnvUUID: sourceEnv.Id(),
			EntityTag:     entity.String(),
			Token:         token,
		},
	}}
}

// RemoveRemoteEntity removes the entity from the remote entities collection,
// and releases the token if the entity belongs to the local environment.
func (r *remoteEntities) RemoveRemoteEntity(
	sourceEnv names.EnvironTag, entity names.Tag,
) error {
	ops := func(attempt int) ([]txn.Op, error) {
		token, err := r.GetToken(sourceEnv, entity)
		if errors.IsNotFound(err) {
			return nil, jujutxn.ErrNoOperations
		}
		ops := []txn.Op{r.removeRemoteEntityOp(sourceEnv, entity)}
		if sourceEnv == r.st.EnvironTag() {
			ops = append(ops, txn.Op{
				C:      tokensC,
				Id:     token,
				Remove: true,
			})
		}
		return ops, nil
	}
	return r.st.run(ops)
}

// removeRemoteEntityOp returns the txn.Op to remove the remote entity
// document. It does not remove the token document for exported entities.
func (r *remoteEntities) removeRemoteEntityOp(
	sourceEnv names.EnvironTag, entity names.Tag,
) txn.Op {
	return txn.Op{
		C:      remoteEntitiesC,
		Id:     r.docID(sourceEnv, entity),
		Remove: true,
	}
}

// GetToken returns the token associated with the entity with the given tag
// and environment.
func (r *remoteEntities) GetToken(sourceEnv names.EnvironTag, entity names.Tag) (string, error) {
	remoteEntities, closer := r.st.getCollection(remoteEntitiesC)
	defer closer()

	var doc remoteEntityDoc
	err := remoteEntities.FindId(r.docID(sourceEnv, entity)).One(&doc)
	if err == mgo.ErrNotFound {
		return "", errors.NotFoundf(
			"token for %s in %s",
			names.ReadableString(entity),
			names.ReadableString(sourceEnv),
		)
	}
	if err != nil {
		return "", errors.Annotatef(
			err, "reading token for %s in %s",
			names.ReadableString(entity),
			names.ReadableString(sourceEnv),
		)
	}
	return doc.Token, nil
}

// GetRemoteEntity returns the tag of the entity associated with the given
// token and environment.
func (r *remoteEntities) GetRemoteEntity(sourceEnv names.EnvironTag, token string) (names.Tag, error) {
	remoteEntities, closer := r.st.getCollection(remoteEntitiesC)
	defer closer()

	var doc remoteEntityDoc
	err := remoteEntities.Find(bson.D{
		{"source-env-uuid", sourceEnv.Id()},
		{"token", token},
	}).One(&doc)
	if err == mgo.ErrNotFound {
		return nil, errors.NotFoundf(
			"entity for token %q in %s",
			token, names.ReadableString(sourceEnv),
		)
	}
	if err != nil {
		return nil, errors.Annotatef(
			err, "getting entity for token %q in %s",
			token, names.ReadableString(sourceEnv),
		)
	}
	return names.ParseTag(doc.EntityTag)
}

func (r *remoteEntities) docID(sourceEnv names.EnvironTag, entity names.Tag) string {
	return sourceEnv.Id() + "-" + entity.String()
}

func (r *remoteEntities) tokenExists(token string) (bool, error) {
	tokens, closer := r.st.getCollection(tokensC)
	defer closer()
	n, err := tokens.FindId(token).Count()
	if err != nil {
		return false, errors.Annotatef(err, "checking existence of token %q", token)
	}
	return n != 0, nil
}
