# Testing

Document the repository's focused-test commands and full merge gate. The gate must include architecture lint, unit tests, race/concurrency tests where relevant, static analysis, a production build, generated-artifact reproducibility, and dependency vulnerability scanning.

Tests must be deterministic, must not require production credentials, and must not mutate live infrastructure. Concurrency-sensitive database behavior should be exercised against the production database engine as well as fast local tests.
