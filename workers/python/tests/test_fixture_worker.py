import json
import pytest
from fixture_worker.canonical import canonical_json_bytes, sha256_canonical
from fixture_worker.worker import FixtureWorker, MODE_NORMAL, MODE_FAIL, MODE_TIMEOUT, MODE_CANCEL_AWARE


def test_canonical_json_and_hashing():
    # Different key ordering must produce identical bytes and sha256
    d1 = {"b": 2, "a": 1, "nested": {"y": "yes", "x": "no"}}
    d2 = {"nested": {"x": "no", "y": "yes"}, "a": 1, "b": 2}

    b1 = canonical_json_bytes(d1)
    b2 = canonical_json_bytes(d2)
    assert b1 == b2
    assert b1 == b'{"a":1,"b":2,"nested":{"x":"no","y":"yes"}}'

    h1 = sha256_canonical(d1)
    h2 = sha256_canonical(d2)
    assert h1 == h2
    assert h1.startswith("sha256:")


def test_worker_initialization():
    w = FixtureWorker(server_addr="127.0.0.1:50051", worker_id="test-w1")
    assert w.worker_id == "test-w1"
    assert w.protocol_version == "runner.v1"
    assert w.session_token is None


def test_simulation_modes_are_wired():
    # The four modes required by the M3 spec must be constructible and preserved.
    for mode in (MODE_NORMAL, MODE_FAIL, MODE_TIMEOUT, MODE_CANCEL_AWARE):
        w = FixtureWorker(mode=mode, timeout_seconds=2.0)
        assert w.mode == mode
    w = FixtureWorker(mode=MODE_TIMEOUT, timeout_seconds=2.0)
    assert w.timeout_seconds == 2.0
