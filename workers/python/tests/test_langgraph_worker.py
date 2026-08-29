"""Offline contract tests for the LangGraph worker.

These tests deliberately do not import LangGraph or call a real model API.
They cover the content-addressed protocol and the provider boundaries that
must remain deterministic in CI.
"""

import hashlib
import json
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

from langgraph_worker.canonical import (
    canonical_json_bytes,
    request_hash,
    result_idempotency_key,
    sha256_canonical,
)
from langgraph_worker.executor import LangGraphExecutor, TrialExecutor
from langgraph_worker.m0_fixture import FixtureExecutionError, execute_csv_case
from langgraph_worker.models import TrialTrace
from langgraph_worker.provider import (
    AnthropicProvider,
    OpenAIProvider,
    ProviderConfigurationError,
    ProviderError,
    ProviderPermanentError,
    ProviderResponseError,
    ProviderTimeoutError,
    ProviderTransientError,
    ProviderUnavailableError,
    RecordedProvider,
    RecordedResponseNotFoundError,
    create_provider,
)
from langgraph_worker.providers import create_llm
from langgraph_worker.skill_loader import SkillLoader, SkillNotFoundError
from langgraph_worker.worker import LangGraphWorker, provider_failure_category
from runner.v1 import runner_pb2


def test_request_hash_matches_go_golden_vector():
    trial = {
        "experiment_id": "e",
        "logical_trial_id": "l",
        "trial_id": "t",
        "pair_id": "p",
        "arm": "with_skill",
        "attempt_no": 1,
    }
    assert request_hash(trial) == "sha256:d14e6c29d72ed21c2d5772682c22de089c39105cf6d6b5464a21cf90f08808eb"


def test_execution_hash_bytes_match_go_canonical_map():
    execution = {
        "case_id": "case-1",
        "case_input": {"prompt": "line1\r\nline2"},
        "skill_hash": "",
        "model": {"name": "mock", "provider": "fixture"},
        "tool_policy": {"tools": []},
        "environment": {},
    }
    assert canonical_json_bytes(execution).decode() == (
        '{"case_id":"case-1","case_input":{"prompt":"line1\\nline2"},'
        '"environment":{},"model":{"name":"mock","provider":"fixture"},'
        '"skill_hash":"","tool_policy":{"tools":[]}}'
    )
    assert sha256_canonical(execution) == "sha256:ccfb9d18e89268a813aef5cad7dd04586177890a74f2d65ae0517b8912082883"
    worker = LangGraphWorker()
    assert worker._compute_execution_hash(execution) == sha256_canonical(execution)


def test_worker_rejects_tampered_execution_and_request_identity():
    execution = {
        "case_id": "case-1",
        "case_input": {},
        "skill_hash": "",
        "model": {"provider": "fixture"},
        "tool_policy": {},
        "environment": {},
    }
    trial = {
        "experiment_id": "e",
        "logical_trial_id": "l",
        "trial_id": "t",
        "pair_id": "p",
        "arm": "without_skill",
        "attempt_no": 1,
        "execution": execution,
        "execution_hash": sha256_canonical(execution),
        "request_hash": request_hash(
            {
                "experiment_id": "e",
                "logical_trial_id": "l",
                "trial_id": "t",
                "pair_id": "p",
                "arm": "without_skill",
                "attempt_no": 1,
            }
        ),
    }
    worker = LangGraphWorker()
    assert worker.verify_hashes(trial)
    trial["execution"]["case_input"]["tampered"] = True
    assert not worker.verify_hashes(trial)
    trial["execution"] = execution
    trial["execution_hash"] = sha256_canonical(execution)
    trial["arm"] = "with_skill"
    assert not worker.verify_hashes(trial)


def test_canonical_json_rejects_non_json_numbers():
    with pytest.raises(ValueError):
        canonical_json_bytes({"value": float("nan")})


