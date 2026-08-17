.PHONY: local-db-up local-up local-api local-down local-logs local-api-logs lint vet test race build check

local-db-up:
	docker compose up -d postgres

# Full stack, against the compose database only. Refuses to run when
# DATABASE_URL points elsewhere: the bot writes player and attendance logs and
# posts to Discord with the production token, so pointing it at the production
# database means two bots duplicating rows, closing each other's sessions as
# orphaned on startup, and announcing the same events twice. Use `local-api` to
# work against a remote database.
local-up: local-db-up
	@test -f .env || (echo ".env missing; run: cp .env.example .env, then set DISCORD_BOT_TOKEN" && exit 1)
	@grep -qE '^DATABASE_URL=postgres://[^@]*@postgres:' .env || { \
		echo "Refusing to start the bot: DATABASE_URL does not point at the compose postgres."; \
		echo "Current host: $$(sed -n 's|^DATABASE_URL=postgres://[^@]*@\([^/]*\)/.*|\1|p' .env)"; \
		echo "Run 'make local-api' to start the API alone against that database."; \
		exit 1; }
	docker compose up --build -d

# API only. Safe against a remote database because nothing here writes on a
# timer or talks to Discord; the API only writes on an explicit PATCH.
local-api:
	@test -f .env || (echo ".env missing; run: cp .env.example .env" && exit 1)
	docker compose up --build -d api

local-down:
	docker compose down

local-logs:
	docker compose logs -f bot api

local-api-logs:
	docker compose logs -f api

# gofmt -l prints the paths it would rewrite and exits 0 either way, so the
# non-empty check is what turns unformatted code into a failure.
lint:
	@unformatted="$$(gofmt -l .)"; \
		if [ -n "$$unformatted" ]; then echo "gofmt required for:"; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

race:
	go test -race ./...

build:
	go build ./...

check: lint vet test race build
