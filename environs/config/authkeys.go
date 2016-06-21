// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package config

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/utils"
	"github.com/juju/utils/ssh"
)

const (
	// AuthKeysConfig is the configuration key for authorised keys.
	AuthKeysConfig = "authorized-keys"
	// JujuSystemKey is the SSH key comment for Juju system keys.
	JujuSystemKey = "juju-system-key"
)

var ErrNoAuthorizedKeys = errors.New("no public ssh keys found")

// ReadAuthorizedKeys implements the standard juju behaviour for finding
// authorized_keys. It returns a set of keys in in authorized_keys format
// (see sshd(8) for a description).  If path is non-empty, it names the
// file to use; otherwise the user's .ssh directory will be searched.
// Home directory expansion will be performed on the path if it starts with
// a ~; if the expanded path is relative, it will be interpreted relative
// to $HOME/.ssh.
//
// The result of utils/ssh.PublicKeyFiles will always be prepended to the
// result. In practice, this means ReadAuthorizedKeys never returns an
// error when the call originates in the CLI.
//
// If no SSH keys are found, ReadAuthorizedKeys returns
// ErrNoAuthorizedKeys.
func ReadAuthorizedKeys(path string) (string, error) {
	files := ssh.PublicKeyFiles()
	if path == "" {
		files = append(files, "id_dsa.pub", "id_rsa.pub", "identity.pub")
	} else {
		files = append(files, path)
	}
	var firstError error
	var keyData []byte
	for _, f := range files {
		f, err := utils.NormalizePath(f)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if !filepath.IsAbs(f) {
			f = filepath.Join(utils.Home(), ".ssh", f)
		}
		data, err := ioutil.ReadFile(f)
		if err != nil {
			if firstError == nil && !os.IsNotExist(err) {
				firstError = err
			}
			continue
		}
		keyData = append(keyData, bytes.Trim(data, "\n")...)
		keyData = append(keyData, '\n')
	}
	if len(keyData) == 0 {
		return "", firstError
	}
	return string(keyData), nil
}

// ConcatAuthKeys concatenates the two sets of authorised keys, interposing
// a newline if necessary, because authorised keys are newline-separated.
func ConcatAuthKeys(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	if a[len(a)-1] != '\n' {
		return a + "\n" + b
	}
	return a + b
}
