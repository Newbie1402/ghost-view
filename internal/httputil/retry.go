package httputil

import (
	"net/http"
	"strconv"
	"time"
)

// RetryAfterTime respects a server cooldown and uses at least one minute when
// Retry-After is missing, invalid, or shorter. Integers cannot overflow Duration.
func RetryAfterTime(header string, now time.Time) time.Time {
	until := now.Add(time.Minute)
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil && seconds > 60 && seconds <= int64((1<<63-1)/time.Second) {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if date, err := http.ParseTime(header); err == nil && date.After(until) {
		return date
	}
	return until
}
