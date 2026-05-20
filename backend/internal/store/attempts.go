package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"mini-paperclip/backend/internal/models"
)

type AttemptInput struct {
	AgentID string `json:"agent_id"`
	CLI     string `json:"cli"`
	Model   string `json:"model"`
	Label   string `json:"label"`
}

type AttemptWakeupsResult struct {
	AttemptsGroupID string               `json:"attempts_group_id"`
	Wakeups         []models.AgentWakeup `json:"wakeups"`
}

type attemptPayload struct {
	AttemptsGroupID string `json:"attempts_group_id"`
	AttemptIndex    int    `json:"attempt_index"`
	AttemptLabel    string `json:"attempt_label"`
	AttemptCLI      string `json:"attempt_cli"`
	AttemptModel    string `json:"attempt_model"`
}

type AttemptDiffSource struct {
	RunID        string `db:"run_id"`
	AttemptIndex *int   `db:"attempt_index"`
	AttemptLabel string `db:"attempt_label"`
	WorktreePath string `db:"worktree_path"`
	BaseRef      string `db:"base_ref"`
}

func (s *Store) EnqueueAttemptWakeups(ctx context.Context, taskID string, attempts []AttemptInput) (AttemptWakeupsResult, error) {
	if len(attempts) == 0 || len(attempts) > 3 {
		return AttemptWakeupsResult{}, errors.New("attempts must contain 1 to 3 entries")
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return AttemptWakeupsResult{}, err
	}
	if task.AssigneeAgentID == nil {
		return AttemptWakeupsResult{}, errors.New("task has no assignee")
	}
	groupID := uuid.NewString()
	wakeups := make([]models.AgentWakeup, 0, len(attempts))
	for index, attempt := range attempts {
		agentID := strings.TrimSpace(attempt.AgentID)
		if agentID == "" {
			agentID = *task.AssigneeAgentID
		}
		cli := strings.TrimSpace(attempt.CLI)
		if cli != "" && cli != "claude" && cli != "codex" {
			return AttemptWakeupsResult{}, fmt.Errorf("invalid attempt cli %q", cli)
		}
		payload, _ := json.Marshal(attemptPayload{
			AttemptsGroupID: groupID,
			AttemptIndex:    index + 1,
			AttemptLabel:    attemptLabel(attempt, index+1),
			AttemptCLI:      cli,
			AttemptModel:    strings.TrimSpace(attempt.Model),
		})
		key := fmt.Sprintf("attempt:%s:%d", groupID, index+1)
		wakeup, err := s.EnqueueWakeup(ctx, taskID, agentID, WakeupInput{
			Source:         "attempt",
			Reason:         "parallel_attempt",
			PayloadJSON:    payload,
			IdempotencyKey: &key,
			RequesterType:  "human",
			RequesterID:    stringPtrStore("ignas"),
		})
		if err != nil {
			return AttemptWakeupsResult{}, err
		}
		wakeups = append(wakeups, wakeup)
	}
	return AttemptWakeupsResult{AttemptsGroupID: groupID, Wakeups: wakeups}, nil
}

func (s *Store) ListAttemptRuns(ctx context.Context, taskID, groupID string) ([]models.Run, error) {
	runs := []models.Run{}
	return runs, s.db.SelectContext(ctx, &runs, `SELECT * FROM runs
		WHERE task_id=$1 AND attempts_group_id=$2
		ORDER BY attempt_index, created_at`, taskID, groupID)
}

func (s *Store) SelectAttempt(ctx context.Context, taskID, groupID, runID string) (models.Task, error) {
	var exists bool
	if err := s.db.GetContext(ctx, &exists, `SELECT EXISTS (
		SELECT 1 FROM runs WHERE id=$1 AND task_id=$2 AND attempts_group_id=$3 AND status='done'
	)`, runID, taskID, groupID); err != nil {
		return models.Task{}, err
	}
	if !exists {
		return models.Task{}, ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks
		SET selected_run_id=$2, checkout_run_id=$2, execution_run_id=$2, status='in_review', execution_state='selected_attempt', updated_at=now()
		WHERE id=$1`, taskID, runID); err != nil {
		return models.Task{}, err
	}
	s.Notify(ctx, "task", taskID)
	return s.GetTask(ctx, taskID)
}

func (s *Store) AttemptDiffSources(ctx context.Context, taskID, groupID string) ([]AttemptDiffSource, error) {
	sources := []AttemptDiffSource{}
	return sources, s.db.SelectContext(ctx, &sources, `SELECT r.id AS run_id, r.attempt_index, r.attempt_label, r.worktree_path, repo.default_branch AS base_ref
		FROM runs r
		JOIN tasks t ON t.id=r.task_id
		JOIN repos repo ON repo.id=t.repo_id
		WHERE r.task_id=$1 AND r.attempts_group_id=$2 AND r.worktree_path IS NOT NULL
		ORDER BY r.attempt_index, r.created_at`, taskID, groupID)
}

func attemptLabel(attempt AttemptInput, index int) string {
	if strings.TrimSpace(attempt.Label) != "" {
		return strings.TrimSpace(attempt.Label)
	}
	parts := []string{}
	if strings.TrimSpace(attempt.AgentID) != "" {
		parts = append(parts, strings.TrimSpace(attempt.AgentID))
	}
	if strings.TrimSpace(attempt.CLI) != "" {
		parts = append(parts, strings.TrimSpace(attempt.CLI))
	}
	if strings.TrimSpace(attempt.Model) != "" {
		parts = append(parts, strings.TrimSpace(attempt.Model))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("attempt-%d", index)
	}
	return strings.Join(parts, " / ")
}

func stringPtrStore(value string) *string {
	return &value
}
