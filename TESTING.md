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

Tests must not require real production credentials or mutate live Railway/GitHub/Linear resources. Use fakes, temporary directories, SQLite, or the disposable CI Postgres service.
