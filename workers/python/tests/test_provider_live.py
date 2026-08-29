"""Opt-in real Provider smoke test.

This file is intentionally skipped by default. It verifies only the live
Provider Adapter boundary; it is not the full SkillGate gRPC/Grading E2E.
"""

import os

import pytest

from langgraph_worker.provider import create_provider
from langgraph_worker.secret_source import SecretError, read_secret


# Hard limits for the opt-in smoke.  They are intentionally constants rather
# than user-controlled defaults; a caller may only lower them.
MAX_SMOKE_CASES = 1
MAX_SMOKE_TOKENS = 64
MAX_SMOKE_TIMEOUT_SECONDS = 30
MAX_SMOKE_COST_USD = 0.05


@pytest.mark.skipif(
    os.getenv("LIVE_PROVIDER_SMOKE") != "1",
    reason="set LIVE_PROVIDER_SMOKE=1 with a controlled Provider credential",
)
def test_live_provider_adapter_smoke():
    provider_name = os.getenv("LIVE_PROVIDER", "").strip().lower()
    model_name = os.getenv("LIVE_PROVIDER_MODEL", "").strip()
    if provider_name not in {"openai", "anthropic"}:
        pytest.fail("LIVE_PROVIDER must be openai or anthropic")
    if not model_name:
        pytest.fail("LIVE_PROVIDER_MODEL must name the exact model to test")

    credential_name = f"{provider_name.upper()}_API_KEY"
    try:
        read_secret(credential_name)
    except SecretError:
        # An explicitly requested smoke must never silently fall back to a
        # fixture provider when the operator has not supplied a key.
        pytest.fail(f"FAIL CLOSED: {credential_name} is required for live smoke")

    configured_tokens = int(os.getenv("LIVE_PROVIDER_MAX_TOKENS", str(MAX_SMOKE_TOKENS)))
    configured_timeout = float(
        os.getenv("LIVE_PROVIDER_TIMEOUT_SECONDS", str(MAX_SMOKE_TIMEOUT_SECONDS))
    )
    configured_cost = float(os.getenv("LIVE_PROVIDER_MAX_COST_USD", str(MAX_SMOKE_COST_USD)))
    if configured_tokens < 1 or configured_tokens > MAX_SMOKE_TOKENS:
        pytest.fail(f"LIVE_PROVIDER_MAX_TOKENS must be in 1..{MAX_SMOKE_TOKENS}")
    if configured_timeout <= 0 or configured_timeout > MAX_SMOKE_TIMEOUT_SECONDS:
        pytest.fail(
            f"LIVE_PROVIDER_TIMEOUT_SECONDS must be in (0,{MAX_SMOKE_TIMEOUT_SECONDS}]")
    if configured_cost <= 0 or configured_cost > MAX_SMOKE_COST_USD:
        pytest.fail(f"LIVE_PROVIDER_MAX_COST_USD must be in (0,{MAX_SMOKE_COST_USD}]")

    provider = create_provider(
        {
            "provider": provider_name,
            "name": model_name,
            "config": {
                "max_tokens": configured_tokens,
                "timeout_seconds": configured_timeout,
                "max_retries": 0,
            },
        }
    )
    result = provider.invoke(
        [
            {
                "role": "user",
                "content": "Reply with exactly: skillgate-provider-smoke-ok",
            }
        ]
    )

    assert result["model"]
    assert result["choices"]
    assert result["choices"][0]["message"]["content"]
    assert result["usage"].get("input_tokens", 0) > 0
    assert result["usage"].get("output_tokens", 0) > 0
    assert result["usage"].get("total_tokens", 0) <= configured_tokens + result["usage"].get("input_tokens", 0)


@pytest.mark.skipif(
    os.getenv("LIVE_PROVIDER_FULL_CHAIN") != "1",
    reason="set LIVE_PROVIDER_FULL_CHAIN=1 for the controlled full-chain preflight",
)
def test_live_provider_full_chain_preflight():
    """Fail closed until an operator supplies the separate full-chain harness.

    The repository's default Compose path intentionally remains the 48-trial
    Fixture experiment.  This marker records the required explicit switch and
    caps without claiming that a real Provider gRPC/Grading/Report run occurred.
    The shell entrypoint performs the same preflight and reports this state.
    """
    provider_name = os.getenv("LIVE_PROVIDER", "").strip().lower()
    model_name = os.getenv("LIVE_PROVIDER_MODEL", "").strip()
    if provider_name not in {"openai", "anthropic"} or not model_name:
        pytest.fail("LIVE_PROVIDER and LIVE_PROVIDER_MODEL are required for full-chain preflight")
    credential_name = f"{provider_name.upper()}_API_KEY"
    try:
        read_secret(credential_name)
    except SecretError:
        pytest.fail(f"FAIL CLOSED: {credential_name} is required for full-chain preflight")
    pytest.skip(
        "full-chain Provider acceptance requires a separately authorized temporary "
        "matched fixture/manifest and is not run by the default Compose demo"
    )
