package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"

	"mini-paperclip/backend/internal/models"
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

func TestCreateTaskArtifactPersistsStructuredContext(t *testing.T) {
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
		WithArgs(sqlmock.AnyArg(), "task-1", "pm_document", "PRD", "body", "markdown", []byte(`{"phase":"pm"}`), "api", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_artifacts WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "kind", "title", "body", "format", "metadata", "created_by", "run_id", "created_at", "updated_at"}).
			AddRow("artifact-1", "task-1", "pm_document", "PRD", "body", "markdown", []byte(`{"phase":"pm"}`), "api", nil, now, now))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	body := "body"
	artifact, err := store.CreateTaskArtifact(context.Background(), "task-1", TaskArtifactInput{
		Kind:     "pm_document",
		Title:    "PRD",
		Body:     &body,
		Metadata: []byte(`{"phase":"pm"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Kind != "pm_document" || artifact.Title != "PRD" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTaskArtifactRejectsRunCreatedArtifacts(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	runID := "run-1"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_artifacts WHERE id=$1")).
		WithArgs("artifact-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_id", "kind", "title", "body", "format", "metadata", "created_by", "run_id", "created_at", "updated_at"}).
			AddRow("artifact-1", "task-1", "run_log", "Run log", "", "text", []byte(`{}`), "agent:qa", runID, now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	err = store.DeleteTaskArtifact(context.Background(), "artifact-1")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
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

	insertQuery := `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority, tags, lifecycle_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	mock.ExpectExec(regexp.QuoteMeta(insertQuery)).
		WithArgs(sqlmock.AnyArg(), "project-1", nil, "Do work", "details", "todo", &backendID, nil, 3, sqlmock.AnyArg(), "default").
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

func TestCreateTaskTreatsStringNullAssigneeAsUnassigned(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	insertQuery := `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority, tags, lifecycle_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	mock.ExpectExec(regexp.QuoteMeta(insertQuery)).
		WithArgs(sqlmock.AnyArg(), "project-1", nil, "Do work", "", "todo", nil, nil, 0, sqlmock.AnyArg(), "default").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "created_at", "updated_at"}).
			AddRow("task-1", "project-1", nil, "Do work", "", "todo", nil, nil, 0, 0, now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	assignee := "null"
	task, err := store.CreateTask(context.Background(), "project-1", TaskInput{Title: "Do work", AssigneeAgentID: &assignee})
	if err != nil {
		t.Fatal(err)
	}
	if task.AssigneeAgentID != nil {
		t.Fatalf("got assignee %v, want nil", *task.AssigneeAgentID)
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

func TestUpdateAgentRuntimeDoesNotChangePromptNameRoleOrSkills(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	agentRows := func(enabled bool) *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "name", "role", "role_prompt", "cli_kind", "cli_profile", "enabled", "created_at", "updated_at"}).
			AddRow("pm", "PM Agent", "pm", "repo prompt", "codex", nil, enabled, now, now)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM agents WHERE id=$1")).
		WithArgs("pm").
		WillReturnRows(agentRows(true))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN agent_skills a ON a.skill_id=s.id WHERE a.agent_id=$1 ORDER BY s.name`)).
		WithArgs("pm").
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agents SET cli_profile=$2, enabled=$3, updated_at=now() WHERE id=$1`)).
		WithArgs("pm", nil, false).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM agents WHERE id=$1")).
		WithArgs("pm").
		WillReturnRows(agentRows(false))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT s.* FROM skills s JOIN agent_skills a ON a.skill_id=s.id WHERE a.agent_id=$1 ORDER BY s.name`)).
		WithArgs("pm").
		WillReturnRows(sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "created_at", "updated_at"}))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	enabled := false
	agent, err := store.UpdateAgentRuntime(context.Background(), "pm", AgentInput{
		Name:       "Changed",
		Role:       "changed",
		RolePrompt: "changed prompt",
		SkillIDs:   []string{"other"},
		Enabled:    &enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Name != "PM Agent" || agent.Role != "pm" || agent.RolePrompt != "repo prompt" || agent.Enabled {
		t.Fatalf("unexpected runtime update result: %+v", agent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApplyAgentResponseUpdatesTagsLifecycleAndAdvancesWithNewRouting(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	emID := "em"
	frontendID := "frontend"
	resolveQuery := `SELECT id FROM agents
		WHERE id=$1 OR role=$1 OR name=$1
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN role=$1 THEN 1 ELSE 2 END, id
		LIMIT 1`
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)")).
		WithArgs(sqlmock.AnyArg(), "task-1", "agent:em", "route to frontend").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(resolveQuery)).
		WithArgs("frontend").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(frontendID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks SET status=$2, assignee_agent_id=$3, retry_count=0, tags=$4, lifecycle_id=$5, updated_at=now() WHERE id=$1")).
		WithArgs("task-1", "todo", sqlmock.AnyArg(), sqlmock.AnyArg(), "frontend-only").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	tx, err := store.DB().BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tags := []string{"frontend-only", "no-backend"}
	lifecycleID := "frontend-only"
	task := models.Task{ID: "task-1", ProjectID: "project-1", AssigneeAgentID: &emID, LifecycleID: "default"}
	err = store.ApplyAgentResponse(context.Background(), tx, task, models.Agent{ID: "em"}, AgentResponse{
		TaskUpdates: TaskUpdates{
			Status:      "done",
			Comment:     "route to frontend",
			Tags:        &tags,
			LifecycleID: &lifecycleID,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
