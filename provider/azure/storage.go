// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/juju/errors"
	"github.com/juju/schema"
	"github.com/juju/utils/set"
	"launchpad.net/gwacl"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/storage"
)

const (
	// volumeSizeMaxGiB is the maximum disk size (in gibibytes) for Azure disks.
	//
	// See: https://azure.microsoft.com/en-gb/documentation/articles/virtual-machines-disks-vhds/
	volumeSizeMaxGiB = 1023
)

// azureStorageProvider is a storage provider for Azure disks.
type azureStorageProvider struct {
	environProvider *azureEnvironProvider
}

var _ storage.Provider = (*azureStorageProvider)(nil)

var azureStorageConfigFields = schema.Fields{}

var azureStorageConfigChecker = schema.FieldMap(
	azureStorageConfigFields,
	schema.Defaults{},
)

type azureStorageConfig struct {
}

func newAzureStorageConfig(attrs map[string]interface{}) (*azureStorageConfig, error) {
	_, err := azureStorageConfigChecker.Coerce(attrs, nil)
	if err != nil {
		return nil, errors.Annotate(err, "validating Azure storage config")
	}
	azureStorageConfig := &azureStorageConfig{}
	return azureStorageConfig, nil
}

// ValidateConfig is defined on the Provider interface.
func (e *azureStorageProvider) ValidateConfig(cfg *storage.Config) error {
	_, err := newAzureStorageConfig(cfg.Attrs())
	return errors.Trace(err)
}

// Supports is defined on the Provider interface.
func (e *azureStorageProvider) Supports(k storage.StorageKind) bool {
	return k == storage.StorageKindBlock
}

// Scope is defined on the Provider interface.
func (e *azureStorageProvider) Scope() storage.Scope {
	return storage.ScopeEnviron
}

// Dynamic is defined on the Provider interface.
func (e *azureStorageProvider) Dynamic() bool {
	return true
}

// VolumeSource is defined on the Provider interface.
func (e *azureStorageProvider) VolumeSource(environConfig *config.Config, cfg *storage.Config) (storage.VolumeSource, error) {
	env, err := newEnviron(e.environProvider, environConfig)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return &azureVolumeSource{env}, nil
}

// FilesystemSource is defined on the Provider interface.
func (e *azureStorageProvider) FilesystemSource(
	environConfig *config.Config, providerConfig *storage.Config,
) (storage.FilesystemSource, error) {
	return nil, errors.NotSupportedf("filesystems")
}

type azureVolumeSource struct {
	env *azureEnviron
}

// CreateVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) CreateVolumes(params []storage.VolumeParams) (_ []storage.CreateVolumesResult, err error) {

	// First, validate the params before we use them.
	results := make([]storage.CreateVolumesResult, len(params))
	instanceIdSet := make(set.Strings)
	for i, p := range params {
		if err := v.ValidateVolumeParams(p); err != nil {
			results[i].Error = err
			continue
		}
		instanceIdSet.Add(string(p.Attachment.InstanceId))
	}
	if instanceIdSet.IsEmpty() {
		return results, nil
	}

	// Fetch all instances at once. Failure to find an instance should
	// only cause operations related to that instance to fail.
	uniqueInstanceIds := make([]instance.Id, instanceIdSet.Size())
	for i, id := range instanceIdSet.SortedValues() {
		uniqueInstanceIds[i] = instance.Id(id)
	}
	instances := make([]instance.Instance, len(params))
	uniqueInstances, err := v.env.instances(
		v.env.resourceGroup,
		uniqueInstanceIds,
		false, /* don't refresh addresses */
	)
	switch err {
	case nil, environs.ErrPartialInstances:
		for i, inst := range uniqueInstances {
			instanceId := uniqueInstanceIds[i]
			for i, p := range params {
				if p.Attachment.InstanceId != instanceId {
					continue
				}
				if inst != nil {
					instances[i] = inst
					continue
				}
				results[i].Error = errors.NotFoundf(
					"instance %v", instanceId,
				)
			}
		}
	case environs.ErrNoInstances:
		for i, p := range params {
			results[i].Error = errors.NotFoundf(
				"instance %v", p.Attachment.InstanceId,
			)
		}
	default:
		return nil, errors.Annotate(err, "getting instances")
	}

	// Update VirtualMachine objects in-memory,
	// and then perform the updates all at once.
	for i, p := range params {
		if results[i].Error != nil {
			continue
		}
		vm := &instances[i].(*azureInstance).VirtualMachine
		volume, volumeAttachment, err := v.createVolume(vm, p)
		if err != nil {
			// clear the instance so we don't try to update it later.
			instances[i] = nil
			results[i].Error = err
			continue
		}
		results[i].Volume = volume
		results[i].VolumeAttachment = volumeAttachment
	}

	// Update each instance once: track which ones we have
	// updated by removing them from instanceIdSet as we go.
	vmsClient := compute.VirtualMachinesClient{v.env.compute}
	for i, inst := range instances {
		if inst == nil {
			continue
		}
		instanceId := string(inst.Id())
		if !instanceIdSet.Contains(instanceId) {
			continue
		}
		instanceIdSet.Remove(instanceId)
		vm := &instances[i].(*azureInstance).VirtualMachine
		if _, err := vmsClient.CreateOrUpdate(v.env.resourceGroup, to.String(vm.Name), *vm); err != nil {
			results[i].Volume = nil
			results[i].VolumeAttachment = nil
			results[i].Error = err
			continue
		}
	}
	return results, nil
}

