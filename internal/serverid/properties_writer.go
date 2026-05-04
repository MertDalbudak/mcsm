package serverid

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PropertiesPath returns the absolute server.properties path for a
// server directory.
func PropertiesPath(serverDir string) string {
	return filepath.Join(serverDir, "server.properties")
}

// PatchProperties rewrites server.properties so that the supplied
// key/value pairs are present, while preserving every other line as-is
// (comments, blank lines, ordering, foreign keys). Keys not yet in the
// file are appended at the end.
//
// This is the path used to inject mcsm-managed values (server-port,
// query.port, enable-rcon, rcon.port, rcon.password) at slot mount.
//
// Atomic via temp-file + rename in the same directory.
func PatchProperties(serverDir string, patch map[string]string) error {
	path := PropertiesPath(serverDir)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var lines []string
	written := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		raw := sc.Text()
		trim := strings.TrimLeft(raw, " \t")
		if trim == "" || trim[0] == '#' || trim[0] == '!' {
			lines = append(lines, raw)
			continue
		}
		// Find separator.
		sep := -1
		for i := 0; i < len(trim); i++ {
			c := trim[i]
			if (c == '=' || c == ':') && (i == 0 || trim[i-1] != '\\') {
				sep = i
				break
			}
		}
		if sep < 0 {
			lines = append(lines, raw)
			continue
		}
		key := strings.TrimRight(trim[:sep], " \t")
		if newVal, ok := patch[key]; ok {
			// Replace the value, keep the original separator character.
			sepChar := trim[sep]
			lines = append(lines, fmt.Sprintf("%s%c%s", key, sepChar, newVal))
			written[key] = true
		} else {
			lines = append(lines, raw)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Append keys that weren't in the original file.
	for k, v := range patch {
		if !written[k] {
			lines = append(lines, fmt.Sprintf("%s=%s", k, v))
		}
	}

	body := strings.Join(lines, "\n") + "\n"
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".server.properties.tmp.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
