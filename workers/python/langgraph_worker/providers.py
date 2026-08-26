"""LLM provider abstraction layer."""

import os
from typing import Any, Dict

from langchain_core.language_models import BaseChatModel


class MockProvider:
    """Mock provider for testing without real API calls."""

    def __init__(self, model_config: Dict[str, Any]):
        self.model_config = model_config

    def create_llm(self) -> BaseChatModel:
        """Create a mock LLM that returns canned responses."""
        from langchain_core.language_models.fake_chat_models import FakeMessagesListChatModel
        from langchain_core.messages import AIMessage

        # Return predefined response
        canned_response = AIMessage(content="Mock response: Task completed successfully.")

        return FakeMessagesListChatModel(responses=[canned_response])


class RecordedProvider:
    """Recorded provider that replays saved interactions."""

    def __init__(self, model_config: Dict[str, Any], recordings_dir: str = "/workspace/recordings"):
        self.model_config = model_config
        self.recordings_dir = recordings_dir

    def create_llm(self) -> BaseChatModel:
        """Create LLM that replays recorded responses."""
        # TODO: Implement VCR-style replay
        # For now, fall back to mock
        return MockProvider(self.model_config).create_llm()


class AnthropicProvider:
    """Anthropic Claude provider."""

    def __init__(self, model_config: Dict[str, Any], environment: Dict[str, Any]):
        self.model_config = model_config
        self.api_key = environment.get("ANTHROPIC_API_KEY", os.getenv("ANTHROPIC_API_KEY"))

        if not self.api_key:
            raise ValueError("ANTHROPIC_API_KEY not provided in environment")

    def create_llm(self) -> BaseChatModel:
        """Create Anthropic LLM."""
        from langchain_anthropic import ChatAnthropic

        return ChatAnthropic(
            model=self.model_config.get("name", "claude-3-5-sonnet-20241022"),
            anthropic_api_key=self.api_key,
            temperature=self.model_config.get("temperature", 0.0),
            max_tokens=self.model_config.get("max_tokens", 4096),
        )


class OpenAIProvider:
    """OpenAI provider."""

    def __init__(self, model_config: Dict[str, Any], environment: Dict[str, Any]):
        self.model_config = model_config
        self.api_key = environment.get("OPENAI_API_KEY", os.getenv("OPENAI_API_KEY"))

        if not self.api_key:
            raise ValueError("OPENAI_API_KEY not provided in environment")

    def create_llm(self) -> BaseChatModel:
        """Create OpenAI LLM."""
        from langchain_openai import ChatOpenAI

        return ChatOpenAI(
            model=self.model_config.get("name", "gpt-4"),
            openai_api_key=self.api_key,
            temperature=self.model_config.get("temperature", 0.0),
            max_tokens=self.model_config.get("max_tokens", 4096),
        )


def create_llm(model_config: Dict[str, Any], environment: Dict[str, Any]) -> BaseChatModel:
    """Factory function to create LLM based on provider."""
    provider = model_config.get("provider", "mock")

    if provider == "mock":
        return MockProvider(model_config).create_llm()
    elif provider == "recorded":
        return RecordedProvider(model_config).create_llm()
    elif provider == "anthropic":
        return AnthropicProvider(model_config, environment).create_llm()
    elif provider == "openai":
        return OpenAIProvider(model_config, environment).create_llm()
    else:
        raise ValueError(f"Unknown provider: {provider}")