// createVolume updates the provided VirtualMachine's StorageProfile with the
// parameters for creating a new data disk. We don't actually interact with
// the Azure API until after all changes to the VirtualMachine are made.
func (v *azureVolumeSource) createVolume(
	vm *compute.VirtualMachine,
	p storage.VolumeParams,
) (*storage.Volume, *storage.VolumeAttachment, error) {

	lun, err := nextAvailableLUN(vm)
	if err != nil {
		return nil, nil, errors.Annotate(err, "choosing LUN")
	}

	dataDisksRoot := dataDiskVhdRoot(v.env.config.storageAccount)
	dataDiskName := p.Tag.String()
	vhdURI := dataDisksRoot + dataDiskName + ".vhd"

	sizeInGib := mibToGib(p.Size)
	dataDisk := compute.DataDisk{
		Lun:          to.IntPtr(lun),
		DiskSizeGB:   to.IntPtr(int(sizeInGib)),
		Name:         to.StringPtr(dataDiskName),
		Vhd:          &compute.VirtualHardDisk{to.StringPtr(vhdURI)},
		Caching:      compute.ReadWrite,
		CreateOption: compute.Empty,
	}

	var dataDisks []compute.DataDisk
	if vm.Properties.StorageProfile.DataDisks != nil {
		dataDisks = *vm.Properties.StorageProfile.DataDisks
	}
	dataDisks = append(dataDisks, dataDisk)
	vm.Properties.StorageProfile.DataDisks = &dataDisks

	// Data disks associate VHDs to machines. In Juju's storage model,
	// the VHD is the volume and the disk is the volume attachment.
	volume := storage.Volume{
		p.Tag,
		storage.VolumeInfo{
			VolumeId: dataDiskName,
			Size:     gibToMib(sizeInGib),
			// We don't currently support persistent volumes in
			// Azure, as it requires removal of "comp=media" when
			// deleting VMs, complicating cleanup.
			Persistent: true,
		},
	}
	volumeAttachment := storage.VolumeAttachment{
		p.Tag,
		p.Attachment.Machine,
		storage.VolumeAttachmentInfo{
			BusAddress: diskBusAddress(lun),
		},
	}
	return &volume, &volumeAttachment, nil
}

// ListVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) ListVolumes() ([]string, error) {
	/*
		disks, err := v.listDisks()
		if err != nil {
			return nil, errors.Trace(err)
		}
		volumeIds := make([]string, len(disks))
		for i, disk := range disks {
			_, volumeId := path.Split(disk.MediaLink)
			volumeIds[i] = volumeId
		}
		return volumeIds, nil
	*/
	return nil, nil
}

func (v *azureVolumeSource) listDisks() ([]gwacl.Disk, error) {
	/*
		disks, err := v.env.api.ListDisks()
		if err != nil {
			return nil, errors.Annotate(err, "listing disks")
		}
		mediaLinkPrefix := v.vhdMediaLinkPrefix()
		matching := make([]gwacl.Disk, 0, len(disks))
		for _, disk := range disks {
			if strings.HasPrefix(disk.MediaLink, mediaLinkPrefix) {
				matching = append(matching, disk)
			}
		}
		return matching, nil
	*/
	return nil, nil
}

// DescribeVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) DescribeVolumes(volIds []string) ([]storage.DescribeVolumesResult, error) {
	/*
		disks, err := v.listDisks()
		if err != nil {
			return nil, errors.Annotate(err, "listing disks")
		}

		byVolumeId := make(map[string]gwacl.Disk)
		for _, disk := range disks {
			_, volumeId := path.Split(disk.MediaLink)
			byVolumeId[volumeId] = disk
		}

		results := make([]storage.DescribeVolumesResult, len(volIds))
		for i, volumeId := range volIds {
			disk, ok := byVolumeId[volumeId]
			if !ok {
				results[i].Error = errors.NotFoundf("volume %v", volumeId)
				continue
			}
			results[i].VolumeInfo = &storage.VolumeInfo{
				VolumeId: volumeId,
				Size:     gibToMib(uint64(disk.LogicalSizeInGB)),
				// We don't support persistent volumes at the moment;
				// see CreateVolumes.
				Persistent: false,
			}
		}

		return results, nil
	*/
	return nil, nil
}

// DestroyVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) DestroyVolumes(volIds []string) ([]error, error) {
	// We don't currently support persistent volumes.
	return nil, errors.NotSupportedf("DestroyVolumes")
}

// ValidateVolumeParams is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) ValidateVolumeParams(params storage.VolumeParams) error {
	if mibToGib(params.Size) > volumeSizeMaxGiB {
		return errors.Errorf(
			"%d GiB exceeds the maximum of %d GiB",
			mibToGib(params.Size),
			volumeSizeMaxGiB,
		)
	}
	return nil
}

// AttachVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) AttachVolumes(attachParams []storage.VolumeAttachmentParams) ([]storage.AttachVolumesResult, error) {
	/*
		// We don't currently support persistent volumes, but we do need to
		// support "reattaching" volumes to machines; i.e. just verify that
		// the attachment is in place, and fail otherwise.

		type maybeRole struct {
			role *gwacl.PersistentVMRole
			err  error
		}

		roles := make(map[instance.Id]maybeRole)
		for _, p := range attachParams {
			if _, ok := roles[p.InstanceId]; ok {
				continue
			}
			role, err := v.getRole(p.InstanceId)
			roles[p.InstanceId] = maybeRole{role, err}
		}

		results := make([]storage.AttachVolumesResult, len(attachParams))
		for i, p := range attachParams {
			maybeRole := roles[p.InstanceId]
			if maybeRole.err != nil {
				results[i].Error = maybeRole.err
				continue
			}
			volumeAttachment, err := v.attachVolume(p, maybeRole.role)
			if err != nil {
				results[i].Error = err
				continue
			}
			results[i].VolumeAttachment = volumeAttachment
		}
		return results, nil
	*/
	return nil, nil
}

func (v *azureVolumeSource) attachVolume(
	p storage.VolumeAttachmentParams,
	role *gwacl.PersistentVMRole,
) (*storage.VolumeAttachment, error) {
	/*

		var disks []gwacl.DataVirtualHardDisk
		if role.DataVirtualHardDisks != nil {
			disks = *role.DataVirtualHardDisks
		}

		// Check if the disk is already attached to the machine.
		mediaLinkPrefix := v.vhdMediaLinkPrefix()
		for _, disk := range disks {
			if !strings.HasPrefix(disk.MediaLink, mediaLinkPrefix) {
				continue
			}
			_, volumeId := path.Split(disk.MediaLink)
			if volumeId != p.VolumeId {
				continue
			}
			return &storage.VolumeAttachment{
				p.Volume,
				p.Machine,
				storage.VolumeAttachmentInfo{
					BusAddress: diskBusAddress(disk.LUN),
				},
			}, nil
		}
	*/

	// If the disk is not attached already, the AttachVolumes call must
	// fail. We do not support persistent volumes at the moment, and if
	// we get here it means that the disk has been detached out of band.
	return nil, errors.NotSupportedf("attaching volumes")
}

// DetachVolumes is specified on the storage.VolumeSource interface.
func (v *azureVolumeSource) DetachVolumes(attachParams []storage.VolumeAttachmentParams) ([]error, error) {
	// We don't currently support persistent volumes.
	return nil, errors.NotSupportedf("detaching volumes")
}

func nextAvailableLUN(vm *compute.VirtualMachine) (int, error) {
	// Pick the smallest LUN not in use. We have to choose them in order,
	// or the disks don't show up.
	var inUse [32]bool
	if vm.Properties.StorageProfile.DataDisks != nil {
		for _, disk := range *vm.Properties.StorageProfile.DataDisks {
			lun := to.Int(disk.Lun)
			if lun < 0 || lun > 31 {
				logger.Warningf("ignore disk with invalid LUN: %+v", disk)
				continue
			}
			inUse[lun] = true
		}
	}
	for i, inUse := range inUse {
		if !inUse {
			return i, nil
		}
	}
	return -1, errors.New("all LUNs are in use")
}

// diskBusAddress returns the value to use in the BusAddress field of
// VolumeAttachmentInfo for a disk with the specified LUN.
func diskBusAddress(lun int) string {
	return fmt.Sprintf("scsi@5:0.0.%d", lun)
}

// mibToGib converts mebibytes to gibibytes.
// AWS expects GiB, we work in MiB; round up
// to nearest GiB.
func mibToGib(m uint64) uint64 {
	return (m + 1023) / 1024
}

// gibToMib converts gibibytes to mebibytes.
func gibToMib(g uint64) uint64 {
	return g * 1024
}
