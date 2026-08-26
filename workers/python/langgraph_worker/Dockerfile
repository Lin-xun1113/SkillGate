FROM python:3.11-slim

# Create non-root user
RUN useradd -m -u 1000 worker

# Install dependencies
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy worker code
COPY . .

# Switch to non-root user
USER worker

# Mount points for CAS (read-only) and artifacts (read-write)
VOLUME ["/cas", "/artifacts"]

# Entry point
CMD ["python", "-m", "langgraph_worker.worker"]
