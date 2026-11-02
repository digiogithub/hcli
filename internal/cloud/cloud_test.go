package cloud

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/spf13/cobra"
)

const serverJSON = `{
  "id": %d, "name": %q, "status": "running", "created": "2026-01-01T00:00:00+00:00",
  "public_net": {"ipv4": {"id": 1, "ip": "%s", "blocked": false, "dns_ptr": "static.example"},
                 "ipv6": {"id": 2, "ip": "2a01:4f8::/64", "blocked": false, "dns_ptr": []},
                 "floating_ips": [], "firewalls": []},
  "private_net": [],
  "server_type": {"id": 1, "name": "cx22", "cores": 2, "memory": 4, "disk": 40, "cpu_type": "shared", "architecture": "x86", "storage_type": "local", "prices": []},
  "location": {"id": 1, "name": "fsn1", "city": "Falkenstein", "country": "DE", "network_zone": "eu-central"},
  "image": null, "iso": null, "rescue_enabled": false, "locked": false, "backup_window": null,
  "outgoing_traffic": 0, "ingoing_traffic": 0, "included_traffic": 0,
  "protection": {"delete": false, "rebuild": false}, "labels": {"env": "prod"},
  "volumes": [], "load_balancers": [], "primary_disk_size": 40, "placement_group": null
}`

type fakeServer struct {
	id       int
	name, ip string
}

func (s fakeServer) json() string {
	return strings.TrimSpace(fmt.Sprintf(serverJSON, s.id, s.name, s.ip))
}

// mockAPI is a tiny subset of api.hetzner.cloud.
type mockAPI struct {
	t       *testing.T
	servers []fakeServer
	mu      sync.Mutex
	deleted []string
	auth    []string
}

func (m *mockAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.auth = append(m.auth, r.Header.Get("Authorization"))
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	meta := `"meta": {"pagination": {"page": 1, "per_page": 50, "previous_page": null, "next_page": null, "last_page": 1, "total_entries": %d}}`
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/servers":
		name := r.URL.Query().Get("name")
		var items []string
		for _, s := range m.servers {
			if name == "" || s.name == name {
				items = append(items, s.json())
			}
		}
		_, _ = io.WriteString(w, `{"servers": [`+strings.Join(items, ",")+`], `+fmt.Sprintf(meta, len(items))+`}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/servers/"):
		id := strings.TrimPrefix(r.URL.Path, "/servers/")
		for _, s := range m.servers {
			if fmt.Sprintf("%d", s.id) == id {
				_, _ = io.WriteString(w, `{"server": `+s.json()+`}`)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error": {"code": "not_found", "message": "server not found"}}`)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/servers/"):
		m.mu.Lock()
		m.deleted = append(m.deleted, strings.TrimPrefix(r.URL.Path, "/servers/"))
		m.mu.Unlock()
		_, _ = io.WriteString(w, `{"action": {"id": 77, "command": "delete_server", "status": "running", "progress": 0, "started": "2026-01-01T00:00:00+00:00", "finished": null, "resources": [], "error": null}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/actions":
		_, _ = io.WriteString(w, `{"actions": [{"id": 77, "command": "delete_server", "status": "success", "progress": 100, "started": "2026-01-01T00:00:00+00:00", "finished": "2026-01-01T00:00:01+00:00", "resources": [], "error": null}], `+fmt.Sprintf(meta, 1)+`}`)
	default:
		m.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error": {"code": "not_found", "message": "not found"}}`)
	}
}

type harness struct {
	api      *mockAPI
	c        *cli.CLI
	out, err *bytes.Buffer
}

