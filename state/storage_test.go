// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/featureflag"
	gc "gopkg.in/check.v1"
	"gopkg.in/mgo.v2"

	"github.com/juju/juju/feature"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/testing"
	"github.com/juju/juju/storage/poolmanager"
	"github.com/juju/juju/storage/provider"
	"github.com/juju/juju/storage/provider/registry"
)

type StorageStateSuite struct {
	ConnSuite
}

var _ = gc.Suite(&StorageStateSuite{})

func (s *StorageStateSuite) SetUpTest(c *gc.C) {
	s.ConnSuite.SetUpTest(c)

	// This suite is all about storage, so enable the feature by default.
	s.PatchEnvironment(osenv.JujuFeatureFlagEnvKey, feature.Storage)
	featureflag.SetFlagsFromEnvironment(osenv.JujuFeatureFlagEnvKey)

	// Create a default pool for block devices.
	pm := poolmanager.New(state.NewStateSettings(s.State))
	_, err := pm.Create("block", provider.LoopProviderType, map[string]interface{}{})
	c.Assert(err, jc.ErrorIsNil)
	registry.RegisterEnvironStorageProviders("someprovider", provider.LoopProviderType)
}

func (s *StorageStateSuite) storageInstanceExists(c *gc.C, tag names.StorageTag) bool {
	_, err := state.TxnRevno(
		s.State,
		state.StorageInstancesC,
		state.DocID(s.State, tag.Id()),
	)
	if err != nil {
		c.Assert(err, gc.Equals, mgo.ErrNotFound)
		return false
	}
	return true
}

func (s *StorageStateSuite) setupSingleStorage(c *gc.C) (*state.Service, *state.Unit, names.StorageTag) {
	ch := s.AddTestingCharm(c, "storage-block")
	storage := map[string]state.StorageConstraints{
		"data": makeStorageCons("block", 1024, 1),
	}
	service := s.AddTestingServiceWithStorage(c, "storage-block", ch, storage)
	unit, err := service.AddUnit()
	c.Assert(err, jc.ErrorIsNil)
	storageTag := names.NewStorageTag("data/0")
	return service, unit, storageTag
}

func makeStorageCons(pool string, size, count uint64) state.StorageConstraints {
	return state.StorageConstraints{Pool: pool, Size: size, Count: count}
}

func (s *StorageStateSuite) TestAddServiceStorageConstraintsWithoutFeature(c *gc.C) {
	// Disable the storage feature, and ensure we can deploy a service from
	// a charm that defines storage, without specifying the storage constraints.
	s.PatchEnvironment(osenv.JujuFeatureFlagEnvKey, "")
	featureflag.SetFlagsFromEnvironment(osenv.JujuFeatureFlagEnvKey)

	ch := s.AddTestingCharm(c, "storage-block2")
	service, err := s.State.AddService("storage-block2", "user-test-admin@local", ch, nil, nil)
	c.Assert(err, jc.ErrorIsNil)
	storageConstraints, err := service.StorageConstraints()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(storageConstraints, gc.HasLen, 0)
}

func (s *StorageStateSuite) TestAddServiceStorageConstraintsValidation(c *gc.C) {
	ch := s.AddTestingCharm(c, "storage-block2")
	addService := func(storage map[string]state.StorageConstraints) (*state.Service, error) {
		return s.State.AddService("storage-block2", "user-test-admin@local", ch, nil, storage)
	}
	assertErr := func(storage map[string]state.StorageConstraints, expect string) {
		_, err := addService(storage)
		c.Assert(err, gc.ErrorMatches, expect)
	}
	assertErr(nil, `.*no constraints specified for store.*`)

	storageCons := map[string]state.StorageConstraints{
		"multi1to10": makeStorageCons("block", 1024, 1),
		"multi2up":   makeStorageCons("block", 1024, 1),
	}
	assertErr(storageCons, `cannot add service "storage-block2": charm "storage-block2" store "multi2up": 2 instances required, 1 specified`)
	storageCons["multi2up"] = makeStorageCons("block", 1024, 2)
	storageCons["multi1to10"] = makeStorageCons("block", 1024, 11)
	assertErr(storageCons, `cannot add service "storage-block2": charm "storage-block2" store "multi1to10": at most 10 instances supported, 11 specified`)
	storageCons["multi1to10"] = makeStorageCons("ebs", 1024, 10)
	assertErr(storageCons, `cannot add service "storage-block2": pool "ebs" not found`)
	storageCons["multi1to10"] = makeStorageCons("block", 1024, 10)
	_, err := addService(storageCons)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *StorageStateSuite) assertAddServiceStorageConstraintsDefaults(c *gc.C, pool string, cons, expect map[string]state.StorageConstraints) {
	if pool != "" {
		err := s.State.UpdateEnvironConfig(map[string]interface{}{
			"storage-default-block-source": pool,
		}, nil, nil)
		c.Assert(err, jc.ErrorIsNil)
	}
	ch := s.AddTestingCharm(c, "storage-block")
	service, err := s.State.AddService("storage-block2", "user-test-admin@local", ch, nil, cons)
	c.Assert(err, jc.ErrorIsNil)
	savedCons, err := service.StorageConstraints()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(savedCons, jc.DeepEquals, expect)
	// TODO(wallyworld) - test pool name stored in data model
}

