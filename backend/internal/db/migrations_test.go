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
