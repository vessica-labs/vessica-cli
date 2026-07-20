# Vessica Stream Protocol v1

`vessica.stream/v1` is the stable streaming interface for Codex skills and other tool consumers.

## Commands

Start or resume a run:

```bash
ves run epic <epic_id> --stream=jsonl
ves run resume <run_id> --stream=jsonl
```

Replay or reconnect after the last observed sequence:

```bash
ves run logs <run_id> --jsonl
ves run watch <run_id> --jsonl --after-seq <seq>
```

Use ordinary `--json` for non-streaming commands such as `epic add`, `ticket list`, and `run view`.
Use `ves run artifacts <run_id> --json` and
`ves run receipt <run_id> --json` for final evidence rather than expanding every
heavy payload in the live stream.

## Transport Contract

- stdout contains one complete JSON object per line and no human-formatted text.
- stderr contains diagnostics and is not part of the protocol.
- Records are written as events occur without application-level buffering.
- Every record has `schema: "vessica.stream/v1"` and a `kind` discriminator.
- Event `seq` values are monotonically increasing within a run.
- Consumers must retain `run_id` and the greatest processed `seq` for reconnection.
- Consumers must ignore unknown fields and unknown event types for forward compatibility.
- A terminal `result` record contains `ok`, the final run in `data`, and an optional structured `error`.
- Process exit status remains authoritative for command execution; `result.ok` and the persisted run status describe the workflow outcome. A preceding success-shaped `agent.message` never overrides a failed terminal record.
- Heavy payload fields such as prompts, commands, patch bodies, and command output are omitted from the live protocol and named in `payload.collapsed_fields`.
- `raw_log_path` and byte offsets remain available on agent events; use `run logs --detail` or `run logs --raw` only when detailed inspection is necessary.

## Event Record

```json
{
  "schema": "vessica.stream/v1",
  "kind": "event",
  "run_id": "run_abc123",
  "seq": 42,
  "timestamp": "2026-07-09T20:15:00Z",
  "event": {
    "id": "evt_abc123",
    "run_id": "run_abc123",
    "seq": 42,
    "type": "agent.activity",
    "payload": {
      "role": "coder",
      "kind": "command",
      "status": "completed",
      "message": "go test ./...",
      "exit_code": 0
    },
    "created_at": "2026-07-09T20:15:00Z"
  }
}
```

Important event types include `run.started`, `run.phase.started`,
`run.infrastructure.stage`, `agent.prompt`, `agent.activity`, `agent.message`,
`agent.usage`, `error`, and `run.completed`. Infrastructure stages report
checkpoint resolution, worker readiness, repository synchronization, dependency
projection/refresh, exact Git trust, bounded context packets, and MCP policy.
The set is extensible.

## Result Record

```json
{
  "schema": "vessica.stream/v1",
  "kind": "result",
  "run_id": "run_abc123",
  "timestamp": "2026-07-09T20:20:00Z",
  "ok": true,
  "data": {
    "id": "run_abc123",
    "status": "completed",
    "preview_url": "http://127.0.0.1:53271",
    "pr_url": "https://github.com/example/repo/pull/123"
  }
}
```

On failure, `ok` is false and `error` has stable `code` and human-readable `message` fields.

## Codex Skill Guidance

1. Invoke mutating commands with an idempotency key.
2. Parse stdout incrementally by newline.
3. Surface selected `agent.message`, phase, preview, PR, and failure events to the user.
4. Record `run_id` from the first event and update the checkpoint from each `seq`.
5. If the tool process is interrupted, reconnect with `run watch --jsonl --after-seq`.
6. Treat prompts, raw logs, and detailed command output as opt-in inspection data rather than normal conversation output.
7. On terminal output, reconcile against `ves run view`/`ves run receipt`; do not infer success from the last conversational message.
