package serverid

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Flavor strings used in API responses and config. Keep in sync with
// docs/api.md (filterable on `type`).
const (
	FlavorPaper   = "paper"
	FlavorVanilla = "vanilla"
	FlavorFabric  = "fabric"
	FlavorForge   = "forge"
	FlavorUnknown = "unknown"
)

// DetectFlavor inspects the contents of a server directory and returns
// the most specific flavor it can identify. Returns FlavorUnknown only
// if the directory is clearly not a Minecraft server.
//
// Detection precedence:
//
//   1. Fabric:   fabric-server-launch.jar present
//   2. Forge:    a forge-*-(installer|universal).jar present, OR
//                "libraries/net/minecraftforge/forge" directory exists
//   3. Paper:    paper.jar OR paper-*.jar OR version_history.json
//   4. Vanilla:  server.jar present (and none of the above matched)
func DetectFlavor(serverDir string) (string, error) {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return "", err
	}
	hasFabric, hasForgeJar, hasPaperJar, hasPaperVH, hasServerJar := false, false, false, false, false
	for _, e := range entries {
		name := e.Name()
		switch {
		case name == "fabric-server-launch.jar":
			hasFabric = true
		case strings.HasPrefix(name, "forge-") && strings.HasSuffix(name, ".jar"):
			hasForgeJar = true
		case name == "paper.jar":
			hasPaperJar = true
		case strings.HasPrefix(name, "paper-") && strings.HasSuffix(name, ".jar"):
			hasPaperJar = true
		case name == "version_history.json":
			hasPaperVH = true
		case name == "server.jar":
			hasServerJar = true
		}
	}
	if hasFabric {
		return FlavorFabric, nil
	}
	if hasForgeJar || dirExists(filepath.Join(serverDir, "libraries", "net", "minecraftforge", "forge")) {
		return FlavorForge, nil
	}
	if hasPaperJar || hasPaperVH {
		return FlavorPaper, nil
	}
	if hasServerJar {
		return FlavorVanilla, nil
	}
	return FlavorUnknown, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	return st.IsDir()
}

// IsLikelyServerDir is a cheap pre-check that returns true if a directory
// looks like it might be a Minecraft server. Used to skip non-server
// directories during discovery without parsing their .mcsm/config.yaml.
//
// "Likely" means: contains either server.properties, OR an already-
// initialized .mcsm/config.yaml.
func IsLikelyServerDir(serverDir string) bool {
	if _, err := os.Stat(filepath.Join(serverDir, "server.properties")); err == nil {
		return true
	}
	if _, err := os.Stat(ConfigPath(serverDir)); err == nil {
		return true
	}
	return false
}

// ErrUnknownFlavor signals that a directory looked server-like but didn't
// match any known flavor heuristic.
var ErrUnknownFlavor = errors.New("unable to identify minecraft server flavor")
