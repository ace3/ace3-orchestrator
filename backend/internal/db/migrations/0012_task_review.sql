ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS last_review_decision TEXT CHECK (last_review_decision IN ('approved','changes_requested','rejected')),
    ADD COLUMN IF NOT EXISTS last_review_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS task_review_comments (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    run_id TEXT REFERENCES runs(id) ON DELETE SET NULL,
    file_path TEXT NOT NULL,
    line_start INTEGER,
    line_end INTEGER,
    body TEXT NOT NULL,
    author TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('open','resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_review_comments_task
    ON task_review_comments(task_id, created_at);

CREATE INDEX IF NOT EXISTS idx_review_comments_open
    ON task_review_comments(task_id, status, file_path, line_start)
    WHERE status = 'open';