def test_model_fallback_defaults_are_instance_local():
    first = TrialTrace()
    second = TrialTrace()
    first.steps.append({"step": "one"})
    assert second.steps == []
    assert first.model_dump() == {"llm_calls": [], "tool_calls": [], "steps": [{"step": "one"}]}


def test_request_identity_rejects_incomplete_or_invalid_attempt():
    with pytest.raises(ValueError):
        request_hash({"experiment_id": "e", "logical_trial_id": "l", "trial_id": "t"})
    with pytest.raises(ValueError):
        request_hash(
            {
                "experiment_id": "e",
                "logical_trial_id": "l",
                "trial_id": "t",
                "attempt_no": 0,
            }
        )


def test_result_idempotency_key_matches_canonical_contract():
    manifest_hash = "sha256:manifest"
    expected = sha256_canonical(
        {"trial_id": "trial-1", "result_manifest_hash": manifest_hash}
    )
    assert result_idempotency_key("trial-1", manifest_hash) == expected


def test_fixture_and_mock_providers_are_deterministic():
    messages = [{"role": "user", "content": "hello"}]
    first = create_provider({"provider": "fixture", "name": "agent-v1"})
    second = create_provider({"provider": "mock", "name": "agent-v1"})
    assert first.complete(messages) == second.complete(messages)
    assert first.complete(messages) == second.complete(messages)


def test_recorded_provider_is_strict_and_does_not_fallback(tmp_path: Path):
    messages = [{"role": "user", "content": "hello"}]
    input_hash = hashlib.sha256(canonical_json_bytes(messages)).hexdigest()
    recording_path = tmp_path / "recordings.json"
    recording_path.write_text(
        json.dumps({input_hash: {"choices": [{"message": {"content": "recorded"}}]}}),
        encoding="utf-8",
    )
    provider = RecordedProvider(recording_path)
    assert provider.complete(messages)["choices"][0]["message"]["content"] == "recorded"
    with pytest.raises(RecordedResponseNotFoundError):
        provider.complete([{"role": "user", "content": "other"}])
    with pytest.raises(ProviderConfigurationError):
        create_provider({"provider": "recorded"})
    with pytest.raises(ProviderConfigurationError):
        create_provider({"provider": "recorded", "recordings_path": str(tmp_path / "missing.json")})


@pytest.mark.parametrize("provider,env_name", [("openai", "OPENAI_API_KEY"), ("anthropic", "ANTHROPIC_API_KEY")])
def test_real_provider_requires_explicit_credential(monkeypatch, provider: str, env_name: str):
    monkeypatch.delenv(env_name, raising=False)
    with pytest.raises(ProviderConfigurationError, match=env_name):
        create_provider({"provider": provider, "name": "test-model"}, {})


def test_real_provider_sdk_is_lazy_but_missing_dependency_is_explicit(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "test-only-key")
    # Force the optional SDK import to fail so this contract test never makes
    # a network request when the development environment happens to include it.
    monkeypatch.setitem(sys.modules, "langchain_openai", None)
    provider = create_provider({"provider": "openai", "name": "test-model"}, {})
    with pytest.raises(ProviderUnavailableError):
        provider.complete([{"role": "user", "content": "hello"}])


