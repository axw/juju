// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"github.com/juju/errors"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/tools"
	"github.com/juju/juju/version"
)

type toolsDoc struct {
	Id      string         `bson:"_id"`
	Version version.Binary `bson:"version"`
	Size    int64          `bson:"size"`
	SHA256  string         `bson:"sha256,omitempty"`
	URL     string         `bson:"url"`
}

// AddTools adds the specified tools to the catalogue,
// failing if there already exist tools with the
// specified version.
func (st *State) AddTools(tools *tools.Tools) error {
	return st.addTools(tools, false)
}

// ReplaceTools adds the specified tools to the catalogue,
// replacing any existing tools with the specified version.
// If there are no existing tools with the specified version,
// a new entry will be created.
func (st *State) ReplaceTools(tools *tools.Tools) error {
	return st.addTools(tools, true)
}

func (st *State) addTools(tools *tools.Tools, replace bool) error {
	doc := toolsToToolsDoc(tools)
	ops := []txn.Op{{
		C:      toolsC,
		Id:     doc.Id,
		Insert: &doc,
	}}
	if replace {
		ops = append(ops, txn.Op{
			C:  toolsC,
			Id: doc.Id,
			Update: bson.D{{
				"$set", bson.D{
					{"size", tools.Size},
					{"sha256", tools.SHA256},
					{"url", tools.URL},
				},
			}},
		})
	} else {
		ops[0].Assert = txn.DocMissing
	}
	err := st.runTransaction(ops)
	if err == txn.ErrAborted {
		return errors.AlreadyExistsf("%v tools", tools.Version)
	}
	return err
}

// AllTools returns the full list of tools in the catalogue.
// The URL fields will not be populated.
func (st *State) AllTools() (tools.List, error) {
	toolsCollection, closer := st.getCollection(toolsC)
	defer closer()
	var docs []toolsDoc
	if err := toolsCollection.Find(nil).All(&docs); err != nil {
		return nil, err
	}
	list := make(tools.List, len(docs))
	for i, doc := range docs {
		list[i] = toolsDocToTools(&doc)
	}
	return list, nil
}

func (st *State) MatchingTools(filter tools.Filter) (tools.List, error) {
	// TODO(axw) do this more efficiently
	all, err := st.AllTools()
	if err != nil {
		return nil, err
	}
	return all.Match(filter)
}

// Tools returns the *tools.Tools for the specified version
// if it exists, else an error satisfying errors.IsNotFound.
func (st *State) Tools(version version.Binary) (*tools.Tools, error) {
	toolsCollection, closer := st.getCollection(toolsC)
	defer closer()
	var doc toolsDoc
	err := toolsCollection.Find(bson.D{{"_id", version.String()}}).One(&doc)
	if err == mgo.ErrNotFound {
		return nil, errors.NotFoundf("%v tools", version)
	} else if err != nil {
		return nil, err
	}
	return toolsDocToTools(&doc), nil
}

func toolsDocToTools(doc *toolsDoc) *tools.Tools {
	return &tools.Tools{
		Version: doc.Version,
		Size:    doc.Size,
		SHA256:  doc.SHA256,
		URL:     doc.URL,
	}
}

func toolsToToolsDoc(tools *tools.Tools) *toolsDoc {
	return &toolsDoc{
		Id:      tools.Version.String(),
		Version: tools.Version,
		Size:    tools.Size,
		SHA256:  tools.SHA256,
		URL:     tools.URL,
	}
}
