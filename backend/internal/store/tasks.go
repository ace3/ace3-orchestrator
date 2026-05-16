package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"mini-paperclip/backend/internal/models"
)

type TaskInput struct {
	RepoID          *string `json:"repo_id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Status          string  `json:"status"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
	ParentID        *string `json:"parent_id"`
	Priority        int     `json:"priority"`
}

const maxTaskRetries = 3

type CommentInput struct {
	Body string `json:"body"`
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
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, projectID, in.RepoID, strings.TrimSpace(in.Title), in.Description, status, in.AssigneeAgentID, in.ParentID, in.Priority); err != nil {
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
		task.AssigneeAgentID = in.AssigneeAgentID
	}
	if in.ParentID != nil {
		task.ParentID = in.ParentID
	}
	task.Priority = in.Priority
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET repo_id=$2, title=$3, description=$4, status=$5, assignee_agent_id=$6, parent_id=$7, priority=$8, updated_at=now() WHERE id=$1`,
		id, task.RepoID, task.Title, task.Description, task.Status, task.AssigneeAgentID, task.ParentID, task.Priority); err != nil {
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
		AgentID string `db:"agent_id"`
		TaskID  string `db:"task_id"`
		CLIKind string `db:"cli_kind"`
	}
	var candidates []candidate
	if err := s.db.SelectContext(ctx, &candidates, `SELECT a.id AS agent_id, t.id AS task_id, p.default_cli_kind AS cli_kind
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
		id := uuid.NewString()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO runs (id, agent_id, task_id, status, cli_kind)
			VALUES ($1,$2,$3,'queued',$4) ON CONFLICT DO NOTHING`, id, candidate.AgentID, candidate.TaskID, candidate.CLIKind); err != nil {
			return count, err
		}
		count++
		s.Notify(ctx, "run", id)
	}
	return count, nil
}

func (s *Store) RunCLIKind(ctx context.Context, taskID string) (string, error) {
	var cliKind string
	err := s.db.GetContext(ctx, &cliKind, `SELECT p.default_cli_kind
		FROM tasks t
		JOIN projects p ON p.id=t.project_id
		WHERE t.id=$1`, taskID)
	if err != nil {
		return "", mapNotFound(err)
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
	_, _ = s.db.ExecContext(ctx, "INSERT INTO run_events (run_id, level, message) VALUES ($1,$2,$3)", runID, level, message)
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
		s.Notify(ctx, "run", runID)
	}
	return err
}

func (s *Store) TaskContext(ctx context.Context, run models.Run) (models.Agent, models.Task, *models.Repo, []models.Comment, error) {
	agent, err := s.GetAgent(ctx, run.AgentID)
	if err != nil {
		return models.Agent{}, models.Task{}, nil, nil, err
	}
	task, err := s.GetTask(ctx, run.TaskID)
	if err != nil {
		return agent, models.Task{}, nil, nil, err
	}
	var repo *models.Repo
	if task.RepoID != nil {
		var r models.Repo
		if err := s.db.GetContext(ctx, &r, "SELECT * FROM repos WHERE id=$1", *task.RepoID); err != nil {
			return agent, task, nil, nil, err
		}
		repo = &r
	}
	comments, err := s.ListComments(ctx, task.ID)
	if err != nil {
		return agent, task, repo, nil, err
	}
	if len(comments) > 10 {
		comments = comments[len(comments)-10:]
	}
	return agent, task, repo, comments, nil
}

func (s *Store) ApplyAgentResponse(ctx context.Context, tx *sqlx.Tx, task models.Task, agent models.Agent, response AgentResponse) error {
	update := response.TaskUpdates
	status := update.Status
	if update.RequestHumanReview {
		status = "in_review"
		update.Comment = "[HUMAN REVIEW REQUESTED] " + update.Comment
	}
	if err := validateTaskStatus(status); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,$3,$4)", uuid.NewString(), task.ID, "agent:"+agent.ID, update.Comment); err != nil {
		return err
	}
	var assignee *string = task.AssigneeAgentID
	if update.ReassignTo != nil && *update.ReassignTo != "" {
		resolved, err := resolveAgentID(ctx, tx, *update.ReassignTo)
		if err != nil {
			return err
		}
		assignee = &resolved
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tasks SET status=$2, assignee_agent_id=$3, retry_count=0, updated_at=now() WHERE id=$1", task.ID, status, assignee); err != nil {
		return err
	}
	for _, subtask := range update.CreateSubtasks {
		id := uuid.NewString()
		var subtaskAssignee *string
		if subtask.AssigneeAgentID != nil && *subtask.AssigneeAgentID != "" {
			resolved, err := resolveAgentID(ctx, tx, *subtask.AssigneeAgentID)
			if err != nil {
				return err
			}
			subtaskAssignee = &resolved
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tasks (id, project_id, repo_id, title, description, status, assignee_agent_id, parent_id, priority)
			VALUES ($1,$2,$3,$4,$5,'todo',$6,$7,$8)`, id, task.ProjectID, task.RepoID, subtask.Title, subtask.Description, subtaskAssignee, task.ID, task.Priority); err != nil {
			return err
		}
		if subtask.InitialComment != "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO comments (id, task_id, author, body) VALUES ($1,$2,'system',$3)", uuid.NewString(), id, subtask.InitialComment); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveAgentID(ctx context.Context, tx *sqlx.Tx, value string) (string, error) {
	var id string
	if err := tx.GetContext(ctx, &id, "SELECT id FROM agents WHERE id=$1 OR role=$1 ORDER BY id LIMIT 1", value); err != nil {
		return "", mapNotFound(err)
	}
	return id, nil
}

type AgentResponse struct {
	TaskUpdates TaskUpdates `json:"task_updates"`
}

type TaskUpdates struct {
	Status             string       `json:"status"`
	Comment            string       `json:"comment"`
	ReassignTo         *string      `json:"reassign_to"`
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
	Kind string `json:"kind"`
	Path string `json:"path"`
	Note string `json:"note"`
}

func validateTaskStatus(status string) error {
	switch status {
	case "todo", "in_progress", "in_review", "blocked", "done", "cancelled":
		return nil
	default:
		return errors.New("invalid task status")
	}
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
