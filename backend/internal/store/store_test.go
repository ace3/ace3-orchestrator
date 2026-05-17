package store

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
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

func TestCreateTaskCanonicalizesRoleAssignee(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	backendID := "11111111-1111-1111-1111-111111111111"
	resolveQuery := `SELECT id FROM agents
		WHERE id=$1 OR role=$1 OR name=$1
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN role=$1 THEN 1 ELSE 2 END, id
		LIMIT 1`
	mock.ExpectQuery(regexp.QuoteMeta(resolveQuery)).
		WithArgs("backend").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(backendID))

	insertQuery := `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	mock.ExpectExec(regexp.QuoteMeta(insertQuery)).
		WithArgs(sqlmock.AnyArg(), "project-1", nil, "Do work", "details", "todo", &backendID, nil, 3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "created_at", "updated_at"}).
			AddRow("task-1", "project-1", nil, "Do work", "details", "todo", backendID, nil, 3, 0, now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	assignee := "backend"
	task, err := store.CreateTask(context.Background(), "project-1", TaskInput{
		Title:           " Do work ",
		Description:     "details",
		AssigneeAgentID: &assignee,
		Priority:        3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.AssigneeAgentID == nil || *task.AssigneeAgentID != backendID {
		t.Fatalf("got assignee %v, want %s", task.AssigneeAgentID, backendID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCreateTaskReturnsClearUnknownAssigneeError(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	resolveQuery := `SELECT id FROM agents
		WHERE id=$1 OR role=$1 OR name=$1
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN role=$1 THEN 1 ELSE 2 END, id
		LIMIT 1`
	mock.ExpectQuery(regexp.QuoteMeta(resolveQuery)).
		WithArgs("missing-agent").
		WillReturnError(sql.ErrNoRows)

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	assignee := "missing-agent"
	_, err = store.CreateTask(context.Background(), "project-1", TaskInput{Title: "Do work", AssigneeAgentID: &assignee})
	if err == nil || !strings.Contains(err.Error(), `unknown task assignee "missing-agent"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
