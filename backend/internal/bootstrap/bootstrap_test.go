package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/store"
)

func TestIsPinnedSHA(t *testing.T) {
	if !isPinnedSHA("0123456789abcdef0123456789abcdef01234567") {
		t.Fatal("expected 40-char hex string to be treated as pinned SHA")
	}
	if isPinnedSHA("main") {
		t.Fatal("branch name should not be treated as pinned SHA")
	}
}

func TestSyncAgentDefinitionsFailsOnMissingSkill(t *testing.T) {
	err := (&Service{}).SyncAgentDefinitions(context.Background(), map[string]models.Skill{})
	if err == nil || !strings.Contains(err.Error(), `requires missing skill`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiscoverSkillsWithPathFilter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills", "wanted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "wanted", "SKILL.md"), []byte("# Wanted"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "ignored", "SKILL.md"), []byte("# Ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	skills, err := discoverSkills(root, "source-1", "skills/wanted/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].Name != "wanted" || skills[0].PathInSource != "skills/wanted/SKILL.md" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
}

func TestCheckSkillDriftDetectsCacheAndDatabaseMismatch(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	root := t.TempDir()
	cacheRoot := filepath.Join(root, "ace3", "main")
	if err := os.MkdirAll(filepath.Join(cacheRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cacheRoot, "skills", "wanted"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "skills", "wanted", "SKILL.md"), []byte("# Wanted"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM skill_sources ORDER BY name")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "upstream_url", "pinned_sha", "path_filter", "last_synced_at", "kind", "has_update", "created_at", "updated_at"}).
			AddRow("source-1", "ace3", "https://example.test/skills.git", "main", "", now, "ace3", false, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false ORDER BY ss.name, s.name`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "ignored", "created_at", "updated_at"}).
			AddRow("skill-1", "source-1", "old", "skills/old/SKILL.md", "", false, false, now, now))
	mock.ExpectQuery("SELECT a.agent_id").
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "skill_id", "skill_name", "archived", "ignored", "source_id", "source_name", "path_in_source"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM lifecycles ORDER BY is_default DESC, id")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "description", "is_default", "created_at", "updated_at"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, enabled FROM agents")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "enabled"}))

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil), root)
	report, err := service.CheckSkillDrift(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("expected drift report to fail")
	}
	if !hasDriftCode(report, "db_skill_file_missing") {
		t.Fatalf("expected db_skill_file_missing issue, got %+v", report.Issues)
	}
	if !hasDriftCode(report, "cache_skill_missing_db") {
		t.Fatalf("expected cache_skill_missing_db issue, got %+v", report.Issues)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func hasDriftCode(report SkillDriftReport, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
