// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import "launchpad.net/loggo"

var logger = loggo.GetLogger("juju.storage")

// BlockDeviceId is an identifier for block devices; either a device name
// (/dev/sdX) or a partition UUID.
type BlockDeviceId string
