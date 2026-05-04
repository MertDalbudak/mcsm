package openapi

import (
	"encoding/json"
	"testing"
)

func TestBuild_RoundTripsAsJSON(t *testing.T) {
	body, err := Build("9.9.9").JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["openapi"] != "3.1.0" {
		t.Errorf("openapi version: %v", back["openapi"])
	}
	info, _ := back["info"].(map[string]any)
	if info["version"] != "9.9.9" {
		t.Errorf("info.version: %v", info["version"])
	}
}

// TestBuild_AllRoutesPresent makes sure the documented surface stays in
// sync with what the router exposes. If you add a route to router.go,
// add it here too.
func TestBuild_AllRoutesPresent(t *testing.T) {
	d := Build("test")
	wantPaths := []string{
		"/healthz", "/readyz", "/version", "/openapi.json",
		"/api/v1/instance",
		"/api/v1/discovery",
		"/api/v1/discovery/refresh",
		"/api/v1/discovery/{server_id}/lock",
		"/api/v1/slots",
		"/api/v1/slots/{name}",
		"/api/v1/slots/{name}/compatible-servers",
		"/api/v1/slots/{name}/start",
		"/api/v1/slots/{name}/stop",
		"/api/v1/slots/{name}/restart",
		"/api/v1/slots/{name}/abort-stop",
		"/api/v1/slots/{name}/events",
		"/api/v1/slots/{name}/server/command",
		"/api/v1/slots/{name}/server/say",
		"/api/v1/slots/{name}/server/players",
		"/api/v1/slots/{name}/server/players/{player}/kick",
		"/api/v1/slots/{name}/server/players/{player}/ban",
		"/api/v1/slots/{name}/server/players/{player}/unban",
		"/api/v1/slots/{name}/server/players/{player}/op",
		"/api/v1/slots/{name}/server/players/{player}/deop",
		"/api/v1/slots/{name}/server/whitelist",
		"/api/v1/slots/{name}/server/whitelist/{player}",
		"/api/v1/slots/{name}/server/whitelist/reload",
		"/api/v1/slots/{name}/server/banlist",
		"/api/v1/slots/{name}/server/banlist/ips",
		"/api/v1/slots/{name}/server/properties",
		"/api/v1/slots/{name}/server/logs",
		"/api/v1/slots/{name}/server/logs/stream",
		"/api/v1/slots/{name}/server/backups",
		"/api/v1/slots/{name}/server/backups/{id}",
		"/api/v1/slots/{name}/server/backups/{id}/restore",
		"/api/v1/slots/{name}/server/update",
		"/api/v1/system/temperature",
		"/api/v1/system/resources",
		"/api/v1/audit",
		"/api/v1/peers",
		"/api/v1/peers/refresh",
		"/api/v1/federation/discovery",
		"/api/v1/federation/slots",
		"/metrics",
	}
	for _, p := range wantPaths {
		if _, ok := d.Paths[p]; !ok {
			t.Errorf("missing path %q in spec", p)
		}
	}
}

func TestBuild_BearerSecurityScheme(t *testing.T) {
	d := Build("x")
	scheme, ok := d.Components.SecuritySchemes["bearer"]
	if !ok {
		t.Fatal("bearer security scheme missing")
	}
	if scheme.Type != "http" || scheme.Scheme != "bearer" {
		t.Errorf("scheme: %+v", scheme)
	}
}

func TestBuild_AllOperationsHaveResponses(t *testing.T) {
	d := Build("x")
	for path, item := range d.Paths {
		for verb, op := range map[string]*Operation{
			"GET": item.Get, "POST": item.Post, "PUT": item.Put,
			"PATCH": item.Patch, "DELETE": item.Delete,
		} {
			if op == nil {
				continue
			}
			if len(op.Responses) == 0 {
				t.Errorf("%s %s has no responses", verb, path)
			}
		}
	}
}
