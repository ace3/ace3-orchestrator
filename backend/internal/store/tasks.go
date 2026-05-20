package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"mini-paperclip/backend/internal/models"
	"mini-paperclip/backend/internal/security"
)

type TaskInput struct {
	RepoID          *string   `json:"repo_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Status          string    `json:"status"`
	AssigneeAgentID *string   `json:"assignee_agent_id"`
	ParentID        *string   `json:"parent_id"`
	Priority        int       `json:"priority"`
	Tags            *[]string `json:"tags,omitempty"`
	LifecycleID     *string   `json:"lifecycle_id,omitempty"`
}

const maxTaskRetries = 3
const maxSubtasksPerRun = 5
const maxTaskTreeDepth = 4
const maxTasksPerRoot = 50

type CommentInput struct {
	Body string `json:"body"`
}

type TaskArtifactInput struct {
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Body      *string         `json:"body"`
	Format    string          `json:"format"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedBy string          `json:"created_by"`
	RunID     *string         `json:"run_id"`
}

type EventPayload struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func (s *Store) Notify(ctx context.Context, kind, id string) {
	body, _ := json.Marshal(EventPayload{Kind: kind, ID: id})
	_, _ = s.db.ExecContext(ctx, "SELECT pg_notify('mp_events', $1)", string(body))
}

func (s *Store) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	tasks := []models.Task{}
	return tasks, s.db.SelectContext(ctx, &tasks, `SELECT * FROM tasks WHERE project_id=$1 ORDER BY priority DESC, updated_at DESC`, projectID)
}

func (s *Store) GetTask(ctx context.Context, id string) (models.Task, error) {
	var task models.Task
	if err := s.db.GetContext(ctx, &task, "SELECT * FROM tasks WHERE id=$1", id); err != nil {
		return task, mapNotFound(err)
	}
	return task, nil
}

func (s *Store) CreateTask(ctx context.Context, projectID string, in TaskInput) (models.Task, error) {
	status := in.Status
	if status == "" {
		status = "todo"
	}
	if err := validateTaskStatus(status); err != nil {
		return models.Task{}, err
	}
	assignee, err := s.resolveAgentRef(ctx, in.AssigneeAgentID)
	if err != nil {
		return models.Task{}, err
	}
	id := uuid.NewString()
	tags := pq.StringArray{}
	if in.Tags != nil {
		tags = pq.StringArray(*in.Tags)
	}
	lifecycleID := "default"
	if in.LifecycleID != nil && strings.TrimSpace(*in.LifecycleID) != "" {
		lifecycleID = strings.TrimSpace(*in.LifecycleID)
	}
	if err := s.validateLifecycleID(ctx, lifecycleID); err != nil {
		return models.Task{}, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority, tags, lifecycle_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, projectID, in.RepoID, strings.TrimSpace(in.Title), in.Description, status, assignee, in.ParentID, in.Priority, tags, lifecycleID); err != nil {
		return models.Task{}, err
	}
	s.Notify(ctx, "task", id)
	return s.GetTask(ctx, id)
}

