package cache

import "time"

type CacheStore interface {
	Get(key string) ([]byte, bool)
	SetWithTTL(key string, value []byte, ttl time.Duration)
	Delete(key string)
	Incr(key string, ttl time.Duration) (int64, error)
}
