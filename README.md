# session-manager
# Session Manager

In-memory MVP registry for active sessions and proxy dataplane pods.

## Run

```bash
PORT=8081 go run ./cmd/server
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
