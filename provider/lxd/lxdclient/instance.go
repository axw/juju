// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// +build go1.3

package lxdclient

import (
	"fmt"
	"net"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/utils/arch"
	"github.com/lxc/lxd/shared"

	"github.com/juju/juju/network"
)

// Constants related to user metadata.
const (
	MetadataNamespace = "user"

	// This is defined by the cloud-init code:
	// http://bazaar.launchpad.net/~cloud-init-dev/cloud-init/trunk/view/head:/cloudinit/sources/
	// http://cloudinit.readthedocs.org/en/latest/
	// Also see https://github.com/lxc/lxd/blob/master/specs/configuration.md.
	UserdataKey = "user-data"
)

func resolveConfigKey(name string, namespace ...string) string {
	parts := append(namespace, name)
	return strings.Join(parts, ".")
}

func splitConfigKey(key string) (string, string) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}
	return parts[0], parts[1]
}

// AliveStatuses are the LXD statuses that indicate a container is "alive".
var AliveStatuses = []string{
	// TODO(ericsnow) Also support StatusOK, StatusPending, and StatusThawed?
	StatusStarting,
	StatusStarted,
	StatusRunning,
}

// InstanceSpec holds all the information needed to create a new LXD
// container.
type InstanceSpec struct {
	// Name is the "name" of the instance.
	Name string

	// Image is the name of the image to use.
	Image string

	// ImageRemote identifies the remote to use for images. By default
	// the client's remote is used.
	ImageRemote string

	// Profiles are the names of the container profiles to apply to the
	// new container, in order.
	Profiles []string

	// Ephemeral indicates whether or not the container should be
	// destroyed when the LXD host is restarted.
	Ephemeral bool

	// Metadata is the instance metadata.
	Metadata map[string]string

	// TODO(ericsnow) Other possible fields:
	// Disks
	// Networks
	// Metadata
	// Tags
}

func (spec InstanceSpec) config() map[string]string {
	return resolveMetadata(spec.Metadata)
}

// InstanceHardware describes the hardware characteristics of a LXC container.
type InstanceHardware struct {
	// Architecture is the CPU architecture.
	Architecture string

	// NumCores is the number of CPU cores.
	NumCores uint

	// MemoryMB is the memory allocation for the container.
	MemoryMB uint

	// RootDiskMB is the size of the root disk, in MB.
	RootDiskMB uint64
}

// InstanceSummary captures all the data needed by Instance.
type InstanceSummary struct {
	// Name is the "name" of the instance.
	Name string

	// Status holds the status of the instance at a certain point in time.
	Status string

	// Hardware describes the instance's hardware characterstics.
	Hardware InstanceHardware

	// Metadata is the instance metadata.
	Metadata map[string]string

	// Addresses
	Addresses []network.Address
}

func newInstanceSummary(info *shared.ContainerInfo, state *shared.ContainerState) InstanceSummary {
	archStr, _ := shared.ArchitectureName(info.Architecture)
	archStr = arch.NormaliseArch(archStr)

	var numCores uint = 0 // default to all
	if raw := info.Config["limits.cpus"]; raw != "" {
		fmt.Sscanf(raw, "%d", &numCores)
	}

	var mem uint = 0 // default to all
	if raw := info.Config["limits.memory"]; raw != "" {
		fmt.Sscanf(raw, "%d", &mem)
	}

	var addrs []network.Address
	for _, info := range state.Ips {
		addr := network.NewAddress(info.Address)

		// Ignore loopback devices.
		// TODO(ericsnow) Move the loopback test to a network.Address method?
		ip := net.ParseIP(addr.Value)
		if ip != nil && ip.IsLoopback() {
			continue
		}

		addrs = append(addrs, addr)
	}

	// TODO(ericsnow) Factor this out into a function.
	statusStr := info.Status
	for status, code := range allStatuses {
		if info.StatusCode == code {
			statusStr = status
			break
		}
	}

	metadata := extractMetadata(info.Config)

	return InstanceSummary{
		Name:      info.Name,
		Status:    statusStr,
		Metadata:  metadata,
		Addresses: addrs,
		Hardware: InstanceHardware{
			Architecture: archStr,
			NumCores:     numCores,
			MemoryMB:     mem,
		},
	}
}

// Instance represents a single realized LXD container.
type Instance struct {
	InstanceSummary

	// spec is the InstanceSpec used to create this instance.
	spec *InstanceSpec
}

func newInstance(info *shared.ContainerInfo, state *shared.ContainerState, spec *InstanceSpec) *Instance {
	summary := newInstanceSummary(info, state)
	return NewInstance(summary, spec)
}

// NewInstance builds an instance from the provided summary and spec
// and returns it.
func NewInstance(summary InstanceSummary, spec *InstanceSpec) *Instance {
	if spec != nil {
		// Make a copy.
		val := *spec
		spec = &val
	}
	return &Instance{
		InstanceSummary: summary,
		spec:            spec,
	}
}

// Status returns a string identifying the status of the instance.
func (gi Instance) Status() string {
	return gi.InstanceSummary.Status
}

// CurrentStatus returns a string identifying the status of the instance.
func (gi Instance) CurrentStatus(client *Client) (string, error) {
	// TODO(ericsnow) Do this a better way?

	inst, err := client.Instance(gi.Name)
	if err != nil {
		return "", errors.Trace(err)
	}
	return inst.Status(), nil
}

// Metadata returns the user-specified metadata for the instance.
func (gi Instance) Metadata() map[string]string {
	// TODO*ericsnow) return a copy?
	return gi.InstanceSummary.Metadata
}

func resolveMetadata(metadata map[string]string) map[string]string {
	config := make(map[string]string)

	for name, val := range metadata {
		key := resolveConfigKey(name, MetadataNamespace)
		config[key] = val
	}

	return config
}

func extractMetadata(config map[string]string) map[string]string {
	metadata := make(map[string]string)

	for key, val := range config {
		namespace, name := splitConfigKey(key)
		if namespace != MetadataNamespace {
			continue
		}
		metadata[name] = val
	}

	return metadata
}
