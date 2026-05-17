package models

import "time"

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
	ID              string    `db:"id" json:"id"`
	ProjectID       string    `db:"project_id" json:"project_id"`
	RepoID          *string   `db:"repo_id" json:"repo_id"`
	Title           string    `db:"title" json:"title"`
	Description     string    `db:"description" json:"description"`
	Status          string    `db:"status" json:"status"`
	AssigneeAgentID *string   `db:"assignee_agent_id" json:"assignee_agent_id"`
	ParentID        *string   `db:"parent_id" json:"parent_id"`
	Priority        int       `db:"priority" json:"priority"`
	RetryCount      int       `db:"retry_count" json:"retry_count"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time `db:"updated_at" json:"updated_at"`
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

type RunEvent struct {
	ID      int64     `db:"id" json:"id"`
	RunID   string    `db:"run_id" json:"run_id"`
	TS      time.Time `db:"ts" json:"ts"`
	Level   string    `db:"level" json:"level"`
	Message string    `db:"message" json:"message"`
}
