package api

import (
	"fmt"
	"net/http"
)

// notImplemented returns a placeholder handler that responds 501 with a
// pointer to the documented endpoint. Used during the phased rewrite so
// every route in the spec is reachable from the start.
func notImplemented(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusNotImplemented, CodeNotImplemented,
			fmt.Sprintf("endpoint %s is not implemented in this build", endpoint),
			map[string]any{
				"endpoint": endpoint,
				"see":      "docs/api.md",
			},
		)
	}
}
