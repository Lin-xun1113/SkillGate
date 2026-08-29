"""Dependency-free OpenAI/Anthropic Provider contract vectors.

The injected clients below deliberately implement only ``invoke``.  This keeps
the contract tests independent from optional SDKs and guarantees no public API
is contacted by the default test command.
"""

from types import SimpleNamespace

import pytest

from langgraph_worker.provider import (
    AnthropicProvider,
    OpenAIProvider,
    ProviderConfigurationError,
    ProviderError,
    ProviderPermanentError,
    ProviderResponseError,
    ProviderTimeoutError,
    ProviderTransientError,
    redact_sensitive,
)
from langgraph_worker.executor import LangGraphExecutor
from langgraph_worker.worker import provider_failure_category
from runner.v1 import runner_pb2


class RecordingClient:
    def __init__(self, response=None, error=None):
        self.response = response
        self.error = error
        self.calls = []

    def invoke(self, messages, **kwargs):
        self.calls.append((messages, kwargs))
        if self.error is not None:
            raise self.error
        return self.response


@pytest.mark.parametrize("provider_cls,provider_name", [(OpenAIProvider, "openai"), (AnthropicProvider, "anthropic")])
def test_injected_provider_forwards_messages_tools_and_normalizes_text(provider_cls, provider_name):
    client = RecordingClient(
        SimpleNamespace(
            id="response-1",
            model="actual-model",
            content=[
                {"type": "text", "text": "hello"},
                {"type": "text", "text": " world"},
                {"type": "image", "source": {"type": "base64", "data": "ignored"}},
            ],
            usage={"input_tokens": 4, "output_tokens": 3},
            response_metadata={"stop_reason": "end_turn"},
        )
    )
    provider = provider_cls(
        {
            "provider": provider_name,
            "name": "configured-model",
            "config": {"temperature": 0.2, "max_tokens": 128, "timeout_seconds": 12, "max_retries": 0},
        },
        {f"{provider_name.upper()}_API_KEY": "test-only-key"},
        client=client,
    )
    tools = [{"type": "function", "function": {"name": "lookup", "parameters": {}}}]
    result = provider.invoke(
        [{"role": "system", "content": "system"}, {"role": "user", "content": "question"}],
        tools=tools,
        params={"stop": ["done"]},
    )

    assert result["id"] == "response-1"
    assert result["model"] == "actual-model"
    assert result["choices"][0]["message"]["content"] == "hello world"
    assert result["choices"][0]["finish_reason"] == "end_turn"
    assert result["usage"] == {
        "prompt_tokens": 4,
        "input_tokens": 4,
        "completion_tokens": 3,
        "output_tokens": 3,
        "total_tokens": 7,
    }
    assert client.calls[0][0][0]["role"] == "system"
    assert client.calls[0][1]["stop"] == ["done"]
    assert client.calls[0][1]["tools"] == tools


def test_openai_tool_call_is_structured_and_complete_is_compatible():
    response = {
        "id": "openai-tool",
        "model": "gpt-actual",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": None,
                    "tool_calls": [
                        {
                            "id": "call-1",
                            "type": "function",
                            "function": {"name": "lookup", "arguments": {"q": "x"}},
                        }
                    ],
                },
                "finish_reason": "tool_calls",
            }
        ],
        "usage": {"prompt_tokens": 2, "completion_tokens": 1},
    }
    client = RecordingClient(response)
    provider = OpenAIProvider(
        {"provider": "openai", "name": "configured-model"},
        {"OPENAI_API_KEY": "test-only-key"},
        client=client,
    )
    result = provider.complete([{"role": "user", "content": "lookup"}], stop=["x"])
    tool_call = result["choices"][0]["message"]["tool_calls"][0]
    assert tool_call["function"] == {"name": "lookup", "arguments": '{"q":"x"}'}
    assert client.calls[0][1]["stop"] == ["x"]


def test_anthropic_tool_use_block_is_structured():
    client = RecordingClient(
        {
            "id": "anthropic-tool",
            "model": "claude-actual",
            "content": [
                {"type": "text", "text": "I will look that up."},
                {"type": "tool_use", "id": "tool-1", "name": "lookup", "input": {"q": "x"}},
            ],
            "stop_reason": "tool_use",
            "usage": {"input_tokens": 6, "output_tokens": 4},
        }
    )
    provider = AnthropicProvider(
        {"provider": "anthropic", "name": "claude-configured"},
        {"ANTHROPIC_API_KEY": "test-only-key"},
        client=client,
    )
    result = provider.invoke([{"role": "user", "content": "lookup"}])
    assert result["model"] == "claude-actual"
    assert result["choices"][0]["finish_reason"] == "tool_use"
    assert result["choices"][0]["message"]["content"] == "I will look that up."
    assert result["choices"][0]["message"]["tool_calls"][0]["function"] == {
        "name": "lookup",
        "arguments": '{"q":"x"}',
    }


@pytest.mark.parametrize(
    "error,expected",
    [
        (type("RateLimitError", (Exception,), {"status_code": 429})("429"), ProviderTransientError),
        (type("AuthenticationError", (Exception,), {"status_code": 401})("401"), ProviderPermanentError),
        (type("ServerError", (Exception,), {"status_code": 500})("500"), ProviderTransientError),
        (TimeoutError("provider timeout"), ProviderTimeoutError),
    ],
)
def test_injected_provider_status_mapping(error, expected):
    provider = AnthropicProvider(
        {"provider": "anthropic", "name": "claude-test"},
        {"ANTHROPIC_API_KEY": "test-only-key"},
        client=RecordingClient(error=error),
    )
    with pytest.raises(expected):
        provider.invoke([{"role": "user", "content": "hello"}])


