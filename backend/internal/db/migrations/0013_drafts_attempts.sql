CREATE TABLE IF NOT EXISTS task_drafts (
    id TEXT PRIMARY KEY,
    author TEXT NOT NULL,
    repo_id TEXT REFERENCES repos(id) ON DELETE SET NULL,
    conversation JSONB NOT NULL DEFAULT '[]'::JSONB,
    preview_brief JSONB,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','submitted','discarded')),
    finalized_task_id TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_drafts_status
    ON task_drafts(status, updated_at DESC);

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS attempts_group_id TEXT,
    ADD COLUMN IF NOT EXISTS attempt_index INTEGER,
    ADD COLUMN IF NOT EXISTS attempt_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attempt_model TEXT NOT NULL DEFAULT '';

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS selected_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_runs_attempts_group
    ON runs(attempts_group_id);
