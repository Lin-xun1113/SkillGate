"""LangGraph execution engine with trace and artifact generation."""

import hashlib
import json
import os
import re
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional

from .models import TrialTrace, Artifact, TrialEvent
from .m0_fixture import execute_csv_case
from .provider import create_provider, redact_sensitive, validate_model_config
from .skill_loader import SkillLoader


class ExecutionContext:
    """Context for one trial execution."""

    def __init__(self, trial_id: str, workspace_dir: Path):
        self.trial_id = trial_id
        self.workspace_dir = workspace_dir
        self.events: List[TrialEvent] = []
        self.trace = TrialTrace()
        self.artifacts: List[Artifact] = []
        self.start_time = time.time()

    def log_event(self, event_type: str, data: Dict[str, Any]):
        """Log an execution event."""
        safe_data = redact_sensitive(data)
        event = TrialEvent(
            trial_id=self.trial_id,
            timestamp=datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            event_type=event_type,
            data=safe_data,
        )
        self.events.append(event)

    def log_llm_call(self, messages: List[Dict[str, Any]], response: Dict[str, Any]):
        """Log LLM call to trace."""
        call_data = {
            "timestamp": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "messages": redact_sensitive(messages),
            "response": redact_sensitive(response),
            "model": redact_sensitive(response.get("model", "unknown")),
            "usage": redact_sensitive(response.get("usage", {})),
        }
        self.trace.llm_calls.append(call_data)
        self.log_event("llm_call", call_data)

    def log_tool_call(self, tool_name: str, args: Dict[str, Any], result: Any):
        """Log tool call to trace."""
        call_data = {
            "timestamp": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "tool": tool_name,
            "args": redact_sensitive(args),
            "result": redact_sensitive(result),
        }
        self.trace.tool_calls.append(call_data)
        self.log_event("tool_call", call_data)

    def log_step(self, step_name: str, data: Dict[str, Any]):
        """Log execution step."""
        step_data = {
            "timestamp": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "step": step_name,
            "data": redact_sensitive(data),
        }
        self.trace.steps.append(step_data)
        self.log_event("step", step_data)

    def write_artifact(self, name: str, content: bytes, mime_type: str = "application/octet-stream") -> Artifact:
        """
        Write artifact to workspace and compute hash.

        Args:
            name: Artifact filename
            content: Binary content
            mime_type: MIME type

        Returns:
            Artifact metadata
        """
        # Keep artifact names relative to the trial workspace.
        artifact_path = (self.workspace_dir / name).resolve()
        workspace_root = self.workspace_dir.resolve()
        if artifact_path != workspace_root and workspace_root not in artifact_path.parents:
            raise ValueError(f"artifact path escapes workspace: {name}")

        # Compute the same prefixed content hash expected by the Go protocol.
        content_hash = "sha256:" + hashlib.sha256(content).hexdigest()

        # Write to workspace
        artifact_path.parent.mkdir(parents=True, exist_ok=True)
        artifact_path.write_bytes(content)

        # Create artifact record
        artifact = Artifact(
            name=name,
            content_hash=content_hash,
            size_bytes=len(content),
            mime_type=mime_type,
        )
        self.artifacts.append(artifact)

        self.log_event("artifact_written", {
            "name": name,
            "hash": content_hash,
            "size": len(content),
        })

        return artifact

    def duration_ms(self) -> int:
        """Get execution duration in milliseconds."""
        return int((time.time() - self.start_time) * 1000)


