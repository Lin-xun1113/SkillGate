# DEPRECATED: control-plane binary

This binary has been deprecated in favor of the unified `skillgate serve` command.

## Migration

Instead of running:
```bash
SKILLGATE_DATABASE_URL_FILE=/run/secrets/database-url control-plane --port 50051 --artifacts ./artifacts --graders ./graders
```

Use:
```bash
SKILLGATE_DATABASE_URL_FILE=/run/secrets/database-url skillgate serve --grpc-addr :50051 --artifacts-dir ./artifacts --graders-dir ./graders
```

## Why Deprecated

The `control-plane` binary only ran the grading poller as a standalone service. This functionality has been integrated into `skillgate serve`, which now runs both:
1. The gRPC RunnerControl service
2. The grading poller for processing experiments

This eliminates the need to run two separate processes and ensures experiments progress through the full lifecycle in production deployments.

## Removal Timeline

This binary will be removed in a future release. Please migrate to `skillgate serve`.
