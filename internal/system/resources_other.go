//go:build !linux

package system

import "runtime"

// SampleResources on non-Linux platforms returns just CPU core count.
// Production target is Linux; this stub keeps `go build` happy on
// developer macOS boxes without dragging in gopsutil.
func SampleResources(dataDir string) (Resources, error) {
	return Resources{CPU: CPU{Cores: runtime.NumCPU()}}, nil
}
