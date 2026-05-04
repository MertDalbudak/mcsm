package openapi

// Build returns the canonical OpenAPI 3.1 document for mcsm v1.
// version is the build version (used for info.version).
func Build(version string) *Doc {
	d := &Doc{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:   "MCSM API",
			Version: version,
			Summary: "Minecraft Server Monitor — control plane for one or more Minecraft servers.",
		},
		Servers:  []Server{{URL: "/"}},
		Tags:     defaultTags(),
		Paths:    map[string]PathItem{},
		Security: []map[string][]string{{"bearer": {}}},
		Components: Components{
			Schemas: defaultSchemas(),
			SecuritySchemes: map[string]SecurityScheme{
				"bearer": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "opaque",
					Description:  "Argon2id-hashed at rest. See docs/api.md §1.3.",
				},
			},
		},
	}
	addMeta(d)
	addInstance(d)
	addDiscovery(d)
	addSlots(d)
	addServerControl(d)
	addLists(d)
	addProperties(d)
	addLogs(d)
	addBackups(d)
	addUpdate(d)
	addSystem(d)
	addAudit(d)
	addPeers(d)
	addFederation(d)
	addMetrics(d)
	return d
}

func defaultTags() []Tag {
	return []Tag{
		{Name: "meta", Description: "Health, version, identity"},
		{Name: "discovery", Description: "Server pool + ownership locks"},
		{Name: "slots", Description: "Per-instance slot lifecycle"},
		{Name: "server", Description: "Mounted server control via RCON"},
		{Name: "logs", Description: "Log tail (REST + WebSocket)"},
		{Name: "backups", Description: "Snapshot + restore"},
		{Name: "updates", Description: "Server-jar update flow"},
		{Name: "system", Description: "Host metrics"},
		{Name: "audit", Description: "Mutation log"},
		{Name: "federation", Description: "Cross-instance aggregation"},
		{Name: "metrics", Description: "Prometheus scrape endpoint"},
	}
}

func ref(name string) Schema { return Schema{Ref: "#/components/schemas/" + name} }

func op(tag, summary string) *Operation {
	return &Operation{
		Tags:    []string{tag},
		Summary: summary,
		Responses: map[string]Response{
			"200": {Description: "OK"},
			"401": {Description: "missing or invalid token", Content: errContent()},
			"403": {Description: "scope denied", Content: errContent()},
			"500": {Description: "internal error", Content: errContent()},
		},
	}
}

func errContent() map[string]MediaType {
	return map[string]MediaType{
		"application/json": {Schema: ref("Error")},
	}
}

func jsonContent(schema Schema) map[string]MediaType {
	return map[string]MediaType{"application/json": {Schema: schema}}
}

// path collects helpers to build a PathItem from operation pointers.
type pathBuilder struct{ p PathItem }

func newPath() *pathBuilder { return &pathBuilder{} }
func (b *pathBuilder) get(o *Operation) *pathBuilder    { b.p.Get = o; return b }
func (b *pathBuilder) post(o *Operation) *pathBuilder   { b.p.Post = o; return b }
func (b *pathBuilder) put(o *Operation) *pathBuilder    { b.p.Put = o; return b }
func (b *pathBuilder) patch(o *Operation) *pathBuilder  { b.p.Patch = o; return b }
func (b *pathBuilder) delete(o *Operation) *pathBuilder { b.p.Delete = o; return b }
func (b *pathBuilder) build() PathItem                  { return b.p }

func nameParam(name string) Parameter {
	return Parameter{Name: name, In: "path", Required: true, Schema: Schema{Type: "string"}}
}

// withResponse replaces the default 200 with a typed schema.
func withResponse(o *Operation, code, desc string, schema Schema) *Operation {
	o.Responses[code] = Response{Description: desc, Content: jsonContent(schema)}
	return o
}

func withRequest(o *Operation, schema Schema) *Operation {
	o.RequestBody = &RequestBody{Required: true, Content: jsonContent(schema)}
	return o
}

func addMeta(d *Doc) {
	d.Paths["/healthz"] = newPath().get(&Operation{
		Tags: []string{"meta"}, Summary: "Liveness check (no auth)",
		Responses: map[string]Response{"200": {Description: "ok"}},
	}).build()
	d.Paths["/readyz"] = newPath().get(&Operation{
		Tags: []string{"meta"}, Summary: "Readiness check (no auth)",
		Responses: map[string]Response{"200": {Description: "ready"}, "503": {Description: "still starting", Content: errContent()}},
	}).build()
	d.Paths["/version"] = newPath().get(withResponse(op("meta", "Build metadata"),
		"200", "version info", ref("Version"))).build()
	d.Paths["/openapi.json"] = newPath().get(&Operation{
		Tags: []string{"meta"}, Summary: "This document",
		Responses: map[string]Response{"200": {Description: "OpenAPI 3.1 document"}},
	}).build()
}

