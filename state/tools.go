// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"fmt"
	"io"

	"github.com/juju/blobstore"
	"github.com/juju/errors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/version"
)

type toolsMetadataDoc struct {
	Id      string         `bson:"_id"`
	Version version.Binary `bson:"version"`
	Size    int64          `bson:"size"`
	SHA256  string         `bson:"sha256,omitempty"`
}

// ToolsMetadata describes a Juju tools tarball.
type ToolsMetadata struct {
	Version version.Binary
	Size    int64
	SHA256  string
}

func toolsPath(v version.Binary) string {
	return fmt.Sprintf("tools-%s", v)
}

// AddToolsMetadata adds the tools metadata to the catalogue,
// failing if there already exist metadata with the specified
// version.
func (st *State) AddTools(v version.Binary, r io.Reader, size int64, sha256 string) error {
	environ, err := st.Environment()
	if err != nil {
		return err
	}
	uuid := environ.UUID()

	// TODO(axw) ensure tools aren't overwritten
	ms := st.getManagedStorage(uuid)
	if err := ms.PutForEnvironment(uuid, toolsPath(v), r, size); err != nil {
		return err
	}

	doc := toolsMetadataDoc{
		Id:      v.String(),
		Version: v,
		Size:    size,
		SHA256:  sha256,
	}
	ops := []txn.Op{{
		C:      toolsC,
		Id:     doc.Id,
		Insert: &doc,
		Assert: txn.DocMissing,
	}}
	err = st.runTransaction(ops)
	if err == txn.ErrAborted {
		return errors.AlreadyExistsf("%v tools metadata", v)
	}
	return err
}

// Tools returns the ToolsMetadata and tools tarball contents
// for the specified version if it exists, else an error
// satisfying errors.IsNotFound.
func (st *State) Tools(v version.Binary) (ToolsMetadata, io.Reader, error) {
	metadata, err := st.toolsMetadata(v)
	if err != nil {
		return ToolsMetadata{}, nil, err
	}
	tools, err := st.toolsTarball(v)
	if err != nil {
		return ToolsMetadata{}, nil, err
	}
	return metadata, tools, nil
}

func (st *State) toolsMetadata(v version.Binary) (ToolsMetadata, error) {
	toolsCollection, closer := st.getCollection(toolsC)
	defer closer()
	var doc toolsMetadataDoc
	err := toolsCollection.Find(bson.D{{"_id", v.String()}}).One(&doc)
	if err == mgo.ErrNotFound {
		return ToolsMetadata{}, errors.NotFoundf("%v tools metadata", v)
	} else if err != nil {
		return ToolsMetadata{}, err
	}
	return ToolsMetadata{
		Version: doc.Version,
		Size:    doc.Size,
		SHA256:  doc.SHA256,
	}, nil
}

func (st *State) toolsTarball(v version.Binary) (io.Reader, error) {
	environ, err := st.Environment()
	if err != nil {
		return nil, err
	}
	uuid := environ.UUID()
	ms := st.getManagedStorage(uuid)
	r, _, err := ms.GetForEnvironment(uuid, toolsPath(v))
	if err != nil {
		return nil, err
	}
	return r, err
}

// AllToolsMetadata returns metadata for the full list of tools in
// the catalogue.
func (st *State) AllToolsMetadata() ([]ToolsMetadata, error) {
	toolsCollection, closer := st.getCollection(toolsC)
	defer closer()
	var docs []toolsMetadataDoc
	if err := toolsCollection.Find(nil).All(&docs); err != nil {
		return nil, err
	}
	list := make([]ToolsMetadata, len(docs))
	for i, doc := range docs {
		metadata := ToolsMetadata{
			Version: doc.Version,
			Size:    doc.Size,
			SHA256:  doc.SHA256,
		}
		list[i] = metadata
	}
	return list, nil
}

func (st *State) getManagedStorage(uuid string) blobstore.ManagedStorage {
	// TODO(axw) create a ManagedStorage wrapper which does all this under
	// the hood, and copies/closes a session as part of each method call.
	session := st.MongoSession()
	rs := blobstore.NewGridFS(st.db.Name, uuid, session)
	db := session.DB(blobstoreDB)
	return blobstore.NewManagedStorage(db, rs)
}
