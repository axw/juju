// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"crypto/rand"
	"crypto/subtle"
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

type credentialsHandler struct {
	ctxt httpContext
}

// ServeHTTP implements the http.Handler interface.
func (h *credentialsHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

func (h *credentialsHandler) processPost(req *http.Request, st *state.State) (*params.SecretKeyLoginResponse, error) {
	data, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	var loginRequest params.SecretKeyLoginRequest
	if err := json.Unmarshal(data, &loginRequest); err != nil {
		return nil, err
	}

	userTag, err := names.ParseUserTag(loginRequest.User)
	if err != nil {
		return nil, err
	}
	if len(loginRequest.Nonce) != 24 {
		return nil, errors.NotValidf("nonce")
	}

	user, err := st.User(userTag)
	if err != nil {
		return nil, err
	}

	if len(user.SecretKey()) != 32 {
		return nil, errors.Errorf("cannot obtain secret key")
	}
	var key [32]byte
	var nonce [24]byte
	copy(key[:], user.SecretKey())
	copy(nonce[:], loginRequest.Nonce)
	payloadBytes, ok := secretbox.Open(nil, []byte(loginRequest.PayloadCiphertext), &nonce, &key)
	if !ok {
		return nil, errors.NotValidf("payload")
	}

	var requestPayload params.SecretKeyLoginRequestPayload
	if err := json.Unmarshal(payloadBytes, &requestPayload); err != nil {
		return nil, errors.Trace(err)
	}
	if subtle.ConstantTimeCompare([]byte(requestPayload.SecretKey), key[:]) == 0 {
		return nil, errors.NotValidf("secret key")
	}

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
		Nonce:             string(nonce[:]),
		PayloadCiphertext: string(secretbox.Seal(nil, payloadBytes, &nonce, &key)),
	}
	return response, nil
}

func (h *credentialsHandler) getSecretKeyLoginResponsePayload(
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
func (h *credentialsHandler) sendError(w io.Writer, req *http.Request, err error) {
	if err != nil {
		logger.Errorf("returning error from %s %s: %s", req.Method, req.URL.Path, errors.Details(err))
	}
	sendJSON(w, &params.ErrorResult{
		Error: common.ServerError(err),
	})
}
