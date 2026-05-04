package serverid

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LaunchPlan is what the slot manager hands to the process supervisor to
// spawn java. Command/Args are concrete; Dir is the server directory.
type LaunchPlan struct {
	Dir     string
	Command string
	Args    []string
}

// DefaultJVMArgs is Aikar's flags trimmed to the entries that are safe
// for arbitrary heap sizes. Used when .mcsm/config.yaml has no
// java.args set.
var DefaultJVMArgs = []string{
	"-XX:+UseG1GC",
	"-XX:+ParallelRefProcEnabled",
	"-XX:MaxGCPauseMillis=200",
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+DisableExplicitGC",
	"-XX:+AlwaysPreTouch",
	"-XX:G1NewSizePercent=30",
	"-XX:G1MaxNewSizePercent=40",
	"-XX:G1HeapRegionSize=8M",
	"-XX:G1ReservePercent=20",
	"-XX:G1HeapWastePercent=5",
	"-XX:G1MixedGCCountTarget=4",
	"-XX:InitiatingHeapOccupancyPercent=15",
	"-XX:G1MixedGCLiveThresholdPercent=90",
	"-XX:G1RSetUpdatingPauseTimePercent=5",
	"-XX:SurvivorRatio=32",
	"-XX:+PerfDisableSharedMem",
	"-XX:MaxTenuringThreshold=1",
}

// PlanLaunch builds the java command for a server. Looks up the launch
// jar based on flavor and combines:
//
//	java <java.args from .mcsm/config.yaml or DefaultJVMArgs> -jar <jar> nogui
//
// Returns an error if no usable jar can be found.
func PlanLaunch(cfg *Config, serverDir string) (*LaunchPlan, error) {
	jar, err := findLaunchJar(serverDir, cfg.Type)
	if err != nil {
		return nil, err
	}
	args := cfg.Java.Args
	if len(args) == 0 {
		args = append([]string(nil), DefaultJVMArgs...)
	} else {
		args = append([]string(nil), args...)
	}
	args = append(args, "-jar", jar, "nogui")
	return &LaunchPlan{
		Dir:     serverDir,
		Command: "java",
		Args:    args,
	}, nil
}

// findLaunchJar returns the path to the launch jar for a given flavor.
// Resolution rules per flavor:
//
//   paper   — paper.jar, then paper-*.jar (lexically last == newest)
//   vanilla — server.jar
//   fabric  — fabric-server-launch.jar
//   forge   — forge-*-universal.jar (older), or fall through to server.jar
//             (modern Forge ships a run script we don't yet honor)
//
// Returned path is relative to serverDir (so cmd.Dir + cmd.Args works).
func findLaunchJar(serverDir, flavor string) (string, error) {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return "", err
	}
	switch flavor {
	case FlavorPaper:
		if exists(serverDir, "paper.jar") {
			return "paper.jar", nil
		}
		latest := latestMatching(entries, "paper-", ".jar")
		if latest != "" {
			return latest, nil
		}
		return "", fmt.Errorf("paper: no paper.jar or paper-*.jar in %s", serverDir)

	case FlavorVanilla:
		if exists(serverDir, "server.jar") {
			return "server.jar", nil
		}
		return "", fmt.Errorf("vanilla: no server.jar in %s", serverDir)

	case FlavorFabric:
		if exists(serverDir, "fabric-server-launch.jar") {
			return "fabric-server-launch.jar", nil
		}
		return "", fmt.Errorf("fabric: no fabric-server-launch.jar in %s", serverDir)

	case FlavorForge:
		latest := latestMatching(entries, "forge-", "-universal.jar")
		if latest != "" {
			return latest, nil
		}
		if exists(serverDir, "server.jar") {
			return "server.jar", nil
		}
		return "", fmt.Errorf("forge: no forge-*-universal.jar in %s (modern Forge launch scripts not yet supported)", serverDir)

	default:
		return "", fmt.Errorf("unknown flavor %q", flavor)
	}
}

func exists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// latestMatching returns the lexically-greatest filename in entries that
// has the given prefix and suffix. Lexical order on "paper-1.21.4-XYZ.jar"
// names happens to track release order well enough — newer Paper builds
// have higher build numbers in the suffix.
func latestMatching(entries []os.DirEntry, prefix, suffix string) string {
	best := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, suffix) && n > best {
			best = n
		}
	}
	return best
}