@pytest.mark.parametrize("payload", [None, {"choices": []}, {"choices": [{"message": {"content": []}}]}])
def test_malformed_provider_response_is_permanent(payload):
    provider = OpenAIProvider(
        {"provider": "openai", "name": "gpt-test"},
        {"OPENAI_API_KEY": "test-only-key"},
        client=RecordingClient(payload),
    )
    with pytest.raises(ProviderResponseError):
        provider.invoke([{"role": "user", "content": "hello"}])


def test_provider_errors_map_to_stable_runner_categories():
    assert provider_failure_category(ProviderTransientError("retry")) == runner_pb2.FAILURE_CATEGORY_TRANSIENT
    assert provider_failure_category(ProviderTimeoutError("timeout")) == runner_pb2.FAILURE_CATEGORY_TIMEOUT
    assert provider_failure_category(ProviderPermanentError("bad request")) == runner_pb2.FAILURE_CATEGORY_PERMANENT
    assert provider_failure_category(ProviderResponseError("malformed")) == runner_pb2.FAILURE_CATEGORY_PERMANENT
    assert provider_failure_category(ProviderError("bug")) == runner_pb2.FAILURE_CATEGORY_INTERNAL
    assert provider_failure_category(TimeoutError("raw timeout")) == runner_pb2.FAILURE_CATEGORY_TIMEOUT


def test_redaction_is_recursive_and_preserves_usage_fields():
    key = "test-only-provider-key"
    value = {
        "headers": {"Authorization": f"Bearer {key}"},
        "message": f"token={key}",
        "usage": {"input_tokens": 3, "output_tokens": 2},
    }
    redacted = redact_sensitive(value, (key,))
    assert key not in str(redacted)
    assert redacted["headers"] == "[REDACTED]"
    assert redacted["usage"] == {"input_tokens": 3, "output_tokens": 2}


def test_trace_and_result_artifacts_do_not_persist_provider_key(monkeypatch, tmp_path):
    key = "test-only-provider-key"
    monkeypatch.setenv("OPENAI_API_KEY", key)

    class KeyEchoProvider:
        def invoke(self, _messages, **_kwargs):
            return {
                "id": "key-echo",
                "model": "fake-model",
                "choices": [{
                    "message": {"role": "assistant", "content": f"answer {key}"},
                    "finish_reason": "stop",
                }],
                "usage": {"input_tokens": 1, "output_tokens": 1},
            }

    monkeypatch.setattr(
        "langgraph_worker.executor.create_provider",
        lambda _config: KeyEchoProvider(),
    )
    context = LangGraphExecutor(str(tmp_path / "cas"), str(tmp_path / "artifacts")).execute(
        trial_id="trace-redaction",
        case_input={"prompt": f"do not repeat {key}", "fixtures": []},
        skill_hash=None,
        model_config={"provider": "openai", "name": "fake-model"},
        tool_policy={},
        environment={},
    )
    trace_text = (tmp_path / "artifacts" / "trace.json").read_text(encoding="utf-8")
    result_text = (tmp_path / "artifacts" / "result.txt").read_text(encoding="utf-8")
    assert key not in trace_text
    assert key not in result_text
    assert "[REDACTED]" in trace_text
    assert context.trace.llm_calls[0]["response"]["choices"][0]["message"]["content"].endswith("[REDACTED]")


def test_fail_trial_redacts_provider_key_before_runner_submission(monkeypatch, tmp_path):
    key = "test-only-provider-key"
    monkeypatch.setenv("OPENAI_API_KEY", key)

    class Stub:
        request = None

        def FailTrial(self, request):
            self.request = request
            return SimpleNamespace(status=runner_pb2.FAIL_TRIAL_STATUS_TERMINAL_FAILED)

    from langgraph_worker.worker import LangGraphWorker

    worker = LangGraphWorker(artifacts_root=tmp_path)
    worker.stub = Stub()
    worker.session_token = "session"
    assert worker.fail_trial(
        {
            "trial_id": "trial-1",
            "logical_trial_id": "logical-1",
            "attempt_no": 1,
            "lease_token": "lease",
            "lease_generation": 1,
            "request_hash": "sha256:request",
        },
        f"provider rejected Authorization: Bearer {key}",
        runner_pb2.FAILURE_CATEGORY_PERMANENT,
    )
    assert key not in worker.stub.request.error_message
    assert key not in worker.stub.request.outcome_manifest_json
    assert "[REDACTED]" in worker.stub.request.error_message


@pytest.mark.parametrize(
    "config",
    [
        {"unknown": 1},
        {"temperature": "0"},
        {"temperature": -0.1},
        {"temperature": 2.1},
        {"max_tokens": 0},
        {"max_tokens": 1.0},
        {"timeout_seconds": 601},
        {"max_retries": 31},
        {"max_tokens": 1, "maxTokens": 1},
    ],
)
def test_model_config_allowlist_and_bounds_are_fail_closed(config):
    with pytest.raises(ProviderConfigurationError):
        OpenAIProvider(
            {"provider": "openai", "name": "gpt-test", "config": config},
            {"OPENAI_API_KEY": "test-only-key"},
            client=RecordingClient({"choices": [{"message": {"content": "ok"}}]}),
        )


def test_fixture_execution_cannot_bypass_model_config_validation(tmp_path):
    executor = LangGraphExecutor(str(tmp_path / "cas"), str(tmp_path / "artifacts"))
    with pytest.raises(ProviderConfigurationError):
        executor.execute(
            trial_id="invalid-fixture-config",
            case_input={"prompt": "hello", "fixtures": []},
            skill_hash=None,
            model_config={"provider": "fixture", "name": "offline", "config": {"top_p": 0.5}},
            tool_policy={},
            environment={},
        )
