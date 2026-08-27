# Docker Deployment

The canonical Docker Compose configuration is located in:

```
deploy/docker-compose.yml
```

## Quick Start

```bash
cd deploy
docker compose up --build
```

This will start:
- PostgreSQL database
- Bootstrap service (runs migrations and initial setup)
- Control Plane gRPC server (port 50051)
- Fixture Worker (processes trial executions)

## Architecture

The deployment uses:
- `deploy/Dockerfile.server` - Builds the Control Plane server
- `deploy/Dockerfile.worker` - Builds the Python fixture worker
- `deploy/compose-bootstrap.sh` - Initialization script

## Historical Note

Previous versions of this project had `docker-compose.yml` and `docker-compose.yaml` files at the repository root. These have been removed as they referenced obsolete commands and non-existent Dockerfiles. All current deployment configuration is maintained in the `deploy/` directory.