func (s *Store) UpdateTask(ctx context.Context, id string, in TaskInput) (models.Task, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return task, err
	}
	if in.Title != "" {
		task.Title = strings.TrimSpace(in.Title)
	}
	task.Description = in.Description
	if in.Status != "" {
		if err := validateTaskStatus(in.Status); err != nil {
			return task, err
		}
		task.Status = in.Status
	}
	if in.RepoID != nil {
		task.RepoID = in.RepoID
	}
	if in.AssigneeAgentID != nil {
		assignee, err := s.resolveAgentRef(ctx, in.AssigneeAgentID)
		if err != nil {
			return task, err
		}
		task.AssigneeAgentID = assignee
	}
	if in.ParentID != nil {
		task.ParentID = in.ParentID
	}
	task.Priority = in.Priority
	if in.Tags != nil {
		task.Tags = pq.StringArray(*in.Tags)
	}
	if in.LifecycleID != nil && strings.TrimSpace(*in.LifecycleID) != "" {
		task.LifecycleID = strings.TrimSpace(*in.LifecycleID)
		if err := s.validateLifecycleID(ctx, task.LifecycleID); err != nil {
			return task, err
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET repo_id=$2, title=$3, description=$4, status=$5, assignee_agent_id=$6, parent_id=$7, priority=$8, tags=$9, lifecycle_id=$10, updated_at=now() WHERE id=$1`,
		id, task.RepoID, task.Title, task.Description, task.Status, task.AssigneeAgentID, task.ParentID, task.Priority, task.Tags, task.LifecycleID); err != nil {
		return task, err
	}
	s.Notify(ctx, "task", id)
	return s.GetTask(ctx, id)
}

func (s *Store) ListComments(ctx context.Context, taskID string) ([]models.Comment, error) {
	comments := []models.Comment{}
	return comments, s.db.SelectContext(ctx, &comments, "SELECT * FROM comments WHERE task_id=$1 ORDER BY created_at", taskID)
}

func (s *Store) AddComment(ctx context.Context, taskID, author, body string) (models.Comment, error) {
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)", id, taskID, author, strings.TrimSpace(body)); err != nil {
		return models.Comment{}, err
	}
	var comment models.Comment
	if err := s.db.GetContext(ctx, &comment, "SELECT * FROM comments WHERE id=$1", id); err != nil {
		return comment, err
	}
	s.Notify(ctx, "comment", taskID)
	return comment, nil
}

func (s *Store) ListTaskArtifacts(ctx context.Context, taskID string) ([]models.TaskArtifact, error) {
	artifacts := []models.TaskArtifact{}
	return artifacts, s.db.SelectContext(ctx, &artifacts, "SELECT * FROM task_artifacts WHERE task_id=$1 ORDER BY updated_at DESC, created_at DESC", taskID)
}

func (s *Store) GetTaskArtifact(ctx context.Context, id string) (models.TaskArtifact, error) {
	var artifact models.TaskArtifact
	if err := s.db.GetContext(ctx, &artifact, "SELECT * FROM task_artifacts WHERE id=$1", id); err != nil {
		return artifact, mapNotFound(err)
	}
	return artifact, nil
}

func (s *Store) CreateTaskArtifact(ctx context.Context, taskID string, in TaskArtifactInput) (models.TaskArtifact, error) {
	artifact, err := s.createTaskArtifact(ctx, s.db, taskID, in)
	if err == nil {
		s.Notify(ctx, "task_artifact", taskID)
	}
	return artifact, err
}

func (s *Store) UpdateTaskArtifact(ctx context.Context, id string, in TaskArtifactInput) (models.TaskArtifact, error) {
	artifact, err := s.GetTaskArtifact(ctx, id)
	if err != nil {
		return artifact, err
	}
	if strings.TrimSpace(in.Kind) != "" {
		if err := validateTaskArtifactKind(in.Kind); err != nil {
			return artifact, err
		}
		artifact.Kind = strings.TrimSpace(in.Kind)
	}
	if strings.TrimSpace(in.Title) != "" {
		artifact.Title = strings.TrimSpace(in.Title)
	}
	if in.Body != nil {
		artifact.Body = *in.Body
	}
	if strings.TrimSpace(in.Format) != "" {
		if err := validateTaskArtifactFormat(in.Format); err != nil {
			return artifact, err
		}
		artifact.Format = strings.TrimSpace(in.Format)
	}
	if len(in.Metadata) > 0 {
		metadata, err := normalizeMetadata(in.Metadata)
		if err != nil {
			return artifact, err
		}
		artifact.Metadata = metadata
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE task_artifacts SET kind=$2, title=$3, body=$4, format=$5, metadata=$6, updated_at=now() WHERE id=$1`,
		id, artifact.Kind, artifact.Title, artifact.Body, artifact.Format, []byte(artifact.Metadata)); err != nil {
		return artifact, err
	}
	s.Notify(ctx, "task_artifact", artifact.TaskID)
	return s.GetTaskArtifact(ctx, id)
}

func (s *Store) DeleteTaskArtifact(ctx context.Context, id string) error {
	artifact, err := s.GetTaskArtifact(ctx, id)
	if err != nil {
		return err
	}
	if artifact.RunID != nil {
		return ErrConflict
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM task_artifacts WHERE id=$1", id); err != nil {
		return err
	}
	s.Notify(ctx, "task_artifact", artifact.TaskID)
	return nil
}

type taskArtifactWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	GetContext(context.Context, any, string, ...any) error
}

