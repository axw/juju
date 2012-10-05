package trivial

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"launchpad.net/juju-core/thirdparty/pbkdf2"
)

var salt = []byte{0x75, 0x82, 0x81, 0xca}

// RandomBytes returns n random bytes.
func RandomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		return nil, fmt.Errorf("cannot read random bytes: %v", err)
	}
	return buf, nil
}

// PasswordHash returns base64-encoded one-way hash of the provided salt
// and password that is computationally hard to crack by iterating
// through possible passwords.
func PasswordHash(password string) string {
	// Generate 18 byte passwords because we know that MongoDB
	// uses the MD5 sum of the password anyway, so there's
	// no point in using more bytes. (18 so we don't get base 64
	// padding characters).
	h := pbkdf2.Key([]byte(password), salt, 8192, 18, sha512.New)
	return base64.StdEncoding.EncodeToString(h)
}
