.PHONY: local-db-up local-up local-down local-logs test check

local-db-up:
	docker compose up -d postgres

local-up: local-db-up
	@test -f .env || (echo ".env missing; run: cp .env.example .env, then set DISCORD_BOT_TOKEN" && exit 1)
	docker compose up --build -d

local-down:
	docker compose down

local-logs:
	docker compose logs -f bot api

test:
	go test ./...

check:
	go test -race ./...
	go vet ./...
	go build ./...
