package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mini-paperclip/backend/internal/models"
)

type WakeupInput struct {
	Source          string          `json:"source"`
	Reason          string          `json:"reason"`
	PayloadJSON     json.RawMessage `json:"payload_json"`
	ContextSnapshot json.RawMessage `json:"context_snapshot"`
	IdempotencyKey  *string         `json:"idempotency_key"`
	RequesterType   string          `json:"requester_type"`
	RequesterID     *string         `json:"requester_id"`
}

type InteractionInput struct {
	Kind               string          `json:"kind"`
	Status             string          `json:"status"`
	Title              string          `json:"title"`
	Summary            string          `json:"summary"`
	Payload            json.RawMessage `json:"payload"`
	ContinuationPolicy string          `json:"continuation_policy"`
	IdempotencyKey     *string         `json:"idempotency_key"`
	SourceCommentID    *string         `json:"source_comment_id"`
	SourceRunID        *string         `json:"source_run_id"`
	CreatedBy          string          `json:"created_by"`
}

type CheckoutInput struct {
	RunID           *string `json:"run_id"`
	AssigneeAgentID *string `json:"assignee_agent_id"`
	ExpectedStatus  *string `json:"expected_status"`
}

type ReleaseInput struct {
	RunID *string `json:"run_id"`
}

type RunContext struct {
	Wakeup       *models.AgentWakeup       `json:"wakeup,omitempty"`
	RuntimeState *models.AgentRuntimeState `json:"runtime_state,omitempty"`
	RecentRuns   []models.Run              `json:"recent_runs,omitempty"`
}

func (s *Store) ListWakeups(ctx context.Context, taskID string) ([]models.AgentWakeup, error) {
	wakeups := []models.AgentWakeup{}
	return wakeups, s.db.SelectContext(ctx, &wakeups, "SELECT * FROM agent_wakeups WHERE task_id=$1 ORDER BY created_at DESC", taskID)
}

func (s *Store) GetWakeup(ctx context.Context, id string) (models.AgentWakeup, error) {
	var wakeup models.AgentWakeup
	if err := s.db.GetContext(ctx, &wakeup, "SELECT * FROM agent_wakeups WHERE id=$1", id); err != nil {
		return wakeup, mapNotFound(err)
	}
	return wakeup, nil
}

func (s *Store) EnqueueTaskWakeup(ctx context.Context, taskID string, in WakeupInput) (models.AgentWakeup, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return models.AgentWakeup{}, err
	}
	if task.AssigneeAgentID == nil {
		return models.AgentWakeup{}, errors.New("task has no assignee")
	}
	return s.EnqueueWakeup(ctx, taskID, *task.AssigneeAgentID, in)
}