func (s *StorageStateSuite) TestAddServiceStorageConstraintsDefaultPool(c *gc.C) {
	storageCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("", 2048, 1),
	}
	expectedCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("block", 2048, 1),
	}
	s.assertAddServiceStorageConstraintsDefaults(c, "block", storageCons, expectedCons)
}

func (s *StorageStateSuite) TestAddServiceStorageConstraintsNoUserDefaultPool(c *gc.C) {
	storageCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("", 2048, 1),
	}
	expectedCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("loop", 2048, 1),
	}
	s.assertAddServiceStorageConstraintsDefaults(c, "", storageCons, expectedCons)
}

func (s *StorageStateSuite) TestAddServiceStorageConstraintsDefaultSize(c *gc.C) {
	storageCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("block", 0, 1),
	}
	expectedCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("block", 1024, 1),
	}
	s.assertAddServiceStorageConstraintsDefaults(c, "block", storageCons, expectedCons)
}

func (s *StorageStateSuite) TestProviderFallbackToType(c *gc.C) {
	ch := s.AddTestingCharm(c, "storage-block")
	addService := func(storage map[string]state.StorageConstraints) (*state.Service, error) {
		return s.State.AddService("storage-block", "user-test-admin@local", ch, nil, storage)
	}
	storageCons := map[string]state.StorageConstraints{
		"data": makeStorageCons("loop", 1024, 1),
	}
	_, err := addService(storageCons)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *StorageStateSuite) TestAddUnit(c *gc.C) {
	err := s.State.UpdateEnvironConfig(map[string]interface{}{
		"storage-default-block-source": "block",
	}, nil, nil)
	c.Assert(err, jc.ErrorIsNil)

	// Each unit added to the service will create storage instances
	// to satisfy the service's storage constraints.
	ch := s.AddTestingCharm(c, "storage-block2")
	storage := map[string]state.StorageConstraints{
		"multi1to10": makeStorageCons("", 1024, 1),
		"multi2up":   makeStorageCons("block", 1024, 2),
	}
	service := s.AddTestingServiceWithStorage(c, "storage-block2", ch, storage)
	for i := 0; i < 2; i++ {
		u, err := service.AddUnit()
		c.Assert(err, jc.ErrorIsNil)
		storageAttachments, err := s.State.StorageAttachments(u.UnitTag())
		c.Assert(err, jc.ErrorIsNil)
		count := make(map[string]int)
		for _, att := range storageAttachments {
			c.Assert(att.Unit(), gc.Equals, u.UnitTag())
			storageInstance, err := s.State.StorageInstance(att.StorageInstance())
			c.Assert(err, jc.ErrorIsNil)
			count[storageInstance.StorageName()]++
			c.Assert(storageInstance.Kind(), gc.Equals, state.StorageKindBlock)
		}
		c.Assert(count, gc.DeepEquals, map[string]int{
			"multi1to10": 1,
			"multi2up":   2,
		})
		// TODO(wallyworld) - test pool name stored in data model
	}
}

func (s *StorageStateSuite) TestUnitEnsureDead(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)
	// destroying a unit with storage attachments is fine; this is what
	// will trigger the death and removal of storage attachments.
	err := u.Destroy()
	c.Assert(err, jc.ErrorIsNil)
	// until all storage attachments are removed, the unit cannot be
	// marked as being dead.
	assertUnitEnsureDeadError := func() {
		err = u.EnsureDead()
		c.Assert(err, gc.ErrorMatches, "unit has storage attachments")
	}
	assertUnitEnsureDeadError()
	err = s.State.EnsureStorageAttachmentDead(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	assertUnitEnsureDeadError()
	err = s.State.DestroyStorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)
	assertUnitEnsureDeadError()
	err = s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	err = u.EnsureDead()
	c.Assert(err, jc.ErrorIsNil)
}

func (s *StorageStateSuite) TestRemoveStorageAttachmentsRemovesDyingInstance(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)

	// Mark the storage instance as Dying, so that it will be removed
	// when the last attachment is removed.
	err := s.State.DestroyStorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)

	si, err := s.State.StorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(si.Life(), gc.Equals, state.Dying)

	err = s.State.EnsureStorageAttachmentDead(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	err = s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	exists := s.storageInstanceExists(c, storageTag)
	c.Assert(exists, jc.IsFalse)
}

