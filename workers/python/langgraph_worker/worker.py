"""Worker main entry point - connects to Control Plane and executes trials."""

import os
import sys
import json
import hashlib
import logging
from pathlib import Path
from typing import Dict, Any, Optional
import grpc
from concurrent import futures
import time

# Import generated protobuf stubs
sys.path.insert(0, str(Path(__file__).parent.parent / "gen"))
from runner.v1 import runner_pb2
from runner.v1 import runner_pb2_grpc

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(name)s: %(message)s'
)
logger = logging.getLogger(__name__)


class LangGraphWorker:
    """LangGraph worker that executes trials in sandbox."""

    def __init__(self, control_plane_addr: str, worker_id: str):
        self.control_plane_addr = control_plane_addr
        self.worker_id = worker_id
        self.cas_root = Path("/cas")
        self.artifacts_root = Path("/artifacts")
        self.channel = None
        self.stub = None
        self.session_token = None
        self.heartbeat_interval_ms = 5000
        self.cancelled = False

    def register(self) -> bool:
        """Register with control plane."""
        try:
            # Create gRPC channel
            self.channel = grpc.insecure_channel(self.control_plane_addr)
            self.stub = runner_pb2_grpc.RunnerControlStub(self.channel)

            # Build registration request
            request = runner_pb2.RegisterWorkerRequest(
                worker_id=self.worker_id,
                protocol_version="runner.v1",
                worker_version="0.1.0",
                capabilities=runner_pb2.WorkerCapabilities(
                    harnesses=["langgraph"],
                    graders=[],
                    sandbox_profiles=["docker-restricted-v1"],
                    max_concurrency=1
                ),
                environment=runner_pb2.WorkerEnvironment(
                    os=os.uname().sysname,
                    arch=os.uname().machine,
                    hostname=os.uname().nodename,
                    python_version=sys.version.split()[0]
                )
            )

            logger.info(f"Registering worker {self.worker_id} with {self.control_plane_addr}")
            response = self.stub.RegisterWorker(request)

            if response.accepted:
                self.session_token = response.session_token
                self.heartbeat_interval_ms = response.heartbeat_interval_ms
                logger.info(f"Registration accepted, session_token={self.session_token[:8]}...")
                return True
            else:
                logger.error(f"Registration rejected: {response.reject_reason}")
                return False

        except grpc.RpcError as e:
            logger.error(f"Registration RPC failed: {e.code()} - {e.details()}")
            return False

    def claim_trial(self) -> Optional[Dict[str, Any]]:
        """Claim a trial from control plane."""
        try:
            request = runner_pb2.ClaimTrialRequest(
                worker_id=self.worker_id,
                session_token=self.session_token,
                harness="langgraph",
                sandbox_profile="docker",
                lease_duration_sec=300
            )

            response = self.stub.ClaimTrial(request)

            if not response.has_trial:
                logger.debug("No trials available")
                time.sleep(1)
                return None

            # Parse trial_request_json
            trial = json.loads(response.trial_request_json)
            trial.update({
                "trial_id": response.trial_id,
                "logical_trial_id": response.logical_trial_id,
                "experiment_id": response.experiment_id,
                "pair_id": response.pair_id,
                "arm": response.arm,
                "attempt": response.attempt_no,
                "lease_token": response.lease_token,
                "lease_generation": response.lease_generation,
                "lease_expires_at_unix_ms": response.lease_expires_at_unix_ms,
                "trial_request_hash": response.request_hash,
            })

            logger.info(f"Claimed trial {response.trial_id} (pair={response.pair_id}, arm={response.arm})")
            return trial

        except grpc.RpcError as e:
            logger.error(f"ClaimTrial RPC failed: {e.code()} - {e.details()}")
            return None

    def verify_hashes(self, trial: Dict[str, Any]) -> bool:
        """Verify trial_request_hash and execution_hash."""
        # Verify trial_request_hash (frozen M3 definition)
        request_hash = self._compute_request_hash(trial)
        if request_hash != trial.get("trial_request_hash"):
            logger.error(f"Request hash mismatch: expected {trial.get('trial_request_hash')}, got {request_hash}")
            return False

        # Verify execution_hash (M4 execution content)
        execution_hash = self._compute_execution_hash(trial.get("execution", {}))
        if execution_hash != trial.get("execution_hash"):
            logger.error(f"Execution hash mismatch: expected {trial.get('execution_hash')}, got {execution_hash}")
            return False

        logger.info(f"Hash verification passed: request={request_hash}, execution={execution_hash}")
        return True

    def _compute_request_hash(self, trial: Dict[str, Any]) -> str:
        """Compute M3 frozen trial_request_hash."""
        payload = {
            "experiment_id": trial.get("experiment_id"),
            "pair_id": trial.get("pair_id"),
            "arm": trial.get("arm"),
            "attempt": trial.get("attempt"),
        }
        canonical = json.dumps(payload, sort_keys=True, separators=(',', ':'))
        return "sha256:" + hashlib.sha256(canonical.encode()).hexdigest()

    def _compute_execution_hash(self, execution: Dict[str, Any]) -> str:
        """Compute M4 execution_hash."""
        payload = {
            "manifest_hash": execution.get("manifest_hash"),
            "case_id": execution.get("case_id"),
            "arm": execution.get("arm"),
            "skill_hash": execution.get("skill_hash", ""),
            "provider": execution.get("provider", {}),
            "context": execution.get("context", {}),
            "evaluator": execution.get("evaluator", {}),
        }
        canonical = json.dumps(payload, sort_keys=True, separators=(',', ':'))
        return "sha256:" + hashlib.sha256(canonical.encode()).hexdigest()

    def load_skill(self, skill_hash: str) -> Optional[Dict[str, Any]]:
        """Load and verify skill from CAS."""
        if not skill_hash:
            logger.info("No skill_hash (without_skill arm)")
            return None

        skill_path = self.cas_root / "skills" / skill_hash / "skill.yaml"
        if not skill_path.exists():
            logger.error(f"Skill not found in CAS: {skill_path}")
            raise FileNotFoundError(f"Skill {skill_hash} not found in CAS")

        # Verify skill content hash
        content = skill_path.read_bytes()
        computed_hash = hashlib.sha256(content).hexdigest()
        if computed_hash != skill_hash:
            logger.error(f"Skill hash mismatch: expected {skill_hash}, got {computed_hash}")
            raise ValueError(f"Skill hash verification failed: expected {skill_hash}, got {computed_hash}")

        logger.info(f"Loaded and verified skill {skill_hash} from CAS")
        # TODO: Parse YAML and return skill definition
        return {"content": content.decode(), "hash": skill_hash}

    def execute_trial(self, trial: Dict[str, Any]) -> Dict[str, Any]:
        """Execute trial and produce trace + artifacts."""
        trial_id = trial.get("trial_id")
        execution = trial.get("execution", {})
        started_at_unix_ms = int(time.time() * 1000)
        event_sequence = 0

        logger.info(f"Executing trial {trial_id}")

        # Reset cancellation flag
        self.cancelled = False

        # Load skill from CAS
        skill_hash = execution.get("skill_hash", "")
        skill = self.load_skill(skill_hash)

        # Start heartbeat in background
        import threading
        heartbeat_stop = threading.Event()

        def heartbeat_loop():
            while not heartbeat_stop.is_set():
                if not self.heartbeat(
                    trial_id,
                    trial.get("lease_token"),
                    trial.get("lease_generation"),
                    "executing",
                    event_sequence,
                    started_at_unix_ms
                ):
                    break
                heartbeat_stop.wait(self.heartbeat_interval_ms / 1000.0)

        heartbeat_thread = threading.Thread(target=heartbeat_loop, daemon=True)
        heartbeat_thread.start()

        try:
            # Check for cancellation before execution
            if self.cancelled:
                raise RuntimeError("Trial cancelled before execution")

            # Execute LangGraph with cancellation support
            trace = self._run_langgraph(trial, skill)

            # Check for cancellation after execution
            if self.cancelled:
                raise RuntimeError("Trial cancelled after execution")

            # Report progress event
            event_sequence += 1
            self.report_event(
                trial_id,
                trial.get("lease_token"),
                trial.get("lease_generation"),
                trial.get("logical_trial_id"),
                trial.get("experiment_id"),
                trial.get("attempt"),
                event_sequence,
                {"type": "execution_complete", "trace_size": len(str(trace))}
            )

            # Check if cancelled during execution
            if self.cancelled:
                raise RuntimeError("Trial cancelled by control plane")

            # Write artifacts
            artifacts = self._write_artifacts(trial_id, trace)

            return {
                "trial_id": trial_id,
                "logical_trial_id": trial.get("logical_trial_id"),
                "attempt": trial.get("attempt"),
                "lease_token": trial.get("lease_token"),
                "lease_generation": trial.get("lease_generation"),
                "trial_request_hash": trial.get("trial_request_hash"),
                "status": "completed",
                "trace": trace,
                "artifacts": artifacts,
                "final_sequence": event_sequence,
                "elapsed_ms": int(time.time() * 1000) - started_at_unix_ms,
            }
        except RuntimeError as e:
            if "cancelled" in str(e).lower():
                # Report cancellation to control plane
                self.report_event(
                    trial_id,
                    trial.get("lease_token"),
                    trial.get("lease_generation"),
                    trial.get("logical_trial_id"),
                    trial.get("experiment_id"),
                    trial.get("attempt"),
                    event_sequence + 1,
                    {"type": "cancelled", "reason": str(e)}
                )
                logger.info(f"Trial {trial_id} cancelled")
                raise
            else:
                raise
        finally:
            # Stop heartbeat thread
            heartbeat_stop.set()
            heartbeat_thread.join(timeout=1.0)

    def _run_langgraph(self, trial: Dict[str, Any], skill: Optional[Dict[str, Any]]) -> Dict[str, Any]:
        """Execute LangGraph with interruption support."""
        from .executor import TrialExecutor
        from .models import ExecutionSpec

        execution = trial.get("execution", {})
        spec = ExecutionSpec(
            case_id=execution.get("case_id"),
            case_input=execution.get("case_input", {}),
            skill_hash=execution.get("skill_hash"),
            model=execution.get("model"),
            tool_policy=execution.get("tool_policy"),
            environment=execution.get("environment", {})
        )

        executor = TrialExecutor(
            cas_root=self.cas_root,
            artifacts_root=self.artifacts_root,
            trial_id=trial.get("trial_id")
        )

        # Pass cancellation flag to executor
        return executor.execute(spec, skill, check_cancelled=lambda: self.cancelled)

    def _write_artifacts(self, trial_id: str, trace: Dict[str, Any]) -> list:
        """Write artifacts to disk and compute hashes."""
        artifacts = []

        # Write trace artifact
        trace_path = self.artifacts_root / f"{trial_id}_trace.json"
        trace_path.write_text(json.dumps(trace, indent=2))
        trace_hash = hashlib.sha256(trace_path.read_bytes()).hexdigest()

        artifacts.append({
            "path": str(trace_path),
            "hash": trace_hash,
            "type": "trace",
        })

        logger.info(f"Wrote {len(artifacts)} artifacts for trial {trial_id}")
        return artifacts

    def heartbeat(self, trial_id: str, lease_token: str, lease_generation: int, phase: str, event_sequence: int, started_at_unix_ms: int) -> bool:
        """Send heartbeat to control plane and check for cancellation."""
        try:
            request = runner_pb2.HeartbeatRequest(
                worker_id=self.worker_id,
                session_token=self.session_token,
                trial_id=trial_id,
                lease_token=lease_token,
                phase=phase,
                event_sequence=event_sequence,
                started_at_unix_ms=started_at_unix_ms,
                lease_generation=lease_generation,
                resource_usage=runner_pb2.ResourceUsage(
                    input_tokens=0,
                    output_tokens=0,
                    elapsed_ms=int(time.time() * 1000) - started_at_unix_ms,
                ),
            )

            response = self.stub.Heartbeat(request)

            # Check for cancellation
            if response.action == runner_pb2.HEARTBEAT_ACTION_CANCEL_REQUESTED:
                logger.warning(f"Trial {trial_id} cancellation requested")
                self.cancelled = True
                return False
            elif response.action == runner_pb2.HEARTBEAT_ACTION_LEASE_REJECTED:
                logger.error(f"Trial {trial_id} lease rejected")
                return False

            logger.debug(f"Heartbeat for trial {trial_id} accepted")
            return True

        except grpc.RpcError as e:
            logger.error(f"Heartbeat RPC failed: {e.code()} - {e.details()}")
            return False

    def report_event(self, trial_id: str, event: Dict[str, Any], lease_token: str, lease_generation: int, logical_trial_id: str, experiment_id: str, attempt_no: int, sequence: int) -> bool:
        """Report event to control plane."""
        try:
            payload_json = json.dumps(event, sort_keys=True)
            payload_hash = hashlib.sha256(payload_json.encode()).hexdigest()

            request = runner_pb2.ReportEventRequest(
                event_id=f"{trial_id}_{sequence}",
                trial_id=trial_id,
                worker_id=self.worker_id,
                session_token=self.session_token,
                lease_token=lease_token,
                lease_generation=lease_generation,
                logical_trial_id=logical_trial_id,
                experiment_id=experiment_id,
                attempt_no=attempt_no,
                sequence=sequence,
                event_type=event.get('type', 'unknown'),
                occurred_at_unix_ms=int(time.time() * 1000),
                payload_json=payload_json,
                payload_hash=payload_hash
            )

            response = self.stub.ReportEvent(request)

            if response.status == runner_pb2.EVENT_REPORT_STATUS_ACCEPTED:
                logger.info(f"Event {sequence} for trial {trial_id} accepted: {event.get('type')}")
                return True
            elif response.status == runner_pb2.EVENT_REPORT_STATUS_DUPLICATE_IGNORED:
                logger.debug(f"Event {sequence} for trial {trial_id} was duplicate")
                return True
            else:
                logger.warning(f"Event {sequence} rejected: {response.message}")
                return False

        except grpc.RpcError as e:
            logger.error(f"ReportEvent RPC failed: {e.code()} - {e.details()}")
            return False

    def complete_trial(self, result: Dict[str, Any]) -> bool:
        """Complete trial and submit results (idempotent)."""
        try:
            trial_id = result.get("trial_id")

            # Build artifact manifests
            artifact_manifests = []
            for artifact in result.get("artifacts", []):
                manifest = runner_pb2.ArtifactManifest(
                    artifact_id=f"{trial_id}_{artifact['type']}",
                    kind=artifact['type'],
                    media_type="application/json",
                    sha256=artifact['hash'],
                    size_bytes=len(Path(artifact['path']).read_bytes()),
                    local_path=artifact['path'],
                    created_at_unix_ms=int(time.time() * 1000)
                )
                artifact_manifests.append(manifest)

            request = runner_pb2.CompleteTrialRequest(
                worker_id=self.worker_id,
                session_token=self.session_token,
                trial_id=trial_id,
                logical_trial_id=result.get("logical_trial_id"),
                attempt_no=result.get("attempt"),
                lease_token=result.get("lease_token"),
                idempotency_key=f"{trial_id}_complete",
                request_hash=result.get("trial_request_hash"),
                final_sequence=result.get("final_sequence", 0),
                exit_code=0,
                outcome="completed",
                artifacts=artifact_manifests,
                lease_generation=result.get("lease_generation"),
                resource_usage=runner_pb2.ResourceUsage(
                    input_tokens=0,
                    output_tokens=0,
                    elapsed_ms=result.get("elapsed_ms", 0),
                    tool_calls=0,
                    peak_memory_bytes=0
                )
            )

            response = self.stub.CompleteTrial(request)

            if response.status == runner_pb2.COMPLETION_STATUS_COMMITTED:
                logger.info(f"Trial {trial_id} completed successfully (result_id={response.result_id})")
                return True
            elif response.status == runner_pb2.COMPLETION_STATUS_ALREADY_COMMITTED:
                logger.info(f"Trial {trial_id} already committed (idempotent)")
                return True
            else:
                logger.error(f"Trial {trial_id} completion failed: {response.message}")
                return False

        except grpc.RpcError as e:
            logger.error(f"CompleteTrial RPC failed: {e.code()} - {e.details()}")
            return False

    def fail_trial(self, trial_id: str, error: str, lease_token: str, lease_generation: int, logical_trial_id: str, experiment_id: str, attempt_no: int, trial_request_hash: str, event_sequence: int) -> bool:
        """Mark trial as failed."""
        try:
            request = runner_pb2.FailTrialRequest(
                worker_id=self.worker_id,
                session_token=self.session_token,
                trial_id=trial_id,
                logical_trial_id=logical_trial_id,
                attempt_no=attempt_no,
                lease_token=lease_token,
                failure_category=runner_pb2.FAILURE_CATEGORY_PERMANENT,
                error_message=error,
                event_sequence=event_sequence,
                request_hash=trial_request_hash,
                lease_generation=lease_generation
            )

            response = self.stub.FailTrial(request)

            if response.status == runner_pb2.FAIL_TRIAL_STATUS_RECORDED:
                logger.error(f"Trial {trial_id} marked as failed: {error}")
                return True
            elif response.status == runner_pb2.FAIL_TRIAL_STATUS_RETRY_SCHEDULED:
                logger.warning(f"Trial {trial_id} will be retried (next_attempt={response.next_attempt_no})")
                return True
            else:
                logger.error(f"FailTrial rejected: {response.message}")
                return False

        except grpc.RpcError as e:
            logger.error(f"FailTrial RPC failed: {e.code()} - {e.details()}")
            return False

    def run(self):
        """Main worker loop."""
        if not self.register():
            logger.error("Registration failed")
            sys.exit(1)

        logger.info("Worker started, polling for trials...")

        while True:
            trial = self.claim_trial()
            if not trial:
                continue

            trial_id = trial.get("trial_id")

            try:
                # Verify hashes
                if not self.verify_hashes(trial):
                    self.fail_trial(trial_id, "Hash verification failed")
                    continue

                # Execute trial
                result = self.execute_trial(trial)

                # Complete trial
                self.complete_trial(result)

            except Exception as e:
                logger.exception(f"Trial {trial_id} failed")
                self.fail_trial(trial_id, str(e))


def main():
    """Entry point."""
    control_plane = os.getenv("CONTROL_PLANE_ADDR", "localhost:50051")
    worker_id = os.getenv("WORKER_ID", "worker-1")

    worker = LangGraphWorker(control_plane, worker_id)
    worker.run()


if __name__ == "__main__":
    main()

