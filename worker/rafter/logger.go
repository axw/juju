// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter

import (
	"bytes"

	"github.com/juju/loggo"
)

type loggoWriter struct {
	l loggo.Logger
}

// Write is part of the io.Writer interface.
func (w loggoWriter) Write(data []byte) (int, error) {
	w.l.Tracef("%s", bytes.TrimRight(data, "\n"))
	return len(data), nil
}
