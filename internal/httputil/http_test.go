package httputil

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONEscapesProviderText(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, 200, map[string]string{"bio": "<script>alert(1)</script>"})
	if w.Code != 200 || strings.Contains(w.Body.String(), "<script>") {
		t.Fatal("provider text not escaped")
	}
	var body Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != nil || body.Data == nil {
		t.Fatalf("envelope %v %v", body, err)
	}
}
func TestFailSuppressesInternalDetails(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, errors.New("password=secret; cookie=session; https://internal.example"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "internal.example") {
		t.Fatalf("unsafe error %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	Fail(w, Private)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("safe private error lost")
	}
}
