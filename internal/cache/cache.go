package cache

import (
	"sync"
	"time"
)

type Cache interface {
	Get(string) ([]byte, bool)
	Set(string, []byte, time.Duration)
}
type entry struct {
	value   []byte
	expires time.Time
}
type Memory struct {
	mu      sync.Mutex
	entries map[string]entry
	max     int
}

func NewMemory(maxEntries int) *Memory {
	if maxEntries < 1 {
		maxEntries = 1000
	}
	return &Memory{entries: make(map[string]entry), max: maxEntries}
}
func (m *Memory) Get(key string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok {
		return nil, false
	}
	if !time.Now().Before(e.expires) {
		delete(m.entries, key)
		return nil, false
	}
	return append([]byte(nil), e.value...), true
}
func (m *Memory) Set(key string, value []byte, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.max {
		for k, e := range m.entries {
			if !time.Now().Before(e.expires) {
				delete(m.entries, k)
			}
		}
		if len(m.entries) >= m.max {
			for k := range m.entries {
				delete(m.entries, k)
				break
			}
		}
	}
	m.entries[key] = entry{append([]byte(nil), value...), time.Now().Add(ttl)}
}
