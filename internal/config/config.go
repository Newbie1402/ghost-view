package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string
	Mode               string
	WebDir             string
	ProviderTimeout    time.Duration
	DownloadTimeout    time.Duration
	DownloadMaxBytes   int64
	RateLimitPerMinute int
	MaxCacheEntries    int
	AllowedOrigins     []string
	ShutdownTimeout    time.Duration
}

func Load() (Config, error) {
	c := Config{Addr: env("ADDR", ":8080"), Mode: env("PROVIDER_MODE", "live"), WebDir: env("WEB_DIR", "web"), ProviderTimeout: 8 * time.Second, DownloadTimeout: 20 * time.Second, DownloadMaxBytes: 25 << 20, RateLimitPerMinute: 120, MaxCacheEntries: 1000, ShutdownTimeout: 10 * time.Second}
	if c.Mode != "mock" && c.Mode != "live" {
		return c, fmt.Errorf("PROVIDER_MODE must be mock or live")
	}
	if _, _, e := net.SplitHostPort(c.Addr); e != nil {
		return c, fmt.Errorf("ADDR must be a host:port address")
	}
	for name, target := range map[string]*time.Duration{"PROVIDER_TIMEOUT": &c.ProviderTimeout, "DOWNLOAD_TIMEOUT": &c.DownloadTimeout, "SHUTDOWN_TIMEOUT": &c.ShutdownTimeout} {
		if v := os.Getenv(name); v != "" {
			d, e := time.ParseDuration(v)
			if e != nil || d < time.Millisecond || d > 5*time.Minute {
				return c, fmt.Errorf("invalid %s", name)
			}
			*target = d
		}
	}
	for name, target := range map[string]*int{"RATE_LIMIT_PER_MINUTE": &c.RateLimitPerMinute, "CACHE_MAX_ENTRIES": &c.MaxCacheEntries} {
		if v := os.Getenv(name); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 1000000 {
				return c, fmt.Errorf("invalid %s", name)
			}
			*target = n
		}
	}
	if v := os.Getenv("DOWNLOAD_MAX_BYTES"); v != "" {
		n, e := strconv.ParseInt(v, 10, 64)
		if e != nil || n < 1 || n > 100<<20 {
			return c, fmt.Errorf("invalid DOWNLOAD_MAX_BYTES")
		}
		c.DownloadMaxBytes = n
	}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		for _, origin := range strings.Split(v, ",") {
			origin = strings.TrimSpace(origin)
			u, e := url.Parse(origin)
			if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
				return c, fmt.Errorf("invalid CORS_ALLOWED_ORIGINS")
			}
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	return c, nil
}
func env(name, defaultValue string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return defaultValue
}
