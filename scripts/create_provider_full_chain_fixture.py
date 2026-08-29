#!/usr/bin/env python3
"""Create an ephemeral one-pair Provider full-chain fixture.

This helper never reads credentials and never contacts a model provider.  It
creates a self-contained project root for the explicit live-smoke entrypoint:
one EvalSuite case, matched baseline/candidate arms, a deterministic
``result.txt`` grader, and a copied immutable Skill package.
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import sys
from pathlib import Path


MAX_TOKENS = 64
MAX_TIMEOUT_SECONDS = 30.0
MAX_COST_USD = 0.05


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project-root", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--provider", required=True, choices=("openai", "anthropic"))
    parser.add_argument("--model", required=True)
    parser.add_argument("--max-output-tokens", required=True, type=int)
    parser.add_argument("--timeout-seconds", required=True, type=float)
    parser.add_argument("--max-cost-usd", required=True, type=float)
    return parser.parse_args()


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def source_skill_hash(project_root: Path) -> str:
    manifest = project_root / "experiments" / "csv-analysis-v1-demo.yaml"
    match = re.search(
        r"^\s*version:\s*(sha256:[0-9a-f]{64})\s*$",
        manifest.read_text(encoding="utf-8"),
        re.MULTILINE,
    )
    if match is None:
        raise ValueError(f"could not find frozen csv-analysis version in {manifest}")
    return match.group(1)


def main() -> int:
    args = parse_args()
    project_root = Path(args.project_root).resolve()
    output = Path(args.output).resolve()
    if args.max_output_tokens < 1 or args.max_output_tokens > MAX_TOKENS:
        raise ValueError(f"max-output-tokens must be in 1..{MAX_TOKENS}")
    if args.timeout_seconds <= 0 or args.timeout_seconds > MAX_TIMEOUT_SECONDS:
        raise ValueError(f"timeout-seconds must be in (0,{MAX_TIMEOUT_SECONDS}]")
    if args.max_cost_usd <= 0 or args.max_cost_usd > MAX_COST_USD:
        raise ValueError(f"max-cost-usd must be in (0,{MAX_COST_USD}]")
    if not args.model.strip():
        raise ValueError("model must be non-empty")
    if output.exists() and any(output.iterdir()):
        raise ValueError(f"output already exists and is not empty: {output}")

    skill_hash = source_skill_hash(project_root)
    output.mkdir(parents=True, exist_ok=True)
    shutil.copytree(project_root / "skills" / "csv-analysis", output / "skills" / "csv-analysis")
    (output / "policies").mkdir(parents=True, exist_ok=True)
    shutil.copy2(
        project_root / "policies" / "conservative-release.yaml",
        output / "policies" / "conservative-release.yaml",
    )

    write_json(
        output / "evals" / "provider-live" / "suite.yaml",
        {
            "apiVersion": "skillgate.dev/v1alpha1",
            "kind": "EvalSuite",
            "metadata": {"name": "provider-live-smoke"},
            "spec": {
                "cases": [
                    {
                        "id": "provider-live-smoke-001",
                        "evaluationMode": "forced_injection",
                        "population": "answer",
                        "prompt": "Reply with exactly: skillgate-provider-full-chain-ok",
                        "fixtures": [],
                    }
                ]
            },
        },
    )
    write_json(
        output / "evals" / "provider-live" / "grader.yaml",
        {
            "apiVersion": "skillgate.dev/v1alpha1",
            "kind": "Grader",
            "metadata": {"name": "provider-live-result-artifact", "version": 1},
            "spec": {
                "type": "deterministic",
                "method": "file_exists",
                "mode": "deterministic_only",
                "llmJudge": False,
                "config": {"artifact_pattern": "result.txt"},
                "scoring": {"passed_score": 1, "failed_score": 0},
            },
        },
    )
    write_json(
        output / "environments" / "provider-live.json",
        {
            "kind": "skillgate.provider-live-environment",
            "name": "provider-live-smoke",
            "version": "v1",
            "credentials": "operator-only-none-in-manifest",
            "network": "operator-controlled-live-smoke",
            "spec": {"workspace_root": "/workspace", "tool_policy": {"allow": [], "deny": ["credential.read", "workspace.escape"]}},
        },
    )

    per_trial_cost = round(args.max_cost_usd / 2, 6)
    model = {
        "provider": args.provider,
        "name": args.model.strip(),
        "config": {
            "temperature": 0,
            "max_tokens": args.max_output_tokens,
            "timeout_seconds": args.timeout_seconds,
            "max_retries": 0,
        },
    }
    common_strategy = {
        "model": model,
        "tools": {"allow": [], "deny": ["credential.read", "workspace.escape"]},
        "budget": {
            "maxInputTokens": 256,
            "maxOutputTokens": args.max_output_tokens,
            "timeoutSeconds": int(args.timeout_seconds),
            "maxCostUsd": per_trial_cost,
        },
        "retry": {"maxAttempts": 1, "retryableCategories": []},
        "sandbox": {"profile": "docker-restricted-v1"},
    }
    baseline = {"name": "baseline", **common_strategy, "skills": []}
    candidate = {
        "name": "candidate",
        **common_strategy,
        "skills": [{"name": "csv-analysis", "version": skill_hash}],
    }
    write_json(
        output / "experiments" / "provider-live-smoke.yaml",
        {
            "apiVersion": "skillgate.dev/v1alpha1",
            "kind": "Experiment",
            "metadata": {"name": "provider-live-smoke"},
            "spec": {
                "suite": "./evals/provider-live/suite.yaml",
                "skills": [{"name": "csv-analysis", "path": "./skills/csv-analysis", "version": skill_hash}],
                "strategies": [baseline, candidate],
                "arms": [
                    {"name": "without_skill", "strategy": "baseline"},
                    {"name": "with_skill", "strategy": "candidate"},
                ],
                "pairing": {"treatment": "skill_version", "baselineArm": "without_skill", "candidateArm": "with_skill"},
                "repetitions": 1,
                "execution": {
                    "harness": "langgraph",
                    "harnessVersion": "provider-live-smoke-v1",
                    "environment": "docker://skillgate/provider-live-smoke:0.1.0",
                    "environmentDescriptor": "./environments/provider-live.json",
                    "timeoutSeconds": int(args.timeout_seconds),
                    "maxConcurrent": 1,
                },
                "grading": {"ref": "./evals/provider-live/grader.yaml", "llm": {"enabled": False}},
                "policy": "./policies/conservative-release.yaml",
            },
        },
    )
    print(json.dumps({"created": True, "root": str(output), "manifest": str(output / "experiments" / "provider-live-smoke.yaml"), "pair_count": 1, "trial_count": 2}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, ValueError) as exc:
        print(f"provider full-chain fixture failed: {exc}", file=sys.stderr)
        raise SystemExit(2)
