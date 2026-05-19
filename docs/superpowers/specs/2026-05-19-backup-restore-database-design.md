# Backup & Restore Database Frontend Design

## Summary

Add a dedicated Backup & Restore admin page with two lanes:

- Full PostgreSQL backup and operator-run restore planning.
- ACE3 application-level JSON export and guarded import.

Full database backups can be created and downloaded from the UI. Full database restore is never executed by the browser; the UI validates a dump and produces operator instructions. ACE3 JSON restore can run from the UI after validation, dry-run review, automatic pre-restore backup, and typed confirmation.

## Architecture

Backend backup routes live under `/api/backups/*`. A backup service owns filesystem artifacts under `MP_BACKUP_DIR`, full database dump creation, dump validation, ACE3 export, ACE3 validation, dry-run counts, and app import.

The frontend adds a `Backup & Restore` sidebar route with two tabs:

- `Full Database`: create backup, list/download backups, upload/validate dump metadata, and generate restore instructions.
- `ACE3 Data`: export all or selected bundles, upload JSON, validate bundles, preview dry-run counts, and restore selected bundles.

The database does not contain the Git/file skill cache. The UI and docs warn operators that `MP_SKILLS_CACHE_DIR` must stay on persistent storage or be recovered by skill sync.

## ACE3 Bundles

ACE3 exports are versioned JSON documents with bundle-level selection:

- `configuration`: agents, skill sources, skills metadata, agent-skill assignments, lifecycles, lifecycle steps, app settings.
- `projects`: projects and repos.
- `tasks`: tasks, comments, task artifacts, task interactions.
- `execution_history`: wakeups, runs, run events, runtime state.

Partial restore is bundle-based, not raw table-based. Dry-run validates dependencies and reports inserted and updated counts per table. Import uses merge overwrite: matching IDs are updated, new IDs are inserted, and unrelated existing records are preserved.

## Safety Rules

App import is blocked if there are active runs or active wakeups. Every app import creates a pre-restore ACE3 export first. Import runs in one transaction and rolls back completely on failure.

Imported active execution history must not restart work. Any imported queued, claimed, or running execution rows are normalized to terminal historical states before writing.

Full database restore is operator-run only. The backend may validate a dump and produce a restore runbook, but it must not run `pg_restore`.

## Testing

Backend tests cover backup listing, full dump metadata and missing-tool errors, restore-plan generation without executing restore, ACE3 export bundle completeness, dry-run dependency validation, active execution blocking, merge overwrite behavior, unrelated-record preservation, and rollback on failure.

Frontend verification covers backup listing, validation states, dry-run summaries, typed confirmation, disabled restore states, and generated operator restore instructions through TypeScript build coverage.
