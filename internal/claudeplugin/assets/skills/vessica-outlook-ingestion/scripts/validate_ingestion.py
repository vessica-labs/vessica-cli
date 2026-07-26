#!/usr/bin/env python3
"""Validate a Claude Desktop Outlook batch before handing it to Vessica."""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import datetime
from pathlib import Path
from typing import Any
from urllib.parse import urlparse


SCHEMA = "vessica.email-ingestion/v1"
CLIENTS = {
    "Agilent",
    "Mastercard",
    "Pacific Life",
    "RBC",
    "Western Digital",
    "Qualcomm",
    "Micron",
    "Cisco",
    "AWS",
    "TD Bank",
    "CIBC",
}
RELATIONSHIP_TYPES = {"client", "colleague", "external_partner", "unknown"}
DIRECTIONS = {"inbound", "outbound", "internal"}
PARTICIPANT_ROLES = {"from", "to", "cc"}
PROHIBITED_KEYS = {
    "attachment",
    "attachments",
    "body",
    "body_html",
    "body_preview",
    "body_text",
    "html",
    "mime",
    "raw",
    "raw_body",
}
EMAIL_RE = re.compile(r"^[^@\s]+@[^@\s]+\.[^@\s]+$")


def _is_dict(value: Any) -> bool:
    return isinstance(value, dict)


def _is_list(value: Any) -> bool:
    return isinstance(value, list)


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _timestamp(value: Any, path: str, errors: list[str]) -> datetime | None:
    if not isinstance(value, str) or not value:
        errors.append(f"{path} must be a non-empty RFC 3339 timestamp")
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        errors.append(f"{path} is not a valid RFC 3339 timestamp")
        return None
    if parsed.tzinfo is None:
        errors.append(f"{path} must include a timezone offset")
        return None
    return parsed


def _required_string(
    obj: dict[str, Any],
    key: str,
    path: str,
    errors: list[str],
    max_length: int | None = None,
) -> str | None:
    value = obj.get(key)
    if not isinstance(value, str) or not value.strip():
        errors.append(f"{path}.{key} must be a non-empty string")
        return None
    if max_length is not None and len(value) > max_length:
        errors.append(f"{path}.{key} exceeds {max_length} characters")
    return value


def _optional_short_string(
    obj: dict[str, Any],
    key: str,
    path: str,
    errors: list[str],
    max_length: int,
) -> None:
    value = obj.get(key)
    if value is not None and (not isinstance(value, str) or len(value) > max_length):
        errors.append(
            f"{path}.{key} must be null or a string of at most {max_length} characters"
        )


def _confidence(value: Any, path: str, errors: list[str]) -> None:
    if not _is_number(value) or not 0 <= value <= 1:
        errors.append(f"{path} must be a number from 0.0 to 1.0")


def _check_prohibited_keys(value: Any, path: str, errors: list[str]) -> None:
    if isinstance(value, dict):
        for key, nested in value.items():
            if key.lower() in PROHIBITED_KEYS:
                errors.append(f"{path}.{key} is prohibited raw-content data")
            _check_prohibited_keys(nested, f"{path}.{key}", errors)
    elif isinstance(value, list):
        for index, nested in enumerate(value):
            _check_prohibited_keys(nested, f"{path}[{index}]", errors)


def _validate_participants(value: Any, path: str, errors: list[str]) -> None:
    if not _is_list(value) or not value:
        errors.append(f"{path} must be a non-empty array")
        return
    from_count = 0
    for index, participant in enumerate(value):
        item_path = f"{path}[{index}]"
        if not _is_dict(participant):
            errors.append(f"{item_path} must be an object")
            continue
        _required_string(participant, "name", item_path, errors, 200)
        email = _required_string(participant, "email", item_path, errors, 320)
        if email and (email != email.lower() or not EMAIL_RE.match(email)):
            errors.append(
                f"{item_path}.email must be a normalized lowercase email address"
            )
        role = participant.get("role")
        if role not in PARTICIPANT_ROLES:
            errors.append(
                f"{item_path}.role must be one of {sorted(PARTICIPANT_ROLES)}"
            )
        if role == "from":
            from_count += 1
    if from_count != 1:
        errors.append(f"{path} must contain exactly one sender")


