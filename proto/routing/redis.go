package routing

import (
	"encoding/base64"
	"errors"
)

const (
	ConnectionTTL    = 45
	connectionPrefix = "knot:gateway:connection:"
	shardPrefix      = "knot:gateway:shard:"
)

func ConnectionKey(userID string, deviceID string) string {
	return connectionPrefix + base64.RawURLEncoding.EncodeToString([]byte(userID)) + ":" + base64.RawURLEncoding.EncodeToString([]byte(deviceID))
}

func ShardChannel(shard string) (string, error) {
	if !ValidShard(shard) {
		return "", errors.New("invalid gateway shard")
	}
	return shardPrefix + shard, nil
}

func ValidShard(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character == '-' || character == '_' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z') {
			return false
		}
	}
	return true
}
