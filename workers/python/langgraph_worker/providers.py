"""Backward-compatible provider facade.

The worker execution path uses :mod:`langgraph_worker.provider`, whose
OpenAI-compatible ``complete`` contract is dependency-free for fixtures. This
module remains for callers that used the earlier LangChain-oriented API, but it
does not import LangChain at module import time.
"""

from __future__ import annotations

from typing import Any, Mapping, Optional

from .provider import (
    AnthropicProvider,
    MockProvider,
    OpenAIProvider,
    ProviderConfigurationError,
    ProviderError,
    ProviderPermanentError,
    ProviderResponseError,
    ProviderTimeoutError,
    ProviderTransientError,
    ProviderUnavailableError,
    RecordedProvider,
    create_provider,
)


def create_llm(
    model_config: Mapping[str, Any] | None,
    environment: Optional[Mapping[str, Any]] = None,
) -> Any:
    """Create a LangChain chat model for legacy integrations.

    Fixture/recorded providers use the worker's ``complete`` contract. A
    LangChain LLM compatibility object for those modes requires optional
    ``langchain-core`` and is loaded lazily; production execution should call
    :func:`create_provider`.
    """
    config = dict(model_config or {})
    provider = create_provider(config, environment)
    return provider.create_llm()


__all__ = [
    "AnthropicProvider",
    "MockProvider",
    "OpenAIProvider",
    "ProviderConfigurationError",
    "ProviderError",
    "ProviderPermanentError",
    "ProviderResponseError",
    "ProviderTimeoutError",
    "ProviderTransientError",
    "ProviderUnavailableError",
    "RecordedProvider",
    "create_llm",
    "create_provider",
]
