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
	Path    string         `bson:"path"`
}

// ToolsMetadata describes a Juju tools tarball.
type ToolsMetadata struct {
	Version version.Binary
	Size    int64
	SHA256  string
}

// toolsPath returns the storage path for the specified
// tools.
func toolsPath(v version.Binary, hash string) string {
	return fmt.Sprintf("tools-%s-%s", v, hash)
}

// AddTools adds the tools tarball and metadata into state,
// failing if there already exist tools with the specified
// version, replacing existing metadata if any exist with
// the specified version.
func (st *State) AddTools(r io.Reader, metadata ToolsMetadata) error {
	environ, err := st.Environment()
	if err != nil {
		return err
	}
	uuid := environ.UUID()

	// Add the tools tarball to storage.
	storage := st.getManagedStorage(uuid)
	path := toolsPath(metadata.Version, metadata.SHA256)
	if err := storage.PutForEnvironment(uuid, path, r, metadata.Size); err != nil {
		return err
	}

	// Add or replace metadata.
	doc := toolsMetadataDoc{
		Id:      metadata.Version.String(),
		Version: metadata.Version,
		Size:    metadata.Size,
		SHA256:  metadata.SHA256,
		Path:    path,
	}
	ops := []txn.Op{{
		C:      toolsC,
		Id:     doc.Id,
		Insert: &doc,
	}, {
		C:  toolsC,
		Id: doc.Id,
		Update: bson.D{{
			"$set", bson.D{
				{"size", metadata.Size},
				{"sha256", metadata.SHA256},
				{"path", path},
			},
		}},
	}}
	return st.runTransaction(ops)
}

// AddToolsAlias adds an alias for the tools with the specified version,
// failing if metadata already exists for the alias version.
func (st *State) AddToolsAlias(alias, version version.Binary) error {
	existingDoc, err := st.toolsMetadata(version)
	if err != nil {
		return err
	}
	newDoc := toolsMetadataDoc{
		Id:      alias.String(),
		Version: alias,
		Size:    existingDoc.Size,
		SHA256:  existingDoc.SHA256,
		Path:    existingDoc.Path,
	}
	ops := []txn.Op{{
		C:      toolsC,
		Id:     newDoc.Id,
		Assert: txn.DocMissing,
		Insert: &newDoc,
	}}
	err = st.runTransaction(ops)
	if err == txn.ErrAborted {
		return errors.AlreadyExistsf("%v tools metadata", alias)
	}
	return err
}

// Tools returns the ToolsMetadata and tools tarball contents
// for the specified version if it exists, else an error
// satisfying errors.IsNotFound.
func (st *State) Tools(v version.Binary) (ToolsMetadata, io.Reader, error) {
	metadataDoc, err := st.toolsMetadata(v)
	if err != nil {
		return ToolsMetadata{}, nil, err
	}
	tools, err := st.toolsTarball(metadataDoc.Path)
	if err != nil {
		return ToolsMetadata{}, nil, err
	}
	metadata := ToolsMetadata{
		Version: metadataDoc.Version,
		Size:    metadataDoc.Size,
		SHA256:  metadataDoc.SHA256,
	}
	return metadata, tools, nil
}

func (st *State) toolsMetadata(v version.Binary) (toolsMetadataDoc, error) {
	toolsCollection, closer := st.getCollection(toolsC)
	defer closer()
	var doc toolsMetadataDoc
	err := toolsCollection.Find(bson.D{{"_id", v.String()}}).One(&doc)
	if err == mgo.ErrNotFound {
		return doc, errors.NotFoundf("%v tools metadata", v)
	} else if err != nil {
		return doc, err
	}
	return doc, nil
}

func (st *State) toolsTarball(path string) (io.Reader, error) {
	environ, err := st.Environment()
	if err != nil {
		return nil, err
	}
	uuid := environ.UUID()
	ms := st.getManagedStorage(uuid)
	r, _, err := ms.GetForEnvironment(uuid, path)
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
