package randutil

import (
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
)

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
