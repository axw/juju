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
	storageProviderSnippet = "[a-zA-Z][a-zA-Z0-9]*"
	storageCountSnippet    = "-?[0-9]+"
	//storageSizeSnippet     = "[:digit:]+(?:[.][:digit:]+)?[MGTP]?"
	storageSizeSnippet    = "-?[0-9]+(?:\\.[0-9]+)?[MGTP]?"
	storageOptionsSnippet = ".*"
)

// ErrStorageProviderMissing is an error that is returned from ParseStorage
// if the provider is unspecified.
var ErrStorageProviderMissing = fmt.Errorf("storage provider missing")

var storageRE = regexp.MustCompile(
	"^" +
		"(?:(" + storageProviderSnippet + "):)?" +
		"(?:(" + storageCountSnippet + ")x)?" +
		"(" + storageSizeSnippet + ")" +
		"(?::(" + storageOptionsSnippet + "))?" +
		"$",
)

// Storage defines a storage specification for creating storage.
// Storage consists of a required provider type, a size with optional
// count (defaulting to 1), and provider-specific options.
type Storage struct {
	// Provider is the storage provider type.
	Provider string

	// Count is the number of instances of the storage to create.
	Count int

	// Size is the size of the storage in MiB.
	Size uint64

	// Options is provider-specific options for storage creation.
	Options string
}

func (s *Storage) String() string {
	return fmt.Sprintf("%s:%dx%d:%s", s.Provider, s.Count, s.Size, s.Options)
}

// ParseStorage attempts to parse the specified string and create a
// corresponding Storage structure.
//
// The acceptable format for storage specifications is:
//    PROVIDER:[COUNTx]SIZE[:OPTIONS]
// where
//    PROVIDER is a string starting with a letter of the alphabet,
//    followed by zero or more alpha-numeric characters.
//
//    COUNT is a decimal number indicating how many instances
//    of the storage to create. If unspecified, 1 is assumed.
//
//    SIZE is a floating point number and optional multiplier from
//    the set (M, G, T, P), which are all treated as powers of 1024.
//
//    OPTIONS is the string remaining the colon (if any) that will
//    be passed onto the storage provider unmodified.
func ParseStorage(s string) (*Storage, error) {
	match := storageRE.FindStringSubmatch(s)
	if match == nil {
		return nil, errors.Errorf("failed to parse storage %q", s)
	}
	if match[1] == "" {
		return nil, ErrStorageProviderMissing
	}
	count, err := parseStorageCount(match[2])
	if err != nil {
		return nil, err
	}
	size, err := parseSize(match[3])
	if err != nil {
		return nil, err
	}
	storage := Storage{
		Provider: match[1],
		Count:    count,
		Size:     *size,
		Options:  match[4],
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
