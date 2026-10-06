package httputil

import (
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterTime(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		header string
		want   time.Duration
	}{
		"missing": {"", time.Minute}, "invalid": {"invalid", time.Minute}, "zero": {"0", time.Minute}, "negative": {"-12", time.Minute}, "short": {"10", time.Minute}, "seconds": {"120", 2 * time.Minute}, "overflow": {"9223372036854775807", time.Minute}, "futuredate": {now.Add(10 * time.Minute).Format(http.TimeFormat), 10 * time.Minute}, "pastdate": {now.Add(-time.Hour).Format(http.TimeFormat), time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			got := RetryAfterTime(tc.header, now)
			if got.Sub(now) != tc.want {
				t.Fatalf("header %q duration%s want%s", tc.header, got.Sub(now), tc.want)
			}
		})
	}
}
