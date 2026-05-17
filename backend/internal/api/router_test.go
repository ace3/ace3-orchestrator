package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"

	"mini-paperclip/backend/internal/config"
	"mini-paperclip/backend/internal/store"
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

func TestAPIRoutesRequireBearerToken(t *testing.T) {
	handler := NewRouter(config.Config{APIToken: "test-token"}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/artifacts", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}
}

func TestCreateTaskArtifactRoute(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "created_at", "updated_at"}).
			AddRow("task-1", "project-1", nil, "Do work", "", "todo", nil, nil, 0, 0, now, now))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO task_artifacts (id, task_id, kind, title, body, format, metadata, created_by, run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`)).
		WithArgs(sqlmock.AnyArg(), "task-1", "em_handoff", "Plan", "body", "markdown", []byte(`{}`), "api", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_artifacts WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "kind", "title", "body", "format", "metadata", "created_by", "run_id", "created_at", "updated_at"}).
			AddRow("artifact-1", "task-1", "em_handoff", "Plan", "body", "markdown", []byte(`{}`), "api", nil, now, now))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/artifacts", strings.NewReader(`{"kind":"em_handoff","title":"Plan","body":"body"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"kind":"em_handoff"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOrchestratorMapRoute(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM skill_sources ORDER BY name")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "upstream_url", "pinned_sha", "last_synced_at", "kind", "has_update", "created_at", "updated_at"}).
			AddRow("source-1", "ace3", "https://example.test/skills", "main", now, "ace3", false, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false ORDER BY ss.name, s.name`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}).
			AddRow("skill-1", "source-1", "backend-developer", "skills/backend-developer/SKILL.md", "", false, now, now))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/orchestrator-map", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"recommended_agents":["backend","em"]`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
