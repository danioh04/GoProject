// Package randutil provides cryptographically secure pseudo-random generators.
package randutil

import (
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
)

// Hex generates a cryptographically secure random hexadecimal string of 2*byteLen characters.
// If crypto/rand fails, it falls back to math/rand/v2 to strictly preserve hexadecimal format and length.
func Hex(byteLen int) string {
	if byteLen <= 0 {
		byteLen = 8
	}
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		for i := range b {
			b[i] = byte(mrand.Uint32())
		}
	}
	return hex.EncodeToString(b)
}