func addInstance(d *Doc) {
	d.Paths["/api/v1/instance"] = newPath().get(withResponse(op("meta", "Instance identity"),
		"200", "instance info", ref("Instance"))).build()
}

func addDiscovery(d *Doc) {
	get := op("discovery", "List discovered servers")
	get.Parameters = []Parameter{
		{Name: "state", In: "query", Schema: Schema{Type: "string", Enum: []string{"free", "owned-self", "owned-other", "stale"}}},
		{Name: "type", In: "query", Schema: Schema{Type: "string", Enum: []string{"paper", "vanilla", "fabric", "forge"}}},
	}
	withResponse(get, "200", "discovery snapshot", ref("DiscoveryResponse"))
	d.Paths["/api/v1/discovery"] = newPath().get(get).build()

	d.Paths["/api/v1/discovery/refresh"] = newPath().post(withResponse(
		op("discovery", "Force a discovery rescan"), "200", "fresh snapshot", ref("DiscoveryResponse"),
	)).build()

	del := op("discovery", "Release a server's ownership lock")
	del.Parameters = []Parameter{nameParam("server_id"),
		{Name: "force", In: "query", Schema: Schema{Type: "boolean"}, Description: "Steal a stale lock"}}
	d.Paths["/api/v1/discovery/{server_id}/lock"] = newPath().delete(del).build()
}

func addSlots(d *Doc) {
	d.Paths["/api/v1/slots"] = newPath().get(withResponse(
		op("slots", "List slots"), "200", "slot snapshots", ref("SlotsListResponse"),
	)).build()

	get := op("slots", "Slot detail")
	get.Parameters = []Parameter{nameParam("name")}
	withResponse(get, "200", "slot snapshot", ref("Slot"))
	d.Paths["/api/v1/slots/{name}"] = newPath().get(get).build()

	compat := op("slots", "Servers this slot can mount")
	compat.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/compatible-servers"] = newPath().get(compat).build()

	for _, action := range []struct{ verb, summary string }{
		{"start", "Mount and start a server in this slot"},
		{"stop", "Graceful shutdown"},
		{"restart", "Stop then start with the same server id"},
		{"abort-stop", "Cancel in-progress graceful shutdown"},
	} {
		o := op("slots", action.summary)
		o.Parameters = []Parameter{nameParam("name")}
		if action.verb == "start" {
			withRequest(o, ref("StartRequest"))
		}
		if action.verb == "stop" || action.verb == "restart" {
			withRequest(o, ref("StopRequest"))
		}
		o.Responses["202"] = Response{Description: "transition accepted", Content: jsonContent(ref("Slot"))}
		d.Paths["/api/v1/slots/{name}/"+action.verb] = newPath().post(o).build()
	}
}

func addServerControl(d *Doc) {
	for _, e := range []struct {
		verb, path, tag, summary string
		req, resp                Schema
	}{
		{"post", "command", "server", "Raw RCON command", ref("CommandRequest"), ref("CommandResponse")},
		{"post", "say", "server", "Broadcast a message", ref("SayRequest"), Schema{}},
		{"get", "players", "server", "List players", Schema{}, ref("PlayersResponse")},
	} {
		o := op(e.tag, e.summary)
		o.Parameters = []Parameter{nameParam("name")}
		if e.req.Ref != "" {
			withRequest(o, e.req)
		}
		if e.resp.Ref != "" {
			o.Responses["200"] = Response{Description: e.summary, Content: jsonContent(e.resp)}
		}
		path := "/api/v1/slots/{name}/server/" + e.path
		pi := PathItem{}
		switch e.verb {
		case "get":
			pi.Get = o
		case "post":
			pi.Post = o
		}
		d.Paths[path] = pi
	}

	for _, action := range []string{"kick", "ban", "unban", "op", "deop"} {
		o := op("server", "Player "+action)
		o.Parameters = []Parameter{nameParam("name"), nameParam("player")}
		d.Paths["/api/v1/slots/{name}/server/players/{player}/"+action] = newPath().post(o).build()
	}
}

