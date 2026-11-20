package cache

import (
	"errors"
	"sync"
	"time"
)

type item struct {
	value   []byte
	expires time.Time
}
type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]item
	stop    chan struct{}
}

func NewMemoryStore(interval time.Duration) *MemoryStore {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s := &MemoryStore{entries: make(map[string]item), stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				s.mu.Lock()
				for k, v := range s.entries {
					if !v.expires.IsZero() && !now.Before(v.expires) {
						delete(s.entries, k)
					}
				}
				s.mu.Unlock()
			case <-s.stop:
				return
			}
		}
	}()
	return s
}
func (s *MemoryStore) Close() { close(s.stop) }
func (s *MemoryStore) Get(k string) ([]byte, bool) {
	s.mu.RLock()
	v, ok := s.entries[k]
	s.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !v.expires.IsZero() && time.Now().After(v.expires) {
		s.Delete(k)
		return nil, false
	}
	return append([]byte(nil), v.value...), true
}
func (s *MemoryStore) SetWithTTL(k string, v []byte, ttl time.Duration) {
	var expiry time.Time
	if ttl > 0 {
		expiry = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.entries[k] = item{append([]byte(nil), v...), expiry}
	s.mu.Unlock()
}
func (s *MemoryStore) Delete(k string) { s.mu.Lock(); delete(s.entries, k); s.mu.Unlock() }
func (s *MemoryStore) Incr(k string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := int64(0)
	if it, ok := s.entries[k]; ok {
		if !it.expires.IsZero() && time.Now().After(it.expires) {
			delete(s.entries, k)
		} else {
			for _, c := range it.value {
				if c < '0' || c > '9' {
					return 0, errors.New("cache value is not an integer")
				}
				n = n*10 + int64(c-'0')
			}
		}
	}
	n++
	expiry := time.Time{}
	if ttl > 0 {
		expiry = time.Now().Add(ttl)
	}
	s.entries[k] = item{[]byte(timeFormatInt(n)), expiry}
	return n, nil
}
func timeFormatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for n > 0 {
		b = append(b, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}
