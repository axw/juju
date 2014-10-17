// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package instance

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/juju/errors"
)

const (
	storageNameSnippet    = "[a-zA-Z][a-zA-Z0-9]*"
	storageSourceSnippet  = "[a-zA-Z][a-zA-Z0-9]*"
	storageCountSnippet   = "-?[0-9]+"
	storageSizeSnippet    = "-?[0-9]+(?:\\.[0-9]+)?[MGTP]?"
	storageOptionsSnippet = ".*"
)

// ErrStorageSourceMissing is an error that is returned from ParseStorage
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

// Storage defines a storage specification for creating storage.
// Storage consists of a required source, an optional count and
// size (count defaults to 1 if size is provided), and source-specific
// options.
type Storage struct {
	// Name is the storage name. This is not unique per storage
	// instance, but identifies a charm storage desire.
	Name string

	// Source is the storage source.
	Source string

	// Count is the number of instances of the storage to create.
	Count int

	// Size is the size of the storage in MiB.
	//
	// For some types of storage, it is not meaningful to specify
	// size (e.g. an NFS share); for others it is mandatory
	// (e.g. an EBS volume).
	Size uint64

	// Options is source-specific options for storage creation.
	Options string
}

func (s *Storage) String() string {
	return fmt.Sprintf("%s:%s:%dx%d:%s", s.Name, s.Source, s.Count, s.Size, s.Options)
}

// ParseStorage attempts to parse the specified string and create a
// corresponding Storage structure.
//
// The acceptable format for storage specifications is:
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
func ParseStorage(s string) (*Storage, error) {
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
	sizeptr, err := parseSize(match[4])
	if err != nil {
		return nil, err
	}
	var count int
	size := *sizeptr
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

	storage := Storage{
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

// MustParseStorage attempts to parse the specified string and create
// a corresponding Storage structure, panicking if an error occurs.
func MustParseStorage(s string) *Storage {
	storage, err := ParseStorage(s)
	if err != nil {
		panic(err)
	}
	return storage
}
