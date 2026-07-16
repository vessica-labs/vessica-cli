# Testing and quality gates

Use focused tests during development, then run the complete gate before merging:

```bash
./scripts/lint-arch.sh
go test ./...
go test -race ./internal/state ./internal/app ./internal/run ./internal/controlplane
go vet ./...
go build ./cmd/ves
(cd web/dashboard && npm ci && npm run generate:api && npm test && npm run build)
./scripts/check-dashboard-assets.sh
```

CI also runs the Postgres integration suite through `TEST_POSTGRES_URL`, dashboard Playwright tests, generated-asset reproducibility, and `govulncheck`. Concurrency-sensitive persistence behavior requires both a deterministic unit test and a Postgres integration test when SQL semantics differ.

For local Postgres coverage, start the pgvector-capable service and initialize both logical databases idempotently:

```bash
docker compose -f compose.dev.yaml up -d postgres
./scripts/init-postgres.sh
TEST_POSTGRES_ADMIN_URL='postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable' \
TEST_POSTGRES_URL='postgres://vessica_control_user:vessica_control_dev@127.0.0.1:5432/vessica_control?sslmode=disable' \
go test ./...
```

The knowledge-server suite uses `VES_KNOWLEDGE_DATABASE_URL` or `TEST_POSTGRES_URL` pointed at `vessica_knowledge`. The control and knowledge suites must never share a database or migration table.

Tests must not require real production credentials or mutate live Railway/GitHub/Linear resources. Use fakes, temporary directories, SQLite, or the disposable CI Postgres service.
