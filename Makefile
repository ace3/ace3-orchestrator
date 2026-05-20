.PHONY: fmt backend-test frontend-build audit test e2e-one-task skills-sync skills-check skills-update-check skills-pin local-env local-cli-env compose-dev-env local-db-up local-db-check local-db-down local-db-ps local-db-logs local-backend local-backend-cli local-backend-watch local-backend-watch-cli local-frontend local-dev local-dev-cli local-dev-run local-status local-stop compose-dev compose-dev-down compose-dev-ps compose-dev-logs docker-up docker-down docker-ps docker-logs

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

compose-dev-env:
	cp -n deploy/.env.compose.dev.example deploy/.env.compose.dev || true

local-db-up: local-env
	docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local up -d

local-db-check: local-env
	@container_id=$$(docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local ps -q db); \
	if [ -z "$$container_id" ]; then \
		echo "local Postgres is not started. Run: make local-db-up"; \
		exit 1; \
	fi; \
	attempt=1; \
	while true; do \
		health=$$(docker inspect --format='{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$$container_id"); \
		if [ "$$health" = "healthy" ] || [ "$$health" = "running" ]; then \
			echo "local Postgres is $$health"; \
			exit 0; \
		fi; \
		if [ "$$health" = "unhealthy" ] || [ "$$attempt" -ge 30 ]; then \
			echo "local Postgres is $$health. Run: make local-db-logs"; \
			exit 1; \
		fi; \
		attempt=$$((attempt + 1)); \
		sleep 1; \
	done

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

local-backend-watch: local-env local-db-check
	@if ! command -v air >/dev/null 2>&1; then \
		echo "air is required. Install with: go install github.com/cosmtrek/air@v1.49.0"; \
		exit 1; \
	fi
	set -a; . ./deploy/.env.local; set +a; cd backend && air -c .air.toml

local-backend-watch-cli: local-cli-env local-db-check
	@if ! command -v air >/dev/null 2>&1; then \
		echo "air is required. Install with: go install github.com/cosmtrek/air@v1.49.0"; \
		exit 1; \
	fi
	set -a; . ./deploy/.env.local.cli; set +a; cd backend && air -c .air.toml

local-frontend: local-env
	set -a; . ./deploy/.env.local; set +a; export VITE_BACKEND_URL="$${VITE_BACKEND_URL:-http://localhost:$${MP_PORT}}"; cd frontend && npm install && npm run dev

local-dev: local-env
	make local-stop
	make local-db-up
	make local-db-check
	make local-stop
	ENV_FILE=deploy/.env.local make local-dev-run

local-dev-cli: local-cli-env
	make local-stop
	make local-db-up
	make local-db-check
	make local-stop
	ENV_FILE=deploy/.env.local.cli make local-dev-run

local-dev-run:
	@ENV_FILE="$(ENV_FILE)" bash -euo pipefail -c ' \
		: "$${ENV_FILE:?ENV_FILE is required}"; \
		if ! command -v air >/dev/null 2>&1; then \
			echo "air is required. Install with: go install github.com/cosmtrek/air@v1.49.0"; \
			exit 1; \
		fi; \
		set -m; \
		set -a; . "$$ENV_FILE"; set +a; \
		export VITE_BACKEND_URL="$${VITE_BACKEND_URL:-http://localhost:$${MP_PORT}}"; \
		stop_pid() { \
			pid="$$1"; \
			if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
				kill -TERM -$$pid 2>/dev/null || kill -TERM "$$pid" 2>/dev/null || true; \
			fi; \
		}; \
		stop_port() { \
			port="$$1"; \
			while pids=$$(lsof -tiTCP:"$$port" -sTCP:LISTEN 2>/dev/null || true); [ -n "$$pids" ]; do \
				kill $$pids 2>/dev/null || true; \
				sleep 1; \
				pids=$$(lsof -tiTCP:"$$port" -sTCP:LISTEN 2>/dev/null || true); \
				[ -z "$$pids" ] || kill -9 $$pids 2>/dev/null || true; \
			done; \
		}; \
		backend_pids() { \
			pgrep -f "$$1" 2>/dev/null | while read -r pid; do \
				comm=$$(ps -p "$$pid" -o comm= 2>/dev/null || true); \
				case "$$comm" in *mini-paperclip*) echo "$$pid";; esac; \
			done; \
		}; \
		stop_file() { \
			path="$$1"; \
			while pids=$$(backend_pids "$$path"); [ -n "$$pids" ]; do \
				kill $$pids 2>/dev/null || true; \
				sleep 1; \
				pids=$$(backend_pids "$$path"); \
				[ -z "$$pids" ] || kill -9 $$pids 2>/dev/null || true; \
			done; \
		}; \
		cleanup() { \
			status=$$?; \
			trap - INT TERM EXIT; \
			stop_pid "$${frontend_pid:-}"; \
			stop_pid "$${backend_pid:-}"; \
			stop_port 5173; \
			stop_port "$${MP_PORT:-18081}"; \
			stop_file "backend/tmp/air/mini-paperclip"; \
			stop_file "backend/tmp/mini-paperclip-local"; \
			wait "$${frontend_pid:-}" 2>/dev/null || true; \
			wait "$${backend_pid:-}" 2>/dev/null || true; \
			make local-db-down; \
			make local-stop || true; \
			exit "$$status"; \
		}; \
		trap cleanup INT TERM EXIT; \
		(cd backend && exec air -c .air.toml) & \
		backend_pid=$$!; \
		sleep 1; \
		if ! kill -0 "$$backend_pid" 2>/dev/null; then \
			set +e; wait "$$backend_pid"; status=$$?; set -e; exit "$$status"; \
		fi; \
		(cd frontend && npm install && exec npm run dev) & \
		frontend_pid=$$!; \
		while true; do \
			if ! kill -0 "$$backend_pid" 2>/dev/null; then \
				set +e; wait "$$backend_pid"; status=$$?; set -e; exit "$$status"; \
			fi; \
			if ! kill -0 "$$frontend_pid" 2>/dev/null; then \
				set +e; wait "$$frontend_pid"; status=$$?; set -e; exit "$$status"; \
			fi; \
			sleep 1; \
		done'

