FROM python:3.11-slim

# Create non-root user
RUN useradd -m -u 1000 worker

# Install dependencies
WORKDIR /app
COPY workers/python/langgraph_worker/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy worker code
COPY workers/python/gen /app/gen
COPY workers/python/langgraph_worker /app/langgraph_worker
# The checked-in fixture provider reads only this immutable, non-secret suite
# bundle. Keep it in the image so both Compose and Go-managed sandboxes can run
# the offline M0 workload without mounting the project workspace.
COPY evals/csv-analysis /app/evals/csv-analysis
ENV PYTHONPATH=/app:/app/gen

# Mount points for CAS (read-only) and artifacts (read-write). The image uses
# the same absolute paths as the Go control-plane container so local artifact
# manifests remain valid across the shared volume.
RUN mkdir -p /app/cas /app/artifacts && chown -R worker:worker /app

# Switch to non-root user
USER worker

VOLUME ["/app/cas", "/app/artifacts"]

# Keep command-line options supplied by Compose/local runs as arguments to the
# worker module instead of attempting to execute --server as a binary.
ENTRYPOINT ["python", "-m", "langgraph_worker"]
CMD []