func (s *Store) createTaskArtifact(ctx context.Context, q taskArtifactWriter, taskID string, in TaskArtifactInput) (models.TaskArtifact, error) {
	if _, err := s.GetTask(ctx, taskID); err != nil {
		return models.TaskArtifact{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = "other"
	}
	if err := validateTaskArtifactKind(kind); err != nil {
		return models.TaskArtifact{}, err
	}
	format := strings.TrimSpace(in.Format)
	if format == "" {
		format = "markdown"
	}
	if err := validateTaskArtifactFormat(format); err != nil {
		return models.TaskArtifact{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return models.TaskArtifact{}, errors.New("artifact title is required")
	}
	body := ""
	if in.Body != nil {
		body = *in.Body
	}
	createdBy := strings.TrimSpace(in.CreatedBy)
	if createdBy == "" {
		createdBy = "api"
	}
	metadata, err := normalizeMetadata(in.Metadata)
	if err != nil {
		return models.TaskArtifact{}, err
	}
	id := uuid.NewString()
	if _, err := q.ExecContext(ctx, `INSERT INTO task_artifacts (id, task_id, kind, title, body, format, metadata, created_by, run_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, taskID, kind, title, body, format, []byte(metadata), createdBy, in.RunID); err != nil {
		return models.TaskArtifact{}, err
	}
	var artifact models.TaskArtifact
	if err := q.GetContext(ctx, &artifact, "SELECT * FROM task_artifacts WHERE id=$1", id); err != nil {
		return artifact, err
	}
	return artifact, nil
}

func (s *Store) EnqueueTaskRun(ctx context.Context, taskID string) (models.Run, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return models.Run{}, err
	}
	if task.AssigneeAgentID == nil {
		return models.Run{}, errors.New("task has no assignee")
	}
	cliKind, err := s.RunCLIKind(ctx, task.ID)
	if err != nil {
		return models.Run{}, err
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO runs (id, agent_id, task_id, status, cli_kind)
		VALUES ($1,$2,$3,'queued',$4)`, id, *task.AssigneeAgentID, task.ID, cliKind); err != nil {
		return models.Run{}, err
	}
	s.Notify(ctx, "run", id)
	return s.GetRun(ctx, id)
}

func (s *Store) DispatchQueuedRuns(ctx context.Context, limit int) (int, error) {
	type candidate struct {
		AgentID     string `db:"agent_id"`
		TaskID      string `db:"task_id"`
		LifecycleID string `db:"lifecycle_id"`
		CLIKind     string `db:"cli_kind"`
	}
	var candidates []candidate
	if err := s.db.SelectContext(ctx, &candidates, `SELECT a.id AS agent_id, t.id AS task_id, t.lifecycle_id, p.default_cli_kind AS cli_kind
		FROM agents a
		JOIN tasks t ON t.assignee_agent_id = a.id
		JOIN projects p ON p.id = t.project_id
		WHERE a.enabled = true
		  AND t.status IN ('todo','in_progress','blocked')
		  AND t.retry_count < $2
		  AND NOT EXISTS (
		      SELECT 1 FROM runs r
		      WHERE r.task_id = t.id AND r.status IN ('queued','running')
		)
		ORDER BY t.priority DESC, t.updated_at ASC
		LIMIT $1`, limit, maxTaskRetries); err != nil {
		return 0, err
	}
	count := 0
	for _, candidate := range candidates {
		cliKind, err := s.lifecycleCLIKind(ctx, candidate.LifecycleID, candidate.AgentID, candidate.CLIKind)
		if err != nil {
			return count, err
		}
		id := uuid.NewString()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO runs (id, agent_id, task_id, status, cli_kind)
			VALUES ($1,$2,$3,'queued',$4) ON CONFLICT DO NOTHING`, id, candidate.AgentID, candidate.TaskID, cliKind); err != nil {
			return count, err
		}
		count++
		s.Notify(ctx, "run", id)
	}
	return count, nil
}

func (s *Store) RunCLIKind(ctx context.Context, taskID string) (string, error) {
	var row struct {
		AgentID     string `db:"agent_id"`
		LifecycleID string `db:"lifecycle_id"`
		CLIKind     string `db:"cli_kind"`
	}
	err := s.db.GetContext(ctx, &row, `SELECT COALESCE(t.assignee_agent_id, '') AS agent_id, t.lifecycle_id, p.default_cli_kind AS cli_kind
		FROM tasks t
		JOIN projects p ON p.id=t.project_id
		WHERE t.id=$1`, taskID)
	if err != nil {
		return "", mapNotFound(err)
	}
	return s.lifecycleCLIKind(ctx, row.LifecycleID, row.AgentID, row.CLIKind)
}

func (s *Store) lifecycleCLIKind(ctx context.Context, lifecycleID, agentID, fallback string) (string, error) {
	if s.lifecycleRouter == nil || strings.TrimSpace(agentID) == "" {
		return fallback, nil
	}
	cliKind, err := s.lifecycleRouter.CLIKindForStep(ctx, lifecycleID, agentID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cliKind) == "" {
		return fallback, nil
	}
	return cliKind, nil
}

func (s *Store) ListRuns(ctx context.Context, taskID string) ([]models.Run, error) {
	runs := []models.Run{}
	return runs, s.db.SelectContext(ctx, &runs, "SELECT * FROM runs WHERE task_id=$1 ORDER BY created_at DESC", taskID)
}

func (s *Store) GetRun(ctx context.Context, id string) (models.Run, error) {
	var run models.Run
	if err := s.db.GetContext(ctx, &run, "SELECT * FROM runs WHERE id=$1", id); err != nil {
		return run, mapNotFound(err)
	}
	return run, nil
}

func (s *Store) ListRunEvents(ctx context.Context, runID string, since int64) ([]models.RunEvent, error) {
	events := []models.RunEvent{}
	return events, s.db.SelectContext(ctx, &events, "SELECT * FROM run_events WHERE run_id=$1 AND id > $2 ORDER BY id", runID, since)
}

func (s *Store) MonthCostUSD(ctx context.Context) (float64, error) {
	var spent float64
	err := s.db.GetContext(ctx, &spent, "SELECT COALESCE(SUM(cost_usd), 0) FROM runs WHERE finished_at >= date_trunc('month', now())")
	return spent, err
}

func (s *Store) RecoverRunningRuns(ctx context.Context) error {
	var runs []models.Run
	if err := s.db.SelectContext(ctx, &runs, "SELECT * FROM runs WHERE status='running'"); err != nil {
		return err
	}
	for _, run := range runs {
		s.AppendRunEvent(ctx, run.ID, "warn", "backend restarted while run was active; marking run as error")
		_, _ = s.db.ExecContext(ctx, `UPDATE runs SET status='error', finished_at=now(), exit_code=1 WHERE id=$1`, run.ID)
		_, _ = s.db.ExecContext(ctx, `UPDATE tasks SET status='blocked', retry_count=retry_count+1, updated_at=now() WHERE id=$1`, run.TaskID)
		_ = s.FinishWakeupForRun(ctx, run.ID, "error", "backend restarted while run was active")
		_ = s.ClearExecutionLock(ctx, run.ID)
		_ = s.UpdateRuntimeState(ctx, run, nil, nil, "error")
		_, _ = s.AddComment(ctx, run.TaskID, "system", "Run recovered after backend restart and marked error.")
		s.Notify(ctx, "run", run.ID)
	}
	return nil
}

func (s *Store) ActiveWorktreePaths(ctx context.Context) (map[string]bool, error) {
	var paths []string
	if err := s.db.SelectContext(ctx, &paths, "SELECT worktree_path FROM runs WHERE worktree_path IS NOT NULL AND status='running'"); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(paths))
	for _, path := range paths {
		out[path] = true
	}
	return out, nil
}

func (s *Store) AppendRunEvent(ctx context.Context, runID, level, message string) {
	_, _ = s.db.ExecContext(ctx, "INSERT INTO run_events (run_id, level, message) VALUES ($1,$2,$3)", runID, level, security.RedactSensitive(message))
	s.Notify(ctx, "run_event", runID)
}

func (s *Store) ClaimQueuedRun(ctx context.Context) (models.Run, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Run{}, false, err
	}
	var run models.Run
	err = tx.GetContext(ctx, &run, `SELECT * FROM runs WHERE status='queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return models.Run{}, false, nil
		}
		return models.Run{}, false, mapNotFound(err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE runs SET status='running', started_at=now() WHERE id=$1", run.ID); err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return models.Run{}, false, err
	}
	s.Notify(ctx, "run", run.ID)
	run, err = s.GetRun(ctx, run.ID)
	return run, true, err
}

func (s *Store) FinishRun(ctx context.Context, runID, status string, exitCode int, tokensIn, tokensOut int, cost float64, prompt string, worktreePath string) error {
	promptHash := sha256.Sum256([]byte(prompt))
	var worktree *string
	if worktreePath != "" {
		worktree = &worktreePath
	}
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET status=$2, finished_at=now(), exit_code=$3, tokens_in=$4, tokens_out=$5, cost_usd=$6, prompt_hash=$7, worktree_path=$8 WHERE id=$1`,
		runID, status, exitCode, tokensIn, tokensOut, cost, hex.EncodeToString(promptHash[:]), worktree)
	if err == nil {
		_ = s.FinishWakeupForRun(ctx, runID, status, "")
		_ = s.ClearExecutionLock(ctx, runID)
		s.Notify(ctx, "run", runID)
	}
	return err
}

func (s *Store) TaskContext(ctx context.Context, run models.Run) (models.Agent, models.Task, *models.Repo, []models.Comment, []models.TaskArtifact, error) {
	agent, err := s.GetAgent(ctx, run.AgentID)
	if err != nil {
		return models.Agent{}, models.Task{}, nil, nil, nil, err
	}
	task, err := s.GetTask(ctx, run.TaskID)
	if err != nil {
		return agent, models.Task{}, nil, nil, nil, err
	}
	var repo *models.Repo
	if task.RepoID != nil {
		var r models.Repo
		if err := s.db.GetContext(ctx, &r, "SELECT * FROM repos WHERE id=$1", *task.RepoID); err != nil {
			return agent, task, nil, nil, nil, err
		}
		repo = &r
	}
	comments, err := s.ListComments(ctx, task.ID)
	if err != nil {
		return agent, task, repo, nil, nil, err
	}
	if len(comments) > 10 {
		comments = comments[len(comments)-10:]
	}
	artifacts, err := s.ListTaskArtifacts(ctx, task.ID)
	if err != nil {
		return agent, task, repo, comments, nil, err
	}
	if len(artifacts) > 10 {
		artifacts = artifacts[:10]
	}
	return agent, task, repo, comments, artifacts, nil
}

func (s *Store) ApplyAgentResponse(ctx context.Context, tx *sqlx.Tx, task models.Task, agent models.Agent, response AgentResponse, runID *string) error {
	update := response.TaskUpdates
	status := update.Status
	if update.RequestHumanReview {
		status = "in_review"
		update.Comment = "[HUMAN REVIEW REQUESTED] " + update.Comment
	}
	rawHumanInteractions := response.HumanInteractions
	if update.RequestHumanReview && len(rawHumanInteractions) == 0 {
		rawHumanInteractions = []HumanInteraction{humanReviewInteraction(update.Comment, runID)}
	}
	humanInteractions, err := normalizeHumanInteractions(rawHumanInteractions, agent.ID, runID)
	if err != nil {
		return err
	}
	if len(humanInteractions) > 0 {
		status = "waiting"
	}
	if err := validateTaskStatus(status); err != nil {
		return err
	}
	nextTags := task.Tags
	if update.Tags != nil {
		nextTags = pq.StringArray(normalizeTags(*update.Tags))
	}
	nextLifecycleID := task.LifecycleID
	if update.LifecycleID != nil && strings.TrimSpace(*update.LifecycleID) != "" && !strings.EqualFold(strings.TrimSpace(*update.LifecycleID), "null") {
		nextLifecycleID = strings.TrimSpace(*update.LifecycleID)
		if err := s.validateLifecycleID(ctx, nextLifecycleID); err != nil {
			return err
		}
	}
	nextTask := task
	nextTask.Tags = nextTags
	nextTask.LifecycleID = nextLifecycleID
	subtasks, err := s.validatedSubtasks(ctx, tx, task, agent, status, update.CreateSubtasks)
	if err != nil {
		return err
	}
	commentID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)", commentID, task.ID, "agent:"+agent.ID, update.Comment); err != nil {
		return err
	}
	var assignee *string = task.AssigneeAgentID
	if len(humanInteractions) > 0 {
		if err := s.createHumanInteractions(ctx, tx, task.ID, commentID, humanInteractions); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,'system',$3)", uuid.NewString(), task.ID, waitingAuditComment(humanInteractions)); err != nil {
			return err
		}
	} else if !emptyAgentRef(update.ReassignTo) {
		resolved, err := resolveAgentRefTx(ctx, tx, update.ReassignTo)
		if err != nil {
			return err
		}
		assignee = resolved
		if status == "done" {
			status = "todo"
		}
	} else if !update.RequestHumanReview && status == "done" {
		next, err := s.advanceLifecycleTx(ctx, tx, nextTask)
		if err != nil {
			return err
		}
		if next != nil {
			assignee = next
			status = "todo"
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status=$2, assignee_agent_id=$3, retry_count=0, tags=$4, lifecycle_id=$5, updated_at=now() WHERE id=$1", task.ID, status, assignee, nextTags, nextLifecycleID); err != nil {
		return err
	}
	for _, subtask := range subtasks {
		id := uuid.NewString()
		var exists bool
		if err := tx.GetContext(ctx, &exists, "SELECT EXISTS (SELECT 1 FROM tasks WHERE parent_id=$1 AND title=$2)", task.ID, subtask.Title); err != nil {
			return err
		}
		if exists {
			continue
		}
		var subtaskAssignee *string
		if !emptyAgentRef(subtask.AssigneeAgentID) {
			resolved, err := resolveAgentRefTx(ctx, tx, subtask.AssigneeAgentID)
			if err != nil {
				return err
			}
			subtaskAssignee = resolved
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority, tags, lifecycle_id)
			VALUES ($1,$2,$3,$4,$5,'todo',$6,$7,$8,$9,$10)`, id, task.ProjectID, task.RepoID, subtask.Title, subtask.Description, subtaskAssignee, task.ID, task.Priority, nextTags, nextLifecycleID); err != nil {
			return err
		}
		if subtask.InitialComment != "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,'system',$3)", uuid.NewString(), id, subtask.InitialComment); err != nil {
				return err
			}
		}
	}
	for _, attachment := range update.Attachments {
		input, ok, err := attachmentArtifactInput(attachment, agent.ID, runID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, err := s.createTaskArtifact(ctx, tx, task.ID, input); err != nil {
			return err
		}
	}
	return nil
}

