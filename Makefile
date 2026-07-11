.PHONY: build test install compile bump-version

build: bump-version compile

bump-version:
	@python3 scripts/bump-version.py VERSION

compile:
	@version=$$(cat VERSION); \
	go build -ldflags "-X github.com/vessica-labs/vessica-cli/internal/version.Version=$$version" -o bin/ves ./cmd/ves

test:
	go test ./internal/... -count=1 -timeout 60s

install: compile
	cp bin/ves $(HOME)/.local/bin/ves
