// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package lease

import (
	"fmt"
	"strings"
	"time"

	"github.com/juju/errors"

	"github.com/juju/juju/core/lease"
)

// These constants define the field names and type values used by documents in
// a lease collection. They *must* remain in sync with the bson marshalling
// annotations in leaseDoc and clockDoc.
const (
	// field* identify the fields in a leaseDoc.
	fieldNamespace = "namespace"
	fieldHolder    = "holder"
	fieldDuration  = "duration"
	fieldProcess   = "process"
	fieldWriter    = "writer"
	fieldVersion   = "version"
)

// leaseDocId returns the _id for the document holding details of the supplied
// namespace and lease.
func leaseDocId(namespace, lease string) string {
	return fmt.Sprintf("%s#%s#", namespace, lease)
}

// leaseDoc is used to serialise lease entries.
type leaseDoc struct {
	// DocID is always "<Namespace>#<Name>#", so that we can extract useful
	// information from a stream of watcher events without incurring extra
	// DB hits.
	//
	// Apart from checking validity on load, though, there's little reason
	// to use Id elsewhere; Namespace and Name are the sources of truth.
	DocID     string `bson:"_id"`
	Namespace string `bson:"namespace"`
	Name      string `bson:"name"`

	// Holder, Expiry, and Version map directly to entry.
	Holder   string `bson:"holder"`
	Duration int64  `bson:"duration"`
	Version  string `bson:"version"`

	// Writer XXX
	Writer string `bson:"writer"`
}

// validate returns an error if any fields are invalid or inconsistent.
func (doc leaseDoc) validate() error {
	// state.multiModelRunner prepends environ ids in our documents, and
	// state.modelStateCollection does not strip them out.
	if !strings.HasSuffix(doc.DocID, leaseDocId(doc.Namespace, doc.Name)) {
		return errors.Errorf("inconsistent _id")
	}
	if err := lease.ValidateString(doc.Holder); err != nil {
		return errors.Annotatef(err, "invalid holder")
	}
	if doc.Duration == 0 {
		return errors.Errorf("invalid duration")
	}
	if err := lease.ValidateString(doc.Writer); err != nil {
		return errors.Annotatef(err, "invalid writer")
	}
	if doc.Version == "" {
		return errors.Errorf("invalid document version")
	}
	return nil
}

// newLeaseDoc returns a valid lease document encoding the supplied lease and
// entry in the supplied namespace, or an error.
func newLeaseDoc(namespace, name, holder, version, writer string, duration time.Duration) (*leaseDoc, error) {
	doc := &leaseDoc{
		DocID:     leaseDocId(namespace, name),
		Namespace: namespace,
		Name:      name,

		Holder:   holder,
		Duration: int64(duration),
		Writer:   writer,
		Version:  version,
	}
	if err := doc.validate(); err != nil {
		return nil, errors.Trace(err)
	}
	return doc, nil
}
