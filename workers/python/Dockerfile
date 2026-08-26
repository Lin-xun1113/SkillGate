FROM python:3.11-slim

# Security: non-root user
RUN useradd -m -u 1000 worker

# Install dependencies
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy worker code
COPY langgraph_worker/ ./langgraph_worker/

# Set ownership
RUN chown -R worker:worker /app

# Switch to non-root user
USER worker

# Default environment
ENV WORKER_ID=langgraph-worker-1
ENV SCHEDULER_URL=http://scheduler:8080
ENV CAS_DIR=/cas
ENV WORKSPACE_DIR=/workspace

# Create workspace directory (will be mounted)
RUN mkdir -p /workspace

ENTRYPOINT ["python", "-m", "langgraph_worker.worker"]
