package gclient

import "time"

type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, data []byte, expiration time.Duration)
}
