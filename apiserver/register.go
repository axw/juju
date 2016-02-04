// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"golang.org/x/crypto/nacl/secretbox"

	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/utils"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
)

// registerUserHandler is an http.Handler for the "/register" endpoint. This is
// used to complete a secure user registration process, and provide controller
// login credentials.
type registerUserHandler struct {
	ctxt httpContext
}

// ServeHTTP implements the http.Handler interface.
func (h *registerUserHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		sendError(w, errors.MethodNotAllowedf("unsupported method: %q", req.Method))
		return
	}
	st, err := h.ctxt.stateForRequestUnauthenticated(req)
	if err != nil {
		sendError(w, err)
		return
	}
	response, err := h.processPost(req, st)
	if err != nil {
		sendError(w, err)
		return
	}
	sendStatusAndJSON(w, http.StatusOK, response)
}

// The client will POST to the "/register" endpoint with a JSON-encoded
// params.SecretKeyLoginRequest. This contains the tag of the user they
// are registering, a (supposedly) unique nonce, and a ciphertext which
// is the result of concatenating the user and nonce values, and then
// encrypting and authenticating them with the NaCl Secretbox algorithm.
//
// If the server can decrypt the ciphertext, then it knows the client
// has the required secret key; thus they are authenticated. The client
// does not have the CA certificate for communicating securely with the
// server, and so must also authenticate the server. The server will
// similarly generate a unique nonce and encrypt the response payload
// using the same secret key as the client. If the client can decrypt
// the payload, it knows the server has the required secret key; thus
// it is also authenticated.
//
// NOTE(axw) it is important that the client and server choose their
// own nonces, because reusing a nonce means that the key-stream can
// be revealed.
func (h *registerUserHandler) processPost(req *http.Request, st *state.State) (*params.SecretKeyLoginResponse, error) {

	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	var loginRequest params.SecretKeyLoginRequest
	if err := json.Unmarshal(data, &loginRequest); err != nil {
		return nil, err
	}

	// Basic validation: ensure that the request contains a valid user tag,
	// nonce, and ciphertext of the expected length.
	userTag, err := names.ParseUserTag(loginRequest.User)
	if err != nil {
		return nil, err
	}
	if len(loginRequest.Nonce) != 24 {
		return nil, errors.NotValidf("nonce")
	}
	expectedPayloadBytes := append([]byte(loginRequest.User), loginRequest.Nonce...)
	if len(loginRequest.PayloadCiphertext) != len(expectedPayloadBytes)+secretbox.Overhead {
		return nil, errors.NotValidf("payload")
	}

	// Decrypt the ciphertext with the user's secret key (if it has one).
	user, err := st.User(userTag)
	if err != nil {
		return nil, err
	}
	if len(user.SecretKey()) != 32 {
		return nil, errors.NotFoundf("secret key for user %q", user.Name())
	}
	var key [32]byte
	var nonce [24]byte
	copy(key[:], user.SecretKey())
	copy(nonce[:], loginRequest.Nonce)
	payloadBytes, ok := secretbox.Open(nil, loginRequest.PayloadCiphertext, &nonce, &key)
	if !ok {
		// Cannot decrypt the ciphertext, which implies that the secret
		// key specified by the client is invalid.
		return nil, errors.NotValidf("secret key")
	}

	// Sanity check: payload should be concatenation of user tag and client
	// nonce. This is not necessary for authentication purposes, as the
	// Open call above will fail if the requester has the key.
	if !bytes.Equal(payloadBytes, expectedPayloadBytes) {
		return nil, errors.NotValidf("payload")
	}

	// Respond with the CA-cert and password, encrypted again with the
	// secret key.
	responsePayload, err := h.getSecretKeyLoginResponsePayload(st, user)
	if err != nil {
		return nil, errors.Trace(err)
	}
	payloadBytes, err = json.Marshal(responsePayload)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.Trace(err)
	}
	response := &params.SecretKeyLoginResponse{
		Nonce:             nonce[:],
		PayloadCiphertext: secretbox.Seal(nil, payloadBytes, &nonce, &key),
	}
	return response, nil
}

// getSecretKeyLoginResponsePayload generates a new password for the user, and
// then returns the information required by the client to login to the controller
// securely.
func (h *registerUserHandler) getSecretKeyLoginResponsePayload(
	st *state.State,
	user *state.User,
) (*params.SecretKeyLoginResponsePayload, error) {
	password, err := utils.RandomPassword()
	if err != nil {
		return nil, err
	}
	if err := user.SetPassword(password); err != nil {
		return nil, err
	}
	payload := params.SecretKeyLoginResponsePayload{
		CACert:   st.CACert(),
		Password: password,
	}
	return &payload, nil
}

// sendError sends a JSON-encoded error response.
func (h *registerUserHandler) sendError(w io.Writer, req *http.Request, err error) {
	if err != nil {
		logger.Errorf("returning error from %s %s: %s", req.Method, req.URL.Path, errors.Details(err))
	}
	sendJSON(w, &params.ErrorResult{Error: common.ServerError(err)})
}
