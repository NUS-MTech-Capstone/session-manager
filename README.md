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
make run-postgres
```

The Postgres tables and indexes are created idempotently at startup.
The Compose database stores its data in the `session-manager-postgres` volume.

Run the store integration tests against Postgres with:

```bash
make test-postgres
```

For an externally managed database, run:

```bash
PORT=8081 \
STORE_DRIVER=postgres \
DATABASE_URL='postgres://user:password@host:5432/database?sslmode=require' \
go run ./cmd/server
```

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

Active sessions must heartbeat before `SESSION_LEASE_SECONDS` expires. Expired
sessions are closed and their pod capacity is released when sessions or
available pods are next accessed.

Session heartbeat and close requests must include the assigned owner:

```json
{
  "ownerPodId": "proxy-dataplane-abc",
  "ownerInstanceId": "boot-uuid-123"
}
```