func (s *Store) EnqueueWakeup(ctx context.Context, taskID, agentID string, in WakeupInput) (models.AgentWakeup, error) {
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "manual"
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = source
	}
	requesterType := strings.TrimSpace(in.RequesterType)
	if requesterType == "" {
		requesterType = "system"
	}
	payload, err := normalizeMetadata(in.PayloadJSON)
	if err != nil {
		return models.AgentWakeup{}, err
	}
	snapshot, err := normalizeMetadata(in.ContextSnapshot)
	if err != nil {
		return models.AgentWakeup{}, err
	}
	if in.IdempotencyKey != nil && strings.TrimSpace(*in.IdempotencyKey) != "" {
		key := strings.TrimSpace(*in.IdempotencyKey)
		var existing models.AgentWakeup
		err := s.db.GetContext(ctx, &existing, `SELECT * FROM agent_wakeups WHERE agent_id=$1 AND task_id=$2 AND idempotency_key=$3`, agentID, taskID, key)
		if err == nil {
			var same bool
			if err := s.db.GetContext(ctx, &same, `SELECT payload_json=$4::jsonb AND context_snapshot=$5::jsonb FROM agent_wakeups WHERE agent_id=$1 AND task_id=$2 AND idempotency_key=$3`, agentID, taskID, key, []byte(payload), []byte(snapshot)); err != nil {
				return models.AgentWakeup{}, err
			}
			if !same {
				return models.AgentWakeup{}, ErrConflict
			}
			return existing, nil
		}
		if !errors.Is(mapNotFound(err), ErrNotFound) {
			return models.AgentWakeup{}, err
		}
		in.IdempotencyKey = &key
	} else {
		var existing models.AgentWakeup
		err := s.db.GetContext(ctx, &existing, `SELECT * FROM agent_wakeups
			WHERE agent_id=$1 AND task_id=$2 AND source=$3 AND reason=$4 AND payload_json=$5::jsonb AND status IN ('queued','claimed','running')
			ORDER BY created_at DESC LIMIT 1`, agentID, taskID, source, reason, []byte(payload))
		if err == nil {
			if _, err := s.db.ExecContext(ctx, "UPDATE agent_wakeups SET coalesced_count=coalesced_count+1, updated_at=now() WHERE id=$1", existing.ID); err != nil {
				return models.AgentWakeup{}, err
			}
			s.Notify(ctx, "wakeup", existing.ID)
			return s.GetWakeup(ctx, existing.ID)
		}
		if !errors.Is(mapNotFound(err), ErrNotFound) {
			return models.AgentWakeup{}, err
		}
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO agent_wakeups (id, agent_id, task_id, source, reason, payload_json, context_snapshot, idempotency_key, requester_type, requester_id, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'queued')`, id, agentID, taskID, source, reason, []byte(payload), []byte(snapshot), in.IdempotencyKey, requesterType, in.RequesterID); err != nil {
		return models.AgentWakeup{}, err
	}
	s.Notify(ctx, "wakeup", id)
	return s.GetWakeup(ctx, id)
}

func (s *Store) DispatchWakeups(ctx context.Context, limit int) (int, error) {
	type candidate struct {
		AgentID string `db:"agent_id"`
		TaskID  string `db:"task_id"`
	}
	var candidates []candidate
	if err := s.db.SelectContext(ctx, &candidates, `SELECT a.id AS agent_id, t.id AS task_id
		FROM agents a
		JOIN tasks t ON t.assignee_agent_id = a.id
		WHERE a.enabled = true
		  AND t.status IN ('todo','in_progress','blocked')
		  AND t.retry_count < $2
		  AND NOT EXISTS (
		      SELECT 1 FROM runs r
		      WHERE r.task_id = t.id AND r.status IN ('queued','running')
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_wakeups w
		      WHERE w.task_id = t.id AND w.status IN ('queued','claimed','running')
		  )
		ORDER BY t.priority DESC, t.updated_at ASC
		LIMIT $1`, limit, maxTaskRetries); err != nil {
		return 0, err
	}
	count := 0
	for _, candidate := range candidates {
		payload := json.RawMessage(fmt.Sprintf(`{"task_id":%q}`, candidate.TaskID))
		if _, err := s.EnqueueWakeup(ctx, candidate.TaskID, candidate.AgentID, WakeupInput{
			Source:          "heartbeat",
			Reason:          "heartbeat",
			PayloadJSON:     payload,
			ContextSnapshot: json.RawMessage(fmt.Sprintf(`{"task_id":%q,"wake_reason":"heartbeat","source":"heartbeat"}`, candidate.TaskID)),
			RequesterType:   "system",
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *Store) ClaimQueuedWakeup(ctx context.Context) (models.Run, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return models.Run{}, false, err
	}
	var wakeup models.AgentWakeup
	err = tx.GetContext(ctx, &wakeup, `SELECT * FROM agent_wakeups
		WHERE status='queued'
		ORDER BY CASE WHEN source='manual' THEN 0 ELSE 1 END, created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return models.Run{}, false, nil
		}
		return models.Run{}, false, mapNotFound(err)
	}
	var route struct {
		LifecycleID string `db:"lifecycle_id"`
		CLIKind     string `db:"cli_kind"`
	}
	if err := tx.GetContext(ctx, &route, `SELECT t.lifecycle_id, p.default_cli_kind AS cli_kind
		FROM tasks t JOIN projects p ON p.id=t.project_id WHERE t.id=$1`, wakeup.TaskID); err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	cliKind, err := s.lifecycleCLIKind(ctx, route.LifecycleID, wakeup.AgentID, route.CLIKind)
	if err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	runID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, agent_id, task_id, wakeup_id, status, cli_kind, started_at)
		VALUES ($1,$2,$3,$4,'running',$5,now())`, runID, wakeup.AgentID, wakeup.TaskID, wakeup.ID, cliKind); err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_wakeups SET status='running', claimed_at=now(), updated_at=now(), run_id=$2 WHERE id=$1`, wakeup.ID, runID); err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tasks
		SET status=CASE WHEN status IN ('todo','blocked') THEN 'in_progress' ELSE status END,
		    checkout_run_id=$2,
		    execution_run_id=$2,
		    execution_state='running',
		    updated_at=now()
		WHERE id=$1`, wakeup.TaskID, runID); err != nil {
		_ = tx.Rollback()
		return models.Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return models.Run{}, false, err
	}
	s.Notify(ctx, "wakeup", wakeup.ID)
	s.Notify(ctx, "run", runID)
	s.Notify(ctx, "task", wakeup.TaskID)
	run, err := s.GetRun(ctx, runID)
	return run, true, err
}

func (s *Store) FinishWakeupForRun(ctx context.Context, runID, status, message string) error {
	wakeupStatus := status
	if status == "cancelled" {
		wakeupStatus = "cancelled"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE agent_wakeups
		SET status=$2, finished_at=now(), error=$3, updated_at=now()
		WHERE run_id=$1`, runID, wakeupStatus, message)
	if err == nil {
		s.Notify(ctx, "wakeup", runID)
	}
	return err
}

