package store

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
)

func TestListInstalledSkillsExcludesArchivedAndOrdersBySourceName(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}).
		AddRow("skill-1", "ace3", "backend-developer", "skills/backend-developer/SKILL.md", "", false, now, now).
		AddRow("skill-2", "verzth", "qa-manager", "skills/qa-manager/SKILL.md", "", false, now, now)

	query := `SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false ORDER BY ss.name, s.name`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	skills, err := store.ListInstalledSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2", len(skills))
	}
	if skills[0].SourceID != "ace3" || skills[0].Name != "backend-developer" {
		t.Fatalf("unexpected first skill: %+v", skills[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
