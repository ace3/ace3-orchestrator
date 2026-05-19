INSERT INTO app_settings (key, value) VALUES ('default_codex_model', 'gpt-5.3-codex')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('planning_codex_model', 'gpt-5.5')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('default_claude_model', 'claude-sonnet-4-6')
ON CONFLICT (key) DO NOTHING;

INSERT INTO app_settings (key, value) VALUES ('planning_claude_model', 'claude-opus-4-7')
ON CONFLICT (key) DO NOTHING;

UPDATE app_settings
SET value = 'gpt-5.3-codex', updated_at = now()
WHERE key = 'default_model' AND value = 'claude-sonnet-4-6';