def _validate_url(
    value: Any,
    path: str,
    errors: list[str],
    warnings: list[str],
) -> None:
    if value is None:
        warnings.append(
            f"{path} is null; no connector-provided Outlook link was available"
        )
        return
    if not isinstance(value, str):
        errors.append(f"{path} must be null or a URL string")
        return
    parsed = urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        errors.append(f"{path} must be an absolute HTTPS URL")
        return
    host = parsed.hostname or ""
    if not (
        host == "outlook.office.com"
        or host.endswith(".outlook.office.com")
        or host == "outlook.office365.com"
        or host.endswith(".outlook.office365.com")
        or host == "outlook.live.com"
        or host.endswith(".outlook.live.com")
        or host.endswith(".microsoft.com")
    ):
        warnings.append(
            f"{path} host {host!r} is not a recognized Microsoft Outlook host"
        )


def _validate_signals(
    value: Any,
    path: str,
    errors: list[str],
    message_timestamp: datetime | None,
) -> tuple[bool, bool]:
    if not _is_dict(value):
        errors.append(f"{path} must be an object")
        return False, False

    requires_response = value.get("requires_response")
    fyi = value.get("fyi")
    if not isinstance(requires_response, bool):
        errors.append(f"{path}.requires_response must be a boolean")
        requires_response = False
    if not isinstance(fyi, bool):
        errors.append(f"{path}.fyi must be a boolean")
        fyi = False
    if requires_response and fyi:
        errors.append(
            f"{path} cannot mark the same message as both response-required and FYI"
        )

    _optional_short_string(value, "response_reason", path, errors, 500)
    _optional_short_string(value, "suggested_response_context", path, errors, 800)
    if requires_response and not value.get("response_reason"):
        errors.append(
            f"{path}.response_reason is required when requires_response is true"
        )

    decisions = value.get("decisions")
    if not _is_list(decisions):
        errors.append(f"{path}.decisions must be an array")
    else:
        for index, decision in enumerate(decisions):
            if (
                not isinstance(decision, str)
                or not decision.strip()
                or len(decision) > 500
            ):
                errors.append(
                    f"{path}.decisions[{index}] must be a non-empty string up to 500 characters"
                )

    commitments = value.get("commitments")
    if not _is_list(commitments):
        errors.append(f"{path}.commitments must be an array")
    else:
        for index, commitment in enumerate(commitments):
            item_path = f"{path}.commitments[{index}]"
            if not _is_dict(commitment):
                errors.append(f"{item_path} must be an object")
                continue
            _required_string(commitment, "owner", item_path, errors, 200)
            _required_string(commitment, "action", item_path, errors, 500)
            due_at = commitment.get("due_at")
            if due_at is not None:
                due = _timestamp(due_at, f"{item_path}.due_at", errors)
                if due and message_timestamp and due < message_timestamp:
                    errors.append(f"{item_path}.due_at precedes the message timestamp")
            _confidence(
                commitment.get("confidence"),
                f"{item_path}.confidence",
                errors,
            )
    return bool(requires_response), bool(fyi)


