package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is an in-memory stand-in for the parts of the Gezor API the tests use.
// Cluster commands complete on the first poll.
type fakeAPI struct {
	t        *testing.T
	mu       sync.Mutex
	seq      int
	roles    map[string]map[string]any
	tenants  map[string]map[string]any
	settings map[string]any
	clusters map[string]map[string]any
	topics   map[string]map[string]any
	subjects map[string]map[string]any
	commands map[string]any
	notebook map[string]map[string]any
	requests []string
}

func newFakeAPI(t *testing.T) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{
		t: t, roles: map[string]map[string]any{}, tenants: map[string]map[string]any{},
		clusters: map[string]map[string]any{}, topics: map[string]map[string]any{},
		subjects: map[string]map[string]any{}, commands: map[string]any{}, notebook: map[string]map[string]any{},
		settings: map[string]any{
			"workspace": map[string]any{"id": "ws_main", "name": "Main", "slug": "main", "description": "", "icon": ""},
			"modules":   map[string]any{"pipelines": true, "fraud": false, "aml": false, "enterpriseLineage": false},
			"security": map[string]any{"session_absolute_seconds": 43200, "session_idle_seconds": 3600,
				"require_mfa": nil, "sso_only": false, "allow_password_login": true},
			"sso": map[string]any{"providers": []any{}},
		},
	}
	f.tenants["ws_main"] = map[string]any{"id": "ws_main", "name": "Main", "slug": "main", "description": "", "is_primary": true}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeAPI) id(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s%d", prefix, f.seq)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) { writeJSON(w, 404, map[string]any{"detail": "not found"}) }

func (f *fakeAPI) enqueue(result any) map[string]any {
	rid := f.id("req-")
	f.commands[rid] = result
	return map[string]any{"request_id": rid, "status": "pending"}
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer gzr_sa_") {
		writeJSON(w, 401, map[string]any{"detail": "authentication required"})
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	p := strings.TrimSuffix(r.URL.Path, "/")
	f.requests = append(f.requests, r.Method+" "+p)
	seg := strings.Split(strings.TrimPrefix(p, "/api/"), "/")

	switch {
	case p == "/api/auth/me":
		writeJSON(w, 200, map[string]any{
			"user": map[string]any{"id": "svc_1", "full_name": "terraform"}, "organization": map[string]any{"id": "org_1", "name": "Acme"},
			"tenant": map[string]any{"id": "ws_main", "slug": "main", "name": "Main"}, "role": "admin", "permissions": []string{"portal.access", "roles.manage"},
		})

	// roles
	case p == "/api/roles" && r.Method == "POST":
		id := f.id("role_")
		role := map[string]any{"id": id, "key": body["key"], "name": body["name"], "description": body["description"],
			"permissions": normalizePerms(body["permissions"]), "builtin": false}
		f.roles[id] = role
		writeJSON(w, 200, map[string]any{"ok": true, "id": id, "role": role})
	case p == "/api/roles" && r.Method == "GET":
		list := []any{}
		for _, role := range f.roles {
			list = append(list, role)
		}
		writeJSON(w, 200, map[string]any{"roles": list})
	case p == "/api/roles/catalog":
		writeJSON(w, 200, map[string]any{"permissions": []any{
			map[string]any{"id": "clusters.read", "group": "Clusters", "label": "View clusters"},
			map[string]any{"id": "clusters.manage", "group": "Clusters", "label": "Manage clusters"},
		}})
	case len(seg) == 2 && seg[0] == "roles":
		role, ok := f.roles[seg[1]]
		if !ok {
			notFound(w)
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, 200, map[string]any{"role": role})
		case "PATCH":
			for _, k := range []string{"name", "description"} {
				if v, ok := body[k]; ok {
					role[k] = v
				}
			}
			if v, ok := body["permissions"]; ok {
				role["permissions"] = normalizePerms(v)
			}
			writeJSON(w, 200, map[string]any{"ok": true, "role": role})
		case "DELETE":
			delete(f.roles, seg[1])
			writeJSON(w, 200, map[string]any{"ok": true})
		}

	// workspaces
	case p == "/api/tenants" && r.Method == "POST":
		id := f.id("ws_")
		f.tenants[id] = map[string]any{"id": id, "name": body["name"], "slug": body["slug"], "description": body["description"], "is_primary": false}
		writeJSON(w, 200, map[string]any{"ok": true, "tenant_id": id, "slug": body["slug"]})
	case p == "/api/tenants" && r.Method == "GET":
		list := []any{}
		for _, t := range f.tenants {
			list = append(list, t)
		}
		writeJSON(w, 200, map[string]any{"tenants": list})
	case len(seg) == 2 && seg[0] == "tenants":
		t, ok := f.tenants[seg[1]]
		if !ok {
			notFound(w)
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, 200, map[string]any{"tenant": t})
		case "PATCH":
			for _, k := range []string{"name", "description"} {
				if v, ok := body[k]; ok {
					t[k] = v
				}
			}
			writeJSON(w, 200, map[string]any{"ok": true, "tenant": t})
		case "DELETE":
			if r.URL.Query().Get("confirm_name") != t["name"] {
				writeJSON(w, 400, map[string]any{"detail": "confirmation name does not match the workspace name"})
				return
			}
			delete(f.tenants, seg[1])
			writeJSON(w, 200, map[string]any{"ok": true})
		}

	// settings
	case p == "/api/settings" && r.Method == "GET":
		writeJSON(w, 200, f.settings)
	case p == "/api/settings/general":
		ws := f.settings["workspace"].(map[string]any)
		for k, v := range body {
			ws[k] = v
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case p == "/api/settings/modules":
		mods := f.settings["modules"].(map[string]any)
		for k, v := range body {
			mods[k] = v
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	case p == "/api/settings/security":
		sec := f.settings["security"].(map[string]any)
		for k, v := range body {
			sec[k] = v
		}
		writeJSON(w, 200, map[string]any{"ok": true})

	// clusters
	case p == "/api/clusters" && r.Method == "POST":
		id := f.id("cl_")
		c := map[string]any{"id": id, "org_id": "org_1", "name": body["name"], "description": body["description"], "tags": body["tags"],
			"environment": body["environment"], "region": body["region"], "hosting_mode": body["hosting_mode"], "status": "pending",
			"online": false, "operator_namespace": body["operator_namespace"], "desired_state": map[string]any{"modules": map[string]any{}},
			"desired_version": float64(1), "reported_status": map[string]any{"applied_version": float64(0), "modules": map[string]any{}}}
		if body["hosting_mode"] == "gezor_hosted" {
			c["polls_until_online"] = 2
			c["provision_progress"] = map[string]any{"done": false, "stage": "vcluster", "error": nil}
		}
		f.clusters[id] = c
		writeJSON(w, 200, map[string]any{"cluster": c, "install_token": "gzr_it_secret", "install_token_id": "tok_1",
			"install_token_expires_at": 1900000000, "install": map[string]any{"helm": "helm upgrade --install gzr-operator ..."}})
	case p == "/api/clusters" && r.Method == "GET":
		list := []any{}
		for _, c := range f.clusters {
			list = append(list, c)
		}
		writeJSON(w, 200, map[string]any{"clusters": list})
	case len(seg) >= 2 && seg[0] == "clusters":
		c, ok := f.clusters[seg[1]]
		if !ok {
			notFound(w)
			return
		}
		f.cluster(w, r, c, seg[2:], body)

	default:
		writeJSON(w, 404, map[string]any{"detail": "no fake for " + r.Method + " " + p})
	}
}

func normalizePerms(v any) []any {
	out := []any{"portal.access"}
	list, _ := v.([]any)
	for _, p := range list {
		if s, _ := p.(string); s != "portal.access" && strings.Contains(s, ".") && !strings.HasPrefix(s, "bogus") {
			out = append(out, s)
		}
	}
	return out
}

func asFloat(v any) float64 { f, _ := v.(float64); return f }

// advanceHosted brings a hosted cluster online after a few polls, then has the operator
// apply the latest settings one poll after they change.
func (f *fakeAPI) advanceHosted(c map[string]any) {
	if n, ok := c["polls_until_online"].(int); ok && n > 0 {
		c["polls_until_online"] = n - 1
		if n == 1 {
			c["online"], c["status"] = true, "connected"
			c["provision_progress"] = map[string]any{"done": true, "stage": "heartbeat", "error": nil}
		}
		return
	}
	if !asBool(c["online"]) {
		return
	}
	rs := c["reported_status"].(map[string]any)
	reported := rs["modules"].(map[string]any)
	if asFloat(rs["applied_version"]) < asFloat(c["desired_version"]) {
		if rs["pending"] == true {
			rs["applied_version"], rs["pending"] = c["desired_version"], false
			for app, m := range c["desired_state"].(map[string]any)["modules"].(map[string]any) {
				if asBool(asMap(m)["enabled"]) {
					reported[app] = map[string]any{"phase": "Ready"}
				}
			}
		} else {
			rs["pending"] = true
			for app := range reported {
				reported[app] = map[string]any{"phase": "Progressing"}
			}
		}
	}
	f.requests = append(f.requests, "poll "+c["id"].(string))
}

func (f *fakeAPI) cluster(w http.ResponseWriter, r *http.Request, c map[string]any, rest []string, body map[string]any) {
	cid := c["id"].(string)
	modules := c["desired_state"].(map[string]any)["modules"].(map[string]any)
	switch {
	case len(rest) == 0 && r.Method == "GET":
		f.advanceHosted(c)
		writeJSON(w, 200, map[string]any{"cluster": c})
	case len(rest) == 0 && r.Method == "PATCH":
		for k, v := range body {
			c[k] = v
		}
		writeJSON(w, 200, map[string]any{"cluster": c})
	case len(rest) == 0 && r.Method == "DELETE":
		delete(f.clusters, cid)
		writeJSON(w, 200, map[string]any{"ok": true})

	case len(rest) == 2 && rest[0] == "modules" && r.Method == "PUT":
		cur, _ := modules[rest[1]].(map[string]any)
		if cur != nil && asBool(cur["enabled"]) && asBool(cur["deletionProtection"]) && !asBool(body["enabled"]) {
			writeJSON(w, 409, map[string]any{"detail": "app is protected from accidental deletion"})
			return
		}
		mod := map[string]any{"replicas": float64(1), "storage": map[string]any{"size": "10Gi"}}
		for k, v := range body {
			mod[k] = v
		}
		modules[rest[1]] = mod
		c["desired_version"] = asFloat(c["desired_version"]) + 1
		writeJSON(w, 200, map[string]any{"cluster": c})

	case len(rest) == 3 && rest[0] == "kafka" && rest[1] == "topics" && r.Method == "POST":
		name := body["topic"].(string)
		key := cid + "/" + name
		switch rest[2] {
		case "create":
			rf := body["replication_factor"]
			if rf == nil {
				rf = float64(3)
			}
			f.topics[key] = map[string]any{"name": name, "exists": true, "partitions": body["partitions"], "replicationFactor": rf, "configs": body["configs"]}
			writeJSON(w, 200, f.enqueue(nil))
		case "describe":
			t, ok := f.topics[key]
			if !ok {
				t = map[string]any{"name": name, "exists": false}
			}
			writeJSON(w, 200, f.enqueue(t))
		case "alter":
			t := f.topics[key]
			if v, ok := body["partitions"]; ok {
				t["partitions"] = v
			}
			if v, ok := body["configs"]; ok {
				t["configs"] = v
			}
			writeJSON(w, 200, f.enqueue(nil))
		case "delete":
			delete(f.topics, key)
			writeJSON(w, 200, f.enqueue(nil))
		}
	case len(rest) == 3 && rest[0] == "kafka" && rest[1] == "topic-actions":
		f.poll(w, rest[2])

	case len(rest) >= 3 && rest[0] == "schema-registry" && rest[1] == "subjects":
		key := cid + "/" + rest[2]
		s, ok := f.subjects[key]
		switch {
		case len(rest) == 4 && rest[3] == "versions":
			if !ok {
				s = map[string]any{"subject": rest[2], "exists": true, "version": float64(0), "compatibility": "BACKWARD"}
				f.subjects[key] = s
			}
			s["version"] = s["version"].(float64) + 1
			s["id"] = float64(100 + f.seq)
			s["schema"] = body["schema"]
			s["schemaType"] = body["schemaType"]
			writeJSON(w, 200, f.enqueue(nil))
		case len(rest) == 4 && rest[3] == "config":
			s["compatibility"] = body["compatibility"]
			writeJSON(w, 200, f.enqueue(nil))
		case len(rest) == 4 && rest[3] == "describe":
			if !ok {
				s = map[string]any{"subject": rest[2], "exists": false}
			}
			writeJSON(w, 200, f.enqueue(s))
		case len(rest) == 3 && r.Method == "DELETE":
			delete(f.subjects, key)
			writeJSON(w, 200, f.enqueue(nil))
		}
	case len(rest) == 3 && rest[0] == "schema-registry" && rest[1] == "actions":
		f.poll(w, rest[2])

	case len(rest) == 1 && rest[0] == "pipeline-notebooks" && r.Method == "POST":
		id := f.id("nb_")
		n := map[string]any{"id": id, "cluster_id": cid, "path": body["path"], "language": body["language"], "content": body["content"]}
		f.notebook[id] = n
		writeJSON(w, 200, map[string]any{"notebook": n})
	case len(rest) == 2 && rest[0] == "pipeline-notebooks":
		n, ok := f.notebook[rest[1]]
		if !ok {
			notFound(w)
			return
		}
		switch r.Method {
		case "GET":
			writeJSON(w, 200, map[string]any{"notebook": n})
		case "PUT":
			for k, v := range body {
				n[k] = v
			}
			writeJSON(w, 200, map[string]any{"notebook": n})
		case "DELETE":
			delete(f.notebook, rest[1])
			writeJSON(w, 200, map[string]any{"ok": true})
		}
	default:
		writeJSON(w, 404, map[string]any{"detail": "no fake for cluster route " + strings.Join(rest, "/")})
	}
}

func (f *fakeAPI) poll(w http.ResponseWriter, rid string) {
	result, ok := f.commands[rid]
	if !ok {
		writeJSON(w, 200, map[string]any{"request_id": rid, "status": "expired", "error": "unknown"})
		return
	}
	writeJSON(w, 200, map[string]any{"request_id": rid, "status": "ready", "result": result})
}