func addLists(d *Doc) {
	get := op("server", "Whitelist (read)")
	get.Parameters = []Parameter{nameParam("name")}
	put := op("server", "Whitelist add")
	put.Parameters = []Parameter{nameParam("name"), nameParam("player")}
	del := op("server", "Whitelist remove")
	del.Parameters = []Parameter{nameParam("name"), nameParam("player")}
	d.Paths["/api/v1/slots/{name}/server/whitelist"] = newPath().get(get).build()
	d.Paths["/api/v1/slots/{name}/server/whitelist/{player}"] = newPath().put(put).delete(del).build()

	reload := op("server", "Whitelist reload")
	reload.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/server/whitelist/reload"] = newPath().post(reload).build()

	bp := op("server", "Banned players")
	bp.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/server/banlist"] = newPath().get(bp).build()
	bi := op("server", "Banned IPs")
	bi.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/server/banlist/ips"] = newPath().get(bi).build()
}

func addProperties(d *Doc) {
	get := op("server", "Read parsed server.properties")
	get.Parameters = []Parameter{nameParam("name")}
	patch := op("server", "Update server.properties (rejects mcsm-managed keys)")
	patch.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/server/properties"] = newPath().get(get).patch(patch).build()
}

func addLogs(d *Doc) {
	get := op("logs", "Historical log tail")
	get.Parameters = []Parameter{nameParam("name"),
		{Name: "tail", In: "query", Schema: Schema{Type: "integer"}},
		{Name: "since", In: "query", Schema: Schema{Type: "string", Format: "date-time"}},
		{Name: "level", In: "query", Schema: Schema{Type: "string", Enum: []string{"info", "warn", "error"}}},
	}
	d.Paths["/api/v1/slots/{name}/server/logs"] = newPath().get(get).build()

	stream := op("logs", "Live log stream (WebSocket Upgrade)")
	stream.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/server/logs/stream"] = newPath().get(stream).build()

	ev := op("slots", "Slot lifecycle event stream (WebSocket Upgrade)")
	ev.Parameters = []Parameter{nameParam("name")}
	d.Paths["/api/v1/slots/{name}/events"] = newPath().get(ev).build()
}

func addBackups(d *Doc) {
	list := op("backups", "List backups")
	list.Parameters = []Parameter{nameParam("name")}
	create := op("backups", "Create a backup")
	create.Parameters = []Parameter{nameParam("name")}
	withRequest(create, Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"label":        {Type: "string"},
			"stop_server":  {Type: "boolean"},
			"include_logs": {Type: "boolean"},
		},
	})
	d.Paths["/api/v1/slots/{name}/server/backups"] = newPath().get(list).post(create).build()

	get := op("backups", "Backup detail")
	get.Parameters = []Parameter{nameParam("name"), nameParam("id")}
	del := op("backups", "Delete a backup")
	del.Parameters = []Parameter{nameParam("name"), nameParam("id")}
	d.Paths["/api/v1/slots/{name}/server/backups/{id}"] = newPath().get(get).delete(del).build()

	rest := op("backups", "Restore (slot must be stopped)")
	rest.Parameters = []Parameter{nameParam("name"), nameParam("id")}
	d.Paths["/api/v1/slots/{name}/server/backups/{id}/restore"] = newPath().post(rest).build()
}

func addUpdate(d *Doc) {
	check := op("updates", "Check for the latest available release")
	check.Parameters = []Parameter{nameParam("name"),
		{Name: "mc_version", In: "query", Required: true, Schema: Schema{Type: "string"}}}
	apply := op("updates", "Download + install (slot must be stopped)")
	apply.Parameters = []Parameter{nameParam("name")}
	withRequest(apply, Schema{
		Type: "object",
		Properties: map[string]*Schema{
			"mc_version": {Type: "string"},
			"backup":     {Type: "boolean"},
		},
		Required: []string{"mc_version"},
	})
	d.Paths["/api/v1/slots/{name}/server/update"] = newPath().get(check).post(apply).build()
}

func addSystem(d *Doc) {
	d.Paths["/api/v1/system/temperature"] = newPath().get(op("system", "CPU temperature")).build()
	d.Paths["/api/v1/system/resources"] = newPath().get(op("system", "Host CPU/mem/disk/load")).build()
}

