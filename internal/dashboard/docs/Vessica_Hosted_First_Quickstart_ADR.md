# Vessica hosted-first quickstart ADR

**ADR ID:** ADR-004  
**Status:** Accepted

## Decision

Vessica has one product onboarding path: `ves up`. It creates or discovers one Vessica installation in the selected Railway workspace, attaches the current Git repository, provisions durable control-plane and knowledge services, prepares the Railway sandbox toolchain, and records a repository map. The installation owns many repository records; every repository-owned run resolves its remote from `repository_id`.

Hosted knowledge starts in healthy lexical mode without an embeddings credential. Semantic-hybrid retrieval is a user-funded, reversible configuration applied later through `ves knowledge embeddings`.

When no Vessica harness exists, onboarding installs the independently versioned engineering-harness pack and creates repository-specific starting files. Existing or partial Vessica harnesses are audited and preserved.

The Codex plugin remains a guidance and bootstrap layer over the Go CLI. It does not implement provisioning or lifecycle behavior.

## Consequences

- Solo and team profiles are not product concepts; local adapters live under `ves dev`.
- Hosted control-plane state is the product authority and there is no writable local fallback.
- Linear is optional and can be connected after onboarding.
- Onboarding is stage-based, idempotent, and resumable after Railway authentication, deployment, or Sandbox Priority Boarding failures.
- Release archives and service images are required inputs to production onboarding; source builds are development-only.
