"""LangGraph execution engine with trace and artifact generation."""

import hashlib
import json
import time
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional

from .models import TrialTrace, Artifact, TrialEvent
from .provider import create_provider
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
        event = TrialEvent(
            trial_id=self.trial_id,
            timestamp=datetime.utcnow().isoformat() + "Z",
            event_type=event_type,
            data=data,
        )
        self.events.append(event)

    def log_llm_call(self, messages: List[Dict[str, Any]], response: Dict[str, Any]):
        """Log LLM call to trace."""
        call_data = {
            "timestamp": datetime.utcnow().isoformat() + "Z",
            "messages": messages,
            "response": response,
            "model": response.get("model", "unknown"),
            "usage": response.get("usage", {}),
        }
        self.trace.llm_calls.append(call_data)
        self.log_event("llm_call", call_data)

    def log_tool_call(self, tool_name: str, args: Dict[str, Any], result: Any):
        """Log tool call to trace."""
        call_data = {
            "timestamp": datetime.utcnow().isoformat() + "Z",
            "tool": tool_name,
            "args": args,
            "result": result,
        }
        self.trace.tool_calls.append(call_data)
        self.log_event("tool_call", call_data)

    def log_step(self, step_name: str, data: Dict[str, Any]):
        """Log execution step."""
        step_data = {
            "timestamp": datetime.utcnow().isoformat() + "Z",
            "step": step_name,
            "data": data,
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
        # Compute content hash
        content_hash = hashlib.sha256(content).hexdigest()

        # Write to workspace
        artifact_path = self.workspace_dir / name
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

    def __init__(self, cas_dir: str, workspace_dir: str):
        self.skill_loader = SkillLoader(cas_dir)
        self.workspace_dir = Path(workspace_dir)

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
            ctx.log_step("initialize", {
                "skill_hash": skill_hash or "none",
                "model": model_config.get("name", "default"),
            })

            # Load skill if present
            skill = None
            if skill_hash:
                ctx.log_step("load_skill", {"hash": skill_hash})
                skill = self.skill_loader.load_skill(skill_hash)
                ctx.log_step("skill_loaded", {
                    "name": skill["metadata"].get("name", "unknown"),
                    "version": skill["metadata"].get("version", "unknown"),
                })

            # Check for cancellation before expensive operations
            if check_cancelled and check_cancelled():
                raise RuntimeError("Trial cancelled before execution")

            # Create provider
            provider = create_provider(model_config)

            # Execute graph (simplified for M4)
            ctx.log_step("execute_graph", {"with_skill": skill is not None})

            # Build initial messages
            messages = self._build_messages(case_input, skill)

            # Check for cancellation before LLM call
            if check_cancelled and check_cancelled():
                raise RuntimeError("Trial cancelled before LLM call")

            # Single LLM call (M5+ will expand to full graph)
            ctx.log_step("llm_call_start", {"message_count": len(messages)})
            response = provider.complete(messages)
            ctx.log_llm_call(messages, response)

            # Check for cancellation after LLM call
            if check_cancelled and check_cancelled():
                raise RuntimeError("Trial cancelled after LLM call")

            # Extract result
            result_text = response["choices"][0]["message"]["content"]

            # Write result as artifact
            ctx.write_artifact(
                "result.txt",
                result_text.encode("utf-8"),
                "text/plain",
            )

            # Write trace as artifact
            trace_json = json.dumps(ctx.trace.dict(), indent=2)
            ctx.write_artifact(
                "trace.json",
                trace_json.encode("utf-8"),
                "application/json",
            )

            ctx.log_step("complete", {"outcome": "success"})

        except Exception as e:
            ctx.log_step("error", {
                "error": str(e),
                "type": type(e).__name__,
            })
            raise

        return ctx

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

        # User message from fixtures
        fixtures = case_input.get("fixtures", [])
        if fixtures:
            user_content = ""
            for fixture in fixtures:
                if fixture.get("type") == "text":
                    user_content += fixture.get("content", "") + "\n"
            messages.append({"role": "user", "content": user_content.strip()})

        return messages
