.PHONY: fmt backend-test frontend-build audit test e2e-one-task skills-sync skills-check skills-update-check skills-pin local-env local-cli-env local-db-up local-db-check local-db-down local-db-ps local-db-logs local-backend local-backend-cli local-frontend local-dev local-dev-cli docker-up docker-down docker-ps docker-logs

fmt:
	cd backend && gofmt -w $$(find . -name '*.go')

backend-test:
	cd backend && go test ./...

frontend-build:
	cd frontend && npm install && npm run build

audit:
	cd frontend && npm audit --audit-level=high

test: fmt backend-test frontend-build audit

e2e-one-task:
	./scripts/e2e-one-task.sh

skills-sync:
	node scripts/skills-api.mjs sync

skills-check:
	node scripts/skills-api.mjs check

skills-update-check:
	node scripts/skills-api.mjs update-check

skills-pin:
	node scripts/skills-api.mjs pin

local-env:
	cp -n deploy/.env.local.example deploy/.env.local || true

local-cli-env:
	cp -n deploy/.env.local.cli.example deploy/.env.local.cli || true

local-db-up: local-env
	docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local up -d

local-db-check: local-env
	@container_id=$$(docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local ps -q db); \
	if [ -z "$$container_id" ]; then \
		echo "local Postgres is not started. Run: make local-db-up"; \
		exit 1; \
	fi; \
	health=$$(docker inspect --format='{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$$container_id"); \
	if [ "$$health" != "healthy" ] && [ "$$health" != "running" ]; then \
		echo "local Postgres is $$health. Run: make local-db-up"; \
		exit 1; \
	fi; \
	echo "local Postgres is $$health"

local-db-down: local-env
	docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local down

local-db-ps: local-env
	docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local ps

local-db-logs: local-env
	docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local logs --tail=120 db

local-backend: local-env local-db-check
	set -a; . ./deploy/.env.local; set +a; cd backend && go run ./cmd/mini-paperclip

local-backend-cli: local-cli-env local-db-check
	set -a; . ./deploy/.env.local.cli; set +a; cd backend && go run ./cmd/mini-paperclip

local-frontend: local-env
	set -a; . ./deploy/.env.local; set +a; export VITE_BACKEND_URL="$${VITE_BACKEND_URL:-http://localhost:$${MP_PORT}}"; cd frontend && npm install && npm run dev

local-dev: local-env local-db-check
	set -a; . ./deploy/.env.local; set +a; \
	export VITE_BACKEND_URL="$${VITE_BACKEND_URL:-http://localhost:$${MP_PORT}}"; \
	(cd backend && go run ./cmd/mini-paperclip) & \
	backend_pid=$$!; \
	trap 'kill "$$backend_pid" 2>/dev/null || true' INT TERM EXIT; \
	sleep 1; \
	if ! kill -0 "$$backend_pid" 2>/dev/null; then wait "$$backend_pid"; exit $$?; fi; \
	cd frontend && npm install && npm run dev

local-dev-cli: local-cli-env local-db-check
	set -a; . ./deploy/.env.local.cli; set +a; \
	export VITE_BACKEND_URL="$${VITE_BACKEND_URL:-http://localhost:$${MP_PORT}}"; \
	(cd backend && go run ./cmd/mini-paperclip) & \
	backend_pid=$$!; \
	trap 'kill "$$backend_pid" 2>/dev/null || true' INT TERM EXIT; \
	sleep 1; \
	if ! kill -0 "$$backend_pid" 2>/dev/null; then wait "$$backend_pid"; exit $$?; fi; \
	cd frontend && npm install && npm run dev

docker-up:
	cp -n deploy/.env.example deploy/.env || true
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build

docker-down:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env down

docker-ps:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps

docker-logs:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs --tail=120 backend frontend