def test_real_provider_injected_client_normalizes_response_and_forwards_contract(monkeypatch):
    class FakeClient:
        def __init__(self):
            self.calls = []

        def invoke(self, messages, **kwargs):
            self.calls.append((messages, kwargs))
            return SimpleNamespace(
                id="resp-1",
                content=[{"type": "text", "text": "hello"}],
                response_metadata={
                    "model_name": "actual-model",
                    "usage": {"input_tokens": 3, "output_tokens": 2},
                    "stop_reason": "end_turn",
                },
            )

    monkeypatch.delenv("OPENAI_BASE_URL", raising=False)
    client = FakeClient()
    provider = OpenAIProvider(
        {
            "provider": "openai",
            "model_id": "configured-model",
            "params": {"timeout_seconds": 7, "max_retries": 2},
        },
        {"OPENAI_API_KEY": "test-only-key"},
        client=client,
        base_url="https://operator.example/",
    )
    result = provider.invoke(
        [{"role": "user", "content": "hello"}],
        tools=[{"type": "function", "function": {"name": "lookup"}}],
        params={"stop": ["done"]},
    )

    assert provider.model_name == "configured-model"
    assert provider.timeout == 7.0
    assert provider.max_retries == 2
    assert provider.base_url == "https://operator.example/"
    assert result["id"] == "resp-1"
    assert result["model"] == "actual-model"
    assert result["choices"][0]["message"]["content"] == "hello"
    assert result["choices"][0]["finish_reason"] == "end_turn"
    assert result["usage"] == {
        "prompt_tokens": 3,
        "input_tokens": 3,
        "completion_tokens": 2,
        "output_tokens": 2,
        "total_tokens": 5,
    }
    assert client.calls[0][0][0]["role"] == "user"
    assert client.calls[0][1]["stop"] == ["done"]
    assert client.calls[0][1]["tools"]


def test_operator_base_url_is_read_from_process_environment_only(monkeypatch):
    monkeypatch.setenv("ANTHROPIC_BASE_URL", "https://operator.example/")
    provider = AnthropicProvider(
        {
            "provider": "anthropic",
            "name": "claude-test",
        },
        {"ANTHROPIC_API_KEY": "test-only-key", "ANTHROPIC_BASE_URL": "https://descriptor.example/"},
        client=SimpleNamespace(invoke=lambda *_args, **_kwargs: {"choices": [{"message": {"content": "ok"}}]}),
    )
    assert provider.base_url == "https://operator.example/"


@pytest.mark.parametrize(
    "key",
    ["base_url", "baseUrl", "endpoint", "api_base", "api_key", "credential", "headers"],
)
def test_real_provider_rejects_untrusted_manifest_runtime_field(key):
    with pytest.raises(ProviderConfigurationError, match="operator-owned"):
        OpenAIProvider(
            {"provider": "openai", "name": "test-model", "config": {key: "https://attacker.example/"}},
            {"OPENAI_API_KEY": "test-only-key"},
            client=SimpleNamespace(invoke=lambda *_args, **_kwargs: None),
        )


@pytest.mark.parametrize(
    "error,expected",
    [
        (type("RateLimitError", (Exception,), {"status_code": 429})("limited"), ProviderTransientError),
        (TimeoutError("slow"), ProviderTimeoutError),
        (type("AuthenticationError", (Exception,), {"status_code": 401})("bad key"), ProviderPermanentError),
    ],
)
def test_real_provider_classifies_sdk_failures_without_sdk_imports(error, expected):
    class FailingClient:
        def invoke(self, *_args, **_kwargs):
            raise error

    provider = OpenAIProvider(
        {"provider": "openai", "name": "test-model"},
        {"OPENAI_API_KEY": "test-only-key"},
        client=FailingClient(),
    )
    with pytest.raises(expected):
        provider.invoke([{"role": "user", "content": "hello"}])


def test_real_provider_redacts_credential_from_sdk_failure():
    api_key = "test-only-super-secret-key"

    class FailingClient:
        def invoke(self, *_args, **_kwargs):
            error = RuntimeError(f"connection rejected for token {api_key}")
            error.status_code = 503
            raise error

    provider = OpenAIProvider(
        {"provider": "openai", "name": "test-model"},
        {"OPENAI_API_KEY": api_key},
        client=FailingClient(),
    )
    with pytest.raises(ProviderTransientError) as exc_info:
        provider.invoke([{"role": "user", "content": "hello"}])
    assert api_key not in str(exc_info.value)
    assert "[REDACTED]" in str(exc_info.value)


def test_real_provider_rejects_malformed_response_as_permanent():
    provider = OpenAIProvider(
        {"provider": "openai", "name": "test-model"},
        {"OPENAI_API_KEY": "test-only-key"},
        client=SimpleNamespace(invoke=lambda *_args, **_kwargs: {"choices": []}),
    )
    with pytest.raises(ProviderResponseError):
        provider.invoke([{"role": "user", "content": "hello"}])


