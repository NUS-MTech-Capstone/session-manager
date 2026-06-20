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
GET  /sessions?userId={userId}&hostId={hostId}&crId={crId}&status=ACTIVE
GET  /sessions/{sessionId}
POST /sessions/{sessionId}/heartbeat
POST /sessions/{sessionId}/close
POST /proxy-pods/register
POST /proxy-pods/heartbeat
GET  /proxy-pods/available
POST /automation/runs
GET  /automation/runs/{runId}
POST /automation/runs/{runId}/close
```

Sessions are assigned to the READY pod with the lowest `activeSessions`.
Pods with heartbeat older than `HEARTBEAT_MAX_AGE_SECONDS` are ignored.
Proxy Dataplane processes register themselves through `POST /proxy-pods/register`
and refresh their records through `POST /proxy-pods/heartbeat`.

Active sessions must heartbeat before `SESSION_LEASE_SECONDS` expires. Expired
sessions are closed and their pod capacity is released when sessions or
available pods are next accessed.

Session records persist `connectionType`; it defaults to `ssh` for compatibility
with existing callers and PostgreSQL rows.

Session listing is an internal runtime lookup for control-plane workflows such
as owner rejoin discovery. Filters are optional at the Session Manager boundary,
but public callers should be mediated by the Proxy Control Plane so authenticated
identity and authorization are applied before querying active sessions.

Proxy pod registration includes `operationsUrl` for internal automation calls.
Automation runs retain `ownerPodId + ownerInstanceId`, expire after
`AUTOMATION_RUN_TTL_SECONDS` (default `3600`), and do not reserve interactive
session capacity while a plan waits for approval.

Session heartbeat and close requests must include the assigned owner:

```json
{
  "ownerPodId": "proxy-dataplane-abc",
  "ownerInstanceId": "boot-uuid-123"
}
```
