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

Optional lifecycle event publication:

```bash
EVENT_PUBLISHER_ENABLED=true
EVENTS_TOPIC_ARN=arn:aws:sns:ap-southeast-1:123456789012:dendenmushi-events-dev
AWS_REGION=ap-southeast-1
```

When enabled, Session Manager publishes authoritative `SessionCreated` and
`SessionClosed` metadata events after the store mutation succeeds. Events
include assignment metadata such as `podId` and `instanceId`, but not session
tokens, terminal streams, command bodies, or secrets. Publish failures are
logged and do not affect assignment, heartbeat, lease, or close behavior.

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

Retained session command/output audit is owned by `audit-service`. Session
Manager remains the source of truth for session metadata, assignment, leases,
pod health, automation run ownership, and owner validation metadata.

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

For Phase 2 agent workflows, Session Manager stores assignment and lifecycle
metadata only. It does not route model prompts, live terminal streams,
retained command/output audit records, or remediation decisions. Automation
callers must use the assigned pod's `operationsUrl` together with STS
capability validation.

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
