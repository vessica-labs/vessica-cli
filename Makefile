.PHONY: build test frontend frontend-test install compile bump-version asset-budget

build: bump-version frontend compile

bump-version:
	@python3 scripts/bump-version.py VERSION

frontend:
	./scripts/embed-dashboard-docs.sh
	cd web/dashboard && npm ci && npm run generate:api && npm run build

frontend-test:
	cd web/dashboard && npm test && npm run test:e2e

asset-budget:
	./scripts/check-dashboard-assets.sh

compile:
	@version=$$(cat VERSION); \
	go build -ldflags "-X github.com/vessica-labs/vessica-cli/internal/version.Version=$$version" -o bin/ves ./cmd/ves

test:
	go test ./internal/... -count=1 -timeout 60s

install: compile
	cp bin/ves $(HOME)/.local/bin/ves
