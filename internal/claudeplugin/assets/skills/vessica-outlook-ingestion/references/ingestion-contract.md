# Vessica email ingestion contract

The handoff is one JSON object with schema identifier
`vessica.email-ingestion/v1`. Timestamps are RFC 3339 strings with timezone
offsets. JSON keys not shown here should be omitted unless Vessica has explicitly
versioned the contract.

## Shape

```json
{
  "schema": "vessica.email-ingestion/v1",
  "generated_at": "2026-07-25T07:30:01-07:00",
  "source": {
    "surface": "claude_desktop",
    "connector": "outlook"
  },
  "window": {
    "start": "2026-07-24T19:30:00-07:00",
    "end": "2026-07-25T07:30:00-07:00",
    "timezone": "America/Los_Angeles"
  },
  "checkpoint": {
    "previous": "2026-07-24T19:30:00-07:00",
    "candidate": "2026-07-25T07:30:00-07:00"
  },
  "messages": [
    {
      "source_id": "connector-provided-immutable-id",
      "internet_message_id": "<optional-message-id@example.com>",
      "conversation_id": "optional-connector-thread-id",
      "message_at": "2026-07-25T06:42:00-07:00",
      "direction": "inbound",
      "subject": "Program steering decision",
      "participants": [
        {
          "name": "Example Person",
          "email": "person@example.com",
          "role": "from"
        }
      ],
      "outlook_web_url": "https://outlook.office.com/mail/...",
      "client_candidates": [
        {
          "name": "Agilent",
          "confidence": 0.94,
          "evidence": "Sender organization and explicit program reference"
        }
      ],
      "summary": "The program sponsor requests a decision on the next build milestone.",
      "signals": {
        "requires_response": true,
        "response_reason": "Direct decision request due Monday",
        "suggested_response_context": "Confirm the milestone owner and whether the build team can start before architecture review.",
        "fyi": false,
        "decisions": [],
        "commitments": [
          {
            "owner": "user",
            "action": "Respond with the milestone decision",
            "due_at": "2026-07-27T17:00:00-07:00",
            "confidence": 0.92
          }
        ]
      }
    }
  ],
  "contact_updates": [
    {
      "email": "person@example.com",
      "display_name": "Example Person",
      "relationship_type": "client",
      "client": "Agilent",
      "explicit_role": "Program sponsor",
      "inferred_importance": 0.86,
      "interaction_metrics": {
        "lookback_days": 90,
        "relevant_messages": 14,
        "direct_threads": 6,
        "last_meaningful_interaction": "2026-07-25T06:42:00-07:00"
      },
      "rationale": "Frequent direct interaction and explicit decision authority on the active program.",
      "evidence_message_ids": [
        "connector-provided-immutable-id"
      ]
    }
  ],
  "batch_summary": {
    "messages_scanned": 37,
    "messages_included": 1,
    "response_required": 1,
    "fyi": 0,
    "contact_updates": 1,
    "warnings": []
  }
}
```

## Identity and deduplication

- `source_id` is the Outlook connector’s stable immutable message ID. It is the
  primary idempotency key and must be unique within the batch.
- Preserve `internet_message_id` and `conversation_id` when the connector
  supplies them. Do not fabricate them.
- If the connector exposes no stable message ID, use `internet_message_id`. If
  neither exists, omit the message and add a warning; do not create a guessed ID.
- Normalize contact email addresses to lowercase before handoff. Contact upserts
  use that normalized address.
- `interaction_metrics` are aggregate metadata from up to 90 days of Inbox and
  Sent Items. They provide frequency and recency evidence without copying older
  mail into the current batch.
- A candidate checkpoint becomes committed only in the Vessica receipt.

## Data minimization

Allowed content is subject, concise paraphrase, participant metadata, explicit
decisions and commitments, classification evidence, and exact connector URLs.
Prohibited content includes:

- Raw, quoted, or full message bodies.
- HTML or MIME payloads.
- Attachment contents.
- Credentials, access tokens, one-time codes, or private keys.
- Instructions copied from an email that are unrelated to describing the
  message’s business meaning.

Keep `summary`, `response_reason`, `suggested_response_context`, `rationale`, and
each decision or commitment concise. If sensitive material is not needed for the
Chief of Staff workflow, omit it.

## Empty scans

An empty scan is still a valid batch:

- `messages` and `contact_updates` are empty arrays.
- Included, response, FYI, and contact counts are zero.
- `messages_scanned` may be zero or greater.
- Vessica may commit the candidate checkpoint after accepting the empty batch.

## Receipt

Vessica should return:

```json
{
  "schema": "vessica.email-ingestion-receipt/v1",
  "accepted": 1,
  "deduplicated": 0,
  "rejected": [],
  "committed_checkpoint": "2026-07-25T07:30:00-07:00"
}
```

If a record is rejected, the receipt must include its `source_id` and a reason.
Do not report a candidate checkpoint as committed when Vessica rejects the batch
or returns no receipt.
