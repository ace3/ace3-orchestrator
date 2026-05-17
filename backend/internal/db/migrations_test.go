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