class LangGraphExecutor:
    """Execute agent graph with skill and model."""

    def __init__(self, cas_dir: str, workspace_dir: str, fixture_root: Optional[str] = None):
        self.skill_loader = SkillLoader(cas_dir) if Path(cas_dir).exists() else None
        self.workspace_dir = Path(workspace_dir)
        configured_fixture_root = fixture_root or os.getenv("SKILLGATE_EVALS_ROOT", "/app/evals/csv-analysis")
        self.fixture_root = Path(configured_fixture_root)

    def execute(
        self,
        trial_id: str,
        case_input: Dict[str, Any],
        skill_hash: Optional[str],
        model_config: Dict[str, Any],
        tool_policy: Dict[str, Any],
        environment: Dict[str, Any],
        check_cancelled: Optional[callable] = None,
    ) -> ExecutionContext:
        """
        Execute trial with LangGraph.

        Args:
            trial_id: Trial ID
            case_input: Input fixtures and evaluation mode
            skill_hash: Skill hash (empty for without_skill)
            model_config: Model provider and configuration
            tool_policy: Tool usage policy
            environment: Environment variables
            check_cancelled: Optional callback to check if trial was cancelled

        Returns:
            ExecutionContext with trace, events, and artifacts
        """
        ctx = ExecutionContext(trial_id, self.workspace_dir)

        try:
            # Validate even fixture/recorded paths; otherwise a malformed
            # model.config could bypass the real-provider constructor simply
            # because an offline fixture happened to be available.
            validate_model_config(model_config, require_name=True)
            ctx.log_step("initialize", {
                "skill_hash": skill_hash or "none",
                "model": model_config.get("name", "default"),
            })

            # Load skill if present
            skill = None
            if skill_hash and case_input.get("evaluationMode") == "autonomous_trigger" and not self._should_discover_skill(case_input):
                # Autonomous trigger cases exercise discovery, so a candidate
                # skill is not force-mounted for an unrelated prompt. This is
                # the deterministic discovery rule for the M0 CSV workload.
                ctx.log_step("skill_not_loaded", {
                    "name": "csv-analysis",
                    "reason": "discovery_no_match",
                })
                skill_hash = None
            if skill_hash:
                if self.skill_loader is None:
                    raise RuntimeError(f"CAS directory does not exist: {skill_hash}")
                ctx.log_step("load_skill", {"hash": skill_hash})
                skill = self.skill_loader.load_skill(skill_hash)
                ctx.log_step("skill_loaded", {
                    "name": skill["metadata"].get("name", "unknown"),
                    "version": skill["metadata"].get("version", "unknown"),
                })

            # Check for cancellation before expensive operations
            if check_cancelled and check_cancelled():
                raise RuntimeError("Trial cancelled before execution")

            # Execute graph (simplified for M4)
            ctx.log_step("execute_graph", {"with_skill": skill is not None})

            provider_name = str(model_config.get("provider", "")).lower()
            fixture_result = None
            if provider_name == "fixture" and self.fixture_root.is_dir():
                fixture_result = execute_csv_case(
                    case_id=case_input.get("id", ""),
                    case_input=case_input,
                    fixture_root=self.fixture_root,
                    log_step=ctx.log_step,
                    write_artifact=ctx.write_artifact,
                )

            if fixture_result is None:
                # The projected execution environment is untrusted experiment
                # identity, not a Secret source. Real Provider credentials and
                # endpoints are read only from the Worker process environment.
                provider = create_provider(model_config)
                messages = self._build_messages(case_input, skill)

                # Check for cancellation before the provider call.
                if check_cancelled and check_cancelled():
                    raise RuntimeError("Trial cancelled before LLM call")

                ctx.log_step("llm_call_start", {"message_count": len(messages)})
                response = provider.invoke(messages, tools=None, params=None)
                ctx.log_llm_call(messages, response)

                if check_cancelled and check_cancelled():
                    raise RuntimeError("Trial cancelled after LLM call")
                result_text = response["choices"][0]["message"]["content"]
            else:
                ctx.log_step("fixture_execution", {"provider": "fixture", "case_id": case_input.get("id", "")})
                result_text = fixture_result

            safe_result_text = redact_sensitive(result_text)
            if not isinstance(safe_result_text, str):
                safe_result_text = str(safe_result_text)
            ctx.write_artifact("result.txt", safe_result_text.encode("utf-8"), "text/plain")

            ctx.log_step("complete", {"outcome": "success"})
            trace_data = ctx.trace.model_dump() if hasattr(ctx.trace, "model_dump") else ctx.trace.dict()
            trace_json = json.dumps(trace_data, indent=2, ensure_ascii=False)
            ctx.write_artifact("trace.json", trace_json.encode("utf-8"), "application/json")

        except Exception as e:
            ctx.log_step("error", {
                "error": redact_sensitive(str(e)),
                "type": type(e).__name__,
            })
            raise

        return ctx

    @staticmethod
    def _should_discover_skill(case_input: Dict[str, Any]) -> bool:
        """Apply the M0 discovery boundary to autonomous trigger prompts."""
        prompt = case_input.get("prompt", "")
        if not isinstance(prompt, str):
            return False
        return bool(re.search(r"csv|sales|销售|表格", prompt, re.IGNORECASE))

    def _build_messages(
        self,
        case_input: Dict[str, Any],
        skill: Optional[Dict[str, Any]],
    ) -> List[Dict[str, Any]]:
        """Build initial messages from case input and skill."""
        messages = []

        # System message with skill context
        if skill:
            system_content = f"You are an AI assistant with the following skill:\n\n"
            system_content += f"Name: {skill['metadata'].get('name', 'unknown')}\n"
            system_content += f"Description: {skill['metadata'].get('description', 'No description')}\n"
            messages.append({"role": "system", "content": system_content})
        else:
            messages.append({
                "role": "system",
                "content": "You are a helpful AI assistant.",
            })

        # The suite prompt is the task input.  Earlier versions only included
        # inline ``type: text`` fixtures, which silently turned path-based M0
        # cases into a prompt containing just the system message.  Preserve
        # fixture declarations as metadata; workers must access their mounted
        # files through the sandbox rather than trusting arbitrary paths.
        prompt = case_input.get("prompt")
        user_parts: List[str] = []
        if isinstance(prompt, str) and prompt.strip():
            user_parts.append(prompt.strip())

        # User message from fixtures
        fixtures = case_input.get("fixtures", [])
        if fixtures:
            for fixture in fixtures:
                if not isinstance(fixture, dict):
                    continue
                if fixture.get("type") == "text":
                    content = fixture.get("content", "")
                    if isinstance(content, str) and content:
                        user_parts.append(content)
                elif isinstance(fixture.get("path"), str):
                    path = fixture["path"].replace("\\", "/")
                    digest = fixture.get("sha256")
                    suffix = f" ({digest})" if isinstance(digest, str) and digest else ""
                    user_parts.append(f"Declared fixture: {path}{suffix}")

        if user_parts:
            messages.append({"role": "user", "content": "\n\n".join(user_parts)})

        return messages


