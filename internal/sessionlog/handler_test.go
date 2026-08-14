package sessionlog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/session-log", strings.NewReader(body))
	rec := httptest.NewRecorder()
	(&Handler{}).ServeHTTP(rec, req)
	return rec
}

func TestSessionLog(t *testing.T) {
	t.Run("valid payload returns ok", func(t *testing.T) {
		rec := post(`{"session_id":"uuid","scenario":"cmf_outsourcing","payload":{"a":1}}`)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
			t.Fatalf("unexpected: %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("garbage body still returns 200", func(t *testing.T) {
		rec := post(`not json at all`)
		if rec.Code != 200 {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}
