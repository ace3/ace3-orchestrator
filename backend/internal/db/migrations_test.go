package db

import (
	"strings"
	"testing"
)

func TestTaskArtifactsCascadeWithTaskDeletion(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0004_task_artifacts.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	if !strings.Contains(sql, "task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE") {
		t.Fatal("task_artifacts.task_id must cascade when a task is deleted")
	}
}

func TestControlPlaneMigrationAddsWakeupsInteractionsAndLiveness(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0005_control_plane.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	required := []string{
		"CREATE TABLE IF NOT EXISTS agent_wakeups",
		"ALTER TABLE runs",
		"ADD COLUMN IF NOT EXISTS wakeup_id TEXT",
		"CREATE TABLE IF NOT EXISTS task_interactions",
		"CREATE TABLE IF NOT EXISTS agent_runtime_state",
		"CREATE OR REPLACE VIEW task_liveness",
		"checkout_run_id",
		"execution_run_id",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("control-plane migration missing %q", item)
		}
	}
}

func TestSkillIgnoreImportMigrationAddsSelectionColumns(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0006_skill_ignore_import.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	required := []string{
		"ADD COLUMN IF NOT EXISTS path_filter TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS ignored BOOLEAN NOT NULL DEFAULT false",
		"CREATE INDEX IF NOT EXISTS idx_skills_installable",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("skill ignore/import migration missing %q", item)
		}
	}
}

func TestLifecyclesMigrationAddsControlPlaneTables(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0007_lifecycles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	required := []string{
		"CREATE TABLE IF NOT EXISTS lifecycles",
		"CREATE TABLE IF NOT EXISTS lifecycle_steps",
		"cli_kind     TEXT NOT NULL DEFAULT ''",
		"include_when TEXT[] NOT NULL DEFAULT '{}'",
		"model_id     TEXT NOT NULL DEFAULT ''",
		"CREATE TABLE IF NOT EXISTS app_settings",
		"('default_model', 'claude-sonnet-4-6')",
	}
	for _, item := range required {
		if !strings.Contains(sql, item) {
			t.Fatalf("lifecycle migration missing %q", item)
		}
	}
}

func TestLifecycleStepCLIKindMigrationBackfillsExistingDatabases(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0008_lifecycle_step_cli_kind.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(body)
	if !strings.Contains(sql, "ADD COLUMN IF NOT EXISTS cli_kind TEXT NOT NULL DEFAULT ''") {
		t.Fatalf("cli_kind migration missing backfill column")
	}
}
