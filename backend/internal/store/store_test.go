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

type testLifecycleRouter struct {
	next   string
	exists bool
}

func (r testLifecycleRouter) NextAgent(context.Context, string, string, []string) (string, bool, error) {
	if r.next == "" {
		return "", true, nil
	}
	return r.next, false, nil
}

func (r testLifecycleRouter) Exists(context.Context, string) (bool, error) {
	return r.exists, nil
}

func (r testLifecycleRouter) CLIKindForStep(context.Context, string, string) (string, error) {
	return "", nil
}

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

	query := `SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false AND s.ignored=false ORDER BY ss.name, s.name`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	skills, err := store.ListInstalledSkills(context.Background(), false)
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

func TestListInstalledSkillsCanIncludeIgnored(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "source_id", "name", "path_in_source", "version", "archived", "ignored", "created_at", "updated_at"}).
		AddRow("skill-1", "ace3", "backend-developer", "skills/backend-developer/SKILL.md", "", false, false, now, now).
		AddRow("skill-2", "ace3", "qa-manager", "skills/qa-manager/SKILL.md", "", false, true, now, now)

	query := `SELECT s.* FROM skills s JOIN skill_sources ss ON ss.id=s.source_id WHERE s.archived=false ORDER BY ss.name, s.name`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(rows)

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	skills, err := store.ListInstalledSkills(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 2 || !skills[1].Ignored {
		t.Fatalf("unexpected skills: %+v", skills)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSetSkillIgnoredRejectsAssignedSkill(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM agent_skills WHERE skill_id=$1")).
		WithArgs("skill-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	err = func() error {
		_, err := store.SetSkillIgnored(context.Background(), "skill-1", true)
		return err
	}()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestUpsertSkillsForSourcePreservesIgnoredOnResync(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE skills SET archived=true, updated_at=now() WHERE source_id=$1")).
		WithArgs("source-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO skills (id, source_id, name, path_in_source, version, archived, ignored)
			VALUES ($1,$2,$3,$4,$5,false,false)
			ON CONFLICT (source_id, path_in_source) DO UPDATE SET name=excluded.name, version=excluded.version, archived=false, updated_at=now()`)).
		WithArgs("skill-1", "source-1", "qa-manager", "skills/qa-manager/SKILL.md", "").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE skill_sources SET last_synced_at=now(), updated_at=now() WHERE id=$1")).
		WithArgs("source-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	if err := store.UpsertSkillsForSource(context.Background(), "source-1", []models.Skill{{
		ID:           "skill-1",
		SourceID:     "source-1",
		Name:         "qa-manager",
		PathInSource: "skills/qa-manager/SKILL.md",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSkillSourceRejectsAssignedActiveSkills(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (SELECT 1 FROM skill_sources WHERE id=$1)")).
		WithArgs("source-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)
		FROM agent_skills a
		JOIN skills sk ON sk.id=a.skill_id
		WHERE sk.source_id=$1 AND sk.archived=false`)).
		WithArgs("source-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	err = store.DeleteSkillSource(context.Background(), "source-1")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteSkillSourceDeletesUnassignedSource(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (SELECT 1 FROM skill_sources WHERE id=$1)")).
		WithArgs("source-1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*)
		FROM agent_skills a
		JOIN skills sk ON sk.id=a.skill_id
		WHERE sk.source_id=$1 AND sk.archived=false`)).
		WithArgs("source-1").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM skill_sources WHERE id=$1")).
		WithArgs("source-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	if err := store.DeleteSkillSource(context.Background(), "source-1"); err != nil {
		t.Fatal(err)
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

func TestCheckoutTaskRejectsDoubleCheckout(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	checkoutRun := "run-1"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "tags", "lifecycle_id", "checkout_run_id", "execution_run_id", "execution_state", "created_at", "updated_at"}).
			AddRow("task-1", "project-1", nil, "Do work", "", "in_progress", "agent-1", nil, 0, 0, "{}", "default", checkoutRun, nil, "checked_out", now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	err = nil
	_, err = store.CheckoutTask(context.Background(), "task-1", CheckoutInput{})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseTaskRejectsStaleRun(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	checkoutRun := "run-1"
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "tags", "lifecycle_id", "checkout_run_id", "execution_run_id", "execution_state", "created_at", "updated_at"}).
			AddRow("task-1", "project-1", nil, "Do work", "", "in_progress", "agent-1", nil, 0, 0, "{}", "default", checkoutRun, nil, "checked_out", now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	staleRun := "run-2"
	_, err = store.ReleaseTask(context.Background(), "task-1", ReleaseInput{RunID: &staleRun})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnqueueWakeupConflictsOnDifferentPayloadForIdempotencyKey(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	key := "same-key"
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM agent_wakeups WHERE agent_id=$1 AND task_id=$2 AND idempotency_key=$3`)).
		WithArgs("agent-1", "task-1", key).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "task_id", "source", "reason", "payload_json", "context_snapshot", "idempotency_key", "requester_type", "status", "coalesced_count", "error", "created_at", "updated_at"}).
			AddRow("wake-1", "agent-1", "task-1", "manual", "manual_run", []byte(`{"old":true}`), []byte(`{}`), key, "api", "queued", 0, "", now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT payload_json=$4::jsonb AND context_snapshot=$5::jsonb FROM agent_wakeups WHERE agent_id=$1 AND task_id=$2 AND idempotency_key=$3`)).
		WithArgs("agent-1", "task-1", key, []byte(`{"new":true}`), []byte(`{}`)).
		WillReturnRows(sqlmock.NewRows([]string{"same"}).AddRow(false))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	_, err = store.EnqueueWakeup(context.Background(), "task-1", "agent-1", WakeupInput{
		Source:         "manual",
		Reason:         "manual_run",
		PayloadJSON:    []byte(`{"new":true}`),
		IdempotencyKey: &key,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimQueuedWakeupCreatesExactlyOneLinkedRun(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM agent_wakeups
		WHERE status='queued'
		ORDER BY CASE WHEN source='manual' THEN 0 ELSE 1 END, created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "task_id", "source", "reason", "payload_json", "context_snapshot", "requester_type", "status", "coalesced_count", "error", "created_at", "updated_at"}).
			AddRow("wake-1", "agent-1", "task-1", "manual", "manual_run", []byte(`{}`), []byte(`{}`), "api", "queued", 0, "", now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT t.lifecycle_id, p.default_cli_kind AS cli_kind
		FROM tasks t JOIN projects p ON p.id=t.project_id WHERE t.id=$1`)).
		WithArgs("task-1").
		WillReturnRows(sqlmock.NewRows([]string{"lifecycle_id", "cli_kind"}).AddRow("default", "codex"))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO runs (id, agent_id, task_id, wakeup_id, status, cli_kind, started_at)
		VALUES ($1,$2,$3,$4,'running',$5,now())`)).
		WithArgs(sqlmock.AnyArg(), "agent-1", "task-1", "wake-1", "codex").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE agent_wakeups SET status='running', claimed_at=now(), updated_at=now(), run_id=$2 WHERE id=$1`)).
		WithArgs("wake-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE tasks
		SET status=CASE WHEN status IN ('todo','waiting','blocked') THEN 'in_progress' ELSE status END,
		    checkout_run_id=$2,
		    execution_run_id=$2,
		    execution_state='running',
		    updated_at=now()
		WHERE id=$1`)).
		WithArgs("task-1", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	for i := 0; i < 3; i++ {
		mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM runs WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "task_id", "wakeup_id", "status", "cli_kind", "tokens_in", "tokens_out", "cost_usd", "prompt_hash", "created_at"}).
			AddRow("run-1", "agent-1", "task-1", "wake-1", "running", "codex", 0, 0, 0, "", now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	run, ok, err := store.ClaimQueuedWakeup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok || run.WakeupID == nil || *run.WakeupID != "wake-1" || run.Status != "running" {
		t.Fatalf("unexpected claimed run: ok=%v run=%+v", ok, run)
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
	store.SetLifecycleRouter(testLifecycleRouter{next: "frontend", exists: true})
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

func TestValidatedSubtasksEnforcesRunCapBeforeDBLookup(t *testing.T) {
	store := New(nil, nil)
	subtasks := make([]Subtask, maxSubtasksPerRun+1)
	for i := range subtasks {
		subtasks[i] = Subtask{Title: "Task"}
	}
	_, err := store.validatedSubtasks(context.Background(), nil, models.Task{ID: "task-1"}, models.Agent{ID: "em"}, "done", subtasks)
	if err == nil || !strings.Contains(err.Error(), "subtask spawn cap exceeded") {
		t.Fatalf("got %v, want spawn cap error", err)
	}
}

func TestValidatedSubtasksSuppressesTerminalQASpawn(t *testing.T) {
	store := New(nil, nil)
	subtasks, err := store.validatedSubtasks(context.Background(), nil, models.Task{ID: "task-1"}, models.Agent{ID: "qa"}, "done", []Subtask{{Title: "Implement backend slice"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 0 {
		t.Fatalf("got %d subtasks, want 0", len(subtasks))
	}
}

func TestContextualizeSubtaskPreservesParentRequest(t *testing.T) {
	parent := models.Task{Title: "Add Recent runs panel", Description: "Show recent run history in the UI."}
	subtask := contextualizeSubtask(parent, Subtask{Title: "Implement backend slice", Description: "Add the API."})
	if subtask.Title != "Add Recent runs panel: implement backend slice" {
		t.Fatalf("unexpected title: %q", subtask.Title)
	}
	if !strings.Contains(subtask.Description, "Parent task context:") || !strings.Contains(subtask.Description, "Add Recent runs panel") {
		t.Fatalf("description lost parent context: %q", subtask.Description)
	}
}

func TestValidatedSubtasksSuppressesGenericSpawnerOutsidePlanningRoles(t *testing.T) {
	store := New(nil, nil)
	subtasks, err := store.validatedSubtasks(context.Background(), nil, models.Task{ID: "task-1"}, models.Agent{ID: "backend", Role: "backend"}, "done", []Subtask{{Title: "Implement backend slice"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 0 {
		t.Fatalf("got %d subtasks, want 0", len(subtasks))
	}
}

func TestContextualizeSubtaskUsesBaseParentTitle(t *testing.T) {
	parent := models.Task{Title: "Add Recent runs panel: implement backend slice", Description: "Show recent run history in the UI."}
	subtask := contextualizeSubtask(parent, Subtask{Title: "Verify implementation"})
	if subtask.Title != "Add Recent runs panel: verify implementation" {
		t.Fatalf("unexpected title: %q", subtask.Title)
	}
}

func TestApplyAgentResponseReassignDoneKeepsTaskRunnable(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	backendID := "backend"
	resolveQuery := `SELECT id FROM agents
		WHERE id=$1 OR role=$1 OR name=$1
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN role=$1 THEN 1 ELSE 2 END, id
		LIMIT 1`
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)")).
		WithArgs(sqlmock.AnyArg(), "task-1", "agent:pm", "route directly").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(resolveQuery)).
		WithArgs("backend").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(backendID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks SET status=$2, assignee_agent_id=$3, retry_count=0, tags=$4, lifecycle_id=$5, updated_at=now() WHERE id=$1")).
		WithArgs("task-1", "todo", sqlmock.AnyArg(), sqlmock.AnyArg(), "default").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	tx, err := store.DB().BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reassign := "backend"
	err = store.ApplyAgentResponse(context.Background(), tx, models.Task{ID: "task-1", ProjectID: "project-1", LifecycleID: "default"}, models.Agent{ID: "pm"}, AgentResponse{
		TaskUpdates: TaskUpdates{Status: "done", Comment: "route directly", ReassignTo: &reassign},
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

func TestApplyAgentResponseCreatesHumanInteractionAndWaits(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	pmID := "pm"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)")).
		WithArgs(sqlmock.AnyArg(), "task-1", "agent:pm", "Need product input").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO task_interactions (id, task_id, kind, status, title, summary, payload, continuation_policy, idempotency_key, source_comment_id, source_run_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`)).
		WithArgs(sqlmock.AnyArg(), "task-1", "ask_user_questions", "open", "Clarify scope", "Need user input.", []byte(`{"question":"Which path?"}`), "wake_assignee", nil, sqlmock.AnyArg(), nil, "agent:pm").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_interactions WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(interactionRows(now).AddRow("interaction-1", "task-1", "ask_user_questions", "open", "Clarify scope", "Need user input.", []byte(`{"question":"Which path?"}`), []byte(`{}`), "wake_assignee", nil, nil, nil, "agent:pm", nil, nil, now, now))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,'system',$3)")).
		WithArgs(sqlmock.AnyArg(), "task-1", "Waiting on human interaction: Clarify scope").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tasks SET status=$2, assignee_agent_id=$3, retry_count=0, tags=$4, lifecycle_id=$5, updated_at=now() WHERE id=$1")).
		WithArgs("task-1", "waiting", sqlmock.AnyArg(), sqlmock.AnyArg(), "default").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	tx, err := store.DB().BeginTxx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = store.ApplyAgentResponse(context.Background(), tx, models.Task{ID: "task-1", ProjectID: "project-1", AssigneeAgentID: &pmID, LifecycleID: "default"}, models.Agent{ID: "pm"}, AgentResponse{
		TaskUpdates: TaskUpdates{Status: "in_progress", Comment: "Need product input"},
		HumanInteractions: []HumanInteraction{{
			Kind:    "ask_user_questions",
			Title:   "Clarify scope",
			Summary: "Need user input.",
			Payload: []byte(`{"question":"Which path?"}`),
		}},
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

func TestResolveQuestionInteractionRecordsAnswerAndQueuesWakeup(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_interactions WHERE id=$1")).
		WithArgs("interaction-1").
		WillReturnRows(interactionRows(now).AddRow("interaction-1", "task-1", "ask_user_questions", "open", "Clarify scope", "Need input.", []byte(`{"question":"Which path?"}`), []byte(`{}`), "wake_assignee", nil, nil, nil, "agent:pm", nil, nil, now, now))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE task_interactions
		SET status=$2, resolution_payload=$3, resolved_by=$4, resolved_at=now(), updated_at=now()
		WHERE id=$1 AND status='open'`)).
		WithArgs("interaction-1", "resolved", []byte(`{"note":"","response":"Use option A.","status":"resolved"}`), "human:ignas").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)")).
		WithArgs(sqlmock.AnyArg(), "task-1", "human:ignas", `Answered "Clarify scope": Use option A.`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM tasks WHERE id=$1")).
		WithArgs("task-1").
		WillReturnRows(taskRows(now).AddRow("task-1", "project-1", nil, "Do work", "", "waiting", "pm", nil, 0, 0, now, now))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM agent_wakeups
			WHERE agent_id=$1 AND task_id=$2 AND source=$3 AND reason=$4 AND payload_json=$5::jsonb AND status IN ('queued','claimed','running')
			ORDER BY created_at DESC LIMIT 1`)).
		WithArgs("pm", "task-1", "interaction", "interaction_resolved", sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO agent_wakeups (id, agent_id, task_id, source, reason, payload_json, context_snapshot, idempotency_key, requester_type, requester_id, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'queued')`)).
		WithArgs(sqlmock.AnyArg(), "pm", "task-1", "interaction", "interaction_resolved", sqlmock.AnyArg(), sqlmock.AnyArg(), nil, "interaction", "interaction-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM agent_wakeups WHERE id=$1")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(wakeupRows(now).AddRow("wakeup-1", "pm", "task-1", "interaction", "interaction_resolved", []byte(`{}`), []byte(`{}`), nil, "interaction", "interaction-1", "queued", 0, nil, nil, nil, "", now, now))
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_notify('mp_events', $1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM task_interactions WHERE id=$1")).
		WithArgs("interaction-1").
		WillReturnRows(interactionRows(now).AddRow("interaction-1", "task-1", "ask_user_questions", "resolved", "Clarify scope", "Need input.", []byte(`{"question":"Which path?"}`), []byte(`{"note":"","response":"Use option A.","status":"resolved"}`), "wake_assignee", nil, nil, nil, "agent:pm", "human:ignas", now, now, now))

	store := New(sqlx.NewDb(rawDB, "sqlmock"), nil)
	interaction, err := store.ResolveInteraction(context.Background(), "interaction-1", "resolved", "human:ignas", InteractionResolutionInput{Response: "Use option A."})
	if err != nil {
		t.Fatal(err)
	}
	if interaction.Status != "resolved" {
		t.Fatalf("got status %q, want resolved", interaction.Status)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func taskRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "project_id", "repo_id", "title", "description", "status", "assignee_agent_id", "parent_id", "priority", "retry_count", "created_at", "updated_at"})
}

func interactionRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "task_id", "kind", "status", "title", "summary", "payload", "resolution_payload", "continuation_policy", "idempotency_key", "source_comment_id", "source_run_id", "created_by", "resolved_by", "resolved_at", "created_at", "updated_at"})
}

func wakeupRows(now time.Time) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "agent_id", "task_id", "source", "reason", "payload_json", "context_snapshot", "idempotency_key", "requester_type", "requester_id", "status", "coalesced_count", "run_id", "claimed_at", "finished_at", "error", "created_at", "updated_at"})
}