@pytest.mark.parametrize(
    "messages,match",
    [
        ([{"role": "unknown", "content": "hello"}], "unsupported model message role"),
        ([{"role": "tool", "content": "result"}], "tool_call_id"),
    ],
)
def test_real_provider_rejects_ambiguous_message_roles(messages, match):
    provider = OpenAIProvider(
        {"provider": "openai", "name": "test-model"},
        {"OPENAI_API_KEY": "test-only-key"},
        client=SimpleNamespace(invoke=lambda *_args, **_kwargs: None),
    )
    with pytest.raises(ProviderConfigurationError, match=match):
        provider.invoke(messages)


def test_openai_sdk_constructor_receives_frozen_model_options(monkeypatch):
    captured = {}

    class FakeChatOpenAI:
        def __init__(self, **kwargs):
            captured.update(kwargs)

        def invoke(self, _messages, **_kwargs):
            return {"choices": [{"message": {"content": "ok"}}]}

    monkeypatch.setitem(sys.modules, "langchain_openai", SimpleNamespace(ChatOpenAI=FakeChatOpenAI))
    monkeypatch.setenv("OPENAI_BASE_URL", "https://operator.example/")
    provider = OpenAIProvider(
        {
            "provider": "openai",
            "name": "test-model",
            "config": {
                "temperature": 0.25,
                "max_tokens": 123,
                "timeoutSeconds": 9,
                "maxRetries": 1,
            },
        },
        {"OPENAI_API_KEY": "test-only-key"},
    )
    provider.create_llm()

    assert captured == {
        "model": "test-model",
        "api_key": "test-only-key",
        "temperature": 0.25,
        "max_tokens": 123,
        "timeout": 9.0,
        "max_retries": 1,
        "base_url": "https://operator.example/",
    }


def test_anthropic_sdk_constructor_receives_frozen_model_options(monkeypatch):
    captured = {}

    class FakeChatAnthropic:
        def __init__(self, **kwargs):
            captured.update(kwargs)

        def invoke(self, _messages, **_kwargs):
            return {"choices": [{"message": {"content": "ok"}}]}

    monkeypatch.setitem(
        sys.modules,
        "langchain_anthropic",
        SimpleNamespace(ChatAnthropic=FakeChatAnthropic),
    )
    monkeypatch.setenv("ANTHROPIC_BASE_URL", "https://operator.example/")
    provider = AnthropicProvider(
        {
            "provider": "anthropic",
            "name": "claude-test",
            "config": {
                "temperature": 0.1,
                "maxTokens": 321,
                "timeout": 8,
                "max_retries": 0,
            },
        },
        {"ANTHROPIC_API_KEY": "test-only-key"},
    )
    provider.create_llm()

    assert captured == {
        "model": "claude-test",
        "api_key": "test-only-key",
        "temperature": 0.1,
        "max_tokens": 321,
        "timeout": 8.0,
        "max_retries": 0,
        "base_url": "https://operator.example/",
    }


@pytest.mark.parametrize(
    "config,match",
    [
        ({"temperature": "not-a-number"}, "temperature"),
        ({"max_tokens": 0}, "max_tokens"),
        ({"timeoutSeconds": 0}, "timeout"),
        ({"maxRetries": -1}, "max_retries"),
    ],
)
def test_real_provider_rejects_invalid_frozen_model_options(config, match):
    with pytest.raises(ProviderConfigurationError, match=match):
        OpenAIProvider(
            {"provider": "openai", "name": "test-model", "config": config},
            {"OPENAI_API_KEY": "test-only-key"},
            client=SimpleNamespace(invoke=lambda *_args, **_kwargs: None),
        )