func humanReviewInteraction(comment string, runID *string) HumanInteraction {
	payload, _ := json.Marshal(map[string]string{
		"question": "Approve this human review request so the workflow can continue?",
	})
	interaction := HumanInteraction{
		Kind:               "approval_request",
		Title:              "Human review requested",
		Summary:            strings.TrimSpace(comment),
		Payload:            payload,
		ContinuationPolicy: "wake_assignee",
	}
	if runID != nil && strings.TrimSpace(*runID) != "" {
		key := "human-review:" + strings.TrimSpace(*runID)
		interaction.IdempotencyKey = &key
	}
	return interaction
}

func normalizeHumanInteractions(items []HumanInteraction, agentID string, runID *string) ([]InteractionInput, error) {
	out := make([]InteractionInput, 0, len(items))
	for _, item := range items {
		kind := strings.TrimSpace(item.Kind)
		switch kind {
		case "ask_user_questions", "request_confirmation", "approval_request":
		default:
			return nil, fmt.Errorf("unsupported human interaction kind %q", kind)
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = kind
		}
		policy := strings.TrimSpace(item.ContinuationPolicy)
		if policy == "" {
			policy = "wake_assignee"
		}
		out = append(out, InteractionInput{
			Kind:               kind,
			Status:             "open",
			Title:              title,
			Summary:            strings.TrimSpace(item.Summary),
			Payload:            item.Payload,
			ContinuationPolicy: policy,
			IdempotencyKey:     item.IdempotencyKey,
			SourceRunID:        runID,
			CreatedBy:          "agent:" + agentID,
		})
	}
	return out, nil
}