class TrialExecutor:
    """Compatibility adapter used by the gRPC worker loop.

    The execution engine returns an ``ExecutionContext`` so callers can submit
    structured events and artifact manifests without depending on LangGraph's
    internal state classes.
    """

    def __init__(self, cas_root: Path | str, artifacts_root: Path | str, trial_id: str = ""):
        self.cas_root = str(cas_root)
        self.artifacts_root = str(artifacts_root)
        self.trial_id = trial_id

    def execute(self, spec, skill=None, check_cancelled=None) -> Dict[str, Any]:
        executor = LangGraphExecutor(self.cas_root, self.artifacts_root)
        ctx = executor.execute(
            trial_id=self.trial_id,
            case_input=spec.case_input,
            skill_hash=spec.skill_hash or None,
            model_config=spec.model,
            tool_policy=spec.tool_policy,
            environment=spec.environment,
            check_cancelled=check_cancelled,
        )

        def dump(value):
            if hasattr(value, "model_dump"):
                return value.model_dump()
            if hasattr(value, "dict"):
                return value.dict()
            return value

        usage = {"input_tokens": 0, "output_tokens": 0, "tool_calls": len(ctx.trace.tool_calls)}
        for call in ctx.trace.llm_calls:
            call_usage = call.get("usage", {}) if isinstance(call, dict) else {}
            if not isinstance(call_usage, dict):
                continue
            usage["input_tokens"] += int(
                call_usage.get("input_tokens", call_usage.get("prompt_tokens", 0)) or 0
            )
            usage["output_tokens"] += int(
                call_usage.get("output_tokens", call_usage.get("completion_tokens", 0)) or 0
            )

        return {
            "trace": dump(ctx.trace),
            "events": [dump(event) for event in ctx.events],
            "artifacts": [dump(artifact) for artifact in ctx.artifacts],
            "usage": usage,
            "duration_ms": ctx.duration_ms(),
        }