def validate_batch(batch: Any) -> tuple[list[str], list[str]]:
    errors: list[str] = []
    warnings: list[str] = []

    if not _is_dict(batch):
        return ["$ must be a JSON object"], warnings
    _check_prohibited_keys(batch, "$", errors)

    if batch.get("schema") != SCHEMA:
        errors.append(f"$.schema must equal {SCHEMA!r}")
    generated_at = _timestamp(batch.get("generated_at"), "$.generated_at", errors)

    source = batch.get("source")
    if not _is_dict(source):
        errors.append("$.source must be an object")
    else:
        if source.get("surface") != "claude_desktop":
            errors.append("$.source.surface must equal 'claude_desktop'")
        if source.get("connector") != "outlook":
            errors.append("$.source.connector must equal 'outlook'")

    window = batch.get("window")
    window_start = window_end = None
    if not _is_dict(window):
        errors.append("$.window must be an object")
    else:
        window_start = _timestamp(window.get("start"), "$.window.start", errors)
        window_end = _timestamp(window.get("end"), "$.window.end", errors)
        if window.get("timezone") != "America/Los_Angeles":
            errors.append("$.window.timezone must equal 'America/Los_Angeles'")
        if window_start and window_end and window_start > window_end:
            errors.append("$.window.start must not be after $.window.end")
        if generated_at and window_end and generated_at < window_end:
            errors.append("$.generated_at must not precede $.window.end")

    checkpoint = batch.get("checkpoint")
    if not _is_dict(checkpoint):
        errors.append("$.checkpoint must be an object")
    else:
        previous = checkpoint.get("previous")
        if previous is not None:
            _timestamp(previous, "$.checkpoint.previous", errors)
        candidate = _timestamp(
            checkpoint.get("candidate"),
            "$.checkpoint.candidate",
            errors,
        )
        if candidate and window_end and candidate != window_end:
            errors.append("$.checkpoint.candidate must equal $.window.end")

    messages = batch.get("messages")
    message_ids: set[str] = set()
    response_count = 0
    fyi_count = 0
    if not _is_list(messages):
        errors.append("$.messages must be an array")
        messages = []
    for index, message in enumerate(messages):
        path = f"$.messages[{index}]"
        if not _is_dict(message):
            errors.append(f"{path} must be an object")
            continue
        source_id = _required_string(message, "source_id", path, errors, 500)
        if source_id:
            if source_id in message_ids:
                errors.append(f"{path}.source_id duplicates another message")
            message_ids.add(source_id)
        _optional_short_string(
            message,
            "internet_message_id",
            path,
            errors,
            1000,
        )
        _optional_short_string(message, "conversation_id", path, errors, 1000)
        message_time = _timestamp(
            message.get("message_at"),
            f"{path}.message_at",
            errors,
        )
        if message_time and window_start and message_time < window_start:
            errors.append(f"{path}.message_at precedes the scan window")
        if message_time and window_end and message_time > window_end:
            errors.append(f"{path}.message_at follows the scan window")
        if message.get("direction") not in DIRECTIONS:
            errors.append(f"{path}.direction must be one of {sorted(DIRECTIONS)}")
        _required_string(message, "subject", path, errors, 500)
        _required_string(message, "summary", path, errors, 1000)
        _validate_participants(
            message.get("participants"),
            f"{path}.participants",
            errors,
        )
        _validate_url(
            message.get("outlook_web_url"),
            f"{path}.outlook_web_url",
            errors,
            warnings,
        )

        candidates = message.get("client_candidates")
        if not _is_list(candidates):
            errors.append(f"{path}.client_candidates must be an array")
        else:
            for candidate_index, candidate in enumerate(candidates):
                candidate_path = (
                    f"{path}.client_candidates[{candidate_index}]"
                )
                if not _is_dict(candidate):
                    errors.append(f"{candidate_path} must be an object")
                    continue
                if candidate.get("name") not in CLIENTS:
                    errors.append(
                        f"{candidate_path}.name must be a canonical scoped account"
                    )
                _confidence(
                    candidate.get("confidence"),
                    f"{candidate_path}.confidence",
                    errors,
                )
                _required_string(
                    candidate,
                    "evidence",
                    candidate_path,
                    errors,
                    500,
                )

        requires_response, fyi = _validate_signals(
            message.get("signals"),
            f"{path}.signals",
            errors,
            message_time,
        )
        response_count += int(requires_response)
        fyi_count += int(fyi)

    contacts = batch.get("contact_updates")
    if not _is_list(contacts):
        errors.append("$.contact_updates must be an array")
        contacts = []
    contact_emails: set[str] = set()
    for index, contact in enumerate(contacts):
        path = f"$.contact_updates[{index}]"
        if not _is_dict(contact):
            errors.append(f"{path} must be an object")
            continue
        email = _required_string(contact, "email", path, errors, 320)
        if email:
            if email != email.lower() or not EMAIL_RE.match(email):
                errors.append(
                    f"{path}.email must be a normalized lowercase email address"
                )
            if email in contact_emails:
                errors.append(f"{path}.email duplicates another contact update")
            contact_emails.add(email)
        _required_string(contact, "display_name", path, errors, 200)
        if contact.get("relationship_type") not in RELATIONSHIP_TYPES:
            errors.append(
                f"{path}.relationship_type must be one of {sorted(RELATIONSHIP_TYPES)}"
            )
        client = contact.get("client")
        if client is not None and client not in CLIENTS:
            errors.append(
                f"{path}.client must be null or a canonical scoped account"
            )
        _optional_short_string(contact, "explicit_role", path, errors, 300)
        _confidence(
            contact.get("inferred_importance"),
            f"{path}.inferred_importance",
            errors,
        )
        metrics = contact.get("interaction_metrics")
        if not _is_dict(metrics):
            errors.append(f"{path}.interaction_metrics must be an object")
        else:
            lookback = metrics.get("lookback_days")
            if (
                not isinstance(lookback, int)
                or isinstance(lookback, bool)
                or not 1 <= lookback <= 90
            ):
                errors.append(
                    f"{path}.interaction_metrics.lookback_days "
                    "must be an integer from 1 to 90"
                )
            for key in ("relevant_messages", "direct_threads"):
                count = metrics.get(key)
                if (
                    not isinstance(count, int)
                    or isinstance(count, bool)
                    or count < 0
                ):
                    errors.append(
                        f"{path}.interaction_metrics.{key} "
                        "must be a non-negative integer"
                    )
            relevant_messages = metrics.get("relevant_messages")
            direct_threads = metrics.get("direct_threads")
            if (
                isinstance(relevant_messages, int)
                and not isinstance(relevant_messages, bool)
                and isinstance(direct_threads, int)
                and not isinstance(direct_threads, bool)
                and direct_threads > relevant_messages
            ):
                errors.append(
                    f"{path}.interaction_metrics.direct_threads "
                    "must not exceed relevant_messages"
                )
            _timestamp(
                metrics.get("last_meaningful_interaction"),
                f"{path}.interaction_metrics.last_meaningful_interaction",
                errors,
            )
        _required_string(contact, "rationale", path, errors, 800)
        evidence = contact.get("evidence_message_ids")
        if not _is_list(evidence) or not evidence:
            errors.append(
                f"{path}.evidence_message_ids must be a non-empty array"
            )
        else:
            for evidence_index, source_id in enumerate(evidence):
                if not isinstance(source_id, str) or source_id not in message_ids:
                    errors.append(
                        f"{path}.evidence_message_ids[{evidence_index}] "
                        "must reference a message in this batch"
                    )

    summary = batch.get("batch_summary")
    if not _is_dict(summary):
        errors.append("$.batch_summary must be an object")
    else:
        expected = {
            "messages_included": len(messages),
            "response_required": response_count,
            "fyi": fyi_count,
            "contact_updates": len(contacts),
        }
        scanned = summary.get("messages_scanned")
        if (
            not isinstance(scanned, int)
            or isinstance(scanned, bool)
            or scanned < len(messages)
        ):
            errors.append(
                "$.batch_summary.messages_scanned must be an integer "
                "at least messages_included"
            )
        for key, expected_value in expected.items():
            if summary.get(key) != expected_value:
                errors.append(
                    f"$.batch_summary.{key} must equal {expected_value}"
                )
        summary_warnings = summary.get("warnings")
        if not _is_list(summary_warnings) or not all(
            isinstance(item, str) for item in summary_warnings
        ):
            errors.append("$.batch_summary.warnings must be an array of strings")

    return errors, warnings


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("batch", type=Path, help="Path to the ingestion JSON file")
    args = parser.parse_args()

    try:
        batch = json.loads(args.batch.read_text(encoding="utf-8"))
    except FileNotFoundError:
        print(f"ERROR: file not found: {args.batch}", file=sys.stderr)
        return 2
    except json.JSONDecodeError as exc:
        print(f"ERROR: invalid JSON: {exc}", file=sys.stderr)
        return 2

    errors, warnings = validate_batch(batch)
    for warning in warnings:
        print(f"WARNING: {warning}")
    for error in errors:
        print(f"ERROR: {error}", file=sys.stderr)
    if errors:
        print(
            f"FAILED: {len(errors)} error(s), {len(warnings)} warning(s)",
            file=sys.stderr,
        )
        return 1
    print(
        "VALID: "
        f"{len(batch['messages'])} message(s), "
        f"{len(batch['contact_updates'])} contact update(s), "
        f"{len(warnings)} warning(s)"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
