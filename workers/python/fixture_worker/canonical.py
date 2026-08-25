import hashlib
import json
from typing import Any


def canonical_json_bytes(obj: Any) -> bytes:
    """Serializes a Python object into canonical JSON bytes with sorted keys and no extra whitespace."""
    return json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")


def sha256_canonical(obj: Any) -> str:
    """Computes sha256:hex for the canonical representation of obj."""
    raw = canonical_json_bytes(obj)
    digest = hashlib.sha256(raw).hexdigest()
    return f"sha256:{digest}"
