package util

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms; panic is acceptable.
		panic(err)
	}
	return b
}

// RandomId returns 8 random bytes as 16 hex chars (port of randomId).
func RandomId() string {
	return hex.EncodeToString(randomBytes(8))
}

// RandomToken returns n random bytes as hex (port of randomToken).
func RandomToken(n int) string {
	return hex.EncodeToString(randomBytes(n))
}

const passwordAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

// GeneratePassword builds a length-char password from random bytes using the
// same alphabet and modulo selection as the Node version.
func GeneratePassword(length int) string {
	raw := randomBytes(length * 2)
	var out strings.Builder
	for i := 0; i < len(raw) && out.Len() < length; i++ {
		out.WriteByte(passwordAlphabet[int(raw[i])%len(passwordAlphabet)])
	}
	return out.String()
}
