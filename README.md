# mcsm — Minecraft Server Monitor

A lightweight daemon that discovers, supervises, and exposes a REST API over Minecraft servers running on a host. Designed to run on bare metal, in LXC, or in a minimal Docker image, and to coordinate across multiple instances that share a server pool.

> **v2 rewrite in progress** on this branch. The legacy Node.js implementation lives on `master` for reference. The wire contract is documented in [`docs/api.md`](docs/api.md).

## Status

| Phase | Scope                                                                                  | State |
| ----- | -------------------------------------------------------------------------------------- | ----- |
| 1     | Skeleton: config, auth, API server, instance/health endpoints, Dockerfile, systemd     | in progress |
| 2     | Discovery, slot manager, RCON, log tail, ownership lock, metrics, federation, audit    | next |
| 3     | Discord bots, backups, auto-update, anti-toxicity / death feed, OpenAPI generation     | later |

Endpoints not yet implemented respond with `501 not_implemented` and a pointer to the spec, so clients can be written against the full surface today.

## Quick start (local dev)

```sh
make build
make run    # starts on 0.0.0.0:8124 with configs/config.example.yaml
curl http://localhost:8124/healthz
```

You will need to fill in a real argon2id token hash before any `/api/v1/*` endpoint will work — see [Generating a token](#generating-a-token).

## Quick start (Docker)

```sh
make docker
docker run --rm -p 8124:8124 \
  -v $PWD/configs:/etc/mcsm:ro \
  -v /mnt/servers:/mnt/servers \
  mcsm:latest
```

## Generating a token

Tokens are stored as argon2id PHC hashes in `config.yaml`. Until the `mcsm tokens new` subcommand lands (Phase 2), generate with any argon2id tool, e.g.:

```sh
echo -n "your-secret-token" | argon2 "$(openssl rand 16)" -id -t 3 -m 16 -p 2 -e
```

Paste the resulting `$argon2id$v=19$...` string into `api.tokens[].hash`.

## Layout

```
cmd/mcsm/             entrypoint
internal/
  api/                HTTP server, middleware, auth, routes, handlers
  buildinfo/          ldflags-injected version/commit/date
  config/             YAML loader + validation + defaults
  ids/                UUIDv7, trace ids
  logging/            slog setup
configs/              example config files
deploy/
  docker/             Dockerfile + compose example
  systemd/            mcsm.service unit
  install/            host install scripts
docs/
  api.md              REST API contract (v1)
```

## Documentation

- [`docs/api.md`](docs/api.md) — REST API specification (v1)

## License

ISC