func (s *StorageStateSuite) TestConcurrentDestroyStorageInstanceRemoveStorageAttachmentsRemovesInstance(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)

	defer state.SetBeforeHooks(c, s.State, func() {
		err := s.State.EnsureStorageAttachmentDead(storageTag, u.UnitTag())
		c.Assert(err, jc.ErrorIsNil)
		err = s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
		c.Assert(err, jc.ErrorIsNil)
	}).Check()

	// Destroying the instance should check that there are no concurrent
	// changes to the storage instance's attachments, and recompute
	// operations if there are.
	err := s.State.DestroyStorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)

	exists := s.storageInstanceExists(c, storageTag)
	c.Assert(exists, jc.IsFalse)
}

func (s *StorageStateSuite) TestConcurrentRemoveStorageAttachment(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)

	err := s.State.DestroyStorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)

	ensureDead := func() {
		err = s.State.EnsureStorageAttachmentDead(storageTag, u.UnitTag())
		c.Assert(err, jc.ErrorIsNil)
	}
	remove := func() {
		err = s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
		c.Assert(err, jc.ErrorIsNil)
	}

	defer state.SetBeforeHooks(c, s.State, ensureDead, remove).Check()
	ensureDead()
	remove()
	exists := s.storageInstanceExists(c, storageTag)
	c.Assert(exists, jc.IsFalse)
}

func (s *StorageStateSuite) TestRemoveAliveStorageAttachmentError(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)

	err := s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
	c.Assert(err, gc.ErrorMatches, "cannot remove storage attachment data/0:storage-block/0: storage attachment is not dead")

	attachments, err := s.State.StorageAttachments(u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(attachments, gc.HasLen, 1)
	c.Assert(attachments[0].StorageInstance(), gc.Equals, storageTag)
}

func (s *StorageStateSuite) TestConcurrentDestroyInstanceRemoveStorageAttachmentsRemovesInstance(c *gc.C) {
	_, u, storageTag := s.setupSingleStorage(c)

	defer state.SetBeforeHooks(c, s.State, func() {
		// Concurrently mark the storage instance as Dying,
		// so that it will be removed when the last attachment
		// is removed.
		err := s.State.DestroyStorageInstance(storageTag)
		c.Assert(err, jc.ErrorIsNil)
	}, nil).Check()

	// Removing the attachment should check that there are no concurrent
	// changes to the storage instance's life, and recompute operations
	// if it does.
	err := s.State.EnsureStorageAttachmentDead(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	err = s.State.RemoveStorageAttachment(storageTag, u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	exists := s.storageInstanceExists(c, storageTag)
	c.Assert(exists, jc.IsFalse)
}

func (s *StorageStateSuite) TestConcurrentDestroyStorageInstance(c *gc.C) {
	_, _, storageTag := s.setupSingleStorage(c)

	defer state.SetBeforeHooks(c, s.State, func() {
		err := s.State.DestroyStorageInstance(storageTag)
		c.Assert(err, jc.ErrorIsNil)
	}).Check()

	err := s.State.DestroyStorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)

	si, err := s.State.StorageInstance(storageTag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(si.Life(), gc.Equals, state.Dying)
}

func (s *StorageStateSuite) TestWatchStorageAttachments(c *gc.C) {
	ch := s.AddTestingCharm(c, "storage-block2")
	storage := map[string]state.StorageConstraints{
		"multi1to10": makeStorageCons("block", 1024, 1),
		"multi2up":   makeStorageCons("block", 1024, 2),
	}
	service := s.AddTestingServiceWithStorage(c, "storage-block2", ch, storage)
	u, err := service.AddUnit()
	c.Assert(err, jc.ErrorIsNil)

	w := s.State.WatchStorageAttachments(u.UnitTag())
	defer testing.AssertStop(c, w)
	wc := testing.NewStringsWatcherC(c, s.State, w)
	wc.AssertChange("multi1to10/0", "multi2up/1", "multi2up/2")
	wc.AssertNoChange()

	err = s.State.DestroyStorageAttachment(names.NewStorageTag("multi2up/1"), u.UnitTag())
	c.Assert(err, jc.ErrorIsNil)
	wc.AssertChange("multi2up/1")
	wc.AssertNoChange()
}

// TODO(axw) StorageAttachments can't be added to Dying StorageInstance
// TODO(axw) StorageInstance without attachments is removed by Destroy
// TODO(axw) StorageInstance becomes Dying when Unit becomes Dying
// TODO(axw) concurrent add-unit and StorageAttachment removal does not
//           remove storage instance.
