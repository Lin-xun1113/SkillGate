"""Canonical JSON and identity helpers shared by the LangGraph worker."""

from __future__ import annotations

import hashlib
import json
from typing import Any, Mapping


def _normalize(value: Any) -> Any:
    if isinstance(value, str):
        return value.replace("\r\n", "\n").replace("\r", "\n")
    if isinstance(value, Mapping):
        return {str(key): _normalize(item) for key, item in value.items()}
    if isinstance(value, list):
        return [_normalize(item) for item in value]
    if isinstance(value, tuple):
        return [_normalize(item) for item in value]
    return value


def canonical_json_bytes(value: Any) -> bytes:
    """Match Go identity.CanonicalJSON byte-for-byte for JSON values."""
    normalized = _normalize(value)
    # encoding/json rejects NaN and +/-Inf. Matching that behavior avoids a
    # Python worker accepting an identity that Go cannot hash.
    try:
        return json.dumps(
            normalized,
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=False,
            allow_nan=False,
        ).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise ValueError(f"value is not canonical JSON: {exc}") from exc


def sha256_canonical(value: Any) -> str:
    return "sha256:" + hashlib.sha256(canonical_json_bytes(value)).hexdigest()


def request_identity(trial: Mapping[str, Any]) -> dict[str, Any]:
    """Return the frozen six-field scheduling identity."""
    if not isinstance(trial, Mapping):
        raise ValueError("trial request must be an object")
    for key in ("experiment_id", "logical_trial_id", "trial_id"):
        if not isinstance(trial.get(key), str) or not trial.get(key):
            raise ValueError(f"{key} is required for trial request identity")
    attempt_value = trial.get("attempt_no", trial.get("attempt", 0))
    if isinstance(attempt_value, bool):
        raise ValueError("attempt_no must be a positive integer")
    try:
        attempt_no = int(attempt_value or 0)
    except (TypeError, ValueError, OverflowError) as exc:
        raise ValueError("attempt_no must be a positive integer") from exc
    if attempt_no < 1:
        raise ValueError("attempt_no must be a positive integer")
    return {
        "experiment_id": trial.get("experiment_id", ""),
        "logical_trial_id": trial.get("logical_trial_id", ""),
        "trial_id": trial.get("trial_id", ""),
        "pair_id": trial.get("pair_id", ""),
        "arm": trial.get("arm", ""),
        "attempt_no": attempt_no,
    }


def request_hash(trial: Mapping[str, Any]) -> str:
    return sha256_canonical(request_identity(trial))


def result_idempotency_key(trial_id: str, result_manifest_hash: str) -> str:
    if not isinstance(trial_id, str) or not trial_id:
        raise ValueError("trial_id is required for result idempotency key")
    if not isinstance(result_manifest_hash, str) or not result_manifest_hash:
        raise ValueError("result_manifest_hash is required for result idempotency key")
    return sha256_canonical(
        {"trial_id": trial_id, "result_manifest_hash": result_manifest_hash}
    )