local-status:
	@ports="5173"; \
	if [ -f deploy/.env.local ]; then \
		set -a; . ./deploy/.env.local; set +a; \
		ports="$$ports $${MP_PORT:-18081}"; \
	else \
		ports="$$ports 18081"; \
	fi; \
	if [ -f deploy/.env.local.cli ]; then \
		set -a; . ./deploy/.env.local.cli; set +a; \
		ports="$$ports $${MP_PORT:-18082}"; \
	else \
		ports="$$ports 18082"; \
	fi; \
	backend_pids() { \
		pgrep -f "$$1" 2>/dev/null | while read -r pid; do \
			comm=$$(ps -p "$$pid" -o comm= 2>/dev/null || true); \
			case "$$comm" in *mini-paperclip*) echo "$$pid";; esac; \
		done; \
	}; \
	found=0; \
	for port in $$ports; do \
		pids=$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null || true); \
		if [ -n "$$pids" ]; then \
			echo "port $$port listener(s): $$pids"; \
			found=1; \
		fi; \
	done; \
	for file in backend/tmp/air/mini-paperclip backend/tmp/mini-paperclip-local; do \
		pids=$$(backend_pids "$$file"); \
		if [ -n "$$pids" ]; then \
			echo "backend process(es) using $$file: $$pids"; \
			found=1; \
		fi; \
	done; \
	db_status=$$(docker compose -f deploy/docker-compose.db.yml --env-file deploy/.env.local ps --status running --services 2>/dev/null | awk 'NF { out = out ? out " " $$0 : $$0 } END { print out }'); \
	if [ -n "$$db_status" ]; then \
		echo "running db compose service(s): $$db_status"; \
		found=1; \
	fi; \
	compose_dev_status=$$(docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev ps --status running --services 2>/dev/null | awk 'NF { out = out ? out " " $$0 : $$0 } END { print out }'); \
	if [ -n "$$compose_dev_status" ]; then \
		echo "running compose-dev service(s): $$compose_dev_status"; \
		found=1; \
	fi; \
	if [ "$$found" -eq 0 ]; then \
		echo "no local runtime processes found"; \
	fi

