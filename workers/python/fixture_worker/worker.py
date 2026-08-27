import argparse
import hashlib
import json
import logging
import os
import sys
import time
from typing import Optional, Dict, Any

# Ensure gen is on sys.path
sys_path_gen = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "gen"))
if sys_path_gen not in sys.path:
    sys.path.insert(0, sys_path_gen)

import grpc
from runner.v1 import runner_pb2, runner_pb2_grpc
try:
    from .canonical import sha256_canonical, canonical_json_bytes
except (ImportError, ValueError):
    try:
        from fixture_worker.canonical import sha256_canonical, canonical_json_bytes
    except (ImportError, ValueError):
        from canonical import sha256_canonical, canonical_json_bytes

logging.basicConfig(level=logging.INFO, format="[%(asctime)s] [%(levelname)s] %(message)s")
logger = logging.getLogger("fixture_worker")

# Simulation modes defined by the M3 spec (docs/comet/changes/m3-runner-protocol/specs/runner-protocol/spec.md).
MODE_NORMAL = "normal"
MODE_FAIL = "fail"
MODE_TIMEOUT = "timeout"
MODE_CANCEL_AWARE = "cancel-aware"


class FixtureWorker:
    def __init__(
        self,
        server_addr: str = "127.0.0.1:50051",
        worker_id: str = "python-fixture-worker-01",
        protocol_version: str = "runner.v1",
        worker_version: str = "0.1.0",
        mode: str = MODE_NORMAL,
        simulate_delay: float = 0.1,
        timeout_seconds: float = 0.0,
    ):
        self.server_addr = server_addr
        self.worker_id = worker_id
        self.protocol_version = protocol_version
        self.worker_version = worker_version
        self.mode = mode
        self.simulate_delay = simulate_delay
        self.timeout_seconds = timeout_seconds

        self.session_token: Optional[str] = None
        self._lease_token: str = ""
        self._lease_generation: int = 0
        self.channel: Optional[grpc.Channel] = None
        self.stub: Optional[runner_pb2_grpc.RunnerControlStub] = None

    def connect(self) -> None:
        logger.info(f"Connecting to gRPC server at {self.server_addr}...")
        self.channel = grpc.insecure_channel(self.server_addr)
        self.stub = runner_pb2_grpc.RunnerControlStub(self.channel)

    def close(self) -> None:
        if self.channel:
            self.channel.close()
            self.channel = None

    def register(self) -> bool:
        req = runner_pb2.RegisterWorkerRequest(
            worker_id=self.worker_id,
            protocol_version=self.protocol_version,
            worker_version=self.worker_version,
            capabilities=runner_pb2.WorkerCapabilities(
                harnesses=["fixture"],
                graders=["deterministic", "fixture"],
                sandbox_profiles=["default"],
                max_concurrency=1,
            ),
            environment=runner_pb2.WorkerEnvironment(
                os=sys.platform,
                arch="any",
                hostname="fixture-host",
                python_version=sys.version.split()[0],
            ),
        )
        resp: runner_pb2.RegisterWorkerResponse = self.stub.RegisterWorker(req)
        if not resp.accepted:
            logger.error(f"Worker registration rejected: {resp.reject_reason}")
            return False
        self.session_token = resp.session_token
        logger.info(f"Registered successfully as {self.worker_id}, session token: {self.session_token[:8]}...")
        return True

    def claim_trial(self, duration_sec: int = 30) -> Optional[runner_pb2.ClaimTrialResponse]:
        req = runner_pb2.ClaimTrialRequest(
            worker_id=self.worker_id,
            session_token=self.session_token,
            lease_duration_sec=duration_sec,
        )
        resp: runner_pb2.ClaimTrialResponse = self.stub.ClaimTrial(req)
        if resp.has_trial:
            self._lease_token = resp.lease_token
            self._lease_generation = resp.lease_generation
            return resp
        return None

    def heartbeat(
        self,
        trial_id: str,
        lease_token: str,
        phase: str,
        seq: int,
        usage: Optional[runner_pb2.ResourceUsage] = None,
    ) -> runner_pb2.HeartbeatResponse:
        req = runner_pb2.HeartbeatRequest(
            worker_id=self.worker_id,
            session_token=self.session_token,
            trial_id=trial_id,
            lease_token=lease_token,
            phase=phase,
            event_sequence=seq,
            started_at_unix_ms=int(time.time() * 1000),
            resource_usage=usage,
            lease_generation=self._lease_generation,
        )
        return self.stub.Heartbeat(req)

    def report_event(
        self,
        event_id: str,
        trial_id: str,
        logical_trial_id: str,
        exp_id: str,
        seq: int,
        event_type: str,
        payload: Dict[str, Any],
    ) -> runner_pb2.ReportEventResponse:
        payload_bytes = canonical_json_bytes(payload)
        payload_hash = sha256_canonical(payload)
        req = runner_pb2.ReportEventRequest(
            event_id=event_id,
            trial_id=trial_id,
            worker_id=self.worker_id,
            session_token=self.session_token,
            lease_token=self._lease_token,
            lease_generation=self._lease_generation,
            logical_trial_id=logical_trial_id,
            experiment_id=exp_id,
            sequence=seq,
            event_type=event_type,
            occurred_at_unix_ms=int(time.time() * 1000),
            payload_json=payload_bytes.decode("utf-8"),
            payload_hash=payload_hash,
        )
        response = self.stub.ReportEvent(req)
        if response.status in (runner_pb2.EVENT_REPORT_STATUS_REJECTED, runner_pb2.EVENT_REPORT_STATUS_SEQUENCE_GAP):
            raise RuntimeError(f"event rejected: {response.message}")
        return response

    def complete_trial(
        self,
        claim: runner_pb2.ClaimTrialResponse,
        seq: int,
        outcome: str = "SUCCEEDED",
        outcome_manifest: Optional[Dict[str, Any]] = None,
    ) -> runner_pb2.CompleteTrialResponse:
        if outcome_manifest is None:
            outcome_manifest = {
                "trial_id": claim.trial_id,
                "pair_id": claim.pair_id,
                "arm": claim.arm,
                "status": outcome,
                "pass": outcome == "SUCCEEDED",
                "summary": "Completed by python fixture worker",
            }
        manifest_bytes = canonical_json_bytes(outcome_manifest).decode("utf-8")
        manifest_hash = sha256_canonical(outcome_manifest)
        idempotency_key = sha256_canonical({"trial_id": claim.trial_id, "result_manifest_hash": manifest_hash})

        # Write actual artifact files to disk.
        artifacts_root = os.getenv("ARTIFACTS_ROOT", "./artifacts")
        trial_dir = os.path.join(artifacts_root, claim.experiment_id, claim.trial_id)
        os.makedirs(trial_dir, exist_ok=True)

        # Write output.txt
        output_content = f"skillgate fixture output for {claim.trial_id}\n".encode("utf-8")
        output_path = os.path.join(trial_dir, "output.txt")
        with open(output_path, "wb") as f:
            f.write(output_content)

        # Write security-finding.json (empty dict for fixture)
        finding_content = json.dumps({}).encode("utf-8")
        finding_path = os.path.join(trial_dir, "security-finding.json")
        with open(finding_path, "wb") as f:
            f.write(finding_content)

        artifact = runner_pb2.ArtifactManifest(
            artifact_id=f"artifact-{claim.trial_id}",
            kind="fixture-output",
            media_type="text/plain",
            sha256=f"sha256:{hashlib.sha256(output_content).hexdigest()}",
            size_bytes=len(output_content),
            created_at_unix_ms=int(time.time() * 1000),
            local_path=output_path,
        )
        req = runner_pb2.CompleteTrialRequest(
            worker_id=self.worker_id,
            session_token=self.session_token,
            trial_id=claim.trial_id,
            logical_trial_id=claim.logical_trial_id,
            attempt_no=claim.attempt_no,
            lease_token=claim.lease_token,
            request_hash=claim.request_hash,
            idempotency_key=idempotency_key,
            lease_generation=self._lease_generation,
            final_sequence=seq,
            exit_code=0 if outcome == "SUCCEEDED" else 1,
            outcome=outcome,
            outcome_manifest_json=manifest_bytes,
            artifacts=[artifact],
            usage=runner_pb2.ResourceUsage(
                input_tokens=150,
                output_tokens=80,
                elapsed_ms=int(self.simulate_delay * 1000),
                tool_calls=2,
            ),
        )
        return self.stub.CompleteTrial(req)

    def fail_trial(
        self,
        claim: runner_pb2.ClaimTrialResponse,
        seq: int,
        error_msg: str,
        category: runner_pb2.FailureCategory = runner_pb2.FAILURE_CATEGORY_TRANSIENT,
    ) -> runner_pb2.FailTrialResponse:
        req = runner_pb2.FailTrialRequest(
            worker_id=self.worker_id,
            session_token=self.session_token,
            trial_id=claim.trial_id,
            logical_trial_id=claim.logical_trial_id,
            attempt_no=claim.attempt_no,
            lease_token=claim.lease_token,
            request_hash=claim.request_hash,
            lease_generation=self._lease_generation,
            failure_category=category,
            error_message=error_msg,
            event_sequence=seq,
        )
        return self.stub.FailTrial(req)

    def execute_one_trial(self, claim: runner_pb2.ClaimTrialResponse) -> bool:
        logger.info(f"Executing trial {claim.trial_id} (pair: {claim.pair_id}, arm: {claim.arm}, attempt: {claim.attempt_no})")

        # 1. Verify Request Hash.
        try:
            req_obj = json.loads(claim.trial_request_json)
            if sha256_canonical(req_obj) != claim.request_hash:
                logger.error("Request hash verification failed!")
                return False
            logger.info("Request hash verified successfully.")
        except Exception as exc:
            logger.error(f"Failed to parse or verify request hash: {exc}")
            return False

        self._lease_token = claim.lease_token
        self._lease_generation = claim.lease_generation
        seq = 1

        # 2. Heartbeat start (also starts the attempt).
        hb_resp = self.heartbeat(claim.trial_id, claim.lease_token, "start", seq)
        if hb_resp.action == runner_pb2.HEARTBEAT_ACTION_LEASE_REJECTED:
            logger.error("Lease rejected during start heartbeat: %s", hb_resp.message)
            return False
        if hb_resp.action == runner_pb2.HEARTBEAT_ACTION_CANCEL_REQUESTED:
            logger.warning("Cancellation received during start heartbeat")
            self.complete_trial(claim, seq, outcome="CANCELLED")
            return True

        # 3. TIMEOUT_SIM: stop sending heartbeats so the lease expires and the
        # Control Plane sweeper marks the attempt TIMED_OUT.
        if self.mode == MODE_TIMEOUT:
            logger.info("TIMEOUT_SIM: sleeping past the lease without heartbeats")
            time.sleep(self.timeout_seconds if self.timeout_seconds > 0 else self.simulate_delay)
            return False

        # 4. Report start event.
        seq += 1
        self.report_event(
            event_id=f"evt-{claim.trial_id}-01",
            trial_id=claim.trial_id,
            logical_trial_id=claim.logical_trial_id,
            exp_id=claim.experiment_id,
            seq=seq,
            event_type="tool_call.started",
            payload={"tool": "csv_reader", "action": "load_data"},
        )

        # 5. Simulated work.
        if self.simulate_delay > 0:
            time.sleep(self.simulate_delay)

        # 6. Mid-work heartbeat; also the cancel-aware checkpoint.
        seq += 1
        hb_resp = self.heartbeat(claim.trial_id, claim.lease_token, "evaluating", seq)
        if hb_resp.action == runner_pb2.HEARTBEAT_ACTION_LEASE_REJECTED:
            logger.error("Lease rejected during evaluation heartbeat: %s", hb_resp.message)
            return False
        if hb_resp.action == runner_pb2.HEARTBEAT_ACTION_CANCEL_REQUESTED:
            logger.warning("Cancellation received during evaluation heartbeat")
            self.complete_trial(claim, seq, outcome="CANCELLED")
            return True

        # 7. Report finish event.
        seq += 1
        self.report_event(
            event_id=f"evt-{claim.trial_id}-02",
            trial_id=claim.trial_id,
            logical_trial_id=claim.logical_trial_id,
            exp_id=claim.experiment_id,
            seq=seq,
            event_type="tool_call.finished",
            payload={"tool": "csv_reader", "status": "ok", "rows_read": 100},
        )

        # 8. Fail or complete.
        if self.mode == MODE_FAIL:
            logger.info("Simulating transient failure...")
            fail_resp = self.fail_trial(claim, seq, "Simulated transient failure")
            logger.info(f"Fail report status: {fail_resp.status}, msg: {fail_resp.message}")
            return True

        comp_resp = self.complete_trial(claim, seq, outcome="SUCCEEDED")
        logger.info(f"Completion status: {comp_resp.status}, result_id: {comp_resp.result_id}")
        return True

    def run_loop(self, max_trials: int = 0, poll_interval: float = 0.5) -> int:
        completed = 0
        logger.info(f"Worker {self.worker_id} entering task claim loop...")
        while max_trials == 0 or completed < max_trials:
            try:
                claim = self.claim_trial()
            except grpc.RpcError as exc:
                logger.warning("ClaimTrial failed (%s), retrying...", exc.code())
                time.sleep(poll_interval)
                continue
            if claim:
                success = self.execute_one_trial(claim)
                if success:
                    completed += 1
            else:
                time.sleep(poll_interval)
        return completed


