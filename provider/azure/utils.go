// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"math/rand"
	"sync"
	"time"
)

const lowerAlpha = "abcdefghijklmnopqrstuvwxyz"
const digits = "0123456789"

var (
	randomStringMu   sync.Mutex
	randomStringRand *rand.Rand
)

func init() {
	randomStringRand = rand.New(
		rand.NewSource(time.Now().UnixNano()),
	)
}

func randomString(n int, validRunes []rune) string {
	randomStringMu.Lock()
	defer randomStringMu.Unlock()
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = validRunes[randomStringRand.Intn(len(validRunes))]
	}
	return string(runes)
}
