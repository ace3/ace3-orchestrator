package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestCreateFullRequiresPgDump(t *testing.T) {
	svc := New(nil, "postgres://example", t.TempDir())
	svc.lookPath = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := svc.CreateFull(context.Background()); err == nil || !strings.Contains(err.Error(), "pg_dump") {
		t.Fatalf("expected pg_dump setup error, got %v", err)
	}
}

func TestRestorePlanDoesNotExecuteRestore(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/full-db-20260519T010203Z.dump", []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(nil, "postgres://example", dir)
	plan, err := svc.RestorePlan("full-db-20260519T010203Z.dump")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Command, "pg_restore") {
		t.Fatalf("restore plan must generate pg_restore command, got %q", plan.Command)
	}
	if strings.Contains(plan.Runbook, "browser") {
		t.Fatalf("restore runbook should be operator-run, got %q", plan.Runbook)
	}
}

func TestValidateAppAutoIncludesDependencies(t *testing.T) {
	now := time.Now().UTC()
	doc := AppExport{
		Version:   AppExportVersion,
		CreatedAt: now,
		Source:    "test",
		Bundles: map[string]map[string][]json.RawMessage{
			"configuration": {"agents": {json.RawMessage(`{"id":"pm"}`)}},
			"projects":      {"projects": {json.RawMessage(`{"id":"project-1"}`)}},
			"tasks":         {"tasks": {json.RawMessage(`{"id":"task-1","project_id":"project-1"}`)}},
		},
	}
	svc := New(nil, "", t.TempDir())
	validation := svc.ValidateApp(doc, []string{"tasks"})
	if !validation.OK {
		t.Fatalf("expected valid selection, got errors %v", validation.Errors)
	}
	want := []string{"configuration", "projects", "tasks"}
	if strings.Join(validation.EffectiveBundles, ",") != strings.Join(want, ",") {
		t.Fatalf("effective bundles = %v, want %v", validation.EffectiveBundles, want)
	}
	if len(validation.Warnings) == 0 {
		t.Fatal("expected dependency auto-include warning")
	}
}

func TestNormalizeImportedExecutionDoesNotReactivateWork(t *testing.T) {
	doc := AppExport{Bundles: map[string]map[string][]json.RawMessage{
		"execution_history": {
			"agent_wakeups": {json.RawMessage(`{"id":"wake-1","status":"queued","run_id":"run-1"}`)},
			"runs":          {json.RawMessage(`{"id":"run-1","status":"running"}`)},
		},
		"tasks": {
			"tasks": {json.RawMessage(`{"id":"task-1","checkout_run_id":"run-1","execution_run_id":"run-1","execution_state":"running"}`)},
		},
	}}
	normalized := normalizeDocumentForImport(doc, []string{"tasks", "execution_history"})
	var wake map[string]any
	if err := json.Unmarshal(normalized.Bundles["execution_history"]["agent_wakeups"][0], &wake); err != nil {
		t.Fatal(err)
	}
	if wake["status"] != "cancelled" {
		t.Fatalf("wakeup status = %v, want cancelled", wake["status"])
	}
	var run map[string]any
	if err := json.Unmarshal(normalized.Bundles["execution_history"]["runs"][0], &run); err != nil {
		t.Fatal(err)
	}
	if run["status"] != "error" {
		t.Fatalf("run status = %v, want error", run["status"])
	}
	var task map[string]any
	if err := json.Unmarshal(normalized.Bundles["tasks"]["tasks"][0], &task); err != nil {
		t.Fatal(err)
	}
	if task["checkout_run_id"] != nil || task["execution_run_id"] != nil || task["execution_state"] != nil {
		t.Fatalf("task execution locks were not cleared: %v", task)
	}
}

func TestActiveExecutionCountIncludesRunsAndWakeups(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	svc := New(sqlx.NewDb(rawDB, "sqlmock"), "", t.TempDir())
	count, err := svc.ActiveExecutionCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