// newHarness points the CLI at an httptest server through the same
// environment variables a user would set.
func newHarness(t *testing.T, token string) *harness {
	t.Helper()
	api := &mockAPI{t: t, servers: []fakeServer{{1, "web1", "192.0.2.10"}, {2, "db1", "192.0.2.20"}}}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	t.Setenv("HCLI_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	t.Setenv("HCLI_CONTEXT", "")
	t.Setenv("HCLOUD_TOKEN", token)
	t.Setenv("HCLOUD_ENDPOINT", srv.URL)
	pollInterval = time.Millisecond
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	c := &cli.CLI{In: strings.NewReader(""), Out: out, Err: errb, OutputFlag: "table"}
	return &harness{api: api, c: c, out: out, err: errb}
}

// run executes `hcli cloud <args>` through a minimal root carrying the same
// persistent flags as the real one.
func (h *harness) run(args ...string) error {
	root := &cobra.Command{Use: "hcli", SilenceUsage: true, SilenceErrors: true}
	pf := root.PersistentFlags()
	pf.StringVarP(&h.c.OutputFlag, "output", "o", "table", "")
	pf.BoolVar(&h.c.NoHeader, "no-header", false, "")
	pf.BoolVarP(&h.c.Yes, "yes", "y", false, "")
	pf.BoolVar(&h.c.Debug, "debug", false, "")
	root.AddCommand(NewCommand(h.c))
	root.SetIn(h.c.In)
	root.SetOut(h.c.Out)
	root.SetErr(h.c.Err)
	if len(args) > 0 && args[0] == cobra.ShellCompRequestCmd {
		root.SetArgs(append([]string{args[0], "cloud"}, args[1:]...))
	} else {
		root.SetArgs(append([]string{"cloud"}, args...))
	}
	return root.Execute()
}

func TestServerListTable(t *testing.T) {
	h := newHarness(t, "secret-token")
	if err := h.run("server", "list"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got:\n%s", h.out)
	}
	for _, col := range []string{"ID", "NAME", "STATUS", "IPV4", "TYPE", "LOCATION", "AGE"} {
		if !strings.Contains(lines[0], col) {
			t.Errorf("header %q lacks %s", lines[0], col)
		}
	}
	for _, want := range []string{"web1", "running", "192.0.2.10", "cx22", "fsn1"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row %q lacks %s", lines[1], want)
		}
	}
	if got := h.api.auth[0]; got != "Bearer secret-token" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestServerListJSON(t *testing.T) {
	h := newHarness(t, "tok")
	if err := h.run("server", "list", "-o", "json"); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, h.out)
	}
	if len(got) != 2 || got[0]["name"] != "web1" {
		t.Fatalf("unexpected payload: %v", got)
	}
	pn := got[0]["public_net"].(map[string]any)["ipv4"].(map[string]any)
	if pn["ip"] != "192.0.2.10" {
		t.Errorf("public_net.ipv4.ip = %v (want API schema field names)", pn["ip"])
	}
}

func TestServerDeleteRequiresConfirmation(t *testing.T) {
	h := newHarness(t, "tok")
	err := h.run("server", "delete", "web1")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("want refusal mentioning --yes, got %v", err)
	}
	if len(h.api.deleted) != 0 {
		t.Fatalf("server deleted without confirmation: %v", h.api.deleted)
	}
}

func TestServerDeleteWithYes(t *testing.T) {
	h := newHarness(t, "tok")
	if err := h.run("server", "delete", "web1", "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(h.api.deleted) != 1 || h.api.deleted[0] != "1" {
		t.Fatalf("deleted = %v, want [1]", h.api.deleted)
	}
	if !strings.Contains(h.out.String(), "Server web1 deleted") {
		t.Errorf("stdout = %q", h.out)
	}
	if !strings.Contains(h.err.String(), "done") {
		t.Errorf("stderr progress = %q", h.err)
	}
}

func TestServerNotFound(t *testing.T) {
	h := newHarness(t, "tok")
	err := h.run("server", "describe", "nope")
	if err == nil || !strings.Contains(err.Error(), "server not found: nope") {
		t.Fatalf("got %v", err)
	}
}

