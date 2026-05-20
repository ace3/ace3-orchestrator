package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearer(t *testing.T) {
	next := Bearer("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	denied := httptest.NewRecorder()
	next.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("got %d want %d", denied.Code, http.StatusUnauthorized)
	}
	allowed := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	req.Header.Set("Authorization", "Bearer secret")
	next.ServeHTTP(allowed, req)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("got %d want %d", allowed.Code, http.StatusNoContent)
	}
}

func TestBearerRejectsQueryToken(t *testing.T) {
	next := Bearer("secret")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events?token=secret", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d want %d", rec.Code, http.StatusUnauthorized)
	}
}
