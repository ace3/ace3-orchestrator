ALTER TABLE skill_sources
    ADD COLUMN IF NOT EXISTS path_filter TEXT NOT NULL DEFAULT '';

ALTER TABLE skills
    ADD COLUMN IF NOT EXISTS ignored BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_skills_installable
    ON skills(source_id, archived, ignored);
