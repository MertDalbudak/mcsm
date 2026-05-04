// Package buildinfo exposes version metadata injected at link time.
//
// Build with:
//
//	go build -ldflags "-X github.com/MertDalbudak/mcsm/internal/buildinfo.Version=2.0.0 \
//	                   -X github.com/MertDalbudak/mcsm/internal/buildinfo.Commit=$(git rev-parse --short HEAD) \
//	                   -X github.com/MertDalbudak/mcsm/internal/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
package buildinfo

var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
	Date    = "unknown"
)
