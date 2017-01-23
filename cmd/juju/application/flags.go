// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package application

import (
	"fmt"
	"strings"

	"github.com/juju/errors"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/storage"
)

type storageFlag struct {
	stores       *map[string]storage.Constraints
	bundleStores *map[string]map[string]storage.Constraints
}

// Set implements gnuflag.Value.Set.
func (f storageFlag) Set(s string) error {
	fields := strings.SplitN(s, "=", 2)
	if len(fields) < 2 {
		if f.bundleStores != nil {
			return errors.New("expected [<application>:]<store>=<constraints>")
		} else {
			return errors.New("expected <store>=<constraints>")
		}
	}
	var appName, storageName string
	if colon := strings.IndexRune(fields[0], ':'); colon >= 0 {
		if f.bundleStores == nil {
			return errors.New("expected <store>=<constraints>")
		}
		appName = fields[0][:colon]
		storageName = fields[0][colon+1:]
	} else {
		storageName = fields[0]
	}
	cons, err := storage.ParseConstraints(fields[1])
	if err != nil {
		return errors.Annotate(err, "cannot parse disk constraints")
	}
	var stores map[string]storage.Constraints
	if appName != "" {
		if *f.bundleStores == nil {
			*f.bundleStores = make(map[string]map[string]storage.Constraints)
		}
		stores = (*f.bundleStores)[appName]
		if stores == nil {
			stores = make(map[string]storage.Constraints)
			(*f.bundleStores)[appName] = stores
		}
	} else {
		if *f.stores == nil {
			*f.stores = make(map[string]storage.Constraints)
		}
		stores = *f.stores
	}
	stores[storageName] = cons
	return nil
}

// String implements gnuflag.Value.String.
func (f storageFlag) String() string {
	strs := make([]string, 0, len(*f.stores))
	for store, cons := range *f.stores {
		strs = append(strs, fmt.Sprintf("%s=%v", store, cons))
	}
	if f.bundleStores != nil {
		for application, stores := range *f.bundleStores {
			for store, cons := range stores {
				strs = append(strs, fmt.Sprintf("%s:%s=%v", application, store, cons))
			}
		}
	}
	return strings.Join(strs, " ")
}

type storageVolumesFlag struct {
	volumes *map[string][]names.VolumeTag
}

// Set implements gnuflag.Value.Set.
func (f storageVolumesFlag) Set(s string) error {
	fields := strings.SplitN(s, "=", 2)
	if len(fields) != 2 {
		return errors.New("expected <store>=<id>[,id...]")
	}
	storageName := fields[0]
	ids := strings.Split(fields[1], ",")
	tags := make([]names.VolumeTag, len(ids))
	for i, id := range ids {
		if !names.IsValidVolume(id) {
			return errors.NewNotValid(nil, fmt.Sprintf(
				"%q is not a valid volume ID", id,
			))
		}
		tags[i] = names.NewVolumeTag(id)
	}
	volumes := *f.volumes
	if volumes == nil {
		volumes = make(map[string][]names.VolumeTag)
		*f.volumes = volumes
	}
	volumes[storageName] = append(volumes[storageName], tags...)
	return nil
}

// String implements gnuflag.Value.String.
func (f storageVolumesFlag) String() string {
	strs := make([]string, 0, len(*f.volumes))
	for store, tags := range *f.volumes {
		ids := make([]string, len(tags))
		for i, tag := range tags {
			ids[i] = tag.Id()
		}
		strs = append(strs, fmt.Sprintf("%s=%v", store, strings.Join(ids, ",")))
	}
	return strings.Join(strs, " ")
}

// stringMap is a type that deserializes a CLI string using gnuflag's Value
// semantics.  It expects a name=value pair, and supports multiple copies of the
// flag adding more pairs, though the names must be unique.
type stringMap struct {
	mapping *map[string]string
}

// Set implements gnuflag.Value's Set method.
func (m stringMap) Set(s string) error {
	if *m.mapping == nil {
		*m.mapping = map[string]string{}
	}
	// make a copy so the following code is less ugly with dereferencing.
	mapping := *m.mapping

	vals := strings.SplitN(s, "=", 2)
	if len(vals) != 2 {
		return errors.NewNotValid(nil, "badly formatted name value pair: "+s)
	}
	name, value := vals[0], vals[1]
	if _, ok := mapping[name]; ok {
		return errors.Errorf("duplicate name specified: %q", name)
	}
	mapping[name] = value
	return nil
}

// String implements gnuflag.Value's String method
func (m stringMap) String() string {
	pairs := make([]string, 0, len(*m.mapping))
	for name, value := range *m.mapping {
		pairs = append(pairs, name+"="+value)
	}
	return strings.Join(pairs, ";")
}
