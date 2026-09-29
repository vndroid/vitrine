// Package auth implements the admin login: password hashes, sessions and
// the failed login throttle.
package auth

import (
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost of new hashes; lowered in tests.
var bcryptCost = 12

// MaxPasswordLength bounds the work spent on a login attempt.
const MaxPasswordLength = 1024

// emptySHA512 is the SHA512 of the empty string, the former h5fs preset
// that means "no password".
const emptySHA512 = "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"

var (
	sha512HexRe = regexp.MustCompile(`^[0-9a-fA-F]{128}$`)
	argon2Re    = regexp.MustCompile(`^\$(argon2id|argon2i)\$v=19\$m=(\d+),t=(\d+),p=(\d+)\$([A-Za-z0-9+/]+)\$([A-Za-z0-9+/]+)$`)
)

// LoginEnabled reports whether a usable password hash is configured:
// bcrypt or argon2 (as written by PHP's password_hash) or a SHA512 hex
// digest; empty or the former preset disable the login.
func LoginEnabled(hash string) bool {
	if hash == "" || strings.EqualFold(hash, emptySHA512) {
		return false
	}
	return isBcrypt(hash) || argon2Re.MatchString(hash) || sha512HexRe.MatchString(hash)
}

// IsEmptyPasswordHash reports whether hash is the SHA512 of the empty
// password, the former h5fs preset that disables the login.
func IsEmptyPasswordHash(hash string) bool {
	return strings.EqualFold(strings.TrimSpace(hash), emptySHA512)
}

func isBcrypt(hash string) bool {
	_, err := bcrypt.Cost([]byte(hash))
	return err == nil
}

// Verify checks a password against a hash in constant time.
func Verify(hash, password string) bool {
	if !LoginEnabled(hash) || len(password) > MaxPasswordLength {
		return false
	}
	switch {
	case isBcrypt(hash):
		// bcrypt only uses the first 72 bytes, like PHP
		if len(password) > 72 {
			password = password[:72]
		}
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	case strings.HasPrefix(hash, "$argon2"):
		return verifyArgon2(hash, password)
	default:
		sum := sha512.Sum512([]byte(password))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(strings.ToLower(hash))) == 1
	}
}

func verifyArgon2(hash, password string) bool {
	m := argon2Re.FindStringSubmatch(hash)
	if m == nil {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscan(m[2], &memory); err != nil {
		return false
	}
	if _, err := fmt.Sscan(m[3], &time); err != nil {
		return false
	}
	if _, err := fmt.Sscan(m[4], &threads); err != nil || threads == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(m[5])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(m[6])
	if err != nil || len(want) == 0 {
		return false
	}
	var got []byte
	if m[1] == "argon2id" {
		got = argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	} else {
		got = argon2.Key([]byte(password), salt, time, memory, threads, uint32(len(want)))
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Hash returns a bcrypt hash for the "passhash" option.
func Hash(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	if len(password) > 72 {
		return "", fmt.Errorf("password longer than 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	return string(h), err
}
