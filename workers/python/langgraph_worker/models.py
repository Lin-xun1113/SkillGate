"""Data models for the LangGraph Worker protocol."""

from typing import Any, Dict, List, Optional

try:
    from pydantic import BaseModel, Field
except ImportError:  # pragma: no cover - keeps the module importable for tooling
    import copy
    class BaseModel:  # minimal compatibility shim for local protocol checks
        def __init__(self, **kwargs):
            # ``Field(default_factory=...)`` is evaluated while the class is
            # declared below. Copy declared defaults onto each instance so
            # traces, events and artifacts never leak between trials.
            for cls in reversed(type(self).__mro__):
                for key in getattr(cls, "__annotations__", {}):
                    if hasattr(cls, key):
                        setattr(self, key, copy.deepcopy(getattr(cls, key)))
            for key, value in kwargs.items():
                setattr(self, key, value)

        def model_dump(self):
            def dump(value):
                if isinstance(value, BaseModel):
                    return {key: dump(item) for key, item in value.__dict__.items()}
                if isinstance(value, list):
                    return [dump(item) for item in value]
                if isinstance(value, dict):
                    return {key: dump(item) for key, item in value.items()}
                return value

            return {key: dump(value) for key, value in self.__dict__.items()}

    def Field(default_factory):
        return default_factory()


class ExecutionSpec(BaseModel):
    """What to execute (skill, case, model, policy)."""
    case_id: str
    case_input: Dict[str, Any]
    skill_hash: str = ""  # empty for without_skill arm
    model: Dict[str, Any]
    tool_policy: Dict[str, Any] = Field(default_factory=dict)
    environment: Dict[str, Any] = Field(default_factory=dict)


class TrialRequest(BaseModel):
    """Trial request payload from scheduler."""
    experiment_id: str
    logical_trial_id: str
    trial_id: str
    pair_id: str
    arm: str
    attempt_no: int
    execution: Optional[ExecutionSpec] = None
    execution_hash: Optional[str] = None


class WorkerRegistration(BaseModel):
    """Worker registration request."""
    worker_id: str
    capabilities: List[str] = Field(default_factory=lambda: ["langgraph"])
    version: str = "1.0.0"


class ClaimRequest(BaseModel):
    """Claim trial request."""
    worker_id: str
    capability: str = "langgraph"


class HeartbeatRequest(BaseModel):
    """Heartbeat to keep trial alive."""
    worker_id: str
    trial_id: str


class TrialEvent(BaseModel):
    """Event during trial execution."""
    trial_id: str
    timestamp: str
    event_type: str  # llm_call, tool_call, step_start, step_end
    data: Dict[str, Any]


class Artifact(BaseModel):
    """Trial artifact."""
    name: str
    content_hash: str
    size_bytes: int
    mime_type: str = "application/octet-stream"


class TrialTrace(BaseModel):
    """Execution trace."""
    llm_calls: List[Dict[str, Any]] = Field(default_factory=list)
    tool_calls: List[Dict[str, Any]] = Field(default_factory=list)
    steps: List[Dict[str, Any]] = Field(default_factory=list)


class TrialResult(BaseModel):
    """Trial execution result."""
    trial_id: str
    worker_id: str = ""
    request_hash: str = ""
    execution_hash: str = ""
    outcome: str = "FAILED"  # SUCCEEDED, FAILED, TIMED_OUT, CANCELLED
    category: str = ""
    error_message: Optional[str] = None
    duration_ms: int = 0
    events: List[TrialEvent] = Field(default_factory=list)
    trace: Optional[TrialTrace] = None
    artifacts: List[Artifact] = Field(default_factory=list)
