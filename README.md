# Session Manager

Registry for active sessions and proxy dataplane pods. It supports an in-memory
store for simple local runs and Postgres for durable deployments.

## Run

```bash
PORT=8081 \
STORE_DRIVER=memory \
go run ./cmd/server
```

Postgres:

```bash
PORT=8081 \
STORE_DRIVER=postgres \
DATABASE_URL='postgres://dendenmushi:dendenmushi@localhost:5432/dendenmushi?sslmode=disable' \
go run ./cmd/server
```

The Postgres tables and indexes are created idempotently at startup.

## Endpoints

```http
GET  /healthz
POST /sessions
GET  /sessions/{sessionId}
POST /sessions/{sessionId}/heartbeat
POST /sessions/{sessionId}/close
POST /proxy-pods/register
POST /proxy-pods/heartbeat
GET  /proxy-pods/available
```

Sessions are assigned to the READY pod with the lowest `activeSessions`.
Pods with heartbeat older than `HEARTBEAT_MAX_AGE_SECONDS` are ignored.
Proxy Dataplane processes register themselves through `POST /proxy-pods/register`
and refresh their records through `POST /proxy-pods/heartbeat`.
