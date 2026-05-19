package models

import (
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

type Agent struct {
	ID             string    `db:"id" json:"id"`
	Name           string    `db:"name" json:"name"`
	Role           string    `db:"role" json:"role"`
	RolePrompt     string    `db:"role_prompt" json:"role_prompt"`
	BasePrompt     string    `db:"-" json:"base_prompt,omitempty"`
	DefinitionHash string    `db:"-" json:"definition_hash,omitempty"`
	CLIKind        string    `db:"cli_kind" json:"cli_kind"`
	CLIProfile     *string   `db:"cli_profile" json:"cli_profile"`
	Enabled        bool      `db:"enabled" json:"enabled"`
	Skills         []Skill   `json:"skills,omitempty"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

type SkillSource struct {
	ID           string     `db:"id" json:"id"`
	Name         string     `db:"name" json:"name"`
	UpstreamURL  string     `db:"upstream_url" json:"upstream_url"`
	PinnedSHA    string     `db:"pinned_sha" json:"pinned_sha"`
	LastSyncedAt *time.Time `db:"last_synced_at" json:"last_synced_at"`
	Kind         string     `db:"kind" json:"kind"`
	HasUpdate    bool       `db:"has_update" json:"has_update"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

type Skill struct {
	ID           string    `db:"id" json:"id"`
	SourceID     string    `db:"source_id" json:"source_id"`
	Name         string    `db:"name" json:"name"`
	PathInSource string    `db:"path_in_source" json:"path_in_source"`
	Version      string    `db:"version" json:"version"`
	Archived     bool      `db:"archived" json:"archived"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

type Project struct {
	ID                    string    `db:"id" json:"id"`
	Name                  string    `db:"name" json:"name"`
	Description           string    `db:"description" json:"description"`
	DefaultCLIKind        string    `db:"default_cli_kind" json:"default_cli_kind"`
	DefaultBranchStrategy string    `db:"default_branch_strategy" json:"default_branch_strategy"`
	Repos                 []Repo    `json:"repos,omitempty"`
	CreatedAt             time.Time `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time `db:"updated_at" json:"updated_at"`
}

type Repo struct {
	ID            string    `db:"id" json:"id"`
	ProjectID     string    `db:"project_id" json:"project_id"`
	LocalPath     string    `db:"local_path" json:"local_path"`
	DefaultBranch string    `db:"default_branch" json:"default_branch"`
	Status        string    `db:"status" json:"status"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
}

type Task struct {
	ID              string         `db:"id" json:"id"`
	ProjectID       string         `db:"project_id" json:"project_id"`
	RepoID          *string        `db:"repo_id" json:"repo_id"`
	Title           string         `db:"title" json:"title"`
	Description     string         `db:"description" json:"description"`
	Status          string         `db:"status" json:"status"`
	AssigneeAgentID *string        `db:"assignee_agent_id" json:"assignee_agent_id"`
	ParentID        *string        `db:"parent_id" json:"parent_id"`
	Priority        int            `db:"priority" json:"priority"`
	RetryCount      int            `db:"retry_count" json:"retry_count"`
	Tags            pq.StringArray `db:"tags" json:"tags"`
	LifecycleID     string         `db:"lifecycle_id" json:"lifecycle_id"`
	CheckoutRunID   *string        `db:"checkout_run_id" json:"checkout_run_id"`
	ExecutionRunID  *string        `db:"execution_run_id" json:"execution_run_id"`
	ExecutionState  *string        `db:"execution_state" json:"execution_state"`
	CreatedAt       time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time      `db:"updated_at" json:"updated_at"`
}

type TaskArtifact struct {
	ID        string          `db:"id" json:"id"`
	TaskID    string          `db:"task_id" json:"task_id"`
	Kind      string          `db:"kind" json:"kind"`
	Title     string          `db:"title" json:"title"`
	Body      string          `db:"body" json:"body"`
	Format    string          `db:"format" json:"format"`
	Metadata  json.RawMessage `db:"metadata" json:"metadata"`
	CreatedBy string          `db:"created_by" json:"created_by"`
	RunID     *string         `db:"run_id" json:"run_id"`
	CreatedAt time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt time.Time       `db:"updated_at" json:"updated_at"`
}

type Comment struct {
	ID        string    `db:"id" json:"id"`
	TaskID    string    `db:"task_id" json:"task_id"`
	Author    string    `db:"author" json:"author"`
	Body      string    `db:"body" json:"body"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Run struct {
	ID           string     `db:"id" json:"id"`
	AgentID      string     `db:"agent_id" json:"agent_id"`
	TaskID       string     `db:"task_id" json:"task_id"`
	WakeupID     *string    `db:"wakeup_id" json:"wakeup_id"`
	Status       string     `db:"status" json:"status"`
	CLIKind      string     `db:"cli_kind" json:"cli_kind"`
	StartedAt    *time.Time `db:"started_at" json:"started_at"`
	FinishedAt   *time.Time `db:"finished_at" json:"finished_at"`
	ExitCode     *int       `db:"exit_code" json:"exit_code"`
	TokensIn     int        `db:"tokens_in" json:"tokens_in"`
	TokensOut    int        `db:"tokens_out" json:"tokens_out"`
	CostUSD      float64    `db:"cost_usd" json:"cost_usd"`
	PromptHash   string     `db:"prompt_hash" json:"prompt_hash"`
	WorktreePath *string    `db:"worktree_path" json:"worktree_path"`
	LogPath      *string    `db:"log_path" json:"log_path"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
}

type AgentWakeup struct {
	ID              string          `db:"id" json:"id"`
	AgentID         string          `db:"agent_id" json:"agent_id"`
	TaskID          string          `db:"task_id" json:"task_id"`
	Source          string          `db:"source" json:"source"`
	Reason          string          `db:"reason" json:"reason"`
	PayloadJSON     json.RawMessage `db:"payload_json" json:"payload_json"`
	ContextSnapshot json.RawMessage `db:"context_snapshot" json:"context_snapshot"`
	IdempotencyKey  *string         `db:"idempotency_key" json:"idempotency_key"`
	RequesterType   string          `db:"requester_type" json:"requester_type"`
	RequesterID     *string         `db:"requester_id" json:"requester_id"`
	Status          string          `db:"status" json:"status"`
	CoalescedCount  int             `db:"coalesced_count" json:"coalesced_count"`
	RunID           *string         `db:"run_id" json:"run_id"`
	ClaimedAt       *time.Time      `db:"claimed_at" json:"claimed_at"`
	FinishedAt      *time.Time      `db:"finished_at" json:"finished_at"`
	Error           string          `db:"error" json:"error"`
	CreatedAt       time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time       `db:"updated_at" json:"updated_at"`
}

type TaskInteraction struct {
	ID                 string          `db:"id" json:"id"`
	TaskID             string          `db:"task_id" json:"task_id"`
	Kind               string          `db:"kind" json:"kind"`
	Status             string          `db:"status" json:"status"`
	Title              string          `db:"title" json:"title"`
	Summary            string          `db:"summary" json:"summary"`
	Payload            json.RawMessage `db:"payload" json:"payload"`
	ContinuationPolicy string          `db:"continuation_policy" json:"continuation_policy"`
	IdempotencyKey     *string         `db:"idempotency_key" json:"idempotency_key"`
	SourceCommentID    *string         `db:"source_comment_id" json:"source_comment_id"`
	SourceRunID        *string         `db:"source_run_id" json:"source_run_id"`
	CreatedBy          string          `db:"created_by" json:"created_by"`
	ResolvedBy         *string         `db:"resolved_by" json:"resolved_by"`
	ResolvedAt         *time.Time      `db:"resolved_at" json:"resolved_at"`
	CreatedAt          time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time       `db:"updated_at" json:"updated_at"`
}

type AgentRuntimeState struct {
	AgentID       string          `db:"agent_id" json:"agent_id"`
	TaskID        string          `db:"task_id" json:"task_id"`
	AdapterType   string          `db:"adapter_type" json:"adapter_type"`
	SessionID     *string         `db:"session_id" json:"session_id"`
	StateJSON     json.RawMessage `db:"state_json" json:"state_json"`
	LastRunID     *string         `db:"last_run_id" json:"last_run_id"`
	LastRunStatus string          `db:"last_run_status" json:"last_run_status"`
	UpdatedAt     time.Time       `db:"updated_at" json:"updated_at"`
}

type TaskLiveness struct {
	TaskID                string    `db:"task_id" json:"task_id"`
	Liveness              string    `db:"liveness" json:"liveness"`
	HasActiveRun          bool      `db:"has_active_run" json:"has_active_run"`
	HasQueuedWake         bool      `db:"has_queued_wake" json:"has_queued_wake"`
	HasWaitingInteraction bool      `db:"has_waiting_interaction" json:"has_waiting_interaction"`
	HasHumanReview        bool      `db:"has_human_review" json:"has_human_review"`
	TaskUpdatedAt         time.Time `db:"task_updated_at" json:"task_updated_at"`
}

type RunEvent struct {
	ID      int64     `db:"id" json:"id"`
	RunID   string    `db:"run_id" json:"run_id"`
	TS      time.Time `db:"ts" json:"ts"`
	Level   string    `db:"level" json:"level"`
	Message string    `db:"message" json:"message"`
}
