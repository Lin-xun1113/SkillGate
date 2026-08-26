"""Data models for LangGraph Worker."""

from typing import Any, Dict, List, Optional
from pydantic import BaseModel, Field


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
    worker_id: str
    request_hash: str
    execution_hash: str
    outcome: str  # success, error, timeout, cancelled
    category: str = ""
    error_message: Optional[str] = None
    duration_ms: int
    events: List[TrialEvent]
    trace: Optional[TrialTrace] = None
    artifacts: List[Artifact] = Field(default_factory=list)
