"""Model provider adapters used by the LangGraph worker.

Fixture providers stay local and deterministic. Real providers are opt-in and
fail closed when credentials or their optional SDK are not available; they are
never silently replaced with a fixture response.

The public provider contract is :meth:`CompletionProvider.invoke`. The
``complete`` method is kept as a compatibility adapter for older callers.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
import re
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Protocol

from .canonical import canonical_json_bytes
from .secret_source import SecretError, read_secret


ProviderResult = Dict[str, Any]


class ProviderError(RuntimeError):
    """Base class for provider failures."""


class ProviderConfigurationError(ProviderError, ValueError):
    """The immutable model configuration is invalid or incomplete."""


class ProviderUnavailableError(ProviderError, ValueError):
    """An explicitly selected provider cannot be initialized locally."""


class ProviderPermanentError(ProviderError, ValueError):
    """A provider request cannot succeed by retrying the same execution."""


class ProviderTransientError(ProviderError):
    """A provider failure which the scheduler may retry."""


class ProviderTimeoutError(ProviderTransientError, TimeoutError):
    """A provider request exceeded its configured timeout."""


class ProviderResponseError(ProviderPermanentError):
    """The provider returned a response outside the normalized contract."""


class RecordedResponseNotFoundError(ProviderError, LookupError):
    """A recorded provider has no response for the requested input."""


def _redact_sensitive_text(value: Any, secrets: tuple[str, ...] = ()) -> str:
    text = str(value)
    known_secrets = list(secrets)
    for env_name, env_value in os.environ.items():
        normalized_name = env_name.upper()
        if (
            normalized_name.endswith(("_API_KEY", "_ACCESS_TOKEN", "_SECRET", "_PASSWORD"))
            or normalized_name in {"OPENAI_API_KEY", "ANTHROPIC_API_KEY"}
        ) and env_value:
            known_secrets.append(env_value)
    for secret in known_secrets:
        if secret:
            text = text.replace(secret, "[REDACTED]")
    # SDK messages occasionally include a bearer credential even when the
    # configured key is wrapped or truncated. Do not copy obvious bearer/key
    # tokens into worker logs or FailTrial evidence.
    text = re.sub(r"(?i)\bBearer\s+[^\s,;]+", "Bearer [REDACTED]", text)
    def mask_assignment(match: re.Match[str]) -> str:
        original = match.group(0)
        separator = ":" if ":" in original else "="
        return original.split(separator, 1)[0] + separator + "[REDACTED]"

    text = re.sub(
        r"(?i)\b(?:api[_-]?key|token|secret|password|authorization)\s*[:=]\s*[^\s,;]+",
        mask_assignment,
        text,
    )
    return text


def redact_sensitive(value: Any, secrets: tuple[str, ...] = ()) -> Any:
    """Return a detached value with credential-bearing fields and text removed."""
    sensitive_keys = {
        "api_key", "apikey", "authorization", "credential", "credentials",
        "password", "secret", "token", "access_token", "id_token", "base_url",
        "baseurl", "endpoint", "api_base", "api_endpoint", "headers", "http_headers",
    }
    if isinstance(value, Mapping):
        sanitized: Dict[str, Any] = {}
        for key, child in value.items():
            key_text = str(key)
            normalized = key_text.strip().lower().replace("-", "_")
            if normalized in sensitive_keys:
                sanitized[key_text] = "[REDACTED]"
            else:
                sanitized[key_text] = redact_sensitive(child, secrets)
        return sanitized
    if isinstance(value, list):
        return [redact_sensitive(child, secrets) for child in value]
    if isinstance(value, tuple):
        return [redact_sensitive(child, secrets) for child in value]
    if isinstance(value, str):
        return _redact_sensitive_text(value, secrets)
    return value


class CompletionProvider(Protocol):
    """Provider boundary shared by fixture and real model adapters."""

    def invoke(
        self,
        messages: List[Dict[str, Any]],
        tools: Optional[List[Dict[str, Any]]] = None,
        params: Optional[Mapping[str, Any]] = None,
    ) -> ProviderResult:
        """Run one completion and return a normalized completion dictionary."""

    def complete(self, messages: List[Dict[str, Any]], **kwargs: Any) -> ProviderResult:
        """Backward-compatible alias for :meth:`invoke`."""


class MockProvider:
    """Deterministic provider for fixture and offline execution."""

    def __init__(self, model_name: str = "mock-model"):
        self.model_name = model_name or "mock-model"
        self.call_count = 0

    def invoke(
        self,
        messages: List[Dict[str, Any]],
        tools: Optional[List[Dict[str, Any]]] = None,
        params: Optional[Mapping[str, Any]] = None,
    ) -> ProviderResult:
        del tools, params
        self.call_count += 1
        input_bytes = canonical_json_bytes(messages)
        input_hash = hashlib.sha256(input_bytes).hexdigest()[:8]
        input_text = input_bytes.decode("utf-8")
        prompt_tokens = max(0, len(input_text) // 4)
        return {
            "id": f"mock-{input_hash}",
            "model": self.model_name,
            "choices": [
                {
                    "index": 0,
                    "message": {
                        "role": "assistant",
                        "content": (
                            f"Mock response for input hash {input_hash}. "
                            "This is a deterministic test response."
                        ),
                    },
                    "finish_reason": "stop",
                }
            ],
            "usage": {
                "prompt_tokens": prompt_tokens,
                "input_tokens": prompt_tokens,
                "completion_tokens": 20,
                "output_tokens": 20,
                "total_tokens": prompt_tokens + 20,
            },
            # Fixture responses must be reproducible; omit wall-clock values.
            "created": 0,
        }

    def complete(self, messages: List[Dict[str, Any]], **kwargs: Any) -> ProviderResult:
        return self.invoke(messages, params=kwargs)

    def create_llm(self) -> Any:
        """Compatibility helper for the legacy LangChain facade."""
        try:
            from langchain_core.language_models.fake_chat_models import (
                FakeMessagesListChatModel,
            )
            from langchain_core.messages import AIMessage
        except ImportError as exc:
            raise ProviderUnavailableError(
                "create_llm requires optional dependency langchain-core; "
                "use invoke() for the worker runtime"
            ) from exc
        return FakeMessagesListChatModel(
            responses=[AIMessage(content="Mock response: Task completed successfully.")]
        )


class RecordedProvider:
    """Replay responses from a content-keyed JSON recording."""

    def __init__(
        self,
        recordings_path: str | os.PathLike[str],
        model_name: str = "recorded-model",
    ):
        self.model_name = model_name or "recorded-model"
        self.recordings_path = str(recordings_path)
        self.recordings: Dict[str, Any] = self._load_recordings(Path(recordings_path))
        self.call_count = 0

    @staticmethod
    def _load_recordings(path: Path) -> Dict[str, Any]:
        if not path.is_file():
            raise ProviderConfigurationError(
                f"recorded provider recordings file does not exist: {path}"
            )
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ProviderConfigurationError(
                f"cannot read recorded provider recordings {path}: {exc}"
            ) from exc
        if not isinstance(value, dict):
            raise ProviderConfigurationError("recordings file must contain a JSON object")
        return value

    @staticmethod
    def _input_hash(messages: List[Dict[str, Any]]) -> str:
        return hashlib.sha256(canonical_json_bytes(messages)).hexdigest()

    def invoke(
        self,
        messages: List[Dict[str, Any]],
        tools: Optional[List[Dict[str, Any]]] = None,
        params: Optional[Mapping[str, Any]] = None,
    ) -> ProviderResult:
        del tools, params
        self.call_count += 1
        input_hash = self._input_hash(messages)
        response = self.recordings.get(input_hash)
        if response is None:
            raise RecordedResponseNotFoundError(
                f"no recorded response for input hash {input_hash} in {self.recordings_path}"
            )
        if not isinstance(response, dict):
            raise ProviderConfigurationError(
                f"recorded response for {input_hash} must be a JSON object"
            )
        # Return a normalized, detached value so retries cannot mutate the
        # recording map and recorded/real providers share one contract.
        detached = json.loads(json.dumps(response, ensure_ascii=False))
        return _normalize_completion_response(detached, "recorded", self.model_name)

    def complete(self, messages: List[Dict[str, Any]], **kwargs: Any) -> ProviderResult:
        return self.invoke(messages, params=kwargs)

    def create_llm(self) -> Any:
        """Compatibility helper retained for pre-gRPC LangChain callers."""
        try:
            from langchain_core.language_models.fake_chat_models import (
                FakeMessagesListChatModel,
            )
            from langchain_core.messages import AIMessage
        except ImportError as exc:
            raise ProviderUnavailableError(
                "create_llm requires optional dependency langchain-core; "
                "use invoke() for the worker runtime"
            ) from exc
        return FakeMessagesListChatModel(
            responses=[
                AIMessage(
                    content="Recorded provider responses are available through invoke()."
                )
            ]
        )


def _config_value(config: Mapping[str, Any], key: str, default: Any = None) -> Any:
    """Read an option from flat, ``config`` or legacy ``params`` forms."""
    if key in config:
        return config[key]
    for section_name in ("config", "params"):
        nested = config.get(section_name)
        if isinstance(nested, Mapping) and key in nested:
            return nested[key]
    return default


# Keep this list in lock-step with internal/manifest/provider_config.go.  The
# wire payload is untrusted, so accepting an SDK-specific option here would
# silently change the execution identity or bypass the scheduler budget.
_PROVIDER_TYPES = frozenset({"fixture", "mock", "recorded", "openai", "anthropic"})
_MODEL_FIELDS = frozenset(
    {"provider", "type", "name", "model", "model_id", "config", "params", "recordings_path"}
)
_CONFIG_ALIASES = {
    "temperature": "temperature",
    "max_tokens": "max_tokens",
    "maxTokens": "max_tokens",
    "timeout_seconds": "timeout_seconds",
    "timeoutSeconds": "timeout_seconds",
    "timeout": "timeout_seconds",
    "max_retries": "max_retries",
    "maxRetries": "max_retries",
}
_FLAT_CONFIG_FIELDS = frozenset(
    {"temperature", "max_tokens", "maxTokens", "timeout_seconds", "timeoutSeconds", "max_retries", "maxRetries"}
)
_MAX_MODEL_NAME_LENGTH = 256
_MAX_TEMPERATURE = 2.0
_MAX_TOKENS = 1_000_000
_MAX_TIMEOUT_SECONDS = 600.0
_MAX_RETRIES = 30


def _validate_model_config(config: Mapping[str, Any], *, require_name: bool = False) -> None:
    """Validate the provider model envelope shared by all Worker providers.

    ``params`` and camelCase spellings are compatibility input only.  They are
    constrained to the same frozen fields as ``config`` and cannot be mixed
    with a duplicate canonical field.
    """
    if not isinstance(config, Mapping):
        raise ProviderConfigurationError("model provider configuration must be an object")
    _assert_no_untrusted_runtime_fields(config)

    for key in config:
        if key not in _MODEL_FIELDS:
            raise ProviderConfigurationError(f"model.{key} is not an allowed field")

    provider_value = config.get("provider")
    type_value = config.get("type")
    if provider_value is not None and not isinstance(provider_value, str):
        raise ProviderConfigurationError("model.provider must be a string")
    if type_value is not None and not isinstance(type_value, str):
        raise ProviderConfigurationError("model.type must be a string")
    if provider_value is not None and type_value is not None:
        if provider_value.strip().lower() != type_value.strip().lower():
            raise ProviderConfigurationError("model.provider and model.type disagree")
    if provider_value is None and type_value is None:
        raise ProviderConfigurationError("model.provider must be a string")
    provider = str(provider_value if provider_value is not None else type_value).strip().lower()
    if provider not in _PROVIDER_TYPES:
        raise ProviderConfigurationError(f"unsupported provider type: {provider}")

    names = []
    for key in ("name", "model_id", "model"):
        if key not in config:
            continue
        value = config[key]
        if not isinstance(value, str) or not value.strip():
            raise ProviderConfigurationError(f"model.{key} must be a non-empty string")
        value = value.strip()
        if len(value) > _MAX_MODEL_NAME_LENGTH:
            raise ProviderConfigurationError(f"model.{key} exceeds {_MAX_MODEL_NAME_LENGTH} characters")
        names.append((key, value))
    if require_name and not names:
        raise ProviderConfigurationError("model.name must be a non-empty string")
    if names and any(value != names[0][1] for _, value in names[1:]):
        raise ProviderConfigurationError("model name compatibility fields disagree")

    if "recordings_path" in config and (
        not isinstance(config["recordings_path"], str) or not config["recordings_path"].strip()
    ):
        raise ProviderConfigurationError("model.recordings_path must be a non-empty string")

    seen = {}
    for section_name in ("config", "params"):
        if section_name not in config:
            continue
        section = config[section_name]
        if not isinstance(section, Mapping):
            raise ProviderConfigurationError(f"model.{section_name} must be an object")
        for key, value in section.items():
            canonical = _CONFIG_ALIASES.get(key)
            field_path = f"model.{section_name}.{key}"
            if canonical is None:
                raise ProviderConfigurationError(f"{field_path} is not an allowed field")
            if canonical in seen:
                raise ProviderConfigurationError(
                    f"{field_path} duplicates {seen[canonical]}"
                )
            seen[canonical] = field_path
            _validate_provider_option(canonical, value, field_path)

    for key in _FLAT_CONFIG_FIELDS:
        if key in config:
            raise ProviderConfigurationError(f"model.{key} must be nested under model.config")


def _validate_provider_option(name: str, value: Any, path: str) -> None:
    if name == "temperature":
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ProviderConfigurationError(f"{path} must be a finite number in 0..2")
        number = float(value)
        if not math.isfinite(number) or number < 0 or number > _MAX_TEMPERATURE:
            raise ProviderConfigurationError(f"{path} must be a finite number in 0..2")
        return
    if name == "max_tokens":
        if isinstance(value, bool) or not isinstance(value, int) or not 1 <= value <= _MAX_TOKENS:
            raise ProviderConfigurationError(f"{path} (max_tokens) must be an integer in 1..1000000")
        return
    if name == "timeout_seconds":
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise ProviderConfigurationError(f"{path} must be a finite number in (0,600]")
        number = float(value)
        if not math.isfinite(number) or number <= 0 or number > _MAX_TIMEOUT_SECONDS:
            raise ProviderConfigurationError(f"{path} must be a finite number in (0,600]")
        return
    if name == "max_retries":
        if isinstance(value, bool) or not isinstance(value, int) or not 0 <= value <= _MAX_RETRIES:
            raise ProviderConfigurationError(f"{path} (max_retries) must be an integer in 0..30")


def validate_model_config(config: Mapping[str, Any], *, require_name: bool = False) -> None:
    """Public validation hook used by the Worker before any execution path."""
    _validate_model_config(config, require_name=require_name)


def _first_config_value(
    config: Mapping[str, Any], keys: tuple[str, ...], default: Any = None
) -> Any:
    for key in keys:
        value = _config_value(config, key, None)
        if value is not None:
            return value
    return default


def _model_name(config: Mapping[str, Any], default: str) -> str:
    value = _first_config_value(config, ("name", "model_id", "model"), default)
    if not isinstance(value, str) or not value.strip():
        raise ProviderConfigurationError("model name must be a non-empty string")
    return value.strip()


def _positive_timeout(config: Mapping[str, Any]) -> Optional[float]:
    value = _first_config_value(
        config, ("timeout", "timeout_seconds", "timeoutSeconds"), 60.0
    )
    if value is None:
        return None
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ProviderConfigurationError("provider timeout must be a positive number")
    timeout = float(value)
    if not math.isfinite(timeout) or timeout <= 0 or timeout > _MAX_TIMEOUT_SECONDS:
        raise ProviderConfigurationError("provider timeout must be a positive number")
    return timeout


def _max_retries(config: Mapping[str, Any]) -> int:
    value = _first_config_value(config, ("max_retries", "maxRetries"), 0)
    if isinstance(value, bool) or not isinstance(value, int):
        raise ProviderConfigurationError(
            "provider max_retries must be a non-negative integer"
        )
    retries = value
    if retries < 0 or retries > _MAX_RETRIES:
        raise ProviderConfigurationError(
            "provider max_retries must be a non-negative integer in 0..30"
        )
    return retries


def _temperature(config: Mapping[str, Any]) -> float:
    value = _config_value(config, "temperature", 0.0)
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ProviderConfigurationError("model temperature must be a finite number")
    temperature = float(value)
    if not math.isfinite(temperature) or temperature < 0 or temperature > _MAX_TEMPERATURE:
        raise ProviderConfigurationError("model temperature must be a finite number")
    return temperature


def _max_tokens(config: Mapping[str, Any]) -> int:
    value = _first_config_value(config, ("max_tokens", "maxTokens"), 4096)
    if isinstance(value, bool) or not isinstance(value, int):
        raise ProviderConfigurationError("model max_tokens must be a positive integer")
    max_tokens = value
    if max_tokens < 1 or max_tokens > _MAX_TOKENS:
        raise ProviderConfigurationError("model max_tokens must be a positive integer")
    return max_tokens


def _operator_base_url(provider: str, value: Optional[str]) -> Optional[str]:
    """Resolve an endpoint from operator state, never from the Manifest.

    A model/skill/Manifest is untrusted input and must not be able to redirect
    credentials to an attacker-controlled endpoint. ``value`` is reserved for
    an explicitly injected test or host-side configuration value.
    """
    if value is None:
        value = os.getenv(f"{provider.upper()}_BASE_URL")
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        raise ProviderConfigurationError("provider base_url must be a non-empty string")
    return value.strip()


def _assert_no_untrusted_runtime_fields(config: Mapping[str, Any]) -> None:
    forbidden = {
        "api_base", "apibase", "api_endpoint", "apiendpoint", "api_key", "apikey",
        "base_url", "baseurl", "credential", "credentials", "endpoint", "headers",
        "http_headers", "httpheaders",
        "password", "secret", "token",
    }
    def walk(value: Any, path: str) -> None:
        if isinstance(value, Mapping):
            for key, child in value.items():
                normalized = str(key).strip().lower().replace("-", "_")
                child_path = f"{path}.{key}" if path else str(key)
                if normalized in forbidden:
                    raise ProviderConfigurationError(
                        f"model.{child_path} is forbidden; provider endpoints are operator-owned"
                    )
                walk(child, child_path)
        elif isinstance(value, list):
            for index, child in enumerate(value):
                walk(child, f"{path}[{index}]")

    walk(config, "")


def _credential(provider: str, environment: Mapping[str, Any]) -> str:
    env_name = f"{provider.upper()}_API_KEY"
    try:
        return read_secret(env_name, environment)
    except SecretError as exc:
        if exc.code.value == "SECRET_MISSING":
            detail = f"{env_name} is required"
        elif exc.code.value == "SECRET_EMPTY":
            detail = f"{env_name} is empty"
        elif exc.code.value == "SECRET_UNREADABLE":
            detail = f"{env_name} file source is unreadable"
        else:
            detail = f"{env_name} is invalid"
        raise ProviderConfigurationError(
            f"{detail} for provider {provider}; real provider calls are disabled "
            "without an explicit credential"
        ) from None


def _status_code(error: BaseException) -> Optional[int]:
    """Best-effort extraction without importing optional provider SDKs."""
    candidates: List[Any] = [
        getattr(error, "status_code", None),
        getattr(error, "status", None),
    ]
    response = getattr(error, "response", None)
    if response is not None:
        candidates.extend(
            [getattr(response, "status_code", None), getattr(response, "status", None)]
        )
    for value in candidates:
        if isinstance(value, bool):
            continue
        try:
            code = int(value)
        except (TypeError, ValueError):
            continue
        if 100 <= code <= 599:
            return code
    return None


def _classify_provider_exception(
    error: Exception, secrets: tuple[str, ...] = ()
) -> ProviderError:
    """Map common SDK/network failures to scheduler-facing stable classes."""
    if isinstance(error, ProviderError):
        # Injected clients can raise a ProviderError constructed from an SDK
        # message.  Preserve its category while replacing any credential that
        # may have been embedded in the message.
        safe = _redact_sensitive_text(error, secrets)
        if safe == str(error):
            return error
        if isinstance(error, ProviderTimeoutError):
            return ProviderTimeoutError(safe)
        if isinstance(error, ProviderTransientError):
            return ProviderTransientError(safe)
        if isinstance(error, ProviderResponseError):
            return ProviderResponseError(safe)
        if isinstance(error, ProviderPermanentError):
            return ProviderPermanentError(safe)
        if isinstance(error, ProviderConfigurationError):
            return ProviderConfigurationError(safe)
        if isinstance(error, ProviderUnavailableError):
            return ProviderUnavailableError(safe)
        if isinstance(error, RecordedResponseNotFoundError):
            return RecordedResponseNotFoundError(safe)
        return ProviderError(safe)

    safe_error = _redact_sensitive_text(error, secrets)
    status = _status_code(error)
    if isinstance(error, TimeoutError) or "timeout" in type(error).__name__.lower():
        return ProviderTimeoutError(safe_error or "provider request timed out")
    if status in {408, 409, 425, 429} or (status is not None and 500 <= status <= 599):
        return ProviderTransientError(
            f"provider returned transient HTTP status {status}: {safe_error}"
        )
    if status in {400, 401, 403, 404, 405, 406, 410, 413, 415, 422}:
        return ProviderPermanentError(
            f"provider rejected the request with HTTP status {status}: {safe_error}"
        )

    name = type(error).__name__.lower()
    text = f"{name} {safe_error}".lower()
    transient_markers = (
        "connection",
        "connecterror",
        "serviceunavailable",
        "internalserver",
        "ratelimit",
        "rate_limit",
        "overloaded",
        "temporar",
        "retry",
        "unavailable",
    )
    if isinstance(error, (ConnectionError, OSError)) or any(
        marker in text for marker in transient_markers
    ):
        return ProviderTransientError(safe_error or "provider request failed temporarily")

    permanent_markers = (
        "authentication",
        "permission",
        "badrequest",
        "invalidrequest",
        "validation",
        "notfound",
        "unprocessable",
        "contentfilter",
        "contextlength",
    )
    if any(marker in text for marker in permanent_markers):
        return ProviderPermanentError(safe_error or "provider rejected the request")

    return ProviderError(safe_error or f"{type(error).__name__} from provider")


def _as_int(value: Any) -> Optional[int]:
    if isinstance(value, bool) or value is None:
        return None
    try:
        number = int(value)
    except (TypeError, ValueError, OverflowError):
        return None
    return number if number >= 0 else None


def _mapping_value(value: Any, key: str, default: Any = None) -> Any:
    if isinstance(value, Mapping):
        return value.get(key, default)
    return getattr(value, key, default)


def _normalize_usage(response: Any, metadata: Mapping[str, Any]) -> Dict[str, int]:
    usage_sources: List[Any] = []
    if isinstance(response, Mapping):
        usage_sources.append(response.get("usage", {}))
    usage_sources.extend(
        [
            getattr(response, "usage_metadata", None),
            metadata.get("token_usage"),
            metadata.get("usage"),
            getattr(response, "usage", None),
        ]
    )
    usage: Dict[str, int] = {}

    def first(*keys: str) -> Optional[int]:
        for source in usage_sources:
            if source is None:
                continue
            for key in keys:
                raw = _mapping_value(source, key, None)
                if raw is None:
                    continue
                # Provider SDKs expose token counts as integers.  Silently
                # truncating strings/floats would make cost evidence unsafe.
                if isinstance(raw, bool) or not isinstance(raw, int) or raw < 0:
                    raise ProviderResponseError(
                        f"provider usage field {key} must be a non-negative integer"
                    )
                return raw
        return None

    prompt = first("prompt_tokens", "input_tokens")
    completion = first("completion_tokens", "output_tokens")
    total = first("total_tokens")
    cached = first("cached_tokens", "cache_read_input_tokens")
    if prompt is not None:
        usage["prompt_tokens"] = prompt
        usage["input_tokens"] = prompt
    if completion is not None:
        usage["completion_tokens"] = completion
        usage["output_tokens"] = completion
    if total is None and prompt is not None and completion is not None:
        total = prompt + completion
    if total is not None:
        usage["total_tokens"] = total
    if cached is not None:
        usage["cached_tokens"] = cached
    return usage


def _normalize_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if content is None:
        return ""
    if isinstance(content, list):
        text_parts: List[str] = []
        for block in content:
            block_type = _mapping_value(block, "type")
            text = _mapping_value(block, "text")
            if block_type in (None, "text") and isinstance(text, str):
                text_parts.append(text)
            elif isinstance(block, str):
                text_parts.append(block)
        return "".join(text_parts)
    if isinstance(content, Mapping) and content.get("type") in (None, "text"):
        text = content.get("text")
        return text if isinstance(text, str) else ""
    # Non-text blocks (for example images) are intentionally omitted from the
    # normalized text channel; callers can still retain structured tool calls.
    return ""


def _normalize_tool_calls(value: Any, provider: str) -> List[Dict[str, Any]]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ProviderResponseError(f"provider {provider} returned malformed tool_calls")
    calls: List[Dict[str, Any]] = []
    for index, raw in enumerate(value):
        if not isinstance(raw, Mapping):
            # SDK objects (e.g. pydantic models) are converted through their
            # public attributes without retaining arbitrary private state.
            raw = {
                key: getattr(raw, key)
                for key in ("id", "type", "function", "name", "arguments", "input")
                if hasattr(raw, key)
            }
        if not raw:
            raise ProviderResponseError(f"provider {provider} returned malformed tool_call {index}")
        call = dict(raw)
        function = call.get("function")
        if function is None and call.get("name") is not None:
            arguments = call.get("arguments", call.get("input", {}))
            if not isinstance(arguments, str):
                arguments = json.dumps(arguments, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            function = {"name": str(call.get("name")), "arguments": arguments}
            call = {"id": call.get("id", f"tool-call-{index}"), "type": "function", "function": function}
        else:
            if not isinstance(function, Mapping):
                function = {
                    key: getattr(function, key)
                    for key in ("name", "arguments", "input")
                    if hasattr(function, key)
                }
            if not isinstance(function, Mapping):
                raise ProviderResponseError(f"provider {provider} returned malformed tool_call {index}")
            function = dict(function)
            if "name" not in function or not isinstance(function.get("name"), str) or not function["name"].strip():
                raise ProviderResponseError(f"provider {provider} returned malformed tool_call {index}")
            arguments = function.get("arguments", "{}")
            if not isinstance(arguments, str):
                arguments = json.dumps(arguments, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            function["arguments"] = arguments
            call["function"] = function
        if not isinstance(call.get("id", f"tool-call-{index}"), str):
            call["id"] = f"tool-call-{index}"
        call.setdefault("type", "function")
        calls.append(call)
    return calls


def _anthropic_content_parts(content: Any, provider: str) -> tuple[str, List[Dict[str, Any]]]:
    """Convert Anthropic content blocks to normalized text and Tool Calls."""
    if not isinstance(content, list):
        return _normalize_content(content), []
    text_parts: List[str] = []
    tool_calls: List[Dict[str, Any]] = []
    for index, block in enumerate(content):
        block_type = _mapping_value(block, "type")
        if block_type == "text":
            text = _mapping_value(block, "text")
            if isinstance(text, str):
                text_parts.append(text)
            continue
        if block_type == "tool_use":
            name = _mapping_value(block, "name")
            if not isinstance(name, str) or not name.strip():
                raise ProviderResponseError(f"provider {provider} returned malformed tool_use block")
            arguments = _mapping_value(block, "input", {})
            if not isinstance(arguments, str):
                arguments = json.dumps(arguments, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            tool_calls.append(
                {
                    "id": str(_mapping_value(block, "id", f"tool-call-{index}")),
                    "type": "function",
                    "function": {"name": name, "arguments": arguments},
                }
            )
    return "".join(text_parts), tool_calls


def _normalize_completion_response(
    response: Any, provider: str, configured_model: str
) -> ProviderResult:
    """Normalize LangChain/OpenAI/Anthropic response variants."""
    if response is None:
        raise ProviderResponseError(f"provider {provider} returned an empty response")
    metadata_value = (
        response.get("response_metadata", {})
        if isinstance(response, Mapping)
        else getattr(response, "response_metadata", {})
    )
    metadata: Mapping[str, Any] = (
        metadata_value if isinstance(metadata_value, Mapping) else {}
    )

    has_choices = isinstance(response, Mapping) and "choices" in response
    raw_choices = response.get("choices") if isinstance(response, Mapping) else getattr(response, "choices", None)
    if has_choices and not isinstance(raw_choices, list):
        raise ProviderResponseError(f"provider {provider} returned malformed choices")
    normalized_choices: List[Dict[str, Any]] = []
    if isinstance(raw_choices, list):
        for index, choice in enumerate(raw_choices):
            message = _mapping_value(choice, "message", None)
            if message is None:
                message = {
                    "role": _mapping_value(choice, "role", "assistant"),
                    "content": _mapping_value(choice, "content", _mapping_value(choice, "text", "")),
                    "tool_calls": _mapping_value(choice, "tool_calls", None),
                }
            elif not isinstance(message, Mapping):
                message = {
                    "role": _mapping_value(message, "role", "assistant"),
                    "content": _mapping_value(message, "content", ""),
                    "tool_calls": _mapping_value(message, "tool_calls", None),
                }
            if not isinstance(message, Mapping):
                raise ProviderResponseError(f"provider {provider} returned malformed completion message")
            raw_tool_calls = message.get("tool_calls")
            tool_calls = _normalize_tool_calls(raw_tool_calls, provider) if raw_tool_calls is not None else []
            normalized_message: Dict[str, Any] = {
                "role": str(message.get("role", "assistant")),
                "content": _normalize_content(message.get("content", "")),
            }
            if tool_calls:
                normalized_message["tool_calls"] = tool_calls
            if not normalized_message["content"] and not tool_calls:
                raise ProviderResponseError(
                    f"provider {provider} returned an empty completion choice"
                )
            choice_index = _as_int(_mapping_value(choice, "index"))
            normalized_choices.append(
                {
                    "index": index if choice_index is None else choice_index,
                    "message": normalized_message,
                    "finish_reason": _mapping_value(choice, "finish_reason", "stop")
                    or "stop",
                }
            )
    else:
        content = _mapping_value(response, "content", "")
        normalized_text, anthropic_tools = _anthropic_content_parts(content, provider)
        normalized_message = {"role": "assistant", "content": normalized_text}
        raw_tool_calls = _mapping_value(response, "tool_calls", None)
        tool_calls = _normalize_tool_calls(raw_tool_calls, provider) if raw_tool_calls is not None else anthropic_tools
        if tool_calls:
            normalized_message["tool_calls"] = tool_calls
        if not normalized_message["content"] and not tool_calls:
            raise ProviderResponseError(f"provider {provider} returned an empty response")
        finish_reason = (
            _mapping_value(response, "finish_reason", None)
            or _mapping_value(response, "stop_reason", None)
            or metadata.get("finish_reason")
            or metadata.get("stop_reason")
            or ("tool_calls" if tool_calls else "stop")
        )
        normalized_choices.append(
            {"index": 0, "message": normalized_message, "finish_reason": finish_reason}
        )

    if not normalized_choices:
        raise ProviderResponseError(f"provider {provider} returned no completion choices")

    response_id = (
        _mapping_value(response, "id", None)
        or metadata.get("id")
        or metadata.get("response_id")
        or f"{provider}-completion"
    )
    model = (
        _mapping_value(response, "model", None)
        or metadata.get("model_name")
        or metadata.get("model")
        or configured_model
    )
    return {
        "id": str(response_id),
        "model": str(model),
        "choices": normalized_choices,
        "usage": _normalize_usage(response, metadata),
    }


class LangChainProvider:
    """Lazy LangChain adapter for a configured real provider."""

    def __init__(
        self,
        provider: str,
        model_config: Mapping[str, Any],
        environment: Optional[Mapping[str, Any]] = None,
        *,
        client: Any = None,
        base_url: Optional[str] = None,
    ):
        if not isinstance(model_config, Mapping):
            raise ProviderConfigurationError("model provider configuration must be an object")
        if environment is not None and not isinstance(environment, Mapping):
            raise ProviderConfigurationError("provider environment must be an object")
        self.provider = provider
        self.model_config = dict(model_config)
        self.environment = dict(environment or {})
        _validate_model_config(self.model_config, require_name=True)
        selected_provider = str(
            self.model_config.get("provider", self.model_config.get("type", ""))
        ).strip().lower()
        if selected_provider != self.provider:
            raise ProviderConfigurationError(
                f"model provider {selected_provider!r} does not match adapter {self.provider!r}"
            )
        self.model_name = _model_name(
            self.model_config,
            "gpt-4o-mini" if provider == "openai" else "claude-3-5-sonnet-20241022",
        )
        self.timeout = _positive_timeout(self.model_config)
        self.max_retries = _max_retries(self.model_config)
        self.temperature = _temperature(self.model_config)
        self.max_tokens = _max_tokens(self.model_config)
        self.base_url = _operator_base_url(provider, base_url)
        self.api_key = _credential(provider, self.environment)
        # ``client`` is intentionally injectable for offline contract tests and
        # for deployments that wrap the LangChain client with a gateway.
        self._client = client
        self._llm: Any = None

    def _create_llm(self) -> Any:
        if self._llm is not None:
            return self._llm
        if self._client is not None:
            candidate = self._client
            if callable(candidate) and not hasattr(candidate, "invoke"):
                candidate = candidate()
            if not hasattr(candidate, "invoke"):
                raise ProviderUnavailableError(
                    "injected provider client must expose invoke(messages, **kwargs)"
                )
            self._llm = candidate
            return self._llm

        options: Dict[str, Any] = {
            "timeout": self.timeout,
            # Scheduler owns retries. A non-zero value is opt-in and explicit
            # in the immutable model configuration.
            "max_retries": self.max_retries,
        }
        if self.base_url:
            options["base_url"] = self.base_url
        try:
            if self.provider == "openai":
                from langchain_openai import ChatOpenAI

                self._llm = ChatOpenAI(
                    model=self.model_name,
                    api_key=self.api_key,
                    temperature=self.temperature,
                    max_tokens=self.max_tokens,
                    **options,
                )
            elif self.provider == "anthropic":
                from langchain_anthropic import ChatAnthropic

                self._llm = ChatAnthropic(
                    model=self.model_name,
                    api_key=self.api_key,
                    temperature=self.temperature,
                    max_tokens=self.max_tokens,
                    **options,
                )
            else:  # pragma: no cover - factory prevents this path
                raise ProviderConfigurationError(f"unsupported real provider: {self.provider}")
        except ProviderError:
            raise
        except ImportError as exc:
            package = "langchain-openai" if self.provider == "openai" else "langchain-anthropic"
            raise ProviderUnavailableError(
                f"provider {self.provider} requires optional dependency {package}"
            ) from exc
        except Exception as exc:
            safe_error = _redact_sensitive_text(exc, (self.api_key,))
            raise ProviderUnavailableError(
                f"failed to initialize provider {self.provider}: "
                f"{type(exc).__name__}: {safe_error}"
            ) from None
        return self._llm

    # Retain the public method used by the original ``providers.py`` facade.
    def create_llm(self) -> Any:
        return self._create_llm()

    def _message_objects(self, messages: List[Dict[str, Any]]) -> List[Any]:
        if not isinstance(messages, list):
            raise ProviderConfigurationError("messages must be a list")

        normalized: List[Dict[str, Any]] = []
        for item in messages:
            if not isinstance(item, Mapping):
                raise ProviderConfigurationError("model message must be an object")
            role = str(item.get("role", "user")).strip().lower()
            if role not in {"system", "developer", "assistant", "tool", "user"}:
                raise ProviderConfigurationError(f"unsupported model message role: {role}")
            if role == "tool":
                tool_call_id = item.get("tool_call_id")
                if not isinstance(tool_call_id, str) or not tool_call_id.strip():
                    raise ProviderConfigurationError(
                        "tool messages require a non-empty tool_call_id"
                    )
            normalized.append(dict(item))

        # Injected clients are an explicit wire-level testing/adapter seam.
        # Keep their input stable regardless of whether LangChain happens to be
        # installed in the current Python environment.
        if self._client is not None:
            return normalized

        try:
            from langchain_core.messages import (
                AIMessage,
                HumanMessage,
                SystemMessage,
                ToolMessage,
            )
        except ImportError as exc:
            raise ProviderUnavailableError(
                "real providers require optional dependency langchain-core"
            ) from exc

        result: List[Any] = []
        for item in normalized:
            role = str(item.get("role", "user")).strip().lower()
            content = item.get("content", "")
            if role in ("system", "developer"):
                result.append(SystemMessage(content=content))
            elif role == "assistant":
                result.append(AIMessage(content=content))
            elif role == "tool":
                tool_call_id = item.get("tool_call_id")
                if not isinstance(tool_call_id, str) or not tool_call_id.strip():
                    raise ProviderConfigurationError(
                        "tool messages require a non-empty tool_call_id"
                    )
                result.append(ToolMessage(content=content, tool_call_id=tool_call_id))
            elif role == "user":
                result.append(HumanMessage(content=content))
        return result

    def invoke(
        self,
        messages: List[Dict[str, Any]],
        tools: Optional[List[Dict[str, Any]]] = None,
        params: Optional[Mapping[str, Any]] = None,
    ) -> ProviderResult:
        if params is None:
            invoke_params: Dict[str, Any] = {}
        elif isinstance(params, Mapping):
            invoke_params = dict(params)
        else:
            raise ProviderConfigurationError("provider invocation params must be an object")
        if tools is not None and not isinstance(tools, list):
            raise ProviderConfigurationError("provider tools must be a list")

        llm = self._create_llm()
        invoke_target = llm
        if tools:
            bind_tools = getattr(llm, "bind_tools", None)
            if callable(bind_tools):
                try:
                    invoke_target = bind_tools(tools)
                except Exception as exc:
                    raise ProviderConfigurationError(
                        f"provider {self.provider} rejected tool definitions: {exc}"
                    ) from exc
            else:
                # A simple injected client may accept tools directly. Real
                # LangChain clients expose bind_tools, so no SDK-specific shape
                # leaks into this contract.
                invoke_params.setdefault("tools", tools)

        try:
            response = invoke_target.invoke(self._message_objects(messages), **invoke_params)
        except Exception as exc:
            # Do not retain the SDK exception as ``__cause__``: many SDKs put
            # request headers (including the API key) in their repr/traceback.
            raise _classify_provider_exception(exc, (self.api_key,)) from None
        return _normalize_completion_response(response, self.provider, self.model_name)

    def complete(self, messages: List[Dict[str, Any]], **kwargs: Any) -> ProviderResult:
        return self.invoke(messages, params=kwargs)


class OpenAIProvider(LangChainProvider):
    def __init__(
        self,
        model_config: Mapping[str, Any],
        environment: Optional[Mapping[str, Any]] = None,
        *,
        client: Any = None,
        base_url: Optional[str] = None,
    ):
        super().__init__("openai", model_config, environment, client=client, base_url=base_url)


class AnthropicProvider(LangChainProvider):
    def __init__(
        self,
        model_config: Mapping[str, Any],
        environment: Optional[Mapping[str, Any]] = None,
        *,
        client: Any = None,
        base_url: Optional[str] = None,
    ):
        super().__init__("anthropic", model_config, environment, client=client, base_url=base_url)


def create_provider(
    provider_config: Mapping[str, Any] | None,
    environment: Optional[Mapping[str, Any]] = None,
    *,
    client: Any = None,
    base_url: Optional[str] = None,
) -> CompletionProvider:
    """Create the provider selected by an execution projection."""
    if provider_config is None:
        provider_config = {}
    if not isinstance(provider_config, Mapping):
        raise ProviderConfigurationError("model provider configuration must be an object")
    # Validate before selecting a concrete adapter so fixture, recorded and
    # real providers enforce the same untrusted model envelope.
    _validate_model_config(provider_config, require_name=True)
    provider_value = provider_config.get("provider", provider_config.get("type", "mock"))
    provider_type = str(provider_value).strip().lower()
    model_name = _model_name(provider_config, "default")
    if provider_type in ("mock", "fixture"):
        return MockProvider(model_name)
    if provider_type == "recorded":
        recordings_path = _first_config_value(provider_config, ("recordings_path",), None)
        if not recordings_path:
            raise ProviderConfigurationError("recorded provider requires model.recordings_path")
        return RecordedProvider(recordings_path, model_name)
    if provider_type == "openai":
        return OpenAIProvider(provider_config, environment, client=client, base_url=base_url)
    if provider_type == "anthropic":
        return AnthropicProvider(provider_config, environment, client=client, base_url=base_url)
    raise ProviderConfigurationError(f"unsupported provider type: {provider_type}")


__all__ = [
    "AnthropicProvider",
    "CompletionProvider",
    "LangChainProvider",
    "MockProvider",
    "OpenAIProvider",
    "ProviderConfigurationError",
    "ProviderError",
    "ProviderPermanentError",
    "ProviderResponseError",
    "ProviderResult",
    "ProviderTimeoutError",
    "ProviderTransientError",
    "ProviderUnavailableError",
    "RecordedProvider",
    "RecordedResponseNotFoundError",
    "SecretError",
    "create_provider",
    "redact_sensitive",
    "validate_model_config",
]
