.PHONY: fmt backend-test frontend-build audit test docker-up docker-down docker-ps docker-logs

fmt:
	cd backend && gofmt -w $$(find . -name '*.go')

backend-test:
	cd backend && go test ./...

frontend-build:
	cd frontend && npm install && npm run build

audit:
	cd frontend && npm audit --audit-level=high

test: fmt backend-test frontend-build audit

docker-up:
	cp -n deploy/.env.example deploy/.env || true
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env up -d --build

docker-down:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env down

docker-ps:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env ps

docker-logs:
	docker compose -f deploy/docker-compose.yml --env-file deploy/.env logs --tail=120 backend frontend