@pytest.mark.parametrize(
    "error,category",
    [
        (ProviderTransientError("retry"), runner_pb2.FAILURE_CATEGORY_TRANSIENT),
        (ProviderTimeoutError("timeout"), runner_pb2.FAILURE_CATEGORY_TIMEOUT),
        (ProviderPermanentError("invalid"), runner_pb2.FAILURE_CATEGORY_PERMANENT),
        (ProviderResponseError("malformed"), runner_pb2.FAILURE_CATEGORY_PERMANENT),
        (ProviderError("unknown"), runner_pb2.FAILURE_CATEGORY_INTERNAL),
    ],
)
def test_worker_maps_provider_failure_categories(error, category):
    assert provider_failure_category(error) == category


def test_legacy_provider_facade_imports_without_langchain(monkeypatch):
    # Importing the compatibility module must not make fixture-only execution
    # depend on langchain-core being installed.
    monkeypatch.setitem(sys.modules, "langchain_core", None)
    with pytest.raises(ProviderUnavailableError):
        create_llm({"provider": "fixture", "name": "offline"}, {})


def test_executor_includes_prompt_and_declared_fixture_metadata(tmp_path: Path):
    executor = LangGraphExecutor(str(tmp_path / "cas"), str(tmp_path / "artifacts"))
    messages = executor._build_messages(
        {
            "prompt": "Summarize the CSV",
            "fixtures": [{"path": "fixtures/sales.csv", "sha256": "sha256:file"}],
        },
        None,
    )
    assert messages[0]["role"] == "system"
    assert messages[1]["role"] == "user"
    assert "Summarize the CSV" in messages[1]["content"]
    assert "fixtures/sales.csv" in messages[1]["content"]


def test_executor_fixture_run_writes_hashed_artifacts(tmp_path: Path):
    executor = LangGraphExecutor(str(tmp_path / "cas"), str(tmp_path / "artifacts"))
    context = executor.execute(
        trial_id="trial-1",
        case_input={"prompt": "hello", "fixtures": []},
        skill_hash=None,
        model_config={"provider": "fixture", "name": "offline"},
        tool_policy={},
        environment={},
    )
    assert (tmp_path / "artifacts" / "result.txt").exists()
    assert (tmp_path / "artifacts" / "trace.json").exists()
    assert context.artifacts
    assert all(item.content_hash.startswith("sha256:") for item in context.artifacts)


def test_trial_executor_aggregates_normalized_provider_usage(monkeypatch, tmp_path: Path):
    class FakeProvider:
        def invoke(self, _messages, tools=None, params=None):
            assert tools is None and params is None
            return {
                "id": "provider-response",
                "model": "fake-live-model",
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": "ok"},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {"input_tokens": 11, "output_tokens": 7, "total_tokens": 18},
            }

    def provider_factory(model_config, *args, **kwargs):
        assert model_config["provider"] == "openai"
        assert args == () and kwargs == {}
        return FakeProvider()

    monkeypatch.setattr("langgraph_worker.executor.create_provider", provider_factory)
    spec = SimpleNamespace(
        case_input={"prompt": "hello", "fixtures": []},
        skill_hash="",
        model={"provider": "openai", "name": "fake-live-model"},
        tool_policy={},
        environment={"OPENAI_API_KEY": "must-not-be-read-from-descriptor"},
    )
    result = TrialExecutor(tmp_path / "cas", tmp_path / "artifacts", "trial-usage").execute(spec)
    assert result["usage"] == {"input_tokens": 11, "output_tokens": 7, "tool_calls": 0}


