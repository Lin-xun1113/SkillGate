from pathlib import Path

import pytest

from langgraph_worker.provider import OpenAIProvider, ProviderConfigurationError
from langgraph_worker.secret_source import SecretCode, SecretError, read_secret


def test_read_secret_prefers_file_and_trims_newline():
    value = read_secret(
        "TEST_TOKEN",
        {"TEST_TOKEN": "direct", "TEST_TOKEN_FILE": "/run/secrets/token"},
        read_file=lambda path: "file-value\n" if path == "/run/secrets/token" else "",
        environ={},
    )
    assert value == "file-value"


@pytest.mark.parametrize(
    "environment,read_file,code",
    [
        ({}, None, SecretCode.MISSING),
        ({"TEST_TOKEN": "  "}, None, SecretCode.EMPTY),
        ({"TEST_TOKEN_FILE": "  "}, None, SecretCode.EMPTY),
        ({"TEST_TOKEN_FILE": "/missing"}, lambda _path: (_ for _ in ()).throw(OSError()), SecretCode.UNREADABLE),
        ({"TEST_TOKEN_FILE": "/empty"}, lambda _path: "\n", SecretCode.EMPTY),
    ],
)
def test_read_secret_has_stable_typed_errors(environment, read_file, code):
    with pytest.raises(SecretError) as exc_info:
        read_secret("TEST_TOKEN", environment, environ={}, read_file=read_file)
    assert exc_info.value.code is code
    assert "sentinel" not in str(exc_info.value)


def test_provider_accepts_docker_secret_file(tmp_path: Path):
    path = tmp_path / "openai-key"
    path.write_text("file-provider-key\n", encoding="utf-8")
    provider = OpenAIProvider(
        {"provider": "openai", "name": "test-model"},
        {"OPENAI_API_KEY_FILE": str(path)},
        client=type("Client", (), {"invoke": lambda self, *_args, **_kwargs: {"choices": [{"message": {"content": "ok"}}]}})(),
    )
    assert provider.api_key == "file-provider-key"


def test_provider_secret_file_errors_do_not_expose_path(tmp_path: Path):
    missing = tmp_path / "missing-key"
    with pytest.raises(ProviderConfigurationError) as exc_info:
        OpenAIProvider(
            {"provider": "openai", "name": "test-model"},
            {"OPENAI_API_KEY_FILE": str(missing)},
            client=object(),
        )
    assert str(missing) not in str(exc_info.value)
