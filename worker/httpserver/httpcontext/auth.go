// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package httpcontext

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gopkg.in/juju/names.v2"
	"gopkg.in/macaroon.v1"

	"github.com/juju/juju/apiserver/params"
)

// LocalMacaroonAuthenticator extends Authenticator with a method of
// creating a local login macaroon. The authenticator is expected to
// honour the resulting macaroon.
type LocalMacaroonAuthenticator interface {
	Authenticator

	// CreateLocalLoginMacaroon creates a macaroon that may be
	// provided to a user as proof that they have logged in with
	// a valid username and password. This macaroon may then be
	// used to obtain a discharge macaroon so that the user can
	// log in without presenting their password for a set amount
	// of time.
	CreateLocalLoginMacaroon(names.UserTag) (*macaroon.Macaroon, error)
}

// Authenticator provides an interface for authenticating a request.
//
// TODO(axw) contract should include macaroon discharge error.
//
// If this returns an error, the handler should return StatusUnauthorized.
type Authenticator interface {
	// Authenticate authenticates the given request, returning the
	// auth info.
	//
	// If the request does not contain any authentication details,
	// then an error satisfying errors.IsNotFound will be returned.
	Authenticate(req *http.Request) (AuthInfo, error)

	// AuthenticateLoginRequest authenticates a LoginRequest.
	//
	// TODO(axw) we shouldn't be using params types here.
	AuthenticateLoginRequest(
		serverHost string,
		modelUUID string,
		req params.LoginRequest,
	) (AuthInfo, error)
}

// Authorizer is a function type for authorizing a request.
//
// If this returns an error, the handler should return StatusForbidden.
type Authorizer interface {
	Authorize(AuthInfo) error
}

// AuthorizerFunc is a function type implementing Authorizer.
type AuthorizerFunc func(AuthInfo) error

// Authorize is part of the Authorizer interface.
func (f AuthorizerFunc) Authorize(info AuthInfo) error {
	return f(info)
}

// AuthInfo is returned by Authenticator and RequestAuthInfo.
type AuthInfo struct {
	// Tag holds the tag of the authenticated entity.
	Tag names.Tag

	// LastConnection returns the time of the last connection for
	// the authenticated entity. If it's the zero value, then the
	// entity has not previously logged in.
	LastConnection time.Time

	// Controller reports whether or not the authenticated
	// entity is a controller agent.
	Controller bool
}

// BasicAuthHandler is an http.Handler that authenticates requests that
// it handles with a specified Authenticator. The auth details can later
// be retrieved using the top-level RequestAuthInfo function in this package.
type BasicAuthHandler struct {
	http.Handler

	// Authenticator is the Authenticator used for authenticating
	// the HTTP requests handled by this handler.
	Authenticator Authenticator

	// Authorizer, if non-nil, will be called with the auth info
	// returned by the Authenticator, to validate it for the route.
	Authorizer Authorizer
}

// ServeHTTP is part of the http.Handler interface.
func (h *BasicAuthHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	authInfo, err := h.Authenticator.Authenticate(req)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="juju"`)
		http.Error(w,
			fmt.Sprintf("authentication failed: %s", err),
			http.StatusUnauthorized,
		)
		return
	}
	if h.Authorizer != nil {
		if err := h.Authorizer.Authorize(authInfo); err != nil {
			http.Error(w,
				fmt.Sprintf("authorization failed: %s", err),
				http.StatusForbidden,
			)
			return
		}
	}
	ctx := context.WithValue(req.Context(), authInfoKey{}, authInfo)
	req = req.WithContext(ctx)
	h.Handler.ServeHTTP(w, req)
}

type authInfoKey struct{}

// RequestAuthInfo returns the AuthInfo associated with the request,
// if any, and a boolean indicating whether or not the request was
// authenticated.
func RequestAuthInfo(req *http.Request) (AuthInfo, bool) {
	authInfo, ok := req.Context().Value(authInfoKey{}).(AuthInfo)
	return authInfo, ok
}
