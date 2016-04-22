// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package lxd

import (
	"path"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/schema"

	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/storage"
)

const (
	hostfsProviderType = storage.ProviderType("host")
)

const (
	hostfsConfigAttrSource = "source"
)

// hostfsProvider creates filesystem sources which mount the host filesystem
// into LXD containers.
type hostfsProvider struct{}

var _ storage.Provider = (*hostfsProvider)(nil)

var hostfsConfigFields = schema.Fields{
	hostfsConfigAttrSource: schema.String(),
}

var hostfsConfigChecker = schema.FieldMap(
	hostfsConfigFields,
	schema.Defaults{},
)

type hostfsConfig struct {
	// source is the path on the host machine that will be mounted
	// into the LXD container.
	source string
}

func newHostfsConfig(attrs map[string]interface{}) (*hostfsConfig, error) {
	out, err := hostfsConfigChecker.Coerce(attrs, nil)
	if err != nil {
		return nil, errors.Annotate(err, "validating hostfs config")
	}
	coerced := out.(map[string]interface{})
	hostfsConfig := &hostfsConfig{
		source: coerced[hostfsConfigAttrSource].(string),
	}
	return hostfsConfig, nil
}

// ValidateConfig is defined on the Provider interface.
func (e *hostfsProvider) ValidateConfig(cfg *storage.Config) error {
	_, err := newHostfsConfig(cfg.Attrs())
	return errors.Annotatef(err, "validating %s storage config", hostfsProviderType)
}

// Supports is defined on the Provider interface.
func (e *hostfsProvider) Supports(k storage.StorageKind) bool {
	return k == storage.StorageKindFilesystem
}

// Scope is defined on the Provider interface.
func (e *hostfsProvider) Scope() storage.Scope {
	return storage.ScopeEnviron
}

// Dynamic is defined on the Provider interface.
func (e *hostfsProvider) Dynamic() bool {
	return true
}

// VolumeSource is defined on the Provider interface.
func (e *hostfsProvider) VolumeSource(environConfig *config.Config, cfg *storage.Config) (storage.VolumeSource, error) {
	return nil, errors.NotSupportedf("volumes")
}

// FilesystemSource is defined on the Provider interface.
func (e *hostfsProvider) FilesystemSource(environConfig *config.Config, providerConfig *storage.Config) (storage.FilesystemSource, error) {
	env, err := providerInstance.Open(environConfig)
	if err != nil {
		return nil, errors.Trace(err)
	}
	// TODO(axw) providerConfig should contain the pool
	// config, but currently does not. We need to fix that.
	// Ideally we'd construct the config here and then
	// we wouldn't need to encode the source in the
	// filesystem IDs.
	source := &hostfsFilesystemSource{env.(*environ)}
	return source, nil
}

type hostfsFilesystemSource struct {
	env *environ
}

var _ storage.FilesystemSource = (*hostfsFilesystemSource)(nil)

// CreateFilesystems is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) CreateFilesystems(params []storage.FilesystemParams) (_ []storage.CreateFilesystemsResult, err error) {
	results := make([]storage.CreateFilesystemsResult, len(params))
	for i, p := range params {
		if results[i].Error != nil {
			continue
		}
		filesystem, err := v.createFilesystem(p)
		if err != nil {
			results[i].Error = err
			continue
		}
		results[i].Filesystem = filesystem
	}
	return results, nil
}

func (v *hostfsFilesystemSource) createFilesystem(p storage.FilesystemParams) (*storage.Filesystem, error) {
	cfg, err := newHostfsConfig(p.Attributes)
	if err != nil {
		return nil, errors.Trace(err)
	}
	filesystem := storage.Filesystem{
		p.Tag,
		p.Volume,
		storage.FilesystemInfo{
			FilesystemId: path.Join(p.Tag.String(), cfg.source),
			// NOTE(axw) ideally we would report the size of the filesystem
			// that contains source path here, but that would mean we'd have
			// to attach the filesystem to the controller. It's probably not
			// worth it, so we just report 0 for now.
		},
	}
	return &filesystem, nil
}

// ListFilesystems is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) ListFilesystems() ([]string, error) {
	// TODO(axw) list filesystems by listing all containers, and then
	// listing all devices with the prefix "juju:filesystem".
	return []string{}, nil
}

// DescribeFilesystems is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) DescribeFilesystems(fsIds []string) ([]storage.DescribeFilesystemsResult, error) {
	results := make([]storage.DescribeFilesystemsResult, len(fsIds))
	for i, id := range fsIds {
		results[i].FilesystemInfo = &storage.FilesystemInfo{
			FilesystemId: id,
		}
	}
	return results, nil
}

// DestroyFilesystems is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) DestroyFilesystems(fsIds []string) ([]error, error) {
	// This is a no-op.
	return make([]error, len(fsIds)), nil
}

// ValidateFilesystemParams is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) ValidateFilesystemParams(params storage.FilesystemParams) error {
	return nil
}

// AttachFilesystems is specified on the storage.FilesystemSource interface.
func (v *hostfsFilesystemSource) AttachFilesystems(attachParams []storage.FilesystemAttachmentParams) ([]storage.AttachFilesystemsResult, error) {
	results := make([]storage.AttachFilesystemsResult, len(attachParams))
	for i, p := range attachParams {
		result, err := v.attachFilesystem(p)
		if err != nil {
			results[i].Error = err
			continue
		}
		results[i].FilesystemAttachment = result
	}
	return results, nil
}

func (s *hostfsFilesystemSource) attachFilesystem(attachParams storage.FilesystemAttachmentParams) (*storage.FilesystemAttachment, error) {
	// Extract the source location from the filesystem ID.
	//
	// path.Split is inappropriate here, because we want to split at the
	// first "/", not at the last one.
	var source string
	if pos := strings.IndexRune(attachParams.FilesystemId, '/'); pos != -1 {
		source = attachParams.FilesystemId[pos:]
	} else {
		source = "/"
	}
	// TODO(axw) don't raise an error if the filesystem is already attached.
	err := s.env.raw.AddHostMount(
		string(attachParams.InstanceId),
		attachParams.FilesystemId,
		source, attachParams.Path,
	)
	if err != nil {
		return nil, errors.Annotate(err, "mounting filesystem")
	}
	return &storage.FilesystemAttachment{
		attachParams.Filesystem,
		attachParams.Machine,
		storage.FilesystemAttachmentInfo{Path: attachParams.Path},
	}, nil
}

// DetachFilesystems is specified on the storage.FilesystemSource interface.
func (s *hostfsFilesystemSource) DetachFilesystems(detachParams []storage.FilesystemAttachmentParams) ([]error, error) {
	errs := make([]error, len(detachParams))
	for i, p := range detachParams {
		errs[i] = s.detachFilesystem(p)
	}
	return errs, nil
}

func (s *hostfsFilesystemSource) detachFilesystem(detachParams storage.FilesystemAttachmentParams) error {
	// TODO(axw) check what happens if we try to
	// do this twice, and ignore whatever that is.
	return s.env.raw.RemoveHostMount(
		string(detachParams.InstanceId),
		detachParams.FilesystemId,
	)
}
