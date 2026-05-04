# mcsm — Minecraft Server Monitor

A lightweight daemon that discovers, supervises, and exposes a REST API over Minecraft servers running on a host. Designed to run on bare metal, in LXC, or in a minimal Docker image, and to coordinate across multiple instances that share a server pool.

> **v2 rewrite** on this branch. The legacy Node.js implementation lives on `master` for reference. The wire contract is documented in [`docs/api.md`](docs/api.md).

## Status

| Phase | Scope                                                                                                | State |
| ----- | ---------------------------------------------------------------------------------------------------- | ----- |
| 1     | Foundation: config, auth, API server, instance/health endpoints, Dockerfile, systemd                 | ✅    |
| 2A/B  | Discovery, ownership lock, slot manager, RCON, process supervisor, SLP                                | ✅    |
| 2C    | Logs, properties (GET+PATCH), whitelist/banlist, compatible-servers, /system/temperature, abort-stop | ✅    |
| 2D    | WebSocket (logs/events), audit log, Prometheus metrics, peer pool, federation, /system/resources     | ✅    |
| 3     | Backups, Paper auto-update, Discord bots, anti-toxicity / death feed / ban-flying, OpenAPI gen       | ✅    |

Every documented v1 endpoint is real. Test coverage across 16 packages.

## Install

### Bare host / LXC — one interactive script

```sh
git clone https://github.com/MertDalbudak/mcsm
cd mcsm
sudo ./deploy/install/setup.sh
```

The script:

1. Auto-detects your distro family (Alpine, Debian/Ubuntu, Arch, Fedora/RHEL, openSUSE — and common derivatives via `ID_LIKE`).
2. Auto-detects the init system (systemd or OpenRC).
3. Installs build + runtime deps (Go ≥ 1.23 via `GOTOOLCHAIN=auto` if your system Go is older, JDK 21).
4. Builds the binaries (`mcsm`, `mcsm-tokens`) with CGO disabled — fully static.
5. **Prompts you** for the basics: instance name, discovery root, one slot (name + port + optional public address), and CPU temperature sensor.
6. **Generates a bearer token**, prints the plaintext one time, writes the argon2id hash into `/etc/mcsm/config.yaml`.
7. Drops the right service file and registers it for boot.

Pass `--no-config` to skip the wizard and just install the example config (you'll edit it later). Pass `--keep-config` to preserve an existing `/etc/mcsm/config.yaml`.

### Docker — env-file driven

```sh
cd deploy/docker
cp .env.example .env
$EDITOR .env                       # set MCSM_API_TOKEN at minimum
docker compose -f docker-compose.example.yml up -d
```

The container's entrypoint renders `/etc/mcsm/config.yaml` from `MCSM_*` env vars at startup, hashing the plaintext token from `MCSM_API_TOKEN` with `mcsm-tokens` before writing. For multi-slot setups, mount your own `config.yaml` into the container — the entrypoint respects an existing file (set `MCSM_RENDER_CONFIG=force` to override).

See [`deploy/docker/.env.example`](deploy/docker/.env.example) for every supported variable.

### Manual / dev build

```sh
make build                                              # → bin/mcsm + bin/mcsm-tokens
./bin/mcsm --config configs/config.example.yaml         # foreground
curl http://localhost:8124/healthz
```

You'll need `go`, `make`, `git`, and a JDK 21 on your `PATH` for any Minecraft server to actually launch.

## Generating tokens manually

Both flows above generate the token automatically, but if you ever need another one:

```sh
echo -n 'your-secret-token' | mcsm-tokens     # hash a token you chose
mcsm-tokens --random                          # generate + hash a random one
```

Then start (or restart) the service:

```sh
# Alpine
sudo rc-service mcsm start
sudo tail -f /var/log/mcsm/out.log

# Debian
sudo systemctl start mcsm
sudo journalctl -u mcsm -f
```

Verify:

```sh
curl http://localhost:8124/healthz
curl -H "Authorization: Bearer your-secret-token" \
     http://localhost:8124/api/v1/instance
```

## Layout

```
cmd/
  mcsm/             daemon entrypoint
  mcsm-tokens/      argon2id token-hash generator
  fake-mc-server/   test fixture (SLP+RCON over TCP)
  probe-fake/       dev tool: hits SLP+RCON without booting mcsm
  probe-ws/         WebSocket smoke probe
internal/
  api/              HTTP server, middleware, auth, routes, handlers
  audit/            JSONL append-only audit log + cursor pagination
  backup/           ZIP archive snapshots + restore
  buildinfo/        ldflags-injected version/commit/date
  config/           YAML loader + validation + defaults
  discord/          per-server bot (slash commands + notifications)
  discovery/        scan + classify ownership + background refresh
  events/           typed slot event bus
  gameplay/         death/chat/flying parsers + toxicity matcher
  ids/              UUIDv7, trace ids
  lock/             cross-instance owner.json + heartbeat
  logging/          slog setup
  logtail/          fsnotify-based tailer + parser + broadcaster
  metrics/          Prometheus text-format emitter
  openapi/          generated OpenAPI 3.1 doc
  peers/            peer client + ping pool
  process/          java supervisor with SIGTERM→SIGKILL escalation
  rcon/             Source RCON client
  serverid/         per-server config + flavor + launch planning
  slot/             state machine: idle → mounting → running → stopping
  slp/              minimal Server List Ping client
  system/           CPU temperature + (Linux) /proc resources
  update/           PaperMC v2 API client + sha256-verified install
configs/
  config.example.yaml
deploy/
  docker/           Dockerfile + compose example
  install/          setup-alpine.sh, setup-debian.sh
  openrc/           Alpine init script
  systemd/          Debian/Ubuntu service unit
docs/
  api.md            REST API + WebSocket contract (v1)
```

## Tests

```sh
make test          # all packages
go test -race ./...
```

## Documentation

- [`docs/api.md`](docs/api.md) — full REST + WebSocket specification (v1)

## License

ISC
