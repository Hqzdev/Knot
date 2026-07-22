package store

import "math"

const maxStoredOneTimePreKeys = 1000

func validOneTimePreKey(preKey OneTimePreKey) bool {
	if preKey.ID > math.MaxInt64 || len(preKey.PublicKey) != 32 {
		return false
	}
	for _, value := range preKey.PublicKey {
		if value != 0 {
			return true
		}
	}
	return false
}
