# mini-Paperclip

Local multi-agent engineering orchestrator from `PRD.md`.

## Quickstart

```sh
cp deploy/.env.example deploy/.env
docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build
```

Open `http://localhost:8080` and keep the API token from `deploy/.env` in the sidebar token field.

![mini-Paperclip projects screen](docs/screenshots/projects.png)

## Development

```sh
make test
make docker-up
make docker-ps
```

Backend runs on `:8081`; frontend dev server proxies `/api` to it.

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

`docker compose down` preserves those volumes. `docker compose down -v` removes them and is destructive.
