"""Mock and recorded providers for deterministic testing."""

from typing import Dict, Any, List, Optional
import json
import hashlib
from datetime import datetime


class MockProvider:
    """Mock LLM provider for testing without API keys."""

    def __init__(self, model_name: str = "mock-model"):
        self.model_name = model_name
        self.call_count = 0

    def complete(self, messages: List[Dict[str, Any]], **kwargs) -> Dict[str, Any]:
        """
        Generate deterministic mock completion.

        Args:
            messages: Chat messages
            **kwargs: Additional parameters (temperature, max_tokens, etc.)

        Returns:
            Mock completion response
        """
        self.call_count += 1

        # Generate deterministic response based on input hash
        input_text = json.dumps(messages, sort_keys=True)
        input_hash = hashlib.sha256(input_text.encode()).hexdigest()[:8]

        return {
            "id": f"mock-{input_hash}",
            "model": self.model_name,
            "choices": [
                {
                    "index": 0,
                    "message": {
                        "role": "assistant",
                        "content": f"Mock response #{self.call_count} for input hash {input_hash}. This is a deterministic test response.",
                    },
                    "finish_reason": "stop",
                }
            ],
            "usage": {
                "prompt_tokens": len(input_text) // 4,
                "completion_tokens": 20,
                "total_tokens": len(input_text) // 4 + 20,
            },
            "created": int(datetime.utcnow().timestamp()),
        }


class RecordedProvider:
    """Recorded provider that replays pre-recorded responses."""

    def __init__(self, recordings_path: str, model_name: str = "recorded-model"):
        """
        Initialize with path to recordings file.

        Args:
            recordings_path: Path to JSON file with recorded responses
            model_name: Model name for identification
        """
        self.model_name = model_name
        self.recordings_path = recordings_path
        self.recordings: Dict[str, Any] = {}
        self.call_count = 0

        try:
            with open(recordings_path, "r") as f:
                self.recordings = json.load(f)
        except FileNotFoundError:
            # If no recordings file, fall back to mock behavior
            self.recordings = {}

    def complete(self, messages: List[Dict[str, Any]], **kwargs) -> Dict[str, Any]:
        """
        Replay recorded completion or fall back to mock.

        Args:
            messages: Chat messages
            **kwargs: Additional parameters

        Returns:
            Recorded or mock completion response
        """
        self.call_count += 1

        # Generate key from messages
        input_text = json.dumps(messages, sort_keys=True)
        input_hash = hashlib.sha256(input_text.encode()).hexdigest()

        # Try to find recording
        if input_hash in self.recordings:
            return self.recordings[input_hash]

        # Fall back to mock
        return MockProvider(self.model_name).complete(messages, **kwargs)


def create_provider(provider_config: Dict[str, Any]) -> Any:
    """
    Factory to create provider based on config.

    Args:
        provider_config: Provider configuration with type and params

    Returns:
        Provider instance (MockProvider or RecordedProvider)
    """
    provider_type = provider_config.get("provider", "mock").lower()
    model_name = provider_config.get("name", "default")

    if provider_type == "mock":
        return MockProvider(model_name)
    elif provider_type == "recorded":
        recordings_path = provider_config.get("recordings_path", "recordings.json")
        return RecordedProvider(recordings_path, model_name)
    else:
        raise ValueError(f"Unsupported provider type: {provider_type}")