func (s *Store) createHumanInteractions(ctx context.Context, tx *sqlx.Tx, taskID, sourceCommentID string, interactions []InteractionInput) error {
	for _, interaction := range interactions {
		commentID := sourceCommentID
		interaction.SourceCommentID = &commentID
		if _, err := s.createInteraction(ctx, tx, taskID, interaction, false); err != nil {
			return err
		}
	}
	return nil
}

func waitingAuditComment(interactions []InteractionInput) string {
	titles := make([]string, 0, len(interactions))
	for _, interaction := range interactions {
		titles = append(titles, interaction.Title)
	}
	return "Waiting on human interaction: " + strings.Join(titles, "; ")
}

type taskTreeStats struct {
	RootID    string `db:"root_id"`
	Depth     int    `db:"depth"`
	TaskCount int    `db:"task_count"`
}

func (s *Store) validatedSubtasks(ctx context.Context, tx *sqlx.Tx, task models.Task, agent models.Agent, status string, subtasks []Subtask) ([]Subtask, error) {
	if len(subtasks) == 0 {
		return nil, nil
	}
	if agent.ID == "qa" && status == "done" {
		return nil, nil
	}
	if !canSpawnGenericSubtasks(agent) {
		filtered := make([]Subtask, 0, len(subtasks))
		for _, subtask := range subtasks {
			if !isGenericSubtaskTitle(subtask.Title) {
				filtered = append(filtered, subtask)
			}
		}
		if len(filtered) == 0 {
			return nil, nil
		}
		subtasks = filtered
	}
	if len(subtasks) > maxSubtasksPerRun {
		return nil, fmt.Errorf("subtask spawn cap exceeded: %d > %d", len(subtasks), maxSubtasksPerRun)
	}
	stats, err := taskTreeStatsFor(ctx, tx, task.ID)
	if err != nil {
		return nil, err
	}
	if stats.Depth+1 > maxTaskTreeDepth {
		return nil, fmt.Errorf("subtask depth cap exceeded for root %s: next depth %d > %d", stats.RootID, stats.Depth+1, maxTaskTreeDepth)
	}
	if stats.TaskCount+len(subtasks) > maxTasksPerRoot {
		return nil, fmt.Errorf("task tree size cap exceeded for root %s: %d + %d > %d", stats.RootID, stats.TaskCount, len(subtasks), maxTasksPerRoot)
	}
	out := make([]Subtask, 0, len(subtasks))
	for _, subtask := range subtasks {
		out = append(out, contextualizeSubtask(task, subtask))
	}
	return out, nil
}

