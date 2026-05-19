ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS checkout_run_id TEXT,
    ADD COLUMN IF NOT EXISTS execution_run_id TEXT,
    ADD COLUMN IF NOT EXISTS execution_state TEXT;

ALTER TABLE runs
    ADD COLUMN IF NOT EXISTS wakeup_id TEXT;

CREATE TABLE IF NOT EXISTS agent_wakeups (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    payload_json JSONB NOT NULL DEFAULT '{}'::JSONB,
    context_snapshot JSONB NOT NULL DEFAULT '{}'::JSONB,
    idempotency_key TEXT,
    requester_type TEXT NOT NULL DEFAULT 'system',
    requester_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('queued','claimed','running','done','error','cancelled','coalesced')),
    coalesced_count INTEGER NOT NULL DEFAULT 0,
    run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    claimed_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE runs
    DROP CONSTRAINT IF EXISTS runs_wakeup_id_fkey,
    ADD CONSTRAINT runs_wakeup_id_fkey FOREIGN KEY (wakeup_id) REFERENCES agent_wakeups(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_wakeups_idempotency
    ON agent_wakeups(agent_id, task_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_agent_wakeups_claim
    ON agent_wakeups(status, created_at);

CREATE INDEX IF NOT EXISTS idx_agent_wakeups_task
    ON agent_wakeups(task_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_runs_wakeup
    ON runs(wakeup_id);

CREATE TABLE IF NOT EXISTS task_interactions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('suggest_tasks','ask_user_questions','request_confirmation','handoff','qa_finding','approval_request')),
    status TEXT NOT NULL CHECK (status IN ('open','accepted','rejected','resolved','cancelled')),
    title TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::JSONB,
    continuation_policy TEXT NOT NULL CHECK (continuation_policy IN ('none','wake_assignee')),
    idempotency_key TEXT,
    source_comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
    source_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    created_by TEXT NOT NULL,
    resolved_by TEXT,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_task_interactions_idempotency
    ON task_interactions(task_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_task_interactions_task
    ON task_interactions(task_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_runtime_state (
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    adapter_type TEXT NOT NULL CHECK (adapter_type IN ('claude','codex')),
    session_id TEXT,
    state_json JSONB NOT NULL DEFAULT '{}'::JSONB,
    last_run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    last_run_status TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, task_id, adapter_type)
);

CREATE OR REPLACE VIEW task_liveness AS
SELECT
    t.id AS task_id,
    CASE
        WHEN t.status IN ('done','cancelled') THEN 'ready'
        WHEN EXISTS (
            SELECT 1 FROM runs r
            WHERE r.task_id = t.id AND r.status = 'running'
        ) THEN 'running'
        WHEN EXISTS (
            SELECT 1 FROM task_interactions i
            WHERE i.task_id = t.id AND i.status = 'open'
        ) OR t.status = 'in_review' THEN 'waiting'
        WHEN EXISTS (
            SELECT 1 FROM agent_wakeups w
            WHERE w.task_id = t.id AND w.status IN ('queued','claimed')
        ) THEN 'ready'
        ELSE 'stalled'
    END AS liveness,
    EXISTS (
        SELECT 1 FROM runs r
        WHERE r.task_id = t.id AND r.status = 'running'
    ) AS has_active_run,
    EXISTS (
        SELECT 1 FROM agent_wakeups w
        WHERE w.task_id = t.id AND w.status IN ('queued','claimed')
    ) AS has_queued_wake,
    EXISTS (
        SELECT 1 FROM task_interactions i
        WHERE i.task_id = t.id AND i.status = 'open'
    ) AS has_waiting_interaction,
    t.status = 'in_review' AS has_human_review,
    t.updated_at AS task_updated_at
FROM tasks t;
