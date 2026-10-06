package middleware

import (
	"errors"
	"ghostview/internal/httputil"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func Security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' https://*.tiktokcdn.com https://*.tiktokcdn-us.com https://*.cdninstagram.com https://*.fbcdn.net; media-src 'self'; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func CORS(origins []string, next http.Handler) http.Handler {
	allow := make(map[string]bool)
	for _, o := range origins {
		allow[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if allow[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")
			} else if r.Method == http.MethodOptions {
				httputil.Fail(w, httputil.NewError("CORS_DENIED", "This origin is not permitted.", 403))
				return
			}
		}
		if r.Method == http.MethodOptions {
			if origin == "" {
				httputil.Fail(w, httputil.Invalid)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type bucket struct {
	count   int
	expires time.Time
}
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]bucket
	limit   int
}

func NewLimiter(limit int) *Limiter {
	if limit < 1 {
		limit = 120
	}
	return &Limiter{buckets: make(map[string]bucket), limit: limit}
}
func (l *Limiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[ip]
	if !ok && len(l.buckets) >= 10000 {
		for k, v := range l.buckets {
			if !now.Before(v.expires) {
				delete(l.buckets, k)
			}
		}
		if len(l.buckets) >= 10000 {
			return false
		}
	}
	if !now.Before(b.expires) {
		b = bucket{expires: now.Add(time.Minute)}
	}
	if b.count >= l.limit {
		return false
	}
	b.count++
	l.buckets[ip] = b
	return true
}
func (l *Limiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			ip, _, e := net.SplitHostPort(r.RemoteAddr)
			if e != nil {
				ip = r.RemoteAddr
			}
			if !l.Allow(ip) {
				w.Header().Set("Retry-After", "60")
				httputil.Fail(w, httputil.RateLimited)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func Limits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 4096 {
			httputil.Fail(w, httputil.NewError("REQUEST_TOO_LARGE", "Request body is too large.", 413))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			w.Header().Set("Allow", "GET, HEAD, OPTIONS")
			httputil.Fail(w, httputil.NewError("METHOD_NOT_ALLOWED", "This method is not supported.", 405))
			return
		}
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				httputil.Fail(w, httputil.NewError("REQUEST_TOO_LARGE", "Request body is too large.", 413))
			} else {
				httputil.Fail(w, httputil.Invalid)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(200)
	}
	return r.ResponseWriter.Write(b)
}
func Observe(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		record := &recorder{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				logger.Error("request panic")
				httputil.Fail(record, httputil.NewError("INTERNAL_ERROR", "Something went wrong. Please try again.", 500))
			}
			status := record.status
			if status == 0 {
				status = 200
			}
			logger.Info("request", "method", r.Method, "status", strconv.Itoa(status), "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(record, r)
	})
}