func TestServerIP(t *testing.T) {
	h := newHarness(t, "tok")
	if err := h.run("server", "ip", "db1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(h.out.String()); got != "192.0.2.20" {
		t.Errorf("ip = %q", got)
	}
	h.out.Reset()
	if err := h.run("server", "ip", "-6", "db1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(h.out.String()); got != "2a01:4f8::1" {
		t.Errorf("ipv6 = %q", got)
	}
}

func TestCompletionServerNames(t *testing.T) {
	h := newHarness(t, "tok")
	if err := h.run("__complete", "server", "describe", ""); err != nil {
		t.Fatal(err)
	}
	got := h.out.String()
	for _, want := range []string{"web1\n", "db1\n", ":4\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("completion output lacks %q:\n%s", want, got)
		}
	}

	h.out.Reset()
	if err := h.run("__complete", "server", "poweroff", "we"); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); !strings.Contains(got, "web1") || strings.Contains(got, "db1") {
		t.Errorf("prefix filtering failed:\n%s", got)
	}
}

func TestCompletionDeleteSkipsGiven(t *testing.T) {
	h := newHarness(t, "tok")
	if err := h.run("__complete", "server", "delete", "web1", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); strings.Contains(got, "web1\n") || !strings.Contains(got, "db1\n") {
		t.Errorf("unexpected completion:\n%s", got)
	}
}

func TestCompletionWithoutTokenIsSilent(t *testing.T) {
	h := newHarness(t, "")
	if err := h.run("__complete", "server", "describe", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.out.String(); strings.Contains(got, "web1") || !strings.Contains(got, ":4") {
		t.Errorf("want no candidates, got:\n%s", got)
	}
	if len(h.api.auth) != 0 {
		t.Errorf("API called without token")
	}
}

func TestNoTokenError(t *testing.T) {
	h := newHarness(t, "")
	err := h.run("server", "list")
	if !errors.Is(err, cli.ErrNoCloudToken) {
		t.Fatalf("got %v, want ErrNoCloudToken", err)
	}
}

func TestDebugRedactsToken(t *testing.T) {
	h := newHarness(t, "super-secret")
	if err := h.run("server", "list", "--debug"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.err.String(), "super-secret") {
		t.Fatal("token leaked to debug output")
	}
	if !strings.Contains(h.err.String(), "GET /servers") {
		t.Errorf("no request dump in debug output:\n%s", h.err)
	}
}

func TestCommandTree(t *testing.T) {
	cmd := NewCommand(&cli.CLI{})
	want := map[string][]string{
		"server":          {"list", "describe", "create", "delete", "poweron", "poweroff", "reboot", "reset", "shutdown", "rebuild", "change-type", "enable-rescue", "disable-rescue", "create-image", "set-rdns", "add-label", "remove-label", "ip", "ssh"},
		"volume":          {"list", "describe", "create", "delete", "attach", "detach", "resize"},
		"network":         {"list", "describe", "create", "delete", "add-subnet", "add-route"},
		"firewall":        {"list", "describe", "create", "delete", "add-rule", "delete-rule", "apply-to-resource", "remove-from-resource"},
		"floating-ip":     {"list", "describe", "create", "delete", "assign", "unassign"},
		"primary-ip":      {"list", "describe", "create", "delete", "assign", "unassign"},
		"load-balancer":   {"list", "describe", "delete"},
		"ssh-key":         {"list", "describe", "create", "delete"},
		"certificate":     {"list", "describe", "delete"},
		"placement-group": {"list", "describe", "delete"},
		"server-type":     {"list", "describe"},
		"image":           {"list", "describe"},
		"location":        {"list", "describe"},
		"iso":             {"list", "describe"},
	}
	for res, subs := range want {
		for _, sub := range subs {
			if c, _, err := cmd.Find([]string{res, sub}); err != nil || c.Name() != sub {
				t.Errorf("missing command cloud %s %s", res, sub)
			}
		}
	}
	if c, _, err := cmd.Find([]string{"pricing"}); err != nil || c.Name() != "pricing" {
		t.Error("missing cloud pricing")
	}
}