func taskTreeStatsFor(ctx context.Context, tx *sqlx.Tx, taskID string) (taskTreeStats, error) {
	var stats taskTreeStats
	err := tx.GetContext(ctx, &stats, `WITH RECURSIVE ancestors AS (
			SELECT id, parent_id, 0 AS depth FROM tasks WHERE id=$1
			UNION ALL
			SELECT t.id, t.parent_id, a.depth+1 FROM tasks t JOIN ancestors a ON a.parent_id=t.id
		),
		root AS (
			SELECT id FROM ancestors WHERE parent_id IS NULL ORDER BY depth DESC LIMIT 1
		),
		descendants AS (
			SELECT t.id, 0 AS depth FROM tasks t JOIN root r ON t.id=r.id
			UNION ALL
			SELECT c.id, d.depth+1 FROM tasks c JOIN descendants d ON c.parent_id=d.id
		)
		SELECT root.id AS root_id, COALESCE((SELECT max(depth) FROM ancestors), 0) AS depth, count(descendants.id) AS task_count
		FROM root, descendants
		GROUP BY root.id`, taskID)
	return stats, err
}

func contextualizeSubtask(parent models.Task, subtask Subtask) Subtask {
	title := strings.TrimSpace(subtask.Title)
	if title == "" {
		title = "Continue " + parent.Title
	}
	if isGenericSubtaskTitle(title) {
		title = baseTaskTitle(parent.Title) + ": " + strings.ToLower(title[:1]) + title[1:]
	}
	description := strings.TrimSpace(subtask.Description)
	parentContext := strings.TrimSpace(parent.Title + "\n" + parent.Description)
	if parentContext != "" && !strings.Contains(strings.ToLower(description), strings.ToLower(parent.Title)) {
		if description == "" {
			description = "Parent task context:\n" + parentContext
		} else {
			description = description + "\n\nParent task context:\n" + parentContext
		}
	}
	subtask.Title = title
	subtask.Description = description
	return subtask
}

