package usage

import (
	"crypto/rand"
	"encoding/hex"
)

const SystemUserID uint64 = 1

func NewRequestID(prefix string) string {
	random := make([]byte, 12)
	_, _ = rand.Read(random)
	return prefix + "_" + hex.EncodeToString(random)
}
