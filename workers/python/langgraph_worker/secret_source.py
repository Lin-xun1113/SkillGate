"""Operator-owned environment and Docker Secret lookup boundary.

The worker receives immutable execution content from the control plane.  This
module is the only supported path for resolving runtime credentials; callers
must pass an operator environment mapping (if any), never a manifest or trial
descriptor.  Error text intentionally excludes paths, values and read errors.
"""

from __future__ import annotations

import os
import re
from enum import Enum
from pathlib import Path
from typing import Callable, Mapping, Optional


class SecretCode(str, Enum):
    MISSING = "SECRET_MISSING"
    EMPTY = "SECRET_EMPTY"
    UNREADABLE = "SECRET_UNREADABLE"
    INVALID_NAME = "SECRET_INVALID_NAME"


class SecretError(RuntimeError):
    """Stable, non-sensitive secret lookup failure."""

    def __init__(self, code: SecretCode, name: str):
        self.code = code
        self.name = name
        super().__init__(f"{code.value}: secret {name} is unavailable")


_NAME_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")


def read_secret(
    name: str,
    environment: Optional[Mapping[str, object]] = None,
    *,
    environ: Optional[Mapping[str, str]] = None,
    read_file: Optional[Callable[[str], str]] = None,
) -> str:
    """Resolve ``NAME_FILE`` first, then ``NAME``.

    ``environment`` is an explicit operator injection used by tests and
    embedding hosts.  Missing keys fall through to the process environment;
    an explicitly configured empty/unreadable primary source never falls back
    to an alias, which makes deployment mistakes fail closed.
    """

    if not _NAME_RE.fullmatch(name):
        raise SecretError(SecretCode.INVALID_NAME, name)
    explicit = dict(environment or {})
    process = dict(environ if environ is not None else os.environ)

    def lookup(key: str) -> tuple[object, bool]:
        # Preserve compatibility with older callers that supplied lower-case
        # environment names while preferring the canonical upper-case form.
        if key in explicit:
            return explicit[key], True
        lower = key.lower()
        if lower in explicit:
            return explicit[lower], True
        if key in process:
            return process[key], True
        return "", False

    file_value, file_configured = lookup(f"{name}_FILE")
    if file_configured:
        if not isinstance(file_value, str) or not file_value.strip():
            raise SecretError(SecretCode.EMPTY, f"{name}_FILE")
        path = file_value.strip()
        try:
            if read_file is None:
                value = Path(path).read_text(encoding="utf-8")
            else:
                value = read_file(path)
        except (OSError, UnicodeError):
            raise SecretError(SecretCode.UNREADABLE, name) from None
        if not isinstance(value, str) or not value.strip():
            raise SecretError(SecretCode.EMPTY, name)
        return value.strip()

    value, configured = lookup(name)
    if not configured:
        raise SecretError(SecretCode.MISSING, name)
    if not isinstance(value, str) or not value.strip():
        raise SecretError(SecretCode.EMPTY, name)
    return value.strip()


def configured(name: str, environment: Optional[Mapping[str, object]] = None) -> bool:
    """Return whether a direct or file-backed source is configured."""

    if not _NAME_RE.fullmatch(name):
        return False
    explicit = environment or {}
    for key in (f"{name}_FILE", f"{name}", f"{name}_FILE".lower(), name.lower()):
        if key in explicit or key in os.environ:
            return True
    return False


# Short aliases keep embedding code readable while preserving one boundary.
lookup = read_secret
resolve = read_secret


__all__ = ["SecretCode", "SecretError", "configured", "lookup", "read_secret", "resolve"]
