package cache

import (
	"sync"
	"testing"
	"time"
)

func TestMemoryTTLAndIsolation(t *testing.T) {
	c := NewMemory(2)
	value := []byte("public")
	c.Set("profile", value, time.Minute)
	value[0] = 'X'
	got, ok := c.Get("profile")
	if !ok || string(got) != "public" {
		t.Fatalf("stored value aliased: %q %v", got, ok)
	}
	got[0] = 'Y'
	got, _ = c.Get("profile")
	if string(got) != "public" {
		t.Fatal("returned value aliases cache")
	}
	c.Set("expired", []byte("old"), -time.Second)
	if _, ok := c.Get("expired"); ok {
		t.Fatal("expired entry returned")
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing entry returned")
	}
}

func TestMemoryBoundAndConcurrentAccess(t *testing.T) {
	c := NewMemory(2)
	for _, key := range []string{"a", "b", "c"} {
		c.Set(key, []byte(key), time.Minute)
	}
	count := 0
	for _, key := range []string{"a", "b", "c"} {
		if _, ok := c.Get(key); ok {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("capacity: got %d entries", count)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); c.Set("concurrent", []byte("ok"), time.Minute); c.Get("concurrent") }()
	}
	wg.Wait()
}
