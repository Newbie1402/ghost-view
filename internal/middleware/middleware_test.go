package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLimiterExpiryCapacityAndIndependentIPs(t *testing.T) {
	l := NewLimiter(1)
	if !l.Allow("a") || l.Allow("a") || !l.Allow("b") {
		t.Fatal("per-IP limit incorrect")
	}
	l.buckets["a"] = bucket{count: 1, expires: time.Now().Add(-time.Second)}
	if !l.Allow("a") {
		t.Fatal("expired limit did not reset")
	}
	for i := 0; i < 10000; i++ {
		l.buckets[string(rune(i))] = bucket{count: 1, expires: time.Now().Add(time.Minute)}
	}
	if l.Allow("new-client") {
		t.Fatal("unbounded limiter state")
	}
}
func TestObserveRecoversWithSafeError(t *testing.T) {
	h := Observe(slog.New(slog.NewTextHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret token") }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || w.Body.String() == "" {
		t.Fatalf("panic response %d %s", w.Code, w.Body.String())
	}
}
