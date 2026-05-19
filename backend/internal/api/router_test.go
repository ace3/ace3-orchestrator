package api

import (
	"encoding/json"
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

func TestDebugHealthRouteIsPublic(t *testing.T) {
	handler := NewRouter(config.Config{APIToken: "test-token"}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/debug/health", nil)
	rec := httptest.NewRecorder()
	before := time.Now().UnixMilli()
	handler.ServeHTTP(rec, req)
	after := time.Now().UnixMilli()

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("got keys %v, want status and timestamp", body)
	}
	status, ok := body["status"].(string)
	if !ok || status != "ok" {
		t.Fatalf("got status %v, want ok", body["status"])
	}
	timestamp, ok := body["timestamp"].(float64)
	if !ok {
		t.Fatalf("got timestamp %v, want numeric unix milliseconds", body["timestamp"])
	}
	timestampMS := int64(timestamp)
	if timestamp != float64(timestampMS) || timestampMS < before || timestampMS > after {
		t.Fatalf("timestamp %v outside request window [%d,%d]", body["timestamp"], before, after)
	}
}

func TestAgentPromptImproveIsBlocked(t *testing.T) {
	handler := NewRouter(config.Config{APIToken: "test-token"}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/pm/improve-prompt", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got status %d, want 405", rec.Code)
	}
}

func TestCreateAgentRouteUsesDatabaseState(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO agents (id, name, role, role_prompt, cli_kind, cli_profile, enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`)).
		WithArgs("custom", "Custom Agent", "custom", "Use DB prompt.", "codex", nil, true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM agent_skills WHERE agent_id=$1")).
		WithArgs("custom").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM agents WHERE id=$1")).
		WithArgs("custom").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "role", "role_prompt", "cli_kind", "cli_profile", "enabled", "created_at", "updated_at"}).
			AddRow("custom", "Custom Agent", "custom", "Use DB prompt.", "codex", nil, true, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN agent_skills a ON a.skill_id=s.id WHERE a.agent_id=$1 ORDER BY s.name`)).
		WithArgs("custom").
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	body := `{"id":"custom","name":"Custom Agent","role":"custom","role_prompt":"Use DB prompt.","cli_kind":"codex","enabled":true,"skill_ids":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"role_prompt":"Use DB prompt."`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteLifecycleAgentRouteConflicts(t *testing.T) {
	handler := NewRouter(config.Config{APIToken: "test-token"}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/agents/pm", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSkillSourceRoute(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO skill_sources (id, name, upstream_url, pinned_sha, path_filter, kind)
		VALUES ($1,$2,$3,$4,$5,$6)`)).
		WithArgs(sqlmock.AnyArg(), "custom", "https://example.test/skills.git", "main", "", "custom").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM skill_sources WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "upstream_url", "pinned_sha", "last_synced_at", "kind", "has_update", "created_at", "updated_at"}).
			AddRow("source-1", "custom", "https://example.test/skills.git", "main", nil, "custom", false, now, now))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/skill-sources", strings.NewReader(`{"name":"custom","upstream_url":"https://example.test/skills.git"}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"name":"custom"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSkillRouteConflictsWhenAssigned(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM agent_skills WHERE skill_id=$1")).
		WithArgs("skill-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	req := httptest.NewRequest(http.MethodPatch, "/api/skills/skill-1", strings.NewReader(`{"ignored":true}`))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("got status %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListSkillsRouteCanIncludeIgnored(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false ORDER BY ss.name, s.name`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "ignored", "created_at", "updated_at"}).
			AddRow("skill-1", "source-1", "qa-manager", "skills/qa-manager/SKILL.md", "", false, true, now, now))

	handler := NewRouter(config.Config{APIToken: "test-token"}, store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/skills?include_ignored=true", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ignored":true`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false AND s.ignored=false ORDER BY ss.name, s.name`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}).
			AddRow("skill-1", "source-1", "backend-developer", "skills/backend-developer/SKILL.md", "", false, now, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM agents ORDER BY role, name")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "role", "role_prompt", "cli_kind", "cli_profile", "enabled", "created_at", "updated_at"}).
			AddRow("custom-backend", "Custom Backend", "backend", "DB prompt", "codex", nil, true, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN agent_skills a ON a.skill_id=s.id WHERE a.agent_id=$1 ORDER BY s.name`)).
		WithArgs("custom-backend").
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
	if !strings.Contains(rec.Body.String(), `"recommended_agents":["backend","em"]`) ||
		!strings.Contains(rec.Body.String(), `"id":"custom-backend"`) ||
		!strings.Contains(rec.Body.String(), `"base_prompt":"DB prompt"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