def test_complete_trial_submits_and_hashes_normalized_usage(tmp_path: Path):
    class FakeStub:
        request = None

        def CompleteTrial(self, request):
            self.request = request
            return SimpleNamespace(status=runner_pb2.COMPLETION_STATUS_COMMITTED)

    worker = LangGraphWorker(artifacts_root=tmp_path)
    worker.stub = FakeStub()
    worker.session_token = "session"
    trial = {
        "trial_id": "trial-usage",
        "logical_trial_id": "logical-usage",
        "experiment_id": "experiment-usage",
        "attempt_no": 1,
        "lease_token": "lease",
        "lease_generation": 1,
        "request_hash": "sha256:request",
        "execution_hash": "sha256:execution",
    }
    result = {
        "outcome": "SUCCEEDED",
        "final_sequence": 2,
        "duration_ms": 25,
        "trace": {"llm_calls": []},
        "artifacts": [],
        "usage": {"input_tokens": 11, "output_tokens": 7, "tool_calls": 2},
    }

    assert worker.complete_trial(trial, result)
    request = worker.stub.request
    assert request.usage.input_tokens == 11
    assert request.usage.output_tokens == 7
    assert request.usage.elapsed_ms == 25
    assert request.usage.tool_calls == 2
    assert json.loads(request.outcome_manifest_json)["usage"] == result["usage"]


def test_m0_fixture_provider_writes_declared_summary(tmp_path: Path):
    written = {}

    def write_artifact(name, content, mime_type):
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        written[name] = mime_type

    result = execute_csv_case(
        case_id="csv-explicit-001",
        case_input={
            "id": "csv-explicit-001",
            "fixtures": [
                {
                    "path": "fixtures/sales.csv",
                    "sha256": "sha256:79dfd4eaa7b72d9e558c68665b285ffd19f73349694f638ac7313d892a74c1d0",
                }
            ],
        },
        fixture_root=Path("evals/csv-analysis"),
        log_step=lambda *_: None,
        write_artifact=write_artifact,
    )
    assert result and "output/summary.json" in written
    summary = json.loads((tmp_path / "output/summary.json").read_text())
    assert summary["total"] == {"order_count": 6, "quantity": 13, "revenue": 205.0}


def test_executor_rejects_artifact_path_escape(tmp_path: Path):
    from langgraph_worker.executor import ExecutionContext

    with pytest.raises(ValueError):
        ExecutionContext("trial", tmp_path / "artifacts").write_artifact("../escape", b"x")


def test_m0_fixture_provider_writes_expected_outputs(tmp_path: Path):
    suite_root = Path(__file__).parents[3] / "evals" / "csv-analysis"
    fixture_path = suite_root / "fixtures" / "sales.csv"
    fixture_hash = "sha256:" + hashlib.sha256(fixture_path.read_bytes()).hexdigest()
    written = []

    def write_artifact(name, content, _mime):
        path = tmp_path / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        written.append(name)

    result = execute_csv_case(
        case_id="csv-explicit-001",
        case_input={
            "id": "csv-explicit-001",
            "fixtures": [{"path": "fixtures/sales.csv", "sha256": fixture_hash}],
        },
        fixture_root=suite_root,
        log_step=lambda _name, _data: None,
        write_artifact=write_artifact,
    )
    assert result and written == ["output/summary.json"]
    actual = json.loads((tmp_path / "output/summary.json").read_text(encoding="utf-8"))
    expected = json.loads((suite_root / "expected/monthly-summary.json").read_text(encoding="utf-8"))
    assert actual == expected


def test_m0_fixture_provider_rejects_tampered_declared_hash(tmp_path: Path):
    with pytest.raises(FixtureExecutionError, match="hash mismatch"):
        execute_csv_case(
            case_id="csv-explicit-001",
            case_input={
                "id": "csv-explicit-001",
                "fixtures": [{"path": "fixtures/sales.csv", "sha256": "sha256:" + "0" * 64}],
            },
            fixture_root=Path(__file__).parents[3] / "evals" / "csv-analysis",
            log_step=lambda _name, _data: None,
            write_artifact=lambda *_args: None,
        )


def test_autonomous_discovery_uses_prompt_relevance():
    assert LangGraphExecutor._should_discover_skill({"prompt": "请检查销售 CSV"})
    assert not LangGraphExecutor._should_discover_skill({"prompt": "解释 Go channel"})


def test_skill_loader_rejects_missing_skill(tmp_path: Path):
    loader = SkillLoader(tmp_path)
    with pytest.raises(SkillNotFoundError):
        loader.load_skill("sha256:" + "0" * 64)
