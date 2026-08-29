"""Load content-addressed Skill packages from the read-only CAS mount."""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path
from typing import Any, Dict

try:
    import yaml
except ImportError:  # pragma: no cover - minimal local/protocol runtime
    yaml = None

from .canonical import canonical_json_bytes


class SkillNotFoundError(Exception):
    """Skill not found in CAS or its content hash does not match."""


class SkillLoader:
    def __init__(self, cas_dir: str | Path):
        self.cas_dir = Path(cas_dir)
        if not self.cas_dir.exists():
            raise ValueError(f"CAS directory does not exist: {cas_dir}")

    def _candidates(self, skill_hash: str) -> list[Path]:
        digest = skill_hash.removeprefix("sha256:")
        paths = [
            self.cas_dir / "skills" / digest[:2] / digest / "canonical.json",
            self.cas_dir / "skills" / digest[:2] / skill_hash / "canonical.json",
            self.cas_dir / "skills" / digest / "canonical.json",
            self.cas_dir / "skills" / digest[:2] / digest / "skill.yaml",
            self.cas_dir / "skills" / digest / "skill.yaml",
        ]
        # Registry mounts retain the logical name between skills/ and hash.
        paths.extend(self.cas_dir.glob(f"**/{digest}/canonical.json"))
        return list(dict.fromkeys(paths))

    def load_skill(self, skill_hash: str) -> Dict[str, Any]:
        if not skill_hash:
            raise SkillNotFoundError("skill_hash is empty")
        digest = skill_hash.removeprefix("sha256:")
        for path in self._candidates(skill_hash):
            if not path.exists():
                continue
            if path.name == "canonical.json":
                canonical = path.read_bytes()
                computed = hashlib.sha256(canonical).hexdigest()
                try:
                    package = json.loads(canonical)
                    files = package.get("files", [])
                    skill_file = next(item for item in files if item.get("path") == "SKILL.md")
                    content = skill_file.get("content", "")
                    metadata = _skill_metadata(content)
                except (ValueError, StopIteration, TypeError) as exc:
                    raise SkillNotFoundError(f"invalid canonical skill package: {exc}") from exc
                # M0 manifests may carry the legacy package hash. Accept it
                # only when it recomputes from the exact canonical file list.
                legacy_files = []
                for item in files:
                    if item.get("encoding", "utf8") != "utf8":
                        legacy_files = []
                        break
                    legacy_files.append({"path": item.get("path", ""), "content": item.get("content", "")})
                legacy_obj = {"files": sorted(legacy_files, key=lambda item: item["path"])}
                legacy_digest = hashlib.sha256(canonical_json_bytes(legacy_obj)).hexdigest()
                if digest not in (computed, legacy_digest):
                    raise SkillNotFoundError(
                        f"skill canonical hash mismatch: expected {skill_hash}, got sha256:{computed}"
                    )
                return {"hash": skill_hash, "path": str(path.parent), "metadata": metadata, "content": content}

            # Legacy CAS entries are accepted only when their declared hash is
            # the raw file digest; package hashes should use canonical.json.
            content = path.read_bytes()
            computed = hashlib.sha256(content).hexdigest()
            if computed != digest:
                raise SkillNotFoundError(
                    f"skill content hash mismatch: expected {skill_hash}, got sha256:{computed}"
                )
            text = content.decode("utf-8")
            return {"hash": skill_hash, "path": str(path.parent), "metadata": _skill_metadata(text), "content": text}
        # A legacy hash does not identify its canonical digest on disk. Scan
        # canonical entries as a compatibility path; each candidate is still
        # fully hash-verified before it is returned.
        for path in self.cas_dir.glob("skills/**/canonical.json"):
            try:
                loaded = self._load_canonical_candidate(path, skill_hash)
            except SkillNotFoundError:
                continue
            if loaded is not None:
                return loaded
        raise SkillNotFoundError(f"Skill not found in CAS: {skill_hash}")

    def _load_canonical_candidate(self, path: Path, skill_hash: str) -> Dict[str, Any] | None:
        canonical = path.read_bytes()
        try:
            package = json.loads(canonical)
            files = package.get("files", [])
            skill_file = next(item for item in files if item.get("path") == "SKILL.md")
        except (ValueError, StopIteration, TypeError):
            return None
        computed = hashlib.sha256(canonical).hexdigest()
        legacy_files = [
            {"path": item.get("path", ""), "content": item.get("content", "")}
            for item in files
            if item.get("encoding", "utf8") == "utf8"
        ]
        legacy_digest = hashlib.sha256(
            canonical_json_bytes({"files": sorted(legacy_files, key=lambda item: item["path"])})
        ).hexdigest()
        digest = skill_hash.removeprefix("sha256:")
        if digest not in (computed, legacy_digest):
            return None
        content = skill_file.get("content", "")
        return {
            "hash": skill_hash,
            "path": str(path.parent),
            "metadata": _skill_metadata(content),
            "content": content,
        }

    def get_skill_path(self, skill_hash: str) -> Path:
        loaded = self.load_skill(skill_hash)
        return Path(loaded["path"])


def _skill_metadata(content: str) -> Dict[str, Any]:
    match = re.match(r"^---\s*\n(.*?)\n---\s*(?:\n|$)", content, re.DOTALL)
    if match:
        if yaml is not None:
            try:
                parsed = yaml.safe_load(match.group(1))
                if isinstance(parsed, dict):
                    return parsed
            except yaml.YAMLError:
                pass
        return _simple_frontmatter(match.group(1))
    if yaml is not None:
        try:
            parsed = yaml.safe_load(content)
            return parsed if isinstance(parsed, dict) else {}
        except yaml.YAMLError:
            return {}
    return {}


def _simple_frontmatter(value: str) -> Dict[str, Any]:
    """Parse the scalar frontmatter fields needed by the executor fallback."""
    result: Dict[str, Any] = {}
    for line in value.splitlines():
        if ":" not in line or line[:1].isspace():
            continue
        key, raw = line.split(":", 1)
        key, raw = key.strip(), raw.strip()
        if not key:
            continue
        if (raw.startswith("\"") and raw.endswith("\"")) or (raw.startswith("'") and raw.endswith("'")):
            raw = raw[1:-1]
        result[key] = raw
    return result
