package filesystem

import "math/rand"

var randomId uint64

func init() {
	randomId = rand.Uint64()
}

func hashId(id uint64) uint64 {
	// obfuscate the id so user code can't rely on it
	return id ^ randomId
}
