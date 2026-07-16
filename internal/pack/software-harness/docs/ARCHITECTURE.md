# Architecture

This file is the repository's architectural contract. Replace the discovery prompts below with concrete package paths during initial setup and keep it current as decisions change.

## Required map

Document the composition roots, transport adapters, application/use-case layer, domain model, persistence boundary, infrastructure adapters, and generated code. State which dependency directions are permitted and which are forbidden.

## Required invariants

Identify the authoritative store for each durable concept, concurrency and idempotency rules, transaction boundaries, background/singleton ownership, context cancellation behavior, and error-propagation policy.

Shared behavior belongs in a use-case service rather than duplicated handlers. Concurrency correctness belongs at the persistence boundary. Transport packages must not become alternate domain layers.

Go source files have a hard limit of 800 lines and a soft warning at 500, enforced by `.vessica/lint-arch.sh`. Split files by cohesive responsibility.

Material architecture changes require an ADR and matching updates to security, testing, and deployment documentation.
