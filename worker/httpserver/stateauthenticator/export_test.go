// Copyright 2014-2018 Canonical Ltd. All rights reserved.
// Licensed under the AGPLv3, see LICENCE file for details.

package stateauthenticator

import (
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/authentication"
)

// TODO update the tests moved from apiserver to test
// via the public interface, and then get rid of this.
func EntityAuthenticator(authenticator *Authenticator, tag names.Tag) (authentication.EntityAuthenticator, error) {
	return authenticator.authContext.authenticator("testing.invalid:1234").authenticatorForTag(tag)
}
