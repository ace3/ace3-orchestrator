ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS lifecycle_id TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_tasks_tags ON tasks USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_tasks_lifecycle ON tasks(lifecycle_id);
