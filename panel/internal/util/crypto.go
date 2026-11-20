package util

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

// scrypt parameters match Node's crypto.scryptSync defaults so hashes are
// byte-compatible with existing settings.json files.
const (
	scryptN = 16384
	scryptR = 8
	scryptP = 1
)

// scryptKey feeds the salt as the ASCII bytes of its hex STRING (not decoded),
// exactly like Node scryptSync(password, saltHexString, keylen).
func scryptKey(password, saltHex string, keyLen int) ([]byte, error) {
	return scrypt.Key([]byte(password), []byte(saltHex), scryptN, scryptR, scryptP, keyLen)
}

// HashPassword returns "scrypt$<salt16hex>$<hash64hex>".
func HashPassword(password string) string {
	salt := hex.EncodeToString(randomBytes(16))
	dk, err := scryptKey(password, salt, 64)
	if err != nil {
		// Constant valid parameters never fail.
		panic(err)
	}
	return "scrypt$" + salt + "$" + hex.EncodeToString(dk)
}

// VerifyPassword checks a password against a stored scrypt hash in constant time.
func VerifyPassword(password, stored string) bool {
	if stored == "" {
		return false
	}
	parts := strings.Split(stored, "$")
	if len(parts) < 3 {
		return false
	}
	alg, salt, hashHex := parts[0], parts[1], parts[2]
	if alg != "scrypt" || salt == "" || hashHex == "" {
		return false
	}
	expected, err := hex.DecodeString(hashHex)
	if err != nil {
		return false
	}
	actual, err := scryptKey(password, salt, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func hmacSha256(key, msg string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

// SignSession returns "<expMs>.<base64url-unpadded HMAC-SHA256(secret, expMs)>".
// The HMAC key is the secret STRING bytes, matching Node createHmac('sha256', secret).
func SignSession(secret string, ttlMs int64) string {
	exp := time.Now().UnixMilli() + ttlMs
	expStr := strconv.FormatInt(exp, 10)
	sig := base64.RawURLEncoding.EncodeToString(hmacSha256(secret, expStr))
	return expStr + "." + sig
}

// VerifySession validates a session token against the secret.
func VerifySession(token, secret string) bool {
	if token == "" {
		return false
	}
	dot := strings.IndexByte(token, '.')
	if dot <= 0 {
		return false
	}
	sig := token[dot+1:]
	exp, err := strconv.ParseInt(token[:dot], 10, 64)
	if err != nil {
		return false
	}
	if exp <= time.Now().UnixMilli() {
		return false
	}
	// Node recomputes the HMAC over String(Number(prefix)) and compares the
	// base64url STRINGS byte-wise; replicate both behaviors.
	expectedSig := base64.RawURLEncoding.EncodeToString(hmacSha256(secret, strconv.FormatInt(exp, 10)))
	return hmac.Equal([]byte(sig), []byte(expectedSig))
}
