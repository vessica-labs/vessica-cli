---
name: use-knowledge
description: Retrieve, create, version, or diagnose hosted Vessica knowledge, including entities, authoritative artifacts, durable memories, work history, and optional embeddings through the ves CLI.
---

# Use Vessica Knowledge

For setup, configuration, Railway operations, command help, or troubleshooting, use the `operate-vessica` skill and its operator reference.

Always call `ves`; never open the SQLite database or knowledge HTTP API directly. Begin with `ves knowledge status --json` and use `ves knowledge context --query "<task>" --token-budget <n> --json` or `ves prime --for codex --json` before work that benefits from repository history.

Use these JSON-only workflows:

- Entity identity: `ves entity resolve "<name>" --json`; create only after preview and confirmation with `ves entity create --type <type> --name "<name>" --dry-run --json`, then `--yes --idempotency-key <unique> --json`.
- Authoritative work: `ves artifact list --status active --json`, `ves artifact get <id> --json`, and confirmed `artifact create|update|activate|supersede` mutations.
- Durable understanding: `ves memory search "<query>" --json`, `ves memory get <id> --json`, and confirmed `memory add|update|supersede|archive` mutations. Create an `instruction` only when the user explicitly requests durable guidance.
- Work history: query for the task, run, epic, ticket, PR, receipt, or commit. Cite the returned artifact IDs, external references, and provenance in the answer.
- Embeddings: quickstart is healthy in lexical mode. Enable semantic retrieval later with `ves knowledge embeddings enable --provider openai --api-key-env <name> --yes`, and confirm before changing retrieval configuration.

Treat `retrieval_mode: lexical` and `embedding_state: not_configured` as a healthy hosted state. Never fall back to local writes on API failure. Report typed errors and preserve the hosted authority.

Never echo tokens or embedding keys in arguments or output. Parse the versioned JSON envelope, not human-readable text.