func (s *Store) ClearExecutionLock(ctx context.Context, runID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks
		SET checkout_run_id=NULL, execution_run_id=NULL, execution_state=NULL, updated_at=now()
		WHERE execution_run_id=$1 OR checkout_run_id=$1`, runID)
	return err
}

func (s *Store) TaskRunContext(ctx context.Context, run models.Run) (models.Agent, models.Task, *models.Repo, []models.Comment, []models.TaskArtifact, RunContext, error) {
	agent, task, repo, comments, artifacts, err := s.TaskContext(ctx, run)
	if err != nil {
		return agent, task, repo, comments, artifacts, RunContext{}, err
	}
	var out RunContext
	if run.WakeupID != nil {
		wakeup, err := s.GetWakeup(ctx, *run.WakeupID)
		if err != nil {
			return agent, task, repo, comments, artifacts, out, err
		}
		out.Wakeup = &wakeup
	}
	var runtime models.AgentRuntimeState
	err = s.db.GetContext(ctx, &runtime, "SELECT * FROM agent_runtime_state WHERE agent_id=$1 AND task_id=$2 AND adapter_type=$3", run.AgentID, run.TaskID, run.CLIKind)
	if err == nil {
		out.RuntimeState = &runtime
	} else if !errors.Is(mapNotFound(err), ErrNotFound) {
		return agent, task, repo, comments, artifacts, out, err
	}
	if err := s.db.SelectContext(ctx, &out.RecentRuns, `SELECT * FROM runs
		WHERE task_id=$1 AND id<>$2
		ORDER BY created_at DESC
		LIMIT 5`, run.TaskID, run.ID); err != nil {
		return agent, task, repo, comments, artifacts, out, err
	}
	return agent, task, repo, comments, artifacts, out, nil
}

func (s *Store) UpdateRuntimeState(ctx context.Context, run models.Run, sessionID *string, state json.RawMessage, status string) error {
	state, err := normalizeMetadata(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_runtime_state (agent_id, task_id, adapter_type, session_id, state_json, last_run_id, last_run_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (agent_id, task_id, adapter_type)
		DO UPDATE SET session_id=$4, state_json=$5, last_run_id=$6, last_run_status=$7, updated_at=now()`,
		run.AgentID, run.TaskID, run.CLIKind, sessionID, []byte(state), run.ID, status)
	return err
}

