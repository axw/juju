// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package local

import (
	"github.com/juju/errors"

	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/storage"
)

const (
	localStorageProviderType = storage.ProviderType("local")
)

// localStorageProvider creates volume sources which use AWS EBS volumes.
type localStorageProvider struct{}

var _ storage.Provider = (*localStorageProvider)(nil)

// ValidateConfig is defined on the Provider interface.
func (e *localStorageProvider) ValidateConfig(providerConfig *storage.Config) error {
	return nil
}

// Supports is defined on the Provider interface.
func (e *localStorageProvider) Supports(k storage.StorageKind) bool {
	return k == storage.StorageKindFilesystem
}

// Scope is defined on the Provider interface.
func (e *localStorageProvider) Scope() storage.Scope {
	return storage.ScopeEnviron
}

// Dynamic is defined on the Provider interface.
func (e *localStorageProvider) Dynamic() bool {
	return false
}

// VolumeSource is defined on the Provider interface.
func (e *localStorageProvider) VolumeSource(environConfig *config.Config, providerConfig *storage.Config) (storage.VolumeSource, error) {
	// Volumes not supported.
	return nil, errors.NotSupportedf("volumes")
}

// FilesystemSource is defined on the Provider interface.
func (e *localStorageProvider) FilesystemSource(environConfig *config.Config, providerConfig *storage.Config) (storage.FilesystemSource, error) {
	// Dynamic storage is not supported; StartInstance will handle storage.
	return nil, errors.NotSupportedf("filesystems")
}
