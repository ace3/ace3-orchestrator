CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    role TEXT NOT NULL,
    role_prompt TEXT NOT NULL,
    cli_kind TEXT NOT NULL CHECK (cli_kind IN ('claude', 'codex')),
    cli_profile TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS skill_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    upstream_url TEXT NOT NULL,
    pinned_sha TEXT NOT NULL,
    last_synced_at TIMESTAMPTZ,
    kind TEXT NOT NULL CHECK (kind IN ('verzth', 'ace3', 'custom')),
    has_update BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL REFERENCES skill_sources(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    path_in_source TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    archived BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (source_id, path_in_source)
);

CREATE TABLE IF NOT EXISTS agent_skills (
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, skill_id)
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    default_cli_kind TEXT NOT NULL CHECK (default_cli_kind IN ('claude', 'codex')),
    default_branch_strategy TEXT NOT NULL CHECK (default_branch_strategy IN ('worktree-per-run', 'shared')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS repos (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    local_path TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT 'main',
    status TEXT NOT NULL CHECK (status IN ('ok', 'missing', 'dirty')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, local_path)
);

CREATE INDEX IF NOT EXISTS idx_agents_enabled ON agents(enabled);
CREATE INDEX IF NOT EXISTS idx_skills_source ON skills(source_id, archived);
CREATE INDEX IF NOT EXISTS idx_repos_project ON repos(project_id);