func (s *Store) CheckoutTask(ctx context.Context, taskID string, in CheckoutInput) (models.Task, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	if task.CheckoutRunID != nil || task.ExecutionRunID != nil {
		return task, ErrConflict
	}
	if in.ExpectedStatus != nil && strings.TrimSpace(*in.ExpectedStatus) != "" && task.Status != strings.TrimSpace(*in.ExpectedStatus) {
		return task, ErrConflict
	}
	assignee := task.AssigneeAgentID
	if in.AssigneeAgentID != nil {
		assignee, err = s.resolveAgentRef(ctx, in.AssigneeAgentID)
		if err != nil {
			return task, err
		}
	}
	runID := uuid.NewString()
	if in.RunID != nil && strings.TrimSpace(*in.RunID) != "" {
		runID = strings.TrimSpace(*in.RunID)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tasks
		SET assignee_agent_id=$2, status='in_progress', checkout_run_id=$3, execution_state='checked_out', updated_at=now()
		WHERE id=$1 AND checkout_run_id IS NULL AND execution_run_id IS NULL`, taskID, assignee, runID)
	if err != nil {
		return task, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return task, ErrConflict
	}
	s.Notify(ctx, "task", taskID)
	return s.GetTask(ctx, taskID)
}

func (s *Store) ReleaseTask(ctx context.Context, taskID string, in ReleaseInput) (models.Task, error) {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return task, err
	}
	if task.CheckoutRunID == nil {
		return task, ErrConflict
	}
	if in.RunID != nil && strings.TrimSpace(*in.RunID) != "" && *task.CheckoutRunID != strings.TrimSpace(*in.RunID) {
		return task, ErrConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tasks
		SET checkout_run_id=NULL, execution_run_id=NULL, execution_state=NULL, updated_at=now()
		WHERE id=$1`, taskID)
	if err != nil {
		return task, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return task, ErrConflict
	}
	s.Notify(ctx, "task", taskID)
	return s.GetTask(ctx, taskID)
}

func (s *Store) ListInteractions(ctx context.Context, taskID string) ([]models.TaskInteraction, error) {
	interactions := []models.TaskInteraction{}
	return interactions, s.db.SelectContext(ctx, &interactions, "SELECT * FROM task_interactions WHERE task_id=$1 ORDER BY created_at DESC", taskID)
}

func (s *Store) GetInteraction(ctx context.Context, id string) (models.TaskInteraction, error) {
	var interaction models.TaskInteraction
	if err := s.db.GetContext(ctx, &interaction, "SELECT * FROM task_interactions WHERE id=$1", id); err != nil {
		return interaction, mapNotFound(err)
	}
	return interaction, nil
}

func (s *Store) CreateInteraction(ctx context.Context, taskID string, in InteractionInput) (models.TaskInteraction, error) {
	if _, err := s.GetTask(ctx, taskID); err != nil {
		return models.TaskInteraction{}, err
	}
	kind := strings.TrimSpace(in.Kind)
	if err := validateInteractionKind(kind); err != nil {
		return models.TaskInteraction{}, err
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "open"
	}
	if err := validateInteractionStatus(status); err != nil {
		return models.TaskInteraction{}, err
	}
	policy := strings.TrimSpace(in.ContinuationPolicy)
	if policy == "" {
		policy = "none"
	}
	if err := validateContinuationPolicy(policy); err != nil {
		return models.TaskInteraction{}, err
	}
	createdBy := strings.TrimSpace(in.CreatedBy)
	if createdBy == "" {
		createdBy = "api"
	}
	payload, err := normalizeMetadata(in.Payload)
	if err != nil {
		return models.TaskInteraction{}, err
	}
	if in.IdempotencyKey != nil && strings.TrimSpace(*in.IdempotencyKey) != "" {
		key := strings.TrimSpace(*in.IdempotencyKey)
		var existing models.TaskInteraction
		err := s.db.GetContext(ctx, &existing, "SELECT * FROM task_interactions WHERE task_id=$1 AND idempotency_key=$2", taskID, key)
		if err == nil {
			var same bool
			if err := s.db.GetContext(ctx, &same, "SELECT payload=$3::jsonb AND kind=$4 AND continuation_policy=$5 FROM task_interactions WHERE task_id=$1 AND idempotency_key=$2", taskID, key, []byte(payload), kind, policy); err != nil {
				return models.TaskInteraction{}, err
			}
			if !same {
				return models.TaskInteraction{}, ErrConflict
			}
			return existing, nil
		}
		if !errors.Is(mapNotFound(err), ErrNotFound) {
			return models.TaskInteraction{}, err
		}
		in.IdempotencyKey = &key
	}
	id := uuid.NewString()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO task_interactions (id, task_id, kind, status, title, summary, payload, continuation_policy, idempotency_key, source_comment_id, source_run_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		id, taskID, kind, status, strings.TrimSpace(in.Title), strings.TrimSpace(in.Summary), []byte(payload), policy, in.IdempotencyKey, in.SourceCommentID, in.SourceRunID, createdBy); err != nil {
		return models.TaskInteraction{}, err
	}
	s.Notify(ctx, "interaction", taskID)
	return s.GetInteraction(ctx, id)
}

