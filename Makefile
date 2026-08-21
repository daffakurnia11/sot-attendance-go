.PHONY: local-up local-api local-down local-logs lint vet test race build check

# Both run targets use DATABASE_URL from .env exactly as written, whether that
# is the compose database or a remote one. Set SKIP_MIGRATIONS=true in .env when
# pointing at a database you do not want this build to migrate.

# API and bot together.
local-up:
	@test -f .env || { echo ".env missing; run: cp .env.example .env, then set DISCORD_BOT_TOKEN"; exit 1; }
	docker compose up --build -d

# API alone.
local-api:
	@test -f .env || { echo ".env missing; run: cp .env.example .env"; exit 1; }
	docker compose up --build -d api

local-down:
	docker compose down

local-logs:
	docker compose logs -f

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
