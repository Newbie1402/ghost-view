package handler_test

import (
	"encoding/json"
	"ghostview/internal/cache"
	"ghostview/internal/config"
	"ghostview/internal/downloader"
	"ghostview/internal/handler"
	"ghostview/internal/model"
	"ghostview/internal/provider"
	"ghostview/internal/provider/mock"
	"ghostview/internal/service"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func app(t *testing.T, limit int) http.Handler {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>GhostView</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := service.New([]provider.SocialProvider{mock.New(model.Instagram), mock.New(model.TikTok), mock.New(model.Facebook)}, cache.NewMemory(100), 5*time.Millisecond)
	return handler.New(svc, downloader.New(time.Second, 1024*1024), config.Config{Mode: "mock", WebDir: dir, RateLimitPerMinute: limit, AllowedOrigins: []string{"https://ghostview.example"}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func execute(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	r.RemoteAddr = "198.51.100.5:4321"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHTTPStatusEnvelopeAndSecurityHeaders(t *testing.T) {
	h := app(t, 1000)
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/api/v1/health", 200, ""}, {"/api/v1/providers", 200, ""}, {"/api/v1/search?platform=instagram&q=alex", 200, ""}, {"/api/v1/profiles/instagram/alex.morgan", 200, ""}, {"/api/v1/profiles/instagram/private.user", 200, ""}, {"/api/v1/profiles/instagram/private.user/posts", 403, "PRIVATE"}, {"/api/v1/profiles/instagram/private.user/stories", 403, "PRIVATE"}, {"/api/v1/profiles/instagram/private.user/highlights", 403, "PRIVATE"}, {"/api/v1/profiles/instagram/missing", 404, "PROFILE_NOT_FOUND"}, {"/api/v1/profiles/instagram/unavailable", 503, "UNAVAILABLE"}, {"/api/v1/profiles/instagram/timeout", 504, "PROVIDER_TIMEOUT"}, {"/api/v1/profiles/instagram/rate.limited", 429, "RATE_LIMITED"}, {"/api/v1/profiles/unknown/alex", 400, "INVALID_INPUT"}, {"/api/v1/search?platform=instagram&q=https%3A%2F%2F127.0.0.1%2Fa", 400, "INVALID_INPUT"}, {"/api/v1/search?platform=instagram", 400, "INVALID_INPUT"}, {"/api/v1/search?platform=instagram&q=alex&q=other", 400, "INVALID_INPUT"}, {"/api/v1/search?platform=instagram&q=alex&url=https%3A%2F%2Fevil.example", 400, "INVALID_INPUT"}, {"/api/v1/profiles/instagram/alex.morgan/posts?cursor=invalid", 400, "INVALID_INPUT"}, {"/api/v1/media/instagram/unknown/download", 404, "DOWNLOAD_UNAVAILABLE"}, {"/api/v1/media/instagram/post-1/download?url=https%3A%2F%2F127.0.0.1", 400, "INVALID_INPUT"}, {"/api/v1/unknown", 404, "NOT_FOUND"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := execute(h, "GET", tc.path, nil)
			if w.Code != tc.status {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			var body struct {
				Data  json.RawMessage `json:"data"`
				Error *struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				if body.Error != nil || string(body.Data) == "null" {
					t.Fatalf("success envelope %s", w.Body.String())
				}
			} else if body.Error == nil || body.Error.Code != tc.code || string(body.Data) != "null" {
				t.Fatalf("failure envelope %s", w.Body.String())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'none'") || !strings.Contains(w.Header().Get("Permissions-Policy"), "camera=()") {
				t.Fatal("security headers missing")
			}
		})
	}
}

func TestHTTPDownloadsAndRequestLimits(t *testing.T) {
	h := app(t, 1000)
	w := execute(h, "GET", "/api/v1/media/instagram/post-1/download", nil)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(w.Body.String(), "GhostView coast abstract demo artwork") {
		t.Fatalf("download %d %s", w.Code, w.Body.String())
	}
	w = execute(h, "POST", "/api/v1/health", nil)
	if w.Code != 405 || !strings.Contains(w.Body.String(), "METHOD_NOT_ALLOWED") {
		t.Fatalf("method %d %s", w.Code, w.Body.String())
	}
	w = execute(h, "GET", "/api/v1/health", strings.NewReader(strings.Repeat("x", 4097)))
	if w.Code != 413 || !strings.Contains(w.Body.String(), "REQUEST_TOO_LARGE") {
		t.Fatalf("body bound %d", w.Code)
	}
	for _, tc := range []struct {
		name         string
		size, status int
	}{{"chunked-oversize", 4097, 413}, {"chunked-small", 32, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/v1/health", strings.NewReader(strings.Repeat("x", tc.size)))
			r.ContentLength = -1
			r.TransferEncoding = []string{"chunked"}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("unknown-length body status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status == 413 {
				var body struct {
					Data  json.RawMessage `json:"data"`
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || string(body.Data) != "null" || body.Error.Code != "REQUEST_TOO_LARGE" {
					t.Fatalf("unsafe size-limit envelope: %s", w.Body.String())
				}
			}
		})
	}
	w = execute(h, "GET", "/", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "GhostView") {
		t.Fatal("static home unavailable")
	}
	w = execute(h, "GET", "/.env", nil)
	if w.Code != 404 {
		t.Fatal("nonpublic static file exposed")
	}
}

func TestHTTPRateLimiterIgnoresForwardedSpoofing(t *testing.T) {
	h := app(t, 2)
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/api/v1/health", nil)
		r.RemoteAddr = "198.51.100.5:1234"
		r.Header.Set("X-Forwarded-For", string(rune('a'+i)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 200
		if i == 2 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("request %d got %d want %d", i, w.Code, want)
		}
		if i == 2 && w.Header().Get("Retry-After") == "" {
			t.Fatal("retry-after missing")
		}
	}
}

func TestHTTPCORS(t *testing.T) {
	h := app(t, 1000)
	for _, tc := range []struct {
		origin string
		status int
		allow  bool
	}{{"https://ghostview.example", 204, true}, {"https://evil.example", 403, false}, {"", 400, false}} {
		r := httptest.NewRequest("OPTIONS", "/api/v1/health", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("origin %q status %d", tc.origin, w.Code)
		}
		if (w.Header().Get("Access-Control-Allow-Origin") != "") != tc.allow {
			t.Fatalf("origin %q CORS mismatch", tc.origin)
		}
	}
}
