"""Main entry point for LangGraph Worker."""

import argparse
import json
import logging
import os
import sys
from pathlib import Path

from .models import TrialRequest, TrialResult
from .executor import TrialExecutor

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s',
    handlers=[logging.StreamHandler(sys.stdout)]
)
logger = logging.getLogger(__name__)


def main():
    """Main entry point."""
    parser = argparse.ArgumentParser(description="LangGraph Worker for SkillGate")
    parser.add_argument("--request", required=True, help="Path to trial request JSON")
    parser.add_argument("--workspace", default="/workspace", help="Workspace directory")
    parser.add_argument("--cas", default="/cas", help="CAS directory (read-only)")
    args = parser.parse_args()

    try:
        # Load trial request
        with open(args.request, 'r') as f:
            request_data = json.load(f)

        request = TrialRequest(**request_data)
        logger.info(f"Starting trial {request.trial_id} (attempt {request.attempt_no})")

        # Execute trial
        executor = TrialExecutor(
            workspace_dir=Path(args.workspace),
            cas_dir=Path(args.cas)
        )

        result = executor.execute(request)

        # Write result
        result_path = Path(args.workspace) / "result.json"
        with open(result_path, 'w') as f:
            json.dump(result.model_dump(), f, indent=2)

        logger.info(f"Trial {request.trial_id} completed with outcome: {result.outcome}")

        # Exit with success
        sys.exit(0)

    except Exception as e:
        logger.error(f"Trial execution failed: {e}", exc_info=True)

        # Write error result
        error_result = TrialResult(
            trial_id=request_data.get("trial_id", "unknown"),
            outcome="error",
            category="worker_error",
            message=str(e),
            events=[],
            artifacts={}
        )

        result_path = Path(args.workspace) / "result.json"
        with open(result_path, 'w') as f:
            json.dump(error_result.model_dump(), f, indent=2)

        sys.exit(1)


if __name__ == "__main__":
    main()