func addAudit(d *Doc) {
	a := op("audit", "Audit log entries")
	a.Parameters = []Parameter{
		{Name: "since", In: "query", Schema: Schema{Type: "string", Format: "date-time"}},
		{Name: "until", In: "query", Schema: Schema{Type: "string", Format: "date-time"}},
		{Name: "actor", In: "query", Schema: Schema{Type: "string"}},
		{Name: "kind", In: "query", Schema: Schema{Type: "string"}},
		{Name: "limit", In: "query", Schema: Schema{Type: "integer"}},
		{Name: "cursor", In: "query", Schema: Schema{Type: "string"}},
	}
	d.Paths["/api/v1/audit"] = newPath().get(a).build()
}

func addPeers(d *Doc) {
	d.Paths["/api/v1/peers"] = newPath().get(op("federation", "List peers")).build()
	d.Paths["/api/v1/peers/refresh"] = newPath().post(op("federation", "Force a peer ping round")).build()
}

func addFederation(d *Doc) {
	d.Paths["/api/v1/federation/discovery"] = newPath().get(
		op("federation", "Aggregated discovery (self + peers)"),
	).build()
	d.Paths["/api/v1/federation/slots"] = newPath().get(
		op("federation", "Aggregated slots, tagged with instance"),
	).build()
}

func addMetrics(d *Doc) {
	d.Paths["/metrics"] = newPath().get(&Operation{
		Tags: []string{"metrics"}, Summary: "Prometheus exposition (no auth by default)",
		Responses: map[string]Response{
			"200": {Description: "Prometheus text format",
				Content: map[string]MediaType{
					"text/plain": {Schema: Schema{Type: "string"}},
				}},
		},
	}).build()
}

// defaultSchemas defines reusable shapes referenced from path responses.
// We intentionally keep these minimal; the source-of-truth shape
// documentation is in mcsm/docs/api.md.
func defaultSchemas() map[string]*Schema {
	return map[string]*Schema{
		"Error": {
			Type: "object",
			Properties: map[string]*Schema{
				"error": {Type: "object", Properties: map[string]*Schema{
					"code":     {Type: "string"},
					"message":  {Type: "string"},
					"trace_id": {Type: "string"},
				}},
			},
		},
		"Version": {
			Type: "object",
			Properties: map[string]*Schema{
				"version": {Type: "string"},
				"commit":  {Type: "string"},
				"date":    {Type: "string"},
			},
		},
		"Instance": {
			Type: "object",
			Properties: map[string]*Schema{
				"name":             {Type: "string"},
				"version":          {Type: "string"},
				"uptime_seconds":   {Type: "integer"},
				"discovery_roots":  {Type: "array", Items: &Schema{Type: "string"}},
				"slot_count":       {Type: "integer"},
			},
		},
		"DiscoveryResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"scanned_at": {Type: "string", Format: "date-time"},
				"servers":    {Type: "array", Items: &Schema{Type: "object"}},
			},
		},
		"Slot": {
			Type: "object",
			Properties: map[string]*Schema{
				"name":               {Type: "string"},
				"port":               {Type: "integer"},
				"state":              {Type: "string", Enum: []string{"idle", "mounting", "starting", "running", "stopping", "crashed", "error"}},
				"state_since":        {Type: "string", Format: "date-time"},
				"mounted_server_id":  {Type: "string"},
				"mounted_server":     {Type: "object"},
			},
		},
		"SlotsListResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"slots": {Type: "array", Items: &Schema{Ref: "#/components/schemas/Slot"}},
			},
		},
		"StartRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"server_id": {Type: "string"},
				"force":     {Type: "boolean"},
			},
			Required: []string{"server_id"},
		},
		"StopRequest": {
			Type: "object",
			Properties: map[string]*Schema{
				"graceful_seconds":   {Type: "integer"},
				"broadcast_every":    {Type: "integer"},
				"broadcast_template": {Type: "string"},
				"kill_grace":         {Type: "string"},
			},
		},
		"CommandRequest": {
			Type: "object",
			Properties: map[string]*Schema{"command": {Type: "string"}},
			Required:   []string{"command"},
		},
		"CommandResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"command":    {Type: "string"},
				"response":   {Type: "string"},
				"elapsed_ms": {Type: "integer"},
			},
		},
		"SayRequest": {
			Type: "object",
			Properties: map[string]*Schema{"message": {Type: "string"}},
			Required:   []string{"message"},
		},
		"PlayersResponse": {
			Type: "object",
			Properties: map[string]*Schema{
				"online":  {Type: "integer"},
				"max":     {Type: "integer"},
				"players": {Type: "array", Items: &Schema{Type: "object"}},
			},
		},
	}
}
