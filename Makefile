.PHONY: local-db-up local-up local-down local-logs lint vet test race build check

local-db-up:
	docker compose up -d postgres

local-up: local-db-up
	@test -f .env || (echo ".env missing; run: cp .env.example .env, then set DISCORD_BOT_TOKEN" && exit 1)
	docker compose up --build -d

local-down:
	docker compose down

local-logs:
	docker compose logs -f bot api

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
