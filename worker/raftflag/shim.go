// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package raftflag

import (
	"github.com/juju/errors"

	"github.com/juju/juju/worker"
)

// NewWorker calls NewFlagWorker but returns a more convenient type. It's
// a suitable default value for ManifoldConfig.NewWorker.
func NewWorker(config FlagConfig) (worker.Worker, error) {
	worker, err := NewFlagWorker(config)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return worker, nil
}
