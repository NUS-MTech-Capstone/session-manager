# Session Manager

Registry for active sessions and proxy dataplane pods. It supports an in-memory store for  local runs and Postgres for durable deployments.

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
GET  /sessions/audit?userId={userId}&hostId={hostId}&crId={crId}&sessionId={sessionId}
POST /sessions/{sessionId}/audit/records
GET  /sessions/{sessionId}/audit/records
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

Session command/output audit is retained by Session Manager, not CloudWatch.
Proxy Dataplane appends audit records with the assigned `ownerPodId` and
`ownerInstanceId`; stale owners are rejected. Records include best-effort
command lines, output bytes encoded as base64 in JSON, control/session events,
per-session sequence numbers, and truncation flags. Defaults:

```bash
SESSION_AUDIT_RETENTION_SECONDS=604800
SESSION_AUDIT_MAX_BYTES_PER_SESSION=16777216
```

When the per-session cap is reached, existing records are preserved, the
truncating record is marked `truncated=true`, and the dataplane stops sending
additional audit output for that session. Command and output bodies must not be
written to normal application logs.

## Design Patterns

The Session Manager uses a Repository pattern through `store.Store`, with
in-memory and Postgres implementations behind the same service boundary.
`store.Open` is a Factory for selecting the configured store driver at startup.
The in-memory store uses `PodSelectionStrategy`; the default
`LeastActivePodSelectionStrategy` picks the READY pod with the lowest
`activeSessions`, using `podId` as a deterministic tie-breaker.

Session listing is an internal runtime lookup for control-plane workflows such
as owner rejoin discovery. Filters are optional at the Session Manager boundary,
but public callers should be mediated by the Proxy Control Plane so authenticated
identity and authorization are applied before querying active sessions.

Proxy pod registration includes `operationsUrl` for internal automation calls.
Automation runs retain `ownerPodId + ownerInstanceId`, expire after
`AUTOMATION_RUN_TTL_SECONDS` (default `3600`), and do not reserve interactive
session capacity while a plan waits for approval.

For Phase 2 agent workflows, Session Manager stores assignment, lifecycle, and
retained session audit metadata only. It does not route model prompts, live
terminal streams, or remediation decisions. Automation callers must use the
assigned pod's `operationsUrl` together with STS capability validation.

Session heartbeat and close requests must include the assigned owner:

```json
{
  "ownerPodId": "proxy-dataplane-abc",
  "ownerInstanceId": "boot-uuid-123"
}
```

## API Specification

The OpenAPI contract is checked in at `api/openapi.yaml` and is served
unauthenticated at:

```http
GET /openapi.yaml
```