func canSpawnGenericSubtasks(agent models.Agent) bool {
	role := strings.ToLower(strings.TrimSpace(agent.Role))
	if role == "" {
		role = strings.ToLower(strings.TrimSpace(agent.ID))
	}
	return role == "pm" || role == "em"
}

func isGenericSubtaskTitle(title string) bool {
	switch strings.ToLower(strings.TrimSpace(title)) {
	case "implement backend slice", "verify implementation", "implement frontend slice", "write tests", "qa verification":
		return true
	default:
		return false
	}
}

func baseTaskTitle(title string) string {
	base := strings.TrimSpace(title)
	for {
		next := base
		for _, suffix := range []string{": implement backend slice", ": verify implementation", ": implement frontend slice", ": write tests", ": qa verification"} {
			if strings.HasSuffix(strings.ToLower(next), suffix) {
				next = strings.TrimSpace(next[:len(next)-len(suffix)])
				break
			}
		}
		if next == base {
			return base
		}
		base = next
	}
}

// advanceLifecycleTx returns the agents.id of the next lifecycle step for task,
// or nil if the lifecycle is exhausted. Uses the task's current assignee as the
// "where we are now" cursor; lifecycle steps are skipped per the task's tags.
// Returns (nil, nil) if no further step applies — caller leaves the task done.
func (s *Store) advanceLifecycleTx(ctx context.Context, tx *sqlx.Tx, task models.Task) (*string, error) {
	if s.lifecycleRouter == nil {
		return nil, errors.New("lifecycle router is not configured")
	}
	current := ""
	if task.AssigneeAgentID != nil {
		// AssigneeAgentID is the agents.id which equals the repoconfig agent id
		// (SyncRepoAgent preserves it), so it's safe to use directly.
		current = *task.AssigneeAgentID
	}
	nextID, done, err := s.lifecycleRouter.NextAgent(ctx, task.LifecycleID, current, []string(task.Tags))
	if err != nil {
		return nil, err
	}
	if done {
		return nil, nil
	}
	resolved, err := resolveAgentRefTx(ctx, tx, &nextID)
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

func (s *Store) resolveAgentRef(ctx context.Context, value *string) (*string, error) {
	if emptyAgentRef(value) {
		return nil, nil
	}
	resolved, err := resolveAgentRefQuery(ctx, s.db, *value)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func resolveAgentRefTx(ctx context.Context, tx *sqlx.Tx, value *string) (*string, error) {
	if emptyAgentRef(value) {
		return nil, nil
	}
	resolved, err := resolveAgentRefQuery(ctx, tx, *value)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

func emptyAgentRef(value *string) bool {
	if value == nil {
		return true
	}
	ref := strings.TrimSpace(*value)
	return ref == "" || strings.EqualFold(ref, "null")
}

type agentResolver interface {
	GetContext(context.Context, any, string, ...any) error
}

func resolveAgentRefQuery(ctx context.Context, q agentResolver, value string) (string, error) {
	ref := strings.TrimSpace(value)
	var id string
	err := q.GetContext(ctx, &id, `SELECT id FROM agents
		WHERE id=$1 OR role=$1 OR name=$1
		ORDER BY CASE WHEN id=$1 THEN 0 WHEN role=$1 THEN 1 ELSE 2 END, id
		LIMIT 1`, ref)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no rows") {
			return "", fmt.Errorf("unknown task assignee %q", ref)
		}
		return "", err
	}
	return id, nil
}

type AgentResponse struct {
	TaskUpdates       TaskUpdates        `json:"task_updates"`
	HumanInteractions []HumanInteraction `json:"human_interactions"`
}

type TaskUpdates struct {
	Status             string       `json:"status"`
	Comment            string       `json:"comment"`
	ReassignTo         *string      `json:"reassign_to"`
	Tags               *[]string    `json:"tags,omitempty"`
	LifecycleID        *string      `json:"lifecycle_id,omitempty"`
	RequestHumanReview bool         `json:"request_human_review"`
	KeepWorktree       bool         `json:"keep_worktree"`
	CreateSubtasks     []Subtask    `json:"create_subtasks"`
	Attachments        []Attachment `json:"attachments"`
}

type Subtask struct {
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
	InitialComment  string  `json:"initial_comment"`
}

type Attachment struct {
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Body     string          `json:"body"`
	Format   string          `json:"format"`
	Path     string          `json:"path"`
	Note     string          `json:"note"`
	Metadata json.RawMessage `json:"metadata"`
}

type HumanInteraction struct {
	Kind               string          `json:"kind"`
	Title              string          `json:"title"`
	Summary            string          `json:"summary"`
	Payload            json.RawMessage `json:"payload"`
	ContinuationPolicy string          `json:"continuation_policy"`
	IdempotencyKey     *string         `json:"idempotency_key"`
}

func attachmentArtifactInput(attachment Attachment, agentID string, runID *string) (TaskArtifactInput, bool, error) {
	if strings.TrimSpace(attachment.Kind) == "" && strings.TrimSpace(attachment.Title) == "" && strings.TrimSpace(attachment.Path) == "" && strings.TrimSpace(attachment.Body) == "" {
		return TaskArtifactInput{}, false, nil
	}
	kind := normalizeAttachmentKind(attachment.Kind)
	title := strings.TrimSpace(attachment.Title)
	if title == "" {
		title = strings.TrimSpace(attachment.Note)
	}
	if title == "" {
		title = strings.TrimSpace(attachment.Path)
	}
	if title == "" {
		title = kind
	}
	metadata, err := mergeAttachmentMetadata(attachment)
	if err != nil {
		return TaskArtifactInput{}, false, err
	}
	body := attachment.Body
	return TaskArtifactInput{
		Kind:      kind,
		Title:     title,
		Body:      &body,
		Format:    attachment.Format,
		Metadata:  metadata,
		CreatedBy: "agent:" + agentID,
		RunID:     runID,
	}, true, nil
}

func normalizeAttachmentKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "pm_document", "pm_handoff", "em_document", "em_handoff", "qa_report", "implementation_note", "run_log", "other":
		return strings.TrimSpace(kind)
	case "log":
		return "run_log"
	case "file":
		return "implementation_note"
	default:
		return "other"
	}
}

