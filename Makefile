.PHONY: build test frontend frontend-test install install-cli compile asset-budget

build: frontend compile

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
	./scripts/install-local.sh bin/ves "$(HOME)/.local/bin"

install-cli: compile
	install -d "$(HOME)/.local/bin"
	install -m 0755 bin/ves "$(HOME)/.local/bin/ves"
	@"$(HOME)/.local/bin/ves" version --json
