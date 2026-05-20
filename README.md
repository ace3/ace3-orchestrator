# Nocturne

Local multi-agent engineering orchestrator from `PRD.md`.

## Quickstart

```sh
cp deploy/.env.example deploy/.env
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build
```

Open `http://localhost:8080` and keep the API token from `deploy/.env` in the sidebar token field.

![Nocturne projects screen](docs/screenshots/projects.png)

## Development

```sh
make test
make docker-up
make docker-ps
```

Backend runs on `:8081`; frontend dev server proxies `/api` to it.

### Local host-run development

Use this mode when you want to run only Postgres in Docker and run the hot-reload app processes on your host.

```sh
make local-dev
```

Open `http://localhost:5173` and use API token `dev-token`.
`make local-dev` starts the db-only Compose service, runs the backend through Air hot reload, and runs the Vite frontend. Press `Ctrl+C` to stop the frontend, backend, and database container; the Postgres volume is preserved.

`make local-dev` loads `deploy/.env.local` and runs the backend in `MP_RUNNER_MODE=mock`. This is the safe default for UI/API testing because task runs are deterministic and do not call real CLIs. Air must be installed on your host; with Go 1.21, use `go install github.com/cosmtrek/air@v1.49.0`.

You can still run each process separately:

```sh
make local-db-up
make local-backend-watch
make local-frontend
```

### Docker Compose hot-reload development

Use this mode when you want Postgres, the Air backend, and the Vite frontend to all run in Docker Compose.

```sh
make compose-dev
```

Open `http://localhost:5173` and use API token `dev-token`.
`make compose-dev` loads `deploy/.env.compose.dev`, mounts the backend and frontend source trees into containers, runs the backend with Air, and runs the frontend with Vite. Press `Ctrl+C` to stop and remove the compose-dev containers; named volumes preserve Postgres data, Go caches, frontend `node_modules`, skills cache, worktrees, and backups.

Use `make local-status` before starting either development mode. If another runtime is already active, stop the relevant mode first with `make local-stop` or `make compose-dev-down`.

#### Lifecycle smoke test

With `make local-dev` or `make compose-dev` already running in another terminal, push one task through the full PM → EM → Backend → Frontend → QA pipeline:

```sh
./scripts/smoke-pipeline.sh
```

The script targets `http://127.0.0.1:18081` with token `dev-token` (override via `BASE_URL` / `API_TOKEN`). It idempotently finds-or-creates a `smoke-pipeline` project, creates a fresh "Add debug health endpoint" task, polls the run to `done`, then re-runs the same task to verify idempotency. Exits non-zero with the event tail on failure. Re-run it after every code change to confirm the orchestrator still walks the lifecycle cleanly.

To run real Codex or Claude from your host shell:

```sh
make local-dev-cli
```

`make local-dev-cli` loads `deploy/.env.local.cli` and uses `MP_RUNNER_MODE=cli`. The host `codex` and/or `claude` commands must already be installed, authenticated, and available on `PATH`; the backend inherits your host environment, so no Docker credential mounts are used.

Local data lives in the `mini-paperclip-local_mp_local_pgdata` Docker volume. `make local-db-down` preserves the volume. If port `5432` is already in use, edit `POSTGRES_PORT` in `deploy/.env.local`; the local backend DSN uses that value. If port `8081` is already in use, edit `MP_PORT`; `make local-frontend` points Vite at that backend port.

## Safety Defaults

- All `/api/*` endpoints require `Authorization: Bearer $MP_API_TOKEN`.
- Repos must be under `MP_REPO_ALLOWLIST`; the backend rejects other paths before git validation.
- CLI runs are capped by `MP_CLI_TIMEOUT`, `MP_RUN_MAX_USD`, and `MP_MONTH_MAX_USD`.
- Runner output is watched for blocked shell patterns such as `curl`, `wget`, `python -c`, `perl -e`, `sudo`, and Docker socket access.
- Backend logs are JSON structured logs on stdout.
- Set `MP_RUNNER_MODE=mock` only for local acceptance smoke tests; default `cli` runs real Claude/Codex CLIs.
- On startup, interrupted `running` runs are marked `error` and stale worktree directories are removed.

## Data

Docker uses named volumes:

- `mini-paperclip_mp_pgdata`
- `mini-paperclip_mp_skills_cache`
- `mini-paperclip_mp_worktrees`
- `mini-paperclip_mp_backups`

`docker compose down` preserves those volumes. `docker compose down -v` removes them and is destructive.

Skill content is Git/file-cache owned. The database stores skill sources, pinned refs, discovered skill metadata, ignored state, and agent assignments; it does not store full skill bundles. Hosted deployments must keep `MP_SKILLS_CACHE_DIR` on persistent storage, such as the `mini-paperclip_mp_skills_cache` volume above. App restart does not delete the cache, but replacing a container without a persistent cache volume will require an admin sync to fetch pinned skill files again.

### Backup & restore

The Admin UI includes `Backup & Restore`.

- Full database backups run server-side with `pg_dump` and are stored under `MP_BACKUP_DIR`.
- Full database restore is operator-run only. The UI validates a dump and generates `pg_restore` instructions, but it never executes restore from the browser.
- Nocturne application data can be exported/imported as versioned JSON bundles. Import uses merge overwrite, creates an automatic pre-restore backup, blocks while runs or wakeups are active, and runs in one transaction.

PostgreSQL backups do not include the Git/file skill cache. Keep `MP_SKILLS_CACHE_DIR` on persistent storage or recover it with skill sync after restoring the database.

Normal hosted skill operations should use the authenticated Admin UI/API. Make targets are local/server wrappers over the same API behavior:

```sh
BASE_URL=http://127.0.0.1:18081 API_TOKEN=dev-token make skills-sync
BASE_URL=http://127.0.0.1:18081 API_TOKEN=dev-token make skills-check
BASE_URL=http://127.0.0.1:18081 API_TOKEN=dev-token make skills-update-check
BASE_URL=http://127.0.0.1:18081 API_TOKEN=dev-token SOURCE=ace3 SHA=<commit-sha> make skills-pin
```

## REST API

See `docs/rest-api.md` for task, artifact, run, heartbeat, and error contracts. Task artifacts are the durable context channel for PM documents, handoffs, engineering plans, QA reports, implementation notes, and run logs. Child task prompts inherit parent artifacts, so PM/EM handoffs stay on the parent task and subtasks are reserved for independently executable work.
