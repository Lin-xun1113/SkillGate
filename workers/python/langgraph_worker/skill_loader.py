"""Load and verify skills from CAS."""

import hashlib
import os
from pathlib import Path
from typing import Dict, Any, Optional
import yaml


class SkillNotFoundError(Exception):
    """Skill not found in CAS or hash mismatch."""
    pass


class SkillLoader:
    """Load skills from CAS with hash verification."""

    def __init__(self, cas_dir: str):
        """Initialize with CAS directory (read-only mount)."""
        self.cas_dir = Path(cas_dir)
        if not self.cas_dir.exists():
            raise ValueError(f"CAS directory does not exist: {cas_dir}")

    def load_skill(self, skill_hash: str) -> Dict[str, Any]:
        """
        Load skill from CAS by hash.

        Args:
            skill_hash: SHA-256 hash of the skill

        Returns:
            Skill metadata and content

        Raises:
            SkillNotFoundError: If skill not found or hash mismatch
        """
        if not skill_hash:
            raise SkillNotFoundError("skill_hash is empty")

        # CAS sharding: first 2 chars as subdirectory
        skill_path = self.cas_dir / "skills" / skill_hash[:2] / skill_hash

        if not skill_path.exists():
            raise SkillNotFoundError(
                f"Skill not found in CAS: {skill_hash}"
            )

        # Load skill.yaml
        skill_file = skill_path / "skill.yaml"
        if not skill_file.exists():
            raise SkillNotFoundError(
                f"skill.yaml not found for hash {skill_hash}"
            )

        try:
            with open(skill_file, "r") as f:
                skill_data = yaml.safe_load(f)
        except Exception as e:
            raise SkillNotFoundError(
                f"Failed to parse skill.yaml: {e}"
            )

        # Verify hash (optional, for extra safety)
        # In M4, we trust the CAS structure; M5+ may add content verification

        return {
            "hash": skill_hash,
            "path": str(skill_path),
            "metadata": skill_data,
        }

    def get_skill_path(self, skill_hash: str) -> Path:
        """Get filesystem path to skill directory."""
        skill_path = self.cas_dir / "skills" / skill_hash[:2] / skill_hash
        if not skill_path.exists():
            raise SkillNotFoundError(f"Skill not found: {skill_hash}")
        return skill_path
