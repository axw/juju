// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/juju/errors"
	"github.com/juju/utils"
)

const (
	storageNameSnippet    = "[a-zA-Z][a-zA-Z0-9-_.]*"
	storageSourceSnippet  = "[a-zA-Z][a-zA-Z0-9]*"
	storageCountSnippet   = "-?[0-9]+"
	storageSizeSnippet    = "-?[0-9]+(?:\\.[0-9]+)?[MGTP]?"
	storageOptionsSnippet = ".*"

	// ProviderSource is a special storage source name that
	// means that Juju should use the storage source of the
	// the current environment's provider.
	ProviderSource = "provider"
)

// ErrStorageSourceMissing is an error that is returned from ParseDirective
// if the source is unspecified.
var ErrStorageSourceMissing = fmt.Errorf("storage source missing")

var storageRE = regexp.MustCompile(
	"^" +
		"(?:(" + storageNameSnippet + ")=)?" +
		"(?:(" + storageSourceSnippet + "):)?" +
		"(?:(" + storageCountSnippet + ")x)?" +
		"(" + storageSizeSnippet + ")?" +
		"(" + storageOptionsSnippet + ")?" +
		"$",
)

// Directive is a storage creation specification.
type Directive struct {
	// Name is the storage name. This is not unique per storage
	// instance, but is user-defined and associated with storage.
	//
	// Name is required.
	Name string

	// Source is the storage source (ebs, cinder, nfs, ...).
	//
	// Source is required.
	Source string

	// Count is the number of instances of the store to acquire.
	//
	// Count is optional. Count will default to 1 if a size is
	// specified, otherwise it will default to 0.
	Count int

	// Size is the size of the storage in MiB.
	//
	// Size's optionality depends on the storage source. For some
	// types of storage (e.g. an NFS share), it is not meaningful
	// to specify a size; for others, it is necessary (e.g. EBS).
	Size uint64

	// Persistent indicates that the storage should be made persistent
	// beyond the lifetime of the instance it is attached to. The user
	// must explicitly remove or disown (from the environment) the store
	// before the environment can be cleanly destroyed.
	//
	// Persistent cannot be directly specified by the user; it is
	// set by Juju when combining a directive and a charm storage
	// specification.
	Persistent bool

	// TODO(axw) shared

	// Options is source-specific options for storage creation.
	Options string
}

func (s *Directive) String() string {
	return fmt.Sprintf(
		"%s:%s:%dx%d,persistent=%v,%s",
		s.Name,
		s.Source,
		s.Count,
		s.Size,
		s.Persistent,
		s.Options,
	)
}

// ParseDirective attempts to parse the string and create a
// corresponding Directive structure.
//
// The acceptable format for storage directives is:
//    NAME=SOURCE:[[COUNTx]SIZE][,OPTIONS]
// where
//    NAME is an identifier for storage instances; multiple
//    instances may share the same storage name. NAME can be a
//    string starting with a letter of the alphabet, followed
//    by zero or more alpha-numeric characters.
//
//    SOURCE identifies the storage source. SOURCE can be a
//    string starting with a letter of the alphabet, followed
//    by zero or more alpha-numeric characters.
//
//    COUNT is a decimal number indicating how many instances
//    of the storage to create. If count is unspecified and a
//    size is specified, 1 is assumed.
//
//    SIZE is a floating point number and optional multiplier from
//    the set (M, G, T, P), which are all treated as powers of 1024.
//
//    OPTIONS is the string remaining the colon (if any) that will
//    be passed onto the storage source unmodified.
func ParseDirective(s string) (*Directive, error) {
	match := storageRE.FindStringSubmatch(s)
	if match == nil {
		return nil, errors.Errorf("failed to parse storage %q", s)
	}
	if match[1] == "" {
		return nil, errors.New("storage name missing")
	}
	if match[2] == "" {
		return nil, ErrStorageSourceMissing
	}

	var size uint64
	var count int
	var err error
	if match[4] != "" {
		size, err = utils.ParseSize(match[4])
		if err != nil {
			return nil, errors.Annotate(err, "failed to parse size")
		}
	}
	options := match[5]

	if size > 0 {
		// Don't bother parsing count unless we have a size too.
		if count, err = parseStorageCount(match[3]); err != nil {
			return nil, err
		}

		// Size was specified, so options must be preceded by a ",".
		if options != "" {
			if options[0] != ',' {
				return nil, errors.Errorf(
					"invalid trailing data %q: options must be preceded by ',' when size is specified",
					options,
				)
			}
			options = options[1:]
		}
	}

	storage := Directive{
		Name:    match[1],
		Source:  match[2],
		Count:   count,
		Size:    size,
		Options: options,
	}
	return &storage, nil
}

func parseStorageCount(count string) (int, error) {
	if count == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return -1, err
	}
	if n <= 0 {
		return -1, errors.New("count must be a positive integer")
	}
	return n, nil
}

// MustParseDirective attempts to parse the string and create a
// corresponding Directive structure, panicking if an error occurs.
func MustParseDirective(s string) *Directive {
	storage, err := ParseDirective(s)
	if err != nil {
		panic(err)
	}
	return storage
}
