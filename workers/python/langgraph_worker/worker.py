"""gRPC LangGraph trial worker.

The worker deliberately stays a thin protocol client: the Go control plane
projects execution content into ``trial_request_json`` and the worker only
verifies identities, executes the configured provider, and submits evidence.
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import sys
import threading
import time
from pathlib import Path
from typing import Any, Dict, Optional

try:  # Keep hashing/execution helpers importable in minimal offline tooling.
    import grpc
except ImportError:  # pragma: no cover - production image installs grpcio
    grpc = None  # type: ignore[assignment]

sys.path.insert(0, str(Path(__file__).parent.parent / "gen"))
try:
    from runner.v1 import runner_pb2, runner_pb2_grpc
except (ImportError, RuntimeError):  # pragma: no cover - optional protocol deps
    runner_pb2 = None  # type: ignore[assignment]
    runner_pb2_grpc = None  # type: ignore[assignment]

from .canonical import (
    canonical_json_bytes,
    request_hash,
    result_idempotency_key,
    sha256_canonical,
)
from .executor import TrialExecutor
from .models import ExecutionSpec
from .provider import (
    ProviderConfigurationError,
    ProviderPermanentError,
    ProviderResponseError,
    ProviderTimeoutError,
    ProviderTransientError,
    ProviderUnavailableError,
    RecordedResponseNotFoundError,
    redact_sensitive,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("langgraph_worker")


class WorkerDependencyError(RuntimeError):
    """The gRPC worker was invoked without its optional protocol dependencies."""


def provider_failure_category(error: BaseException) -> int:
    """Translate provider failures to the frozen runner protocol categories."""
    if runner_pb2 is None:
        raise WorkerDependencyError(
            "provider failure classification requires generated runner protocol modules"
        )
    if isinstance(error, (ProviderTimeoutError, TimeoutError)):
        return runner_pb2.FAILURE_CATEGORY_TIMEOUT
    if isinstance(error, ProviderTransientError):
        return runner_pb2.FAILURE_CATEGORY_TRANSIENT
    if isinstance(
        error,
        (
            ProviderConfigurationError,
            ProviderPermanentError,
            ProviderResponseError,
            ProviderUnavailableError,
            RecordedResponseNotFoundError,
        ),
    ):
        return runner_pb2.FAILURE_CATEGORY_PERMANENT
    return runner_pb2.FAILURE_CATEGORY_INTERNAL


class LangGraphWorker:
    def __init__(
        self,
        control_plane_addr: str = "localhost:50051",
        worker_id: str = "langgraph-worker-1",
        cas_root: str | Path = "/cas",
        artifacts_root: str | Path = "/artifacts",
        max_trials: int = 0,
        poll_interval: float = 0.5,
    ):
        self.control_plane_addr = control_plane_addr
        self.worker_id = worker_id
        self.cas_root = Path(cas_root)
        self.artifacts_root = Path(artifacts_root)
        self.max_trials = max_trials
        self.poll_interval = poll_interval
        self.channel: Optional[grpc.Channel] = None
        self.stub: Optional[runner_pb2_grpc.RunnerControlStub] = None
        self.session_token = ""
        self.heartbeat_interval_ms = 3000
        self.cancelled = False
        self._heartbeat_stop: Optional[threading.Event] = None
        self._heartbeat_thread: Optional[threading.Thread] = None

    def connect(self) -> None:
        if grpc is None or runner_pb2_grpc is None:
            raise WorkerDependencyError(
                "gRPC worker mode requires grpcio and generated runner protocol modules"
            )
        self.channel = grpc.insecure_channel(self.control_plane_addr)
        self.stub = runner_pb2_grpc.RunnerControlStub(self.channel)

    def close(self) -> None:
        self._stop_heartbeat()
        if self.channel is not None:
            self.channel.close()
            self.channel = None

    def register(self) -> bool:
        if self.stub is None:
            self.connect()
        req = runner_pb2.RegisterWorkerRequest(
            worker_id=self.worker_id,
            protocol_version="runner.v1",
            worker_version="langgraph-worker@0.1.0",
            capabilities=runner_pb2.WorkerCapabilities(
                harnesses=["langgraph"],
                graders=["deterministic", "llm"],
                sandbox_profiles=["docker-restricted-v1"],
                max_concurrency=1,
            ),
            environment=runner_pb2.WorkerEnvironment(
                os=os.name,
                arch=os.uname().machine if hasattr(os, "uname") else "unknown",
                hostname=os.uname().nodename if hasattr(os, "uname") else "unknown",
                python_version=sys.version.split()[0],
            ),
        )
        try:
            resp = self.stub.RegisterWorker(req)
        except grpc.RpcError as exc:
            logger.error("RegisterWorker failed: %s", redact_sensitive(str(exc)))
            return False
        if not resp.accepted:
            logger.error("worker registration rejected: %s", redact_sensitive(resp.reject_reason))
            return False
        self.session_token = resp.session_token
        if resp.heartbeat_interval_ms > 0:
            self.heartbeat_interval_ms = resp.heartbeat_interval_ms
        return True

    def claim_trial(self) -> Optional[Dict[str, Any]]:
        if self.stub is None:
            self.connect()
        req = runner_pb2.ClaimTrialRequest(
            worker_id=self.worker_id,
            session_token=self.session_token,
            harness="langgraph",
            sandbox_profile="docker-restricted-v1",
            lease_duration_sec=max(1, int(self.heartbeat_interval_ms / 1000) * 10),
        )
        try:
            resp = self.stub.ClaimTrial(req)
        except grpc.RpcError as exc:
            logger.warning("ClaimTrial failed: %s", redact_sensitive(str(exc)))
            if exc.code() == grpc.StatusCode.UNAUTHENTICATED:
                # Session TTLs are intentionally finite. Re-register instead
                # of spinning forever with a token the server has rejected.
                self.session_token = ""
                self.register()
            return None
        if not resp.has_trial:
            return None
        try:
            trial = json.loads(resp.trial_request_json)
        except (TypeError, json.JSONDecodeError) as exc:
            raise ValueError(f"invalid trial_request_json: {exc}") from exc
        if not isinstance(trial, dict):
            raise ValueError("trial_request_json must be an object")

        expected_top = {
            "trial_id": resp.trial_id,
            "logical_trial_id": resp.logical_trial_id,
            "experiment_id": resp.experiment_id,
            "pair_id": resp.pair_id,
            "arm": resp.arm,
            "attempt_no": resp.attempt_no,
        }
        for key, value in expected_top.items():
            if trial.get(key) != value:
                raise ValueError(f"claim identity mismatch for {key}")
        trial["lease_token"] = resp.lease_token
        trial["lease_generation"] = resp.lease_generation
        trial["lease_expires_at_unix_ms"] = resp.lease_expires_at_unix_ms
        trial["trial_request_hash"] = resp.request_hash
        trial["request_hash"] = resp.request_hash
        return trial

    @staticmethod
    def _compute_request_hash(trial: Dict[str, Any]) -> str:
        return request_hash(trial)

    @staticmethod
    def _compute_execution_hash(execution: Dict[str, Any]) -> str:
        return sha256_canonical(execution)

    def verify_hashes(self, trial: Dict[str, Any]) -> bool:
        try:
            supplied_request = trial.get("trial_request_hash") or trial.get("request_hash")
            if not isinstance(supplied_request, str) or not supplied_request:
                logger.error("request hash missing for trial %s", trial.get("trial_id"))
                return False
            if self._compute_request_hash(trial) != supplied_request:
                logger.error("request hash mismatch for trial %s", trial.get("trial_id"))
                return False
            execution = trial.get("execution")
            supplied_execution = trial.get("execution_hash")
            if not isinstance(execution, dict) or not isinstance(supplied_execution, str) or not supplied_execution:
                logger.error("execution projection/hash missing for trial %s", trial.get("trial_id"))
                return False
            if self._compute_execution_hash(execution) != supplied_execution:
                logger.error("execution hash mismatch for trial %s", trial.get("trial_id"))
                return False
            return True
        except (TypeError, ValueError, OverflowError) as exc:
            logger.error("invalid trial identity for %s: %s", trial.get("trial_id"), exc)
            return False

    def _start_heartbeat(self, trial: Dict[str, Any], sequence: int = 0) -> None:
        self.cancelled = False
        stop = threading.Event()
        self._heartbeat_stop = stop
        started_ms = int(time.time() * 1000)

        def loop() -> None:
            first = True
            while not stop.is_set():
                phase = "start" if first else "executing"
                first = False
                if not self.heartbeat(trial, phase, sequence, started_ms):
                    break
                stop.wait(max(self.heartbeat_interval_ms, 100) / 1000.0)

        self._heartbeat_thread = threading.Thread(target=loop, daemon=True)
        self._heartbeat_thread.start()

    def _stop_heartbeat(self) -> None:
        if self._heartbeat_stop is not None:
            self._heartbeat_stop.set()
        if self._heartbeat_thread is not None and self._heartbeat_thread is not threading.current_thread():
            self._heartbeat_thread.join(timeout=1)
        self._heartbeat_stop = None
        self._heartbeat_thread = None

    def heartbeat(self, trial: Dict[str, Any], phase: str, sequence: int, started_ms: int) -> bool:
        if self.stub is None:
            self.connect()
        try:
            response = self.stub.Heartbeat(
                runner_pb2.HeartbeatRequest(
                    worker_id=self.worker_id,
                    session_token=self.session_token,
                    trial_id=trial["trial_id"],
                    lease_token=trial["lease_token"],
                    lease_generation=trial.get("lease_generation", 0),
                    phase=phase,
                    event_sequence=sequence,
                    started_at_unix_ms=started_ms,
                    resource_usage=runner_pb2.ResourceUsage(
                        elapsed_ms=max(0, int(time.time() * 1000) - started_ms)
                    ),
                )
            )
        except grpc.RpcError as exc:
            logger.warning("Heartbeat failed: %s", redact_sensitive(str(exc)))
            return False
        if response.action == runner_pb2.HEARTBEAT_ACTION_CANCEL_REQUESTED:
            self.cancelled = True
            return False
        return response.action != runner_pb2.HEARTBEAT_ACTION_LEASE_REJECTED

    def report_event(self, *, trial: Dict[str, Any], sequence: int, event_type: str, payload: Dict[str, Any]) -> bool:
        if self.stub is None:
            self.connect()
        payload_json = canonical_json_bytes(payload).decode("utf-8")
        try:
            resp = self.stub.ReportEvent(
                runner_pb2.ReportEventRequest(
                    event_id=f"{trial['trial_id']}:{sequence}",
                    trial_id=trial["trial_id"],
                    worker_id=self.worker_id,
                    session_token=self.session_token,
                    lease_token=trial["lease_token"],
                    lease_generation=trial.get("lease_generation", 0),
                    logical_trial_id=trial["logical_trial_id"],
                    experiment_id=trial["experiment_id"],
                    attempt_no=trial["attempt_no"],
                    sequence=sequence,
                    event_type=event_type,
                    occurred_at_unix_ms=int(time.time() * 1000),
                    payload_json=payload_json,
                    payload_hash=sha256_canonical(payload),
                )
            )
        except grpc.RpcError as exc:
            logger.warning("ReportEvent failed: %s", redact_sensitive(str(exc)))
            return False
        return resp.status in (
            runner_pb2.EVENT_REPORT_STATUS_ACCEPTED,
            runner_pb2.EVENT_REPORT_STATUS_DUPLICATE_IGNORED,
        )

    def execute_trial(self, trial: Dict[str, Any]) -> Dict[str, Any]:
        execution = trial["execution"]
        workspace = self.artifacts_root / trial["experiment_id"] / trial["trial_id"]
        workspace.mkdir(parents=True, exist_ok=True)
        self._start_heartbeat(trial)
        try:
            if self.cancelled:
                raise RuntimeError("trial cancelled before execution")
            self.report_event(trial=trial, sequence=1, event_type="agent.graph_started", payload={"arm": trial["arm"]})
            spec = ExecutionSpec(
                case_id=execution["case_id"],
                case_input=execution.get("case_input", {}),
                skill_hash=execution.get("skill_hash", ""),
                model=execution.get("model", {}),
                tool_policy=execution.get("tool_policy", {}),
                environment=execution.get("environment", {}),
            )
            result = TrialExecutor(self.cas_root, workspace, trial["trial_id"]).execute(
                spec, check_cancelled=lambda: self.cancelled
            )
            if self.cancelled:
                raise RuntimeError("trial cancelled during execution")
            self.report_event(trial=trial, sequence=2, event_type="agent.graph_finished", payload={"status": "ok"})
            result.update(
                {
                    "trial_id": trial["trial_id"],
                    "logical_trial_id": trial["logical_trial_id"],
                    "attempt_no": trial["attempt_no"],
                    "lease_token": trial["lease_token"],
                    "lease_generation": trial.get("lease_generation", 0),
                    "request_hash": trial["request_hash"],
                    "execution_hash": trial["execution_hash"],
                    "final_sequence": 2,
                    "outcome": "SUCCEEDED",
                }
            )
            return result
        finally:
            self._stop_heartbeat()

    def complete_trial(self, trial: Dict[str, Any], result: Dict[str, Any]) -> bool:
        if self.stub is None:
            self.connect()
        artifacts = []
        for index, item in enumerate(result.get("artifacts", [])):
            if hasattr(item, "model_dump"):
                item = item.model_dump()
            elif hasattr(item, "dict"):
                item = item.dict()
            path = str(self.artifacts_root / trial["experiment_id"] / trial["trial_id"] / item.get("name", f"artifact-{index}"))
            artifact_path = Path(path)
            if not artifact_path.exists():
                continue
            artifacts.append(
                runner_pb2.ArtifactManifest(
                    artifact_id=f"{trial['trial_id']}:{index}",
                    kind=item.get("name", "artifact"),
                    media_type=item.get("mime_type", "application/octet-stream"),
                    sha256=item.get("content_hash", ""),
                    size_bytes=artifact_path.stat().st_size,
                    local_path=path,
                    created_at_unix_ms=int(time.time() * 1000),
                )
            )
        usage = result.get("usage", {})
        if not isinstance(usage, dict):
            usage = {}
        manifest = {
            "trial_id": trial["trial_id"],
            "execution_hash": trial["execution_hash"],
            "outcome": result.get("outcome", "SUCCEEDED"),
            "trace": redact_sensitive(result.get("trace", {})),
            "usage": usage,
        }
        manifest_json = canonical_json_bytes(manifest).decode("utf-8")
        manifest_hash = sha256_canonical(manifest)
        try:
            resp = self.stub.CompleteTrial(
                runner_pb2.CompleteTrialRequest(
                    worker_id=self.worker_id,
                    session_token=self.session_token,
                    trial_id=trial["trial_id"],
                    logical_trial_id=trial["logical_trial_id"],
                    attempt_no=trial["attempt_no"],
                    lease_token=trial["lease_token"],
                    lease_generation=trial.get("lease_generation", 0),
                    request_hash=trial["request_hash"],
                    idempotency_key=result_idempotency_key(trial["trial_id"], manifest_hash),
                    final_sequence=result.get("final_sequence", 0),
                    exit_code=0,
                    outcome=result.get("outcome", "SUCCEEDED"),
                    outcome_manifest_json=manifest_json,
                    artifacts=artifacts,
                    usage=runner_pb2.ResourceUsage(
                        input_tokens=max(0, int(usage.get("input_tokens", 0) or 0)),
                        output_tokens=max(0, int(usage.get("output_tokens", 0) or 0)),
                        elapsed_ms=max(0, int(result.get("duration_ms", 0) or 0)),
                        tool_calls=max(0, int(usage.get("tool_calls", 0) or 0)),
                    ),
                    grades_json="{}",
                )
            )
        except grpc.RpcError as exc:
            logger.error("CompleteTrial failed: %s", redact_sensitive(str(exc)))
            return False
        return resp.status in (
            runner_pb2.COMPLETION_STATUS_COMMITTED,
            runner_pb2.COMPLETION_STATUS_ALREADY_COMMITTED,
        )

    def fail_trial(
        self,
        trial: Dict[str, Any],
        error: str,
        category: Optional[int] = None,
        sequence: int = 0,
    ) -> bool:
        if runner_pb2 is None:
            raise WorkerDependencyError(
                "FailTrial requires grpcio and generated runner protocol modules"
            )
        if category is None:
            category = runner_pb2.FAILURE_CATEGORY_INTERNAL
        if self.stub is None:
            self.connect()
        safe_error = redact_sensitive(str(error))
        manifest = {"trial_id": trial.get("trial_id", ""), "error": safe_error, "category": int(category)}
        try:
            resp = self.stub.FailTrial(
                runner_pb2.FailTrialRequest(
                    worker_id=self.worker_id,
                    session_token=self.session_token,
                    trial_id=trial["trial_id"],
                    logical_trial_id=trial["logical_trial_id"],
                    attempt_no=trial["attempt_no"],
                    lease_token=trial["lease_token"],
                    lease_generation=trial.get("lease_generation", 0),
                    request_hash=trial.get("request_hash", ""),
                    failure_category=category,
                    error_message=safe_error,
                    event_sequence=sequence,
                    outcome_manifest_json=canonical_json_bytes(manifest).decode("utf-8"),
                )
            )
        except grpc.RpcError as exc:
            logger.error("FailTrial failed: %s", redact_sensitive(str(exc)))
            return False
        return resp.status != runner_pb2.FAIL_TRIAL_STATUS_REJECTED

    def run_loop(self) -> int:
        if not self.register():
            return 1
        completed = 0
        while self.max_trials <= 0 or completed < self.max_trials:
            trial: Optional[Dict[str, Any]] = None
            try:
                trial = self.claim_trial()
                if trial is None:
                    time.sleep(self.poll_interval)
                    continue
                if not self.verify_hashes(trial):
                    self.fail_trial(trial, "request or execution hash verification failed", runner_pb2.FAILURE_CATEGORY_PERMANENT)
                    continue
                result = self.execute_trial(trial)
                if self.complete_trial(trial, result):
                    completed += 1
            except Exception as exc:  # keep the worker alive for independent attempts
                # Avoid logging an exception chain: provider SDKs may include
                # request headers in their traceback even when the message is
                # redacted.
                logger.error("trial execution failed: %s", redact_sensitive(str(exc)))
                if isinstance(trial, dict):
                    category = provider_failure_category(exc)
                    self.fail_trial(trial, redact_sensitive(str(exc)), category)
        return 0


def main() -> None:
    parser = argparse.ArgumentParser(description="SkillGate LangGraph Worker")
    parser.add_argument("--request", help="Execute one mounted request JSON without gRPC (sandbox mode)")
    parser.add_argument("--server", default=os.getenv("CONTROL_PLANE_ADDR", "localhost:50051"))
    parser.add_argument("--worker-id", default=os.getenv("WORKER_ID", "langgraph-worker-1"))
    parser.add_argument("--cas", default=os.getenv("CAS_DIR", "/cas"))
    parser.add_argument("--artifacts", default=os.getenv("ARTIFACTS_DIR", "/artifacts"))
    parser.add_argument("--max-trials", type=int, default=int(os.getenv("MAX_TRIALS", "0")))
    args = parser.parse_args()
    if args.request:
        _run_local_request(args.request, args.cas, args.artifacts)
        return
    worker = LangGraphWorker(args.server, args.worker_id, args.cas, args.artifacts, args.max_trials)
    try:
        raise SystemExit(worker.run_loop())
    finally:
        worker.close()


def _run_local_request(request_path: str, cas_root: str, artifacts_root: str) -> None:
    """Run a single request for TrialRunner's Docker sandbox path."""
    request = json.loads(Path(request_path).read_text(encoding="utf-8"))
    execution = request.get("execution") or {}
    if not isinstance(execution, dict):
        raise ValueError("execution must be an object")
    supplied_execution = request.get("execution_hash")
    if not isinstance(supplied_execution, str) or not supplied_execution:
        raise ValueError("execution_hash is required")
    computed_execution = sha256_canonical(execution)
    if computed_execution != supplied_execution:
        raise ValueError(
            f"execution hash mismatch: expected {supplied_execution}, got {computed_execution}"
        )
    spec = ExecutionSpec(
        case_id=execution["case_id"],
        case_input=execution.get("case_input", {}),
        skill_hash=execution.get("skill_hash", ""),
        model=execution.get("model", {}),
        tool_policy=execution.get("tool_policy", {}),
        environment=execution.get("environment", {}),
    )
    trial_id = request.get("trial_id", "local-trial")
    workspace = Path(artifacts_root)
    workspace.mkdir(parents=True, exist_ok=True)
    try:
        result = TrialExecutor(cas_root, workspace, trial_id).execute(spec)
    except Exception as exc:
        # Local sandbox mode has no FailTrial RPC boundary.  Avoid emitting an
        # SDK traceback that could include an operator credential.
        raise RuntimeError(str(redact_sensitive(str(exc)))) from None
    result.update(
        {
            "trial_id": trial_id,
            "request_hash": request_hash(request),
            "execution_hash": request.get("execution_hash", ""),
            "outcome": "SUCCEEDED",
        }
    )
    (workspace / "result.json").write_text(json.dumps(result, ensure_ascii=False), encoding="utf-8")


if __name__ == "__main__":
    main()
