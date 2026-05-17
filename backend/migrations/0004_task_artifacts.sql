CREATE TABLE IF NOT EXISTS task_artifacts (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('pm_document','pm_handoff','em_document','em_handoff','qa_report','implementation_note','run_log','other')),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    format TEXT NOT NULL CHECK (format IN ('markdown','text','json')),
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_by TEXT NOT NULL,
    run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_task_artifacts_task ON task_artifacts(task_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_artifacts_kind ON task_artifacts(task_id, kind, updated_at DESC);
