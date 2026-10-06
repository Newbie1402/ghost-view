package config

import "testing"

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ADDR", "PROVIDER_MODE", "WEB_DIR", "PROVIDER_TIMEOUT", "DOWNLOAD_TIMEOUT", "SHUTDOWN_TIMEOUT", "DOWNLOAD_MAX_BYTES", "RATE_LIMIT_PER_MINUTE", "CACHE_MAX_ENTRIES", "CORS_ALLOWED_ORIGINS"} {
		t.Setenv(name, "")
	}
}
func TestLoadDefaultsAndOverrides(t *testing.T) {
	cleanEnv(t)
	c, err := Load()
	if err != nil || c.Addr != ":8080" || c.Mode != "live" || c.RateLimitPerMinute < 1 || c.DownloadMaxBytes < 1 {
		t.Fatalf("defaults %v %v", c, err)
	}
	t.Setenv("PROVIDER_MODE", "mock")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://ghostview.example,http://localhost:8080")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "50")
	c, err = Load()
	if err != nil || c.Mode != "mock" || len(c.AllowedOrigins) != 2 || c.RateLimitPerMinute != 50 {
		t.Fatalf("overrides %v %v", c, err)
	}
}
func TestLoadRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ name, value string }{{"ADDR", "invalid"}, {"PROVIDER_MODE", "scrape"}, {"PROVIDER_TIMEOUT", "0s"}, {"DOWNLOAD_TIMEOUT", "not-duration"}, {"SHUTDOWN_TIMEOUT", "10h"}, {"DOWNLOAD_MAX_BYTES", "0"}, {"RATE_LIMIT_PER_MINUTE", "-1"}, {"CACHE_MAX_ENTRIES", "0"}, {"CORS_ALLOWED_ORIGINS", "*"}, {"CORS_ALLOWED_ORIGINS", "https://user:secret@example.com"}, {"CORS_ALLOWED_ORIGINS", "https://example.com/path"}} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
