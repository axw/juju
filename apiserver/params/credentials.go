// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package params

type SecretKeyLoginRequest struct {
	// User is the tag-representation of the user that the
	// requester wishes to authenticate as.
	User string `json:"user"`

	// Nonce is the nonce used by the client to encrypt
	// and authenticate PayloadCiphertext.
	Nonce string `json:"nonce"`

	// PayloadCiphertext is the encrypted and authenticated payload,
	// which is a JSON-encoded SecretKeyLoginRequestPayload.
	PayloadCiphertext string `json:"ciphertext"`
}

type SecretKeyLoginResponse struct {
	// Nonce is the nonce used by the server to encrypt and
	// authenticate PayloadCiphertext.
	Nonce string `json:"nonce"`

	// PayloadCiphertext is the encrypted and authenticated payload,
	// which is a JSON-encoded SecretKeyLoginResponsePayload.
	PayloadCiphertext string `json:"ciphertext"`
}

// SecretKeyLoginRequestPayload contains the secret key, which the
// requester must provide in order to prove its identity.
type SecretKeyLoginRequestPayload struct {
	SecretKey string `json:"secret-key"`
}

// SecretKeyLoginResponsePayload is JSON-encoded and then encrypted
// and authenticated with the NaCl crypto_secretbox algorithm.
type SecretKeyLoginResponsePayload struct {
	Password string `json:"password"`
	CACert   string `json:"ca-cert"`
}
