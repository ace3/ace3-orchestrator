CREATE TABLE IF NOT EXISTS lifecycles (
    id          TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    is_default  BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lifecycle_steps (
    id           TEXT PRIMARY KEY,
    lifecycle_id TEXT NOT NULL REFERENCES lifecycles(id) ON DELETE CASCADE,
    position     INT NOT NULL,
    agent_id     TEXT NOT NULL,
    cli_kind     TEXT NOT NULL DEFAULT '',
    skip_when    TEXT[] NOT NULL DEFAULT '{}',
    include_when TEXT[] NOT NULL DEFAULT '{}',
    model_id     TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycle_steps_order ON lifecycle_steps(lifecycle_id, position);
CREATE INDEX IF NOT EXISTS idx_lifecycle_steps_lc ON lifecycle_steps(lifecycle_id);

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value) VALUES ('default_model', 'gpt-5.3-codex')
ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('default_codex_model', 'gpt-5.3-codex')
ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('planning_codex_model', 'gpt-5.5')
ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('default_claude_model', 'claude-sonnet-4-6')
ON CONFLICT (key) DO NOTHING;
INSERT INTO app_settings (key, value) VALUES ('planning_claude_model', 'claude-opus-4-7')
ON CONFLICT (key) DO NOTHING;
