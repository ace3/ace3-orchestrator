package lifecycles

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"mini-paperclip/backend/internal/store"
)

type testStep struct {
	id      string
	agent   string
	cliKind string
	skip    []string
	include []string
	model   string
}

func TestNextAgentMatchesLegacySkipAndIncludeRules(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{
		{id: "s1", agent: "pm"},
		{id: "s2", agent: "backend"},
		{id: "s3", agent: "frontend", include: []string{"has-ui"}},
		{id: "s4", agent: "qa", skip: []string{"always"}, include: []string{"has-qa"}},
		{id: "s5", agent: "done"},
	})

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	next, done, err := service.NextAgent(context.Background(), "", "backend", []string{"has-ui", "has-qa"})
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "frontend" {
		t.Fatalf("got next=%q done=%v, want frontend false", next, done)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestNextAgentTreatsMissingIncludeTagAsInactive(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{
		{id: "s1", agent: "backend"},
		{id: "s2", agent: "frontend", include: []string{"has-ui"}},
		{id: "s3", agent: "qa"},
	})

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	next, done, err := service.NextAgent(context.Background(), "default", "backend", nil)
	if err != nil {
		t.Fatal(err)
	}
	if done || next != "qa" {
		t.Fatalf("got next=%q done=%v, want qa false", next, done)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestModelForStepUsesCLIModelDefaults(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{{id: "s1", agent: "backend"}})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM app_settings WHERE key=$1")).
		WithArgs(DefaultCodexModelSetting).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("gpt-5.3-codex"))

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	model, err := service.ModelForStep(context.Background(), "default", "backend", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.3-codex" {
		t.Fatalf("got model %q", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestModelForStepUsesPlanningModelForPlanningAgents(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{{id: "s1", agent: "em"}})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM app_settings WHERE key=$1")).
		WithArgs(PlanningCodexModelSetting).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("gpt-5.5"))

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	model, err := service.ModelForStep(context.Background(), "default", "em", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.5" {
		t.Fatalf("got model %q", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestModelForStepUsesClaudePlanningModel(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{{id: "s1", agent: "pm"}})
	mock.ExpectQuery(regexp.QuoteMeta("SELECT value FROM app_settings WHERE key=$1")).
		WithArgs(PlanningClaudeModelSetting).
		WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow("claude-opus-4-7"))

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	model, err := service.ModelForStep(context.Background(), "default", "pm", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if model != "claude-opus-4-7" {
		t.Fatalf("got model %q", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestModelForStepKeepsExplicitStepOverride(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{{id: "s1", agent: "backend", model: "custom-model"}})

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	model, err := service.ModelForStep(context.Background(), "default", "backend", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if model != "custom-model" {
		t.Fatalf("got model %q", model)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCLIKindForStepReturnsStepOverride(t *testing.T) {
	rawDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	expectLifecycle(mock, "default", []testStep{{id: "s1", agent: "backend", cliKind: "claude"}})

	service := New(store.New(sqlx.NewDb(rawDB, "sqlmock"), nil))
	cliKind, err := service.CLIKindForStep(context.Background(), "default", "backend")
	if err != nil {
		t.Fatal(err)
	}
	if cliKind != "claude" {
		t.Fatalf("got cli kind %q", cliKind)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectLifecycle(mock sqlmock.Sqlmock, id string, steps []testStep) {
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM lifecycles WHERE id=$1")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "description", "is_default", "created_at", "updated_at"}).
			AddRow(id, "Lifecycle", true, now, now))
	rows := sqlmock.NewRows([]string{"id", "lifecycle_id", "position", "agent_id", "cli_kind", "skip_when", "include_when", "model_id", "created_at", "updated_at"})
	for i, step := range steps {
		rows.AddRow(step.id, id, i, step.agent, step.cliKind, pq.StringArray(step.skip), pq.StringArray(step.include), step.model, now, now)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM lifecycle_steps WHERE lifecycle_id=$1 ORDER BY position")).
		WithArgs(id).
		WillReturnRows(rows)
}
