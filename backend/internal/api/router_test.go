package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mini-paperclip/backend/internal/config"
)

func TestAgentMutatorsAreBlocked(t *testing.T) {
	handler := NewRouter(config.Config{APIToken: "test-token"}, nil, nil, nil)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/agents", body: `{}`},
		{method: http.MethodDelete, path: "/api/agents/pm"},
		{method: http.MethodPost, path: "/api/agents/pm/duplicate"},
		{method: http.MethodPost, path: "/api/agents/pm/improve-prompt", body: `{}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s got status %d, want 405", tc.method, tc.path, rec.Code)
		}
	}
}
