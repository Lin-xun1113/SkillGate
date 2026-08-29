#!/bin/sh
set -eu

if [ -z "${SKILLGATE_DATABASE_URL_FILE:-}" ]; then
  echo "SECRET_MISSING: SKILLGATE_DATABASE_URL_FILE is required" >&2
  exit 1
fi
/usr/local/bin/skillgate db migrate

# Materialize the compiled manifest and referenced Skills into the shared CAS.
# cas prepare accepts both current package hashes and legacy M0 hashes.
/usr/local/bin/skillgate cas prepare /app/experiments/csv-analysis-v1-demo.yaml \
  --project-root /app --cas-dir /app/cas
# CAS entries are immutable public evaluation inputs. The bootstrap process is
# root in the server image, while the LangGraph worker runs as uid 1000; make
# the shared read-only volume readable without granting it write access.
chmod -R a+rX /app/cas

# Keep the grader definition and all relative schema/expected references
# available to the control-plane registry volume.
mkdir -p /app/graders
if [ ! -f /app/evals/csv-analysis/grader.yaml ]; then
  echo "grader bundle is missing: /app/evals/csv-analysis/grader.yaml" >&2
  exit 1
fi
cp -R /app/evals/csv-analysis/. /app/graders/

/usr/local/bin/skillgate experiment materialize /app/experiments/csv-analysis-v1-demo.yaml \
  --budget-timeout 5m \
  --trial-timeout 10s \
  --backoff-base 50ms \
  --backoff-cap 1s
