ALTER TABLE tasks
    DROP CONSTRAINT IF EXISTS tasks_status_check,
    ADD CONSTRAINT tasks_status_check CHECK (status IN ('todo','in_progress','in_review','waiting','blocked','done','cancelled'));

ALTER TABLE task_interactions
    ADD COLUMN IF NOT EXISTS resolution_payload JSONB NOT NULL DEFAULT '{}'::JSONB;

CREATE OR REPLACE VIEW task_liveness AS
SELECT
    t.id AS task_id,
    CASE
        WHEN t.status IN ('done','cancelled') THEN 'ready'
        WHEN EXISTS (
            SELECT 1 FROM runs r
            WHERE r.task_id = t.id AND r.status = 'running'
        ) THEN 'running'
        WHEN t.status = 'waiting' OR EXISTS (
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
    t.status = 'waiting' OR EXISTS (
        SELECT 1 FROM task_interactions i
        WHERE i.task_id = t.id AND i.status = 'open'
    ) AS has_waiting_interaction,
    t.status = 'in_review' AS has_human_review,
    t.updated_at AS task_updated_at
FROM tasks t;
