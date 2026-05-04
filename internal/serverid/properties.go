// Package serverid handles per-server identity and config — reading and
// writing <server-dir>/.mcsm/config.yaml, parsing server.properties, and
// detecting the server flavor from on-disk artifacts.
package serverid

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Properties is a parsed Java .properties file. Comments and blank lines
// are stripped; we don't preserve order or formatting because we never
// rewrite this file (mcsm's PATCH endpoint will do a careful round-trip,
// but that's a different code path).
type Properties map[string]string

// ReadProperties parses a Java .properties file. Handles:
//   - # and ! line comments
//   - = and : as key/value separators
//   - leading whitespace before keys
//   - trailing whitespace stripping on keys
//
// Does NOT handle Java's full unicode-escape format; Minecraft only ever
// writes ASCII into server.properties so this is sufficient.
func ReadProperties(path string) (Properties, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := Properties{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), " \t")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// Find the earliest unescaped '=' or ':'.
		sep := -1
		for i := 0; i < len(line); i++ {
			c := line[i]
			if (c == '=' || c == ':') && (i == 0 || line[i-1] != '\\') {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		key := strings.TrimRight(line[:sep], " \t")
		val := strings.TrimLeft(line[sep+1:], " \t")
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}
