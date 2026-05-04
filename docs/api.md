# MCSM REST API — v1

> Status: design contract for the v2 rewrite. This document is what `mcsw` (and any other client) codes against. It is the authoritative copy; a mirror lives at `mcsw/docs/mcsm-api.md` — keep them in sync.

## 1. Conventions

### 1.1 Base URL & versioning

```
http(s)://<host>:<port>/api/v1
```

`v1` is locked once shipped. Breaking changes go to `v2`. Additive changes (new fields, new endpoints, new optional query params) are non-breaking and ship under `v1`.

Clients **must** ignore unknown fields in responses.

### 1.2 Content type

`application/json; charset=utf-8` for all request and response bodies, except:

- WebSocket endpoints — frames are NDJSON (one JSON object per text frame).
- `GET /metrics` — Prometheus exposition format.

### 1.3 Authentication

Every endpoint except `/healthz`, `/readyz`, `/openapi.json`, `/version`, and `/metrics` (the last two configurable) requires:

```
Authorization: Bearer <token>
```

Tokens are configured in `config.yaml`, stored hashed with argon2id. Each token has a `name`, `scopes`, and an optional `rate_limit`. Wildcard scope `*` grants everything; otherwise scopes are checked per endpoint (see [§12 Scope reference](#12-scope-reference)).

Failure responses:

| HTTP | code            | When                                  |
| ---- | --------------- | ------------------------------------- |
| 401  | `missing_token` | No `Authorization` header.            |
| 401  | `invalid_token` | Token doesn't match a configured one. |
| 403  | `scope_denied`  | Token lacks the required scope.       |

### 1.4 Rate limiting

Sliding-window per token. Defaults: `600` requests / minute / token. Config is per-token:

```yaml
api:
  tokens:
    - name: mcsw
      hash: $argon2id$...
      scopes: ["*"]
      rate_limit: { per_minute: 600 }    # 0 = unlimited
```

Every response carries:

```
RateLimit-Limit: 600
RateLimit-Remaining: 542
RateLimit-Reset: 47          # seconds until window resets
```

When exhausted: `429 rate_limited` with `Retry-After: <seconds>`.

WebSocket connections count as **one** request at upgrade time, not per-frame. Long-running streams will not exhaust your budget.

### 1.5 CORS

CORS is **off by default** (no `Access-Control-Allow-Origin` header). The recommended posture is for a browser UI (e.g. `mcsw`) to **proxy through its own backend**, so bearer tokens stay server-side.

If you want direct browser access:

```yaml
api:
  cors:
    allowed_origins: ["https://mcsw.example.com"]
    allow_credentials: true
    max_age: 600
```

Preflight (`OPTIONS`) is handled automatically. Requests from origins not in the allowlist are responded to without CORS headers — the browser blocks them as if CORS were never configured.

### 1.6 Transport (TLS)

Two supported postures:

**A. Behind a reverse proxy** (recommended for production) — leave TLS off in MCSM, terminate at Caddy/Traefik/nginx. MCSM listens plain HTTP on a private interface or unix socket.

**B. Built-in TLS** — set both files in config:

```yaml
api:
  bind: 0.0.0.0:8124
  tls:
    cert_file: /etc/mcsm/tls/fullchain.pem
    key_file:  /etc/mcsm/tls/privkey.pem
    min_version: "1.2"           # default 1.2; "1.3" to require TLS 1.3
    hsts_max_age: 31536000       # set to 0 to disable HSTS
```

When TLS is on, MCSM serves HTTP/2 and adds `Strict-Transport-Security`. For dev, `mcsm gen-cert --self-signed --out /etc/mcsm/tls/` generates a 365-day self-signed cert.

### 1.7 Timestamps & IDs

- Timestamps: RFC 3339 / ISO 8601 with timezone (`2026-05-02T12:34:56.789Z`).
- Durations: Go-style strings on input (`"30s"`, `"4h"`, `"500ms"`); also accept integer seconds. Always serialized as Go-style strings on output.
- Server IDs: UUIDv7 (e.g. `01890b6f-9c8d-7c2a-9b3a-1b2c3d4e5f60`). Generated once per server directory, persisted in `<server-dir>/.mcsm/config.yaml`. Never reused, never rewritten.
- Slot names: `[a-z0-9-]{1,32}`, unique within an instance.
- Instance names: `[a-z0-9-]{1,64}`, must be unique across a federation (collision → federation endpoints flag a `peer_conflict` warning).

### 1.8 Error format

Every error shares one shape:

```json
{
  "error": {
    "code": "slot_busy",
    "message": "Slot 'creative' is already running a server",
    "details": {
      "slot": "creative",
      "current_server_id": "01890b6f-..."
    },
    "trace_id": "5b9d8c..."
  }
}
```

`code` is a stable machine-readable string (catalog in [§10](#10-error-catalog)). `message` is a human sentence. `details` is endpoint-specific. `trace_id` is always present and matches the corresponding log line.

### 1.9 Idempotency

Mutating endpoints accept an optional `Idempotency-Key` header (any opaque string ≤ 128 chars). If the same key is replayed within 10 minutes, the cached response is returned and the operation is not repeated.

### 1.10 Async operations

Long operations (slot start / stop / restart, backup create, federation refresh) return **immediately** with the new transitional state. The API does not use job IDs. Clients observe progress via:

1. Polling the relevant detail endpoint (`GET /api/v1/slots/{name}`).
2. Subscribing to `WS /api/v1/slots/{name}/events`.

---

## 2. Resource model

### 2.1 Slot states

```
                  ┌──────┐
        ┌────────►│ idle │◄────────┐
        │         └──┬───┘         │
        │            │ start       │ stop completed
        │            ▼             │
        │       ┌─────────┐        │
        │       │mounting │        │
        │       └────┬────┘        │
        │            │ lock ok     │
        │            ▼             │
        │       ┌──────────┐       │
        │       │ starting │       │
        │       └────┬─────┘       │
        │            │ healthy     │
        │            ▼             │
        │       ┌─────────┐        │
        │       │ running │────┐   │
        │       └────┬────┘    │   │
        │            │ stop    │   │
        │            ▼         │   │
        │       ┌──────────┐   │   │
        │       │ stopping │───┴───┘
        │       └────┬─────┘   │
        │            │ crash   │
        │            ▼         │
        └─────┐ crashed │      │
              └─────────┘      │
                               │
                 ┌────────┐    │
                 │ error  │◄───┘  (lock conflict, missing files, etc.)
                 └────────┘
```

Terminal states: `idle`, `crashed`, `error`. Transitional: `mounting`, `starting`, `stopping`. Steady-state: `running`.

### 2.2 Server ownership states

Reported on every server returned by discovery.

| State           | Meaning                                                                 |
| --------------- | ----------------------------------------------------------------------- |
| `free`          | No `.mcsm/owner.json` present.                                          |
| `owned-self`    | This MCSM instance holds the lock; includes `slot` field.               |
| `owned-other`   | Another live MCSM holds the lock; includes `instance`, `host`, `since`. |
| `stale`         | Lock file present but heartbeat is older than 60s and SLP probe failed. Eligible for steal via `force=true`. |

---

## 3. Instance & discovery

### `GET /api/v1/instance`

Identity, version, configuration summary. Useful as a smoke test.

**Scope:** `instance:read`

**Response 200:**

```json
{
  "name": "node-a",
  "version": "2.0.0",
  "build": { "commit": "a1b2c3d", "date": "2026-04-30T10:00:00Z" },
  "started_at": "2026-05-02T08:11:23Z",
  "uptime_seconds": 14123,
  "discovery_roots": ["/mnt/servers", "/mnt/extra"],
  "slot_count": 2,
  "platform": { "os": "linux", "arch": "amd64", "java": "21.0.5" }
}
```

---

### `GET /api/v1/discovery`

All servers visible to this instance, with ownership state. This is the endpoint `mcsw` uses to render "what's where, who has it."

**Scope:** `discovery:read`

**Query params:**
- `state` — filter by ownership state (`free | owned-self | owned-other | stale`), repeatable.
- `type` — filter by server flavor (`paper | vanilla | fabric | forge`), repeatable.

**Response 200:**

```json
{
  "scanned_at": "2026-05-02T12:00:00Z",
  "servers": [
    {
      "id": "01890b6f-9c8d-7c2a-9b3a-1b2c3d4e5f60",
      "name": "Survival 2024",
      "type": "paper",
      "version": "1.21.4",
      "path": "/mnt/servers/survival-2024",
      "discovered_root": "/mnt/servers",
      "level_name": "world",
      "icon_b64": null,
      "ownership": {
        "state": "owned-self",
        "instance": "node-a",
        "slot": "creative",
        "host": "192.168.1.10",
        "pid": 4711,
        "since": "2026-05-02T08:14:01Z",
        "heartbeat": "2026-05-02T11:59:54Z"
      }
    },
    {
      "id": "01890b70-...",
      "name": "Skyblock",
      "type": "fabric",
      "ownership": { "state": "free" }
    }
  ]
}
```

---

### `POST /api/v1/discovery/refresh`

Force a re-scan of all `discovery.roots`. Cheap; safe to call frequently. Returns the same shape as `GET /discovery`.

**Scope:** `discovery:write`

---

### `DELETE /api/v1/discovery/{server_id}/lock`

Release a lock. Only allowed when the lock is `owned-self` (normal release happens automatically on stop) or `stale`. To steal a stale lock from another instance, pass `?force=true`.

**Scope:** `discovery:write`

**Errors:** `409 lock_held` if the lock is `owned-other` and not stale.

---

## 4. Slots

### `GET /api/v1/slots`

List all slots configured on this instance.

**Scope:** `slot:read`

**Response 200:**

```json
{
  "slots": [
    {
      "name": "creative",
      "port": 25565,
      "public_address": "mc.example.com",
      "state": "running",
      "state_since": "2026-05-02T08:14:30Z",
      "mounted_server_id": "01890b6f-...",
      "auto_mount": null
    },
    {
      "name": "survival",
      "port": 25566,
      "state": "idle",
      "mounted_server_id": null
    }
  ]
}
```

---

### `GET /api/v1/slots/{name}`

Detailed view of one slot, including the mounted server's runtime state.

**Scope:** `slot:read`

**Response 200:**

```json
{
  "name": "creative",
  "port": 25565,
  "public_address": "mc.example.com",
  "state": "running",
  "state_since": "2026-05-02T08:14:30Z",
  "mounted_server": {
    "id": "01890b6f-...",
    "name": "Survival 2024",
    "type": "paper",
    "version": "1.21.4",
    "path": "/mnt/servers/survival-2024",
    "pid": 4711,
    "started_at": "2026-05-02T08:14:30Z",
    "rcon_connected": true,
    "slp": {
      "online": true,
      "players": { "online": 3, "max": 20 },
      "tps_1m": 19.98,
      "motd": "A Minecraft Server",
      "latency_ms": 4
    }
  },
  "last_error": null
}
```

If `state == "error"` or `crashed`, `last_error` is populated:

```json
"last_error": {
  "code": "process_exited_nonzero",
  "message": "java exited with code 137 (likely OOM)",
  "at": "2026-05-02T11:42:09Z"
}
```

---

### `POST /api/v1/slots/{name}/start`

Mount and start a server in this slot. Returns immediately with the new state (`mounting` → `starting`). Observe completion via polling or events.

**Scope:** `slot:write`

**Request:**

```json
{
  "server_id": "01890b6f-...",
  "force": false
}
```

- `force` — steal a `stale` ownership lock if present.

**Response 202:**

```json
{
  "name": "creative",
  "state": "mounting",
  "state_since": "2026-05-02T12:00:00.123Z",
  "mounted_server_id": "01890b6f-..."
}
```

**Errors:**
- `409 slot_busy` — slot not in `idle`, `crashed`, or `error`.
- `409 server_in_use` — another live instance owns this server (set `force=true` only if state is `stale`).
- `404 server_not_found` — id not in discovery.
- `400 server_incompatible` — server's port preference / type doesn't match slot constraints (see [§7 Slot constraints](#7-slot-constraints)).

---

### `POST /api/v1/slots/{name}/stop`

Graceful shutdown. Server is told to save & stop via RCON; if `graceful_seconds` elapses without exit, java is SIGTERM'd; another `kill_grace` later, SIGKILL.

**Scope:** `slot:write`

**Request:**

```json
{
  "graceful_seconds": 30,
  "broadcast_every": 10,
  "broadcast_template": "Server shutting down in {remaining}",
  "kill_grace": "10s"
}
```

All fields optional; defaults from per-server config.

**Response 202:**

```json
{ "name": "creative", "state": "stopping", "state_since": "..." }
```

---

### `POST /api/v1/slots/{name}/restart`

Equivalent to `stop` then `start` with the same `server_id`. Same request shape as `stop`.

**Scope:** `slot:write`

---

### `POST /api/v1/slots/{name}/abort-stop`

Cancel an in-progress graceful shutdown if the timer hasn't fired yet.

**Scope:** `slot:write`

**Response 200:** slot detail (state should return to `running`).

**Errors:** `409 not_stopping`.

---

### `WS /api/v1/slots/{name}/events`

Subscribe to slot state transitions and lifecycle events.

**Scope:** `slot:read`

**Frames (server → client), one JSON per text frame:**

```json
{ "type": "state", "from": "starting", "to": "running", "at": "..." }
{ "type": "player_join", "player": "Steve", "at": "..." }
{ "type": "player_leave", "player": "Steve", "at": "..." }
{ "type": "player_death", "player": "Steve", "killer": "Zombie", "cause": "mob",
  "message": "Steve was slain by Zombie", "at": "..." }
{ "type": "player_kick", "player": "Steve", "reason": "flying", "at": "..." }
{ "type": "chat", "player": "Steve", "message": "hello world", "at": "..." }
{ "type": "tps_sample", "tps_1m": 19.4, "tps_5m": 19.7, "at": "..." }
{ "type": "error", "code": "rcon_disconnected", "message": "...", "at": "..." }
```

Client may send `{ "type": "ping" }` to keep idle connections alive (server replies with `pong`).

---

## 5. Mounted server — control & introspection

All endpoints below require the slot's `state == running`. Otherwise they return `409 server_not_running`.

### `POST /api/v1/slots/{name}/server/command`

Raw RCON passthrough. Use sparingly — prefer typed endpoints.

**Scope:** `server:command`

**Request:**

```json
{ "command": "weather clear" }
```

**Response 200:**

```json
{ "command": "weather clear", "response": "Set the weather to clear", "elapsed_ms": 4 }
```

---

### `POST /api/v1/slots/{name}/server/say`

Broadcast a `say` to all players. Equivalent to `/say <msg>` in console.

**Scope:** `server:command`

**Request:** `{ "message": "Server restarting in 5 minutes" }`

---

### `GET /api/v1/slots/{name}/server/players`

Live player list (from RCON `list` + SLP).

**Scope:** `server:read`

**Response 200:**

```json
{
  "online": 3,
  "max": 20,
  "players": [
    { "name": "Steve", "uuid": "...", "ping_ms": 42, "joined_at": "..." },
    { "name": "Alex",  "uuid": "...", "ping_ms": 88, "joined_at": "..." }
  ]
}
```

---

### `POST /api/v1/slots/{name}/server/players/{player}/kick`

**Scope:** `server:moderate`

**Request:** `{ "reason": "AFK" }`

**Errors:** `404 player_not_online`.

---

### `POST /api/v1/slots/{name}/server/players/{player}/ban`

**Scope:** `server:moderate`

**Request:**

```json
{ "reason": "Griefing", "duration": "7d", "ban_ip": false }
```

`duration` is optional; omitted means permanent. Format: `"30m"`, `"24h"`, `"7d"`, `"4w"`.

If `ban_ip: true`, also bans the player's last-known IP.

---

### `POST /api/v1/slots/{name}/server/players/{player}/unban`

**Scope:** `server:moderate`

---

### `POST /api/v1/slots/{name}/server/players/{player}/op`

**Scope:** `server:admin`

**Request:** `{ "level": 4 }` — optional, defaults to 4.

---

### `POST /api/v1/slots/{name}/server/players/{player}/deop`

**Scope:** `server:admin`

---

### Whitelist

```
GET    /api/v1/slots/{name}/server/whitelist
PUT    /api/v1/slots/{name}/server/whitelist/{player}
DELETE /api/v1/slots/{name}/server/whitelist/{player}
POST   /api/v1/slots/{name}/server/whitelist/reload
```

`GET` response:

```json
{ "enabled": true, "players": [{ "name": "Steve", "uuid": "..." }] }
```

`scope: server:admin` for mutations, `server:read` for `GET`.

---

### Banlist

```
GET    /api/v1/slots/{name}/server/banlist
GET    /api/v1/slots/{name}/server/banlist/ips
```

**Scope:** `server:read`

```json
{
  "players": [
    {
      "name": "Bob",
      "uuid": "...",
      "reason": "Griefing",
      "source": "Console",
      "created": "2026-04-30T...",
      "expires": "2026-05-07T..."
    }
  ]
}
```

---

### `GET /api/v1/slots/{name}/server/properties`

Parsed `server.properties`.

**Scope:** `server:read`

**Response 200:**

```json
{
  "values": {
    "motd": "A Minecraft Server",
    "max-players": 20,
    "difficulty": "normal",
    "view-distance": 10
  },
  "raw_path": "/mnt/servers/survival-2024/server.properties"
}
```

---

### `PATCH /api/v1/slots/{name}/server/properties`

Partial update. Changes are written to `server.properties` immediately but only take effect on next restart (a few keys can be hot-reloaded — see `requires_restart` in the response).

**Scope:** `server:admin`

**Request:**

```json
{ "values": { "motd": "Welcome!", "view-distance": 12 } }
```

**Response 200:**

```json
{
  "applied": ["motd", "view-distance"],
  "requires_restart": ["view-distance"],
  "rejected": []
}
```

Reserved keys MCSM manages itself (`server-port`, `query.port`, `rcon.*`) cannot be set this way; they're returned in `rejected`.

---

## 6. Logs

### `GET /api/v1/slots/{name}/server/logs`

Historical tail.

**Scope:** `server:read`

**Query:**
- `tail` — last N lines (default 200, max 5000). Mutually exclusive with `since`.
- `since` — ISO 8601 timestamp; return entries after this point.
- `level` — repeatable filter (`info | warn | error`).
- `format` — `parsed` (default) or `raw`.

**Response 200:**

```json
{
  "entries": [
    {
      "ts": "2026-05-02T12:00:01.234Z",
      "thread": "Server thread",
      "level": "INFO",
      "source": null,
      "message": "Steve joined the game",
      "raw": "[12:00:01] [Server thread/INFO]: Steve joined the game"
    }
  ],
  "truncated": false
}
```

---

### `WS /api/v1/slots/{name}/server/logs/stream`

Live tail. Frames are `LogEntry` objects (same shape as above).

**Scope:** `server:read`

**Query at connect:**
- `tail` — initial backfill (default 0).
- `since` — backfill from a cursor.
- `level` — filter.

**Client → server frames:**

```json
{ "type": "filter", "level": ["warn", "error"] }
{ "type": "ping" }
```

Reconnect with `?since=<last-entry-ts>` to resume without gaps.

---

## 7. Slot constraints

A slot can declare requirements that constrain which servers it'll accept:

```yaml
# config.yaml
slots:
  - name: creative
    port: 25565
    accepts:
      types: [paper, vanilla]      # default: any
      max_memory_mb: 8192
```

`POST /slots/{name}/start` rejects with `400 server_incompatible` if the chosen server's declared `java.args -Xmx` exceeds `max_memory_mb`, or `type` isn't in `accepts.types`. Discoverable up-front:

### `GET /api/v1/slots/{name}/compatible-servers`

**Scope:** `slot:read`

Returns the subset of `GET /discovery` that this slot can mount. Useful to populate a dropdown in `mcsw`.

---

## 8. Backups, system, audit, metrics

### Backups

```
GET    /api/v1/slots/{name}/server/backups
POST   /api/v1/slots/{name}/server/backups
GET    /api/v1/slots/{name}/server/backups/{id}
POST   /api/v1/slots/{name}/server/backups/{id}/restore
DELETE /api/v1/slots/{name}/server/backups/{id}
```

`POST /backups` body:

```json
{ "label": "before-update", "stop_server": true, "include_logs": false }
```

If `stop_server: true`, slot transitions `running → stopping → idle → mounting → starting → running` around the snapshot. Otherwise an online snapshot is taken (RCON `save-off` + flush + copy + `save-on`).

**Scope:** `server:admin`

---

### Updates

```
GET  /api/v1/slots/{name}/server/update?mc_version=1.21.4
POST /api/v1/slots/{name}/server/update
```

`GET` returns the latest available release for the server's flavor:

```json
{
  "flavor": "paper", "mc_version": "1.21.4", "build": 50,
  "download_url": "https://api.papermc.io/.../paper-1.21.4-50.jar",
  "sha256": "0123abcd...", "jar_name": "paper-1.21.4-50.jar",
  "published_at": "2026-04-30T..."
}
```

`POST` (slot must be **stopped first**):

```json
{ "mc_version": "1.21.4", "backup": true }
```

When `backup: true`, an offline backup with label `pre-update` is taken before the swap. The previous jar is preserved as `paper.jar.previous` next to the new one.

**Scope:** `server:admin`

**Errors:** `409 slot_busy` if the slot isn't terminal; `502` for upstream PaperMC failures or sha256 mismatches.

> Currently supports `flavor=paper` only. Vanilla / Fabric / Forge return `400 validation_failed`.

---

### System

```
GET /api/v1/system/temperature        # last sample + recent history
GET /api/v1/system/resources          # cpu, mem, disk, load
```

**Scope:** `system:read`

```json
{
  "celsius": 62.4,
  "sensor": "/sys/class/thermal/thermal_zone0/temp",
  "sampled_at": "2026-05-02T12:00:00Z",
  "policy_band": "moderate",
  "history": [
    { "at": "2026-05-02T11:55:00Z", "celsius": 60.1 },
    { "at": "2026-05-02T11:56:00Z", "celsius": 61.0 }
  ]
}
```

---

### Audit log

```
GET /api/v1/audit
```

**Scope:** `audit:read`

**Query:** `since`, `until`, `actor`, `kind`, `limit` (max 1000), `cursor`.

```json
{
  "entries": [
    {
      "id": 9123,
      "at": "2026-05-02T12:00:00Z",
      "actor": { "kind": "token", "name": "mcsw" },
      "kind": "slot.start",
      "subject": { "slot": "creative", "server_id": "01890b6f-..." },
      "result": "ok"
    }
  ],
  "next_cursor": "9123"
}
```

Audited events: token auth (success/fail), slot lifecycle, player moderation, properties changes, backup/restore, lock steals, config reloads, peer reachability changes.

---

### Metrics

```
GET /metrics                  # Prometheus exposition
```

By default unauthenticated (Prometheus scrapers usually live in a private network). Set `metrics.require_auth: true` to require a bearer token with `metrics:read` scope.

Series exported (labels in `{}`):

```
mcsm_instance_info{name, version}                                  gauge (always 1)
mcsm_slot_state{slot, state}                                       gauge (1 for current state)
mcsm_slot_uptime_seconds{slot}                                     gauge
mcsm_server_players_online{slot, server_id}                        gauge
mcsm_server_players_max{slot, server_id}                           gauge
mcsm_server_tps{slot, server_id, window="1m|5m|15m"}               gauge
mcsm_server_memory_bytes{slot, server_id}                          gauge
mcsm_server_cpu_seconds_total{slot, server_id}                     counter
mcsm_server_restart_total{slot, reason}                            counter
mcsm_rcon_command_duration_seconds{slot, command}                  histogram
mcsm_lock_steal_total{server_id}                                   counter
mcsm_system_temperature_celsius{sensor}                            gauge
mcsm_api_request_duration_seconds{route, method, status}           histogram
mcsm_peer_reachable{peer_name}                                     gauge (1/0)
mcsm_peer_rtt_seconds{peer_name}                                   gauge
```

---

## 9. Peers & federation

Each MCSM instance can be configured with a list of **peer instances** — other MCSMs that may share discovery roots or simply belong to the same logical fleet. This replaces ad-hoc network scanning (e.g. `mcsw`'s former `evilscan` sweep) with explicit, authenticated peer relationships.

### 9.1 Configuration

```yaml
peers:
  - name: node-b
    url: https://node-b.internal:8124
    token: ${MCSM_PEER_TOKEN_NODE_B}
  - name: node-c
    url: https://node-c.internal:8124
    token: ${MCSM_PEER_TOKEN_NODE_C}
  ping_interval: 30s
  timeout: 5s
```

Peer relationships are **symmetric in protocol but configured independently** — A listing B as a peer doesn't auto-add A on B. Each instance pings its configured peers every `ping_interval` and tracks reachability + version.

The `token` should be a peer-scope token issued by the remote (`scopes: ["instance:read", "discovery:read", "slot:read", "server:read"]` is sufficient for read-only federation).

### 9.2 Endpoints

#### `GET /api/v1/peers`

**Scope:** `peer:read`

```json
{
  "peers": [
    {
      "name": "node-b",
      "url": "https://node-b.internal:8124",
      "reachable": true,
      "last_seen": "2026-05-02T11:59:58Z",
      "rtt_ms": 12,
      "remote_version": "2.0.0",
      "remote_name": "node-b"
    },
    {
      "name": "node-c",
      "url": "https://node-c.internal:8124",
      "reachable": false,
      "last_error": "dial tcp: i/o timeout",
      "last_attempt": "2026-05-02T12:00:00Z"
    }
  ]
}
```

#### `GET /api/v1/federation/discovery`

**Scope:** `discovery:read` + `peer:read`

Aggregates `GET /api/v1/discovery` from this instance and all reachable peers, merged. Servers reported by multiple peers are deduplicated by `id`. Ownership conflicts (two peers both claim ownership) are resolved in favor of the freshest heartbeat; the loser is downgraded to `stale` in the response with a `conflict_with` field.

```json
{
  "scanned_at": "2026-05-02T12:00:00Z",
  "sources": [
    { "instance": "node-a", "self": true,  "ok": true },
    { "instance": "node-b", "self": false, "ok": true,  "rtt_ms": 12 },
    { "instance": "node-c", "self": false, "ok": false, "error": "dial tcp: i/o timeout" }
  ],
  "servers": [
    {
      "id": "01890b6f-...",
      "name": "Survival 2024",
      "type": "paper",
      "version": "1.21.4",
      "ownership": {
        "state": "owned-other",
        "instance": "node-b",
        "slot": "creative",
        "host": "10.0.0.21",
        "since": "2026-05-02T08:14:01Z",
        "heartbeat": "2026-05-02T11:59:54Z"
      },
      "reachable_via": "node-b"
    }
  ]
}
```

#### `GET /api/v1/federation/slots`

**Scope:** `slot:read` + `peer:read`

Aggregates `GET /api/v1/slots` from self + peers. Each slot is decorated with `instance` so the client knows where to address mutations.

```json
{
  "sources": [ { "instance": "node-a", "self": true, "ok": true }, ... ],
  "slots": [
    { "instance": "node-a", "name": "creative", "port": 25565, "state": "running", "mounted_server_id": "..." },
    { "instance": "node-b", "name": "creative", "port": 25565, "state": "idle",    "mounted_server_id": null }
  ]
}
```

> **Federation endpoints are read-only by design.** Mutations always go directly to the owning instance — `mcsw` reads `instance` off the federation response and addresses the right URL.

#### `POST /api/v1/peers/refresh`

**Scope:** `peer:read`

Force an immediate ping of all configured peers; returns the same shape as `GET /peers`.

---

## 10. Error catalog

| HTTP | code                     | When                                                              |
| ---- | ------------------------ | ----------------------------------------------------------------- |
| 400  | `bad_request`            | Malformed JSON, missing field.                                    |
| 400  | `validation_failed`      | Field present but invalid; `details` lists field-level errors.    |
| 400  | `server_incompatible`    | Server doesn't satisfy slot constraints.                          |
| 401  | `missing_token`          | No `Authorization` header.                                        |
| 401  | `invalid_token`          | Token doesn't match any configured token.                         |
| 403  | `scope_denied`           | Token lacks required scope.                                       |
| 404  | `slot_not_found`         | Slot name not configured.                                         |
| 404  | `server_not_found`       | Server id not in discovery.                                       |
| 404  | `player_not_online`      | Targeted player isn't online.                                     |
| 404  | `backup_not_found`       | Backup id unknown.                                                |
| 404  | `peer_not_found`         | Peer name not configured.                                         |
| 409  | `slot_busy`              | Slot not in a state that accepts the requested transition.        |
| 409  | `server_not_running`     | Endpoint requires `state == running`.                             |
| 409  | `server_in_use`          | Another live instance owns the requested server.                  |
| 409  | `lock_held`              | Lock is `owned-other` and not stale; need `force=true` (if stale). |
| 409  | `not_stopping`           | `abort-stop` called when slot isn't in `stopping`.                |
| 409  | `peer_conflict`          | Federation detected duplicate `instance` names.                   |
| 423  | `instance_locked`        | Instance is in maintenance mode (returned by every mutating endpoint). |
| 429  | `rate_limited`           | Token exceeded its rate budget; `Retry-After` header set.         |
| 500  | `internal`               | Unexpected; correlation id in `details.trace_id`.                 |
| 502  | `rcon_unreachable`       | Server is allegedly running but RCON is not responding.           |
| 502  | `peer_unreachable`       | Federation endpoint couldn't reach a peer (partial result returned with `sources[].ok=false`). |
| 503  | `not_ready`              | Instance still initializing (during boot).                        |
| 504  | `command_timeout`        | RCON command exceeded the per-command deadline.                   |

---

## 11. Health & meta

```
GET /healthz       # liveness; always 200 if process is up
GET /readyz        # readiness; 200 once config loaded, lock dir writable, all configured slots resolved
GET /openapi.json  # generated OpenAPI 3.1 spec
GET /version       # { "version": "...", "commit": "...", "date": "..." }
```

`/healthz` and `/readyz` are unauthenticated. `/version` and `/openapi.json` are unauthenticated by default (`api.public_meta: false` to require auth).

---

## 12. Scope reference

| Scope             | Grants                                                              |
| ----------------- | ------------------------------------------------------------------- |
| `*`               | Everything.                                                         |
| `instance:read`   | `GET /instance`, `/version`.                                        |
| `discovery:read`  | `GET /discovery`, `GET /slots/{n}/compatible-servers`, `GET /federation/discovery` (with `peer:read`). |
| `discovery:write` | `POST /discovery/refresh`, `DELETE /discovery/{id}/lock`.           |
| `slot:read`       | All `GET /slots/...`, slot event WebSocket, `GET /federation/slots` (with `peer:read`). |
| `slot:write`      | `start`, `stop`, `restart`, `abort-stop`.                           |
| `server:read`     | Players, properties, banlist, whitelist (read), logs, log stream.   |
| `server:command`  | `command`, `say`.                                                   |
| `server:moderate` | `kick`, `ban`, `unban`.                                             |
| `server:admin`    | `op`, `deop`, whitelist mutations, properties PATCH, backups.       |
| `system:read`     | `/system/*`.                                                        |
| `audit:read`      | `GET /audit`.                                                       |
| `metrics:read`    | `GET /metrics` (only when `metrics.require_auth` is on).            |
| `peer:read`       | `GET /peers`, `POST /peers/refresh`, federation endpoints.          |

A typical `mcsw` token gets `*`. A "peer" token (used for federation) gets `instance:read discovery:read slot:read server:read peer:read`. A "moderator" token might get `slot:read server:read server:command server:moderate`. A Prometheus scraper gets `metrics:read` (or no auth at all if `require_auth` is off).

---

## 13. Worked example — full mount cycle

```bash
TOKEN="..."
H="Authorization: Bearer $TOKEN"

# 1. Federated view: what's running across all my MCSMs?
curl -s -H "$H" https://mcsm-a/api/v1/federation/discovery \
  | jq '.servers[] | {id, name, on: .ownership.instance, slot: .ownership.slot}'

# 2. Mount on the right instance — say the server is "free" and we want it on node-a slot 'creative'
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
  https://mcsm-a/api/v1/slots/creative/start \
  -d '{"server_id":"01890b6f-9c8d-7c2a-9b3a-1b2c3d4e5f60"}'
# → 202 { "state": "mounting", ... }

# 3. Watch it come up (poll or WS)
curl -s -H "$H" https://mcsm-a/api/v1/slots/creative | jq '.state'
# "mounting" → "starting" → "running"

# 4. Operate
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
  https://mcsm-a/api/v1/slots/creative/server/say \
  -d '{"message":"Server is up!"}'

curl -s -H "$H" https://mcsm-a/api/v1/slots/creative/server/players

# 5. Stream logs
websocat -H "$H" wss://mcsm-a/api/v1/slots/creative/server/logs/stream?tail=50

# 6. Graceful shutdown
curl -s -X POST -H "$H" -H "Content-Type: application/json" \
  https://mcsm-a/api/v1/slots/creative/stop \
  -d '{"graceful_seconds":30,"broadcast_every":10}'
```
