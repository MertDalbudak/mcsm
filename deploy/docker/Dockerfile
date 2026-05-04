# syntax=docker/dockerfile:1.7
# ---------- build stage ----------
FROM golang:1.23-alpine AS build

WORKDIR /src

# Cache deps separately from source for fast incremental builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags "\
        -s -w \
        -X github.com/MertDalbudak/mcsm/internal/buildinfo.Version=${VERSION} \
        -X github.com/MertDalbudak/mcsm/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/MertDalbudak/mcsm/internal/buildinfo.Date=${BUILD_DATE}" \
        -o /out/mcsm ./cmd/mcsm

# ---------- runtime stage ----------
# Temurin JRE base — needed because mcsm spawns Java for each Minecraft server.
FROM eclipse-temurin:21-jre-alpine

RUN apk add --no-cache ca-certificates tzdata curl tini && \
    addgroup -g 1000 mcsm && \
    adduser -D -u 1000 -G mcsm -s /sbin/nologin mcsm && \
    mkdir -p /etc/mcsm /var/lib/mcsm && \
    chown -R mcsm:mcsm /etc/mcsm /var/lib/mcsm

COPY --from=build /out/mcsm /usr/local/bin/mcsm

USER mcsm

EXPOSE 8124
VOLUME ["/etc/mcsm", "/var/lib/mcsm"]

# tini is PID 1 so signals reach mcsm and orphaned java children are reaped.
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/mcsm"]
CMD ["--config", "/etc/mcsm/config.yaml"]
