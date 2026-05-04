package api

import "github.com/MertDalbudak/mcsm/internal/serverid"

// readPropsAt is a small shim used by handlers that don't want to
// import serverid directly. Returns an empty map on read errors so
// callers can ignore missing files.
func readPropsAt(dir string) (map[string]string, error) {
	props, err := serverid.ReadProperties(serverid.PropertiesPath(dir))
	if err != nil {
		return map[string]string{}, err
	}
	return props, nil
}
