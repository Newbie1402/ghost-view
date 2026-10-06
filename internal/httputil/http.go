package httputil

import (
	"encoding/json"
	"errors"
	"net/http"
)

type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *Error) Error() string                         { return e.Code }
func NewError(code, message string, status int) *Error { return &Error{code, message, status} }

var (
	NotFound            = NewError("PROFILE_NOT_FOUND", "Profile was not found.", 404)
	Private             = NewError("PRIVATE", "This profile is private. GhostView only displays publicly accessible content.", 403)
	Unavailable         = NewError("UNAVAILABLE", "This resource is unavailable. GhostView only displays publicly accessible content.", 503)
	Unsupported         = NewError("UNSUPPORTED_CAPABILITY", "This provider does not support this operation.", 422)
	Invalid             = NewError("INVALID_INPUT", "Enter a valid username or supported public profile URL.", 400)
	Timeout             = NewError("PROVIDER_TIMEOUT", "The provider did not respond in time. Please try again.", 504)
	RateLimited         = NewError("RATE_LIMITED", "Too many requests. Please try again later.", 429)
	DownloadUnavailable = NewError("DOWNLOAD_UNAVAILABLE", "This media is not available for download.", 404)
)

type Envelope struct {
	Data  any    `json:"data"`
	Error *Error `json:"error"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Data: data})
}
func Fail(w http.ResponseWriter, err error) {
	var safe *Error
	if !errors.As(err, &safe) {
		safe = NewError("INTERNAL_ERROR", "Something went wrong. Please try again.", 500)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(safe.HTTPStatus)
	_ = json.NewEncoder(w).Encode(Envelope{Error: safe})
}