local-stop:
	@compose_dev_status=$$(docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev ps --status running --services 2>/dev/null | awk 'NF { out = out ? out " " $$0 : $$0 } END { print out }'); \
	if [ -n "$$compose_dev_status" ]; then \
		echo "stopping compose-dev service(s): $$compose_dev_status"; \
		docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev down; \
	fi; \
	ports="5173"; \
	if [ -f deploy/.env.local ]; then \
		set -a; . ./deploy/.env.local; set +a; \
		ports="$$ports $${MP_PORT:-18081}"; \
	else \
		ports="$$ports 18081"; \
	fi; \
	if [ -f deploy/.env.local.cli ]; then \
		set -a; . ./deploy/.env.local.cli; set +a; \
		ports="$$ports $${MP_PORT:-18082}"; \
	else \
		ports="$$ports 18082"; \
	fi; \
	backend_pids() { \
		pgrep -f "$$1" 2>/dev/null | while read -r pid; do \
			comm=$$(ps -p "$$pid" -o comm= 2>/dev/null || true); \
			case "$$comm" in *mini-paperclip*) echo "$$pid";; esac; \
		done; \
	}; \
	killed=0; \
	for port in $$ports; do \
		attempt=1; \
		while pids=$$(lsof -tiTCP:$$port -sTCP:LISTEN 2>/dev/null || true); [ -n "$$pids" ]; do \
			if [ "$$attempt" -eq 1 ]; then \
				echo "stopping listener(s) on port $$port: $$pids"; \
				kill $$pids 2>/dev/null || true; \
			elif [ "$$attempt" -le 5 ]; then \
				echo "force stopping listener(s) on port $$port: $$pids"; \
				kill -9 $$pids 2>/dev/null || true; \
			else \
				echo "failed to stop listener(s) on port $$port: $$pids"; \
				exit 1; \
			fi; \
			killed=1; \
			attempt=$$((attempt + 1)); \
			sleep 1; \
		done; \
	done; \
	for file in backend/tmp/air/mini-paperclip backend/tmp/mini-paperclip-local; do \
		while pids=$$(backend_pids "$$file"); [ -n "$$pids" ]; do \
			echo "stopping backend process(es) using $$file: $$pids"; \
			kill $$pids 2>/dev/null || true; \
			sleep 1; \
			pids=$$(backend_pids "$$file"); \
			if [ -n "$$pids" ]; then \
				echo "force stopping backend process(es) using $$file: $$pids"; \
				kill -9 $$pids 2>/dev/null || true; \
			fi; \
			killed=1; \
		done; \
	done; \
	if [ "$$killed" -eq 0 ]; then \
		echo "no local app listeners found"; \
	fi

compose-dev: compose-dev-env
	make local-stop
	make local-db-down
	@trap 'docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev down; exit 130' INT TERM; \
	docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev up --build; \
	status=$$?; \
	docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev down; \
	exit $$status

compose-dev-down: compose-dev-env
	docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev down

compose-dev-ps: compose-dev-env
	docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev ps

compose-dev-logs: compose-dev-env
	docker compose -f deploy/docker-compose.dev.yml --env-file deploy/.env.compose.dev logs --tail=120 backend frontend db

docker-up:
	cp -n deploy/.env.example deploy/.env || true
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build

docker-down:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env down

docker-ps:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps

docker-logs:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs --tail=120 backend frontend
