---
name: use-knowledge
description: Retrieve, create, version, or diagnose hosted Vessica knowledge, including entities, authoritative artifacts, durable memories, work history, and optional embeddings through the ves CLI.
---

# Use Vessica Knowledge

For setup, configuration, Railway operations, command help, or troubleshooting, use the `operate-vessica` skill and its operator reference.

Always call `ves`; never open the SQLite database or knowledge HTTP API directly. Include `ves knowledge status --json` in the first read-only shell invocation. When resolving a named subject, combine status and `ves entity resolve` in that one invocation instead of creating a separate agent round trip. Do not search Codex local memories, rollout summaries, or session logs when the user asks for Vessica durable knowledge; those are a separate memory system and would make provenance ambiguous.

Choose the narrowest retrieval path:

- For durable memories, use one `ves memory retrieve "<query>" --limit 5 --rerank auto --json` call. If the user names or clearly identifies a project, account, or person, first run `ves entity resolve "<name>" --json`; when it returns one exact or alias match, pass that ID with `--entity <id>`. Inspect `ambiguity`, ranking explanations, index freshness, and reranker metadata before restoring anything. `ambiguity: "ambiguous_subject"` is a hard safety stop: do not quote, summarize, or apply any returned candidate merely because it ranks first. If an identified subject was not resolved before retrieval, resolve it and make one final entity-constrained retrieval; otherwise ask the user to identify the applicable subject. Fetch a selected record with `ves memory get <id> --json` only when its complete version or provenance is needed. Treat an empty result as absent; do not fan out across near-synonym searches.
- For repository history or mixed authoritative context, use one `ves knowledge context --query "<task>" --token-budget <n> [--entity <id>] --json` or `ves prime --for codex --json`. Inspect omissions before increasing the budget. Do not issue repeated near-synonym context queries.
- When a request spans distinct subjects, such as an account plus scheduling preferences, use one focused memory query per subject and synthesize only the returned records.

Use these JSON-only workflows:

- Entity identity: `ves entity resolve "<name>" --json`; create only after preview and confirmation with `ves entity create --type <type> --name "<name>" --dry-run --json`, then `--yes --idempotency-key <unique> --json`.
- Authoritative work: `ves artifact list --status active --json`, `ves artifact get <id> --json`, and confirmed `artifact create|update|activate|supersede` mutations.
- Durable understanding: `ves memory retrieve "<query>" --limit 5 --rerank auto --json`, `ves memory get <id> --json`, and confirmed `memory add|update|supersede|archive` mutations. Keep `ves memory search` for administrative lexical inspection. Create an `instruction` only when the user explicitly requests durable guidance.
- Work history: query for the task, run, epic, ticket, PR, receipt, or commit. Cite the returned artifact IDs, external references, and provenance in the answer.
- Embeddings: quickstart is healthy in lexical mode. Enable semantic retrieval later with `ves knowledge embeddings enable --provider openai --api-key-env <name> --yes`, and confirm before changing retrieval configuration.
- Reranking: leave it disabled unless benchmark promotion gates pass. Preview with `ves knowledge reranking enable --provider openai --api-key-env <name> --model gpt-5.6-luna --dry-run --json`; enabling requires separate confirmation because readable candidate text is sent to the provider.

Treat `retrieval_mode: lexical` and `embedding_state: not_configured` as a healthy hosted state. Never fall back to local writes on API failure. Report typed errors and preserve the hosted authority.

Never echo tokens or embedding keys in arguments or output. Parse the versioned JSON envelope, not human-readable text.
