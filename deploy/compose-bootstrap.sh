#!/bin/sh
set -eu

DB_URL="${DATABASE_URL:?DATABASE_URL is required}"
/usr/local/bin/skillgate db migrate --database-url "$DB_URL"
/usr/local/bin/skillgate experiment materialize /app/experiments/csv-analysis-v1-demo.yaml \
  --database-url "$DB_URL" \
  --budget-timeout 5m \
  --trial-timeout 10s \
  --backoff-base 50ms \
  --backoff-cap 1s