func (s *Store) ResolveInteraction(ctx context.Context, id, status, resolver string) (models.TaskInteraction, error) {
	if err := validateInteractionStatus(status); err != nil {
		return models.TaskInteraction{}, err
	}
	if status != "accepted" && status != "rejected" {
		return models.TaskInteraction{}, errors.New("interaction can only be accepted or rejected")
	}
	interaction, err := s.GetInteraction(ctx, id)
	if err != nil {
		return interaction, err
	}
	if interaction.Status != "open" {
		return interaction, ErrConflict
	}
	if strings.TrimSpace(resolver) == "" {
		resolver = "api"
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE task_interactions
		SET status=$2, resolved_by=$3, resolved_at=now(), updated_at=now()
		WHERE id=$1 AND status='open'`, id, status, resolver); err != nil {
		return interaction, err
	}
	if status == "accepted" && interaction.ContinuationPolicy == "wake_assignee" {
		body, _ := json.Marshal(map[string]string{"interaction_id": interaction.ID, "kind": interaction.Kind, "status": "accepted"})
		_, err := s.EnqueueTaskWakeup(ctx, interaction.TaskID, WakeupInput{
			Source:          "interaction",
			Reason:          "interaction_accepted",
			PayloadJSON:     body,
			ContextSnapshot: json.RawMessage(fmt.Sprintf(`{"task_id":%q,"interaction_id":%q,"wake_reason":"interaction_accepted","source":"task_interaction"}`, interaction.TaskID, interaction.ID)),
			RequesterType:   "interaction",
			RequesterID:     &interaction.ID,
		})
		if err != nil {
			return interaction, err
		}
	}
	s.Notify(ctx, "interaction", interaction.TaskID)
	return s.GetInteraction(ctx, id)
}

func (s *Store) GetTaskLiveness(ctx context.Context, taskID string) (models.TaskLiveness, error) {
	var liveness models.TaskLiveness
	if err := s.db.GetContext(ctx, &liveness, "SELECT * FROM task_liveness WHERE task_id=$1", taskID); err != nil {
		return liveness, mapNotFound(err)
	}
	return liveness, nil
}

func (s *Store) GetActiveRun(ctx context.Context, taskID string) (*models.Run, error) {
	var run models.Run
	err := s.db.GetContext(ctx, &run, `SELECT * FROM runs WHERE task_id=$1 AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1`, taskID)
	if err != nil {
		if errors.Is(mapNotFound(err), ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &run, nil
}

func validateInteractionKind(kind string) error {
	switch kind {
	case "suggest_tasks", "ask_user_questions", "request_confirmation", "handoff", "qa_finding", "approval_request":
		return nil
	default:
		return errors.New("invalid interaction kind")
	}
}

func validateInteractionStatus(status string) error {
	switch status {
	case "open", "accepted", "rejected", "resolved", "cancelled":
		return nil
	default:
		return errors.New("invalid interaction status")
	}
}

func validateContinuationPolicy(policy string) error {
	switch policy {
	case "none", "wake_assignee":
		return nil
	default:
		return errors.New("invalid continuation policy")
	}
}