func mergeAttachmentMetadata(attachment Attachment) (json.RawMessage, error) {
	var metadata map[string]any
	if len(attachment.Metadata) > 0 {
		if err := json.Unmarshal(attachment.Metadata, &metadata); err != nil {
			return nil, errors.New("invalid artifact metadata")
		}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if strings.TrimSpace(attachment.Path) != "" {
		metadata["path"] = strings.TrimSpace(attachment.Path)
	}
	if strings.TrimSpace(attachment.Note) != "" {
		metadata["note"] = strings.TrimSpace(attachment.Note)
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func validateTaskArtifactKind(kind string) error {
	switch strings.TrimSpace(kind) {
	case "pm_document", "pm_handoff", "em_document", "em_handoff", "qa_report", "implementation_note", "run_log", "other":
		return nil
	default:
		return errors.New("invalid task artifact kind")
	}
}

func validateTaskArtifactFormat(format string) error {
	switch strings.TrimSpace(format) {
	case "markdown", "text", "json":
		return nil
	default:
		return errors.New("invalid task artifact format")
	}
}

func normalizeMetadata(metadata json.RawMessage) (json.RawMessage, error) {
	if len(metadata) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(metadata, &value); err != nil {
		return nil, errors.New("invalid artifact metadata")
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("artifact metadata must be an object")
	}
	return metadata, nil
}

func validateTaskStatus(status string) error {
	switch status {
	case "todo", "in_progress", "in_review", "waiting", "blocked", "done", "cancelled":
		return nil
	default:
		return errors.New("invalid task status")
	}
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		normalized := strings.ToLower(strings.TrimSpace(tag))
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func (s *Store) validateLifecycleID(ctx context.Context, id string) error {
	if s.lifecycleRouter == nil {
		return nil
	}
	ok, err := s.lifecycleRouter.Exists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("unknown lifecycle %q", id)
	}
	return nil
}

func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "no rows") {
		return ErrNotFound
	}
	return err
}