def main():
    parser = argparse.ArgumentParser(description="SkillGate Python Fixture Worker")
    parser.add_argument("--server", default=os.getenv("RUNNER_GRPC_ADDR", "127.0.0.1:50051"), help="gRPC Server address")
    parser.add_argument("--worker-id", default="fixture-worker-py-1", help="Worker ID")
    parser.add_argument("--max-trials", type=int, default=0, help="Max trials to execute (0=infinite)")
    parser.add_argument("--once", action="store_true", help="Claim and run one trial, then exit")
    parser.add_argument("--mode", default=MODE_NORMAL, choices=[MODE_NORMAL, MODE_FAIL, MODE_TIMEOUT, MODE_CANCEL_AWARE], help="Simulation mode")
    parser.add_argument("--fail", action="store_true", help="Deprecated alias for --mode fail")
    parser.add_argument("--delay", type=float, default=0.05, help="Simulated execution delay in seconds")
    parser.add_argument("--timeout-seconds", type=float, default=0.0, help="TIMEOUT_SIM sleep duration in seconds")

    args = parser.parse_args()
    mode = MODE_FAIL if args.fail else args.mode

    worker = FixtureWorker(
        server_addr=args.server,
        worker_id=args.worker_id,
        mode=mode,
        simulate_delay=args.delay,
        timeout_seconds=args.timeout_seconds,
    )
    try:
        worker.connect()
        if not worker.register():
            sys.exit(1)
        max_trials = 1 if args.once else args.max_trials
        done = worker.run_loop(max_trials=max_trials)
        logger.info(f"Worker finished {done} trial(s). Exiting.")
    finally:
        worker.close()


if __name__ == "__main__":
    main()
