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

### Any Linux distro (one script)

```sh
git clone https://github.com/MertDalbudak/mcsm
cd mcsm
sudo ./deploy/install/setup.sh
```

The script auto-detects your distro family (Alpine, Debian/Ubuntu, Arch, Fedora/RHEL, openSUSE) and init system (systemd or OpenRC), installs build + runtime deps (Go, make, JDK 21), builds the binaries with CGO disabled (fully static), drops the right service file, and registers it for boot.

Supports the common derivatives via `ID_LIKE` in `/etc/os-release` — Manjaro, Mint, Pop, Rocky, AlmaLinux, etc. all work without modification.

### Docker

```sh
make docker
docker run --rm -p 8124:8124 \
  -v $PWD/configs:/etc/mcsm:ro \
  -v /mnt/servers:/mnt/servers \
  mcsm:latest
```

### Manual / dev build

If you don't want to install as a service:

```sh
make build                                              # → bin/mcsm + bin/mcsm-tokens
./bin/mcsm --config configs/config.example.yaml         # foreground
curl http://localhost:8124/healthz
```

You'll need `go`, `make`, `git`, and a JDK 21 on your `PATH` for any Minecraft server to actually launch.

## After install — generate a token

The example config ships with an unusable placeholder hash. Generate a real one:

```sh
# Pipe a secret you choose:
echo -n 'your-secret-token' | mcsm-tokens

# Or have one generated for you (prints both plaintext and hash):
mcsm-tokens --random
```

Paste the `$argon2id$v=19$...` line into `api.tokens[0].hash` in `/etc/mcsm/config.yaml`. The plaintext goes to your client (`mcsw` or `curl`).

Then start the service:

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
