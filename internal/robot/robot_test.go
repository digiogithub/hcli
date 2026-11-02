package robot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/robot/client"
)

const serversJSON = `[
 {"server":{"server_ip":"123.123.123.123","server_ipv6_net":"2a01:4f8:111:4221::","server_number":321,"server_name":"server1","product":"DS 3000","dc":"NBG1-DC1","traffic":"5 TB","status":"ready","cancelled":false,"paid_until":"2010-09-02","ip":["123.123.123.123"],"subnet":[{"ip":"2a01:4f8:111:4221::","mask":"64"}]}},
 {"server":{"server_ip":"123.123.123.124","server_ipv6_net":"2a01:4f8:111:4222::","server_number":421,"server_name":"server2","product":"X5","dc":"FSN1-DC10","traffic":"2 TB","status":"ready","cancelled":true,"paid_until":"2010-06-11","ip":["123.123.123.124","124.124.124.124"],"subnet":null}}
]`

// fakeRobot records requests and answers from a route table.
type fakeRobot struct {
	mu       sync.Mutex
	requests []string
	forms    map[string]map[string][]string
	routes   map[string]string // "METHOD /path" -> JSON body
}

func (f *fakeRobot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.requests = append(f.requests, key)
	if f.forms == nil {
		f.forms = map[string]map[string][]string{}
	}
	f.forms[key] = r.PostForm
	body, ok := f.routes[key]
	f.mu.Unlock()
	if u, p, _ := r.BasicAuth(); u != "robot-user" || p != "robot-pass" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"status":404,"code":"NOT_FOUND","message":"not found"}}`)
		return
	}
	fmt.Fprint(w, body)
}

func (f *fakeRobot) called(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if r == key {
			return true
		}
	}
	return false
}

func setup(t *testing.T, routes map[string]string) (*fakeRobot, *cli.CLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	fake := &fakeRobot{routes: routes}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	t.Setenv("HCLI_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("HCLI_CONTEXT", "")
	t.Setenv("HROBOT_ENDPOINT", srv.URL)
	t.Setenv("HROBOT_USER", "robot-user")
	t.Setenv("HROBOT_PASSWORD", "robot-pass")
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	c := &cli.CLI{In: &bytes.Buffer{}, Out: out, Err: errb, OutputFlag: "table"}
	return fake, c, out, errb
}

func run(c *cli.CLI, args ...string) error {
	cmd := NewCommand(c)
	cmd.SetArgs(args)
	cmd.SetOut(c.Out)
	cmd.SetErr(c.Err)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func TestServerListTable(t *testing.T) {
	_, c, out, _ := setup(t, map[string]string{"GET /server": serversJSON})
	if err := run(c, "server", "list"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("output:\n%s", out)
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "NUMBER NAME IP PRODUCT DC STATUS PAID_UNTIL CANCELLED" {
		t.Fatalf("header: %q", lines[0])
	}
	for _, want := range []string{"321", "server1", "123.123.123.123", "NBG1-DC1", "2010-09-02", "no"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("row 1 missing %q: %q", want, lines[1])
		}
	}
	if !strings.Contains(lines[2], "421") || !strings.HasSuffix(strings.TrimSpace(lines[2]), "yes") {
		t.Errorf("row 2: %q", lines[2])
	}
}

func TestServerListJSON(t *testing.T) {
	_, c, out, _ := setup(t, map[string]string{"GET /server": serversJSON})
	c.OutputFlag = "json"
	if err := run(c, "server", "list"); err != nil {
		t.Fatal(err)
	}
	var got []client.Server
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if len(got) != 2 || got[0].ServerNumber != 321 || got[1].IP[1] != "124.124.124.124" {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(out.String(), `"server": {`) {
		t.Fatal("json output must be unwrapped")
	}
}

func TestNoCredentials(t *testing.T) {
	_, c, _, _ := setup(t, nil)
	t.Setenv("HROBOT_USER", "")
	t.Setenv("HROBOT_PASSWORD", "")
	if err := run(c, "server", "list"); !errors.Is(err, cli.ErrNoRobotCredentials) {
		t.Fatalf("err = %v", err)
	}
	// Completion fails silently.
	if got := completeValues(c, completeServers); got != nil {
		t.Fatalf("completion without credentials: %v", got)
	}
}

func TestServerDescribeByNameAndIP(t *testing.T) {
	fake, c, out, _ := setup(t, map[string]string{
		"GET /server":     serversJSON,
		"GET /server/421": `{"server":{"server_number":421,"server_name":"server2","server_ip":"123.123.123.124","reset":true,"rescue":true,"wol":false}}`,
	})
	if err := run(c, "server", "describe", "server2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Features:") || !strings.Contains(out.String(), "reset, rescue") {
		t.Fatalf("output:\n%s", out)
	}
	out.Reset()
	if err := run(c, "server", "describe", "124.124.124.124"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "421") || !fake.called("GET /server/421") {
		t.Fatalf("output:\n%s", out)
	}
	if err := run(c, "server", "describe", "nope"); err == nil {
		t.Fatal("expected error for unknown name")
	}
}

func TestResetExecuteRequiresConfirmation(t *testing.T) {
	fake, c, _, _ := setup(t, map[string]string{
		"POST /reset/321": `{"reset":{"server_ip":"123.123.123.123","type":"hw"}}`,
	})
	err := run(c, "reset", "execute", "321", "--type", "hw")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected refusal, got %v", err)
	}
	if fake.called("POST /reset/321") {
		t.Fatal("reset was sent without confirmation")
	}
	c.Yes = true
	if err := run(c, "reset", "execute", "321", "--type", "hw"); err != nil {
		t.Fatal(err)
	}
	if got := fake.forms["POST /reset/321"]["type"]; len(got) != 1 || got[0] != "hw" {
		t.Fatalf("type = %v", got)
	}
	if err := run(c, "reset", "execute", "321", "--type", "bogus"); err == nil {
		t.Fatal("invalid type accepted")
	}
}

func TestRescueEnablePasswordOnStderrOnly(t *testing.T) {
	fake, c, out, errb := setup(t, map[string]string{
		"POST /boot/321/rescue": `{"rescue":{"server_ip":"123.123.123.123","server_number":321,"os":"linux","active":true,"password":"jEt0dtUvomlyOwRr","authorized_key":[],"host_key":[]}}`,
	})
	c.OutputFlag = "json"
	if err := run(c, "rescue", "enable", "321", "--ssh-key", "aa:bb", "--ssh-key", "cc:dd"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "jEt0dtUvomlyOwRr") {
		t.Fatalf("password leaked to stdout:\n%s", out)
	}
	if strings.Count(errb.String(), "jEt0dtUvomlyOwRr") != 1 {
		t.Fatalf("stderr: %q", errb)
	}
	form := fake.forms["POST /boot/321/rescue"]
	if form["os"][0] != "linux" || len(form["authorized_key[]"]) != 2 {
		t.Fatalf("form: %v", form)
	}
}

func TestFailoverSwitchResolvesServer(t *testing.T) {
	fake, c, _, _ := setup(t, map[string]string{
		"GET /server":                serversJSON,
		"POST /failover/78.46.1.200": `{"failover":{"ip":"78.46.1.200","netmask":"255.255.255.255","server_number":321,"active_server_ip":"123.123.123.124"}}`,
	})
	c.Yes = true
	if err := run(c, "failover", "switch", "78.46.1.200", "--to", "server2"); err != nil {
		t.Fatal(err)
	}
	if got := fake.forms["POST /failover/78.46.1.200"]["active_server_ip"]; len(got) != 1 || got[0] != "123.123.123.124" {
		t.Fatalf("active_server_ip = %v", got)
	}
}

func TestVSwitchAddServerResolvesNames(t *testing.T) {
	fake, c, _, _ := setup(t, map[string]string{
		"GET /server":            serversJSON,
		"POST /vswitch/9/server": ``,
	})
	if err := run(c, "vswitch", "add-server", "9", "server1", "421"); err == nil {
		t.Fatal("expected confirmation refusal")
	}
	c.Yes = true
	if err := run(c, "vswitch", "add-server", "9", "server1", "421"); err != nil {
		t.Fatal(err)
	}
	if got := fake.forms["POST /vswitch/9/server"]["server[]"]; strings.Join(got, ",") != "321,421" {
		t.Fatalf("server[] = %v", got)
	}
}

func TestParseFirewallFile(t *testing.T) {
	ok := `{"status":"active","whitelist_hos":true,"rules":{"input":[{"name":"ssh","ip_version":"ipv4","protocol":"tcp","dst_port":"22","src_ip":"10.0.0.0/8","action":"accept"}],"output":[{"action":"accept"}]}}`
	fw, err := parseFirewallFile([]byte(ok))
	if err != nil || len(fw.Rules.Input) != 1 || !fw.WhitelistHOS {
		t.Fatalf("%v %+v", err, fw)
	}
	if _, err := parseFirewallFile([]byte(`{"firewall":` + ok + `}`)); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if fw, err := parseFirewallFile([]byte(`{"input":[{"action":"discard"}]}`)); err != nil || len(fw.Rules.Input) != 1 {
		t.Fatalf("rules only: %v", err)
	}
	bad := map[string]string{
		"action":     `{"rules":{"input":[{"name":"x","action":"drop"}]}}`,
		"protocol":   `{"rules":{"input":[{"ip_version":"ipv4","protocol":"sctp","action":"accept"}]}}`,
		"proto-nov":  `{"rules":{"input":[{"protocol":"tcp","action":"accept"}]}}`,
		"src_ip v6":  `{"rules":{"input":[{"ip_version":"ipv6","src_ip":"::1","action":"accept"}]}}`,
		"port":       `{"rules":{"input":[{"dst_port":"http","action":"accept"}]}}`,
		"unknown":    `{"rules":{"input":[{"dport":"22","action":"accept"}]}}`,
		"empty":      `{"rules":{}}`,
		"not json":   `[1,2]`,
		"bad status": `{"status":"on","rules":{"input":[{"action":"accept"}]}}`,
	}
	for name, data := range bad {
		if _, err := parseFirewallFile([]byte(data)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestMarketFilter(t *testing.T) {
	offers := []client.MarketProduct{
		{ID: 1, CPU: "Intel Xeon E3-1275V6", MemorySize: 64, Price: "40.0000", Datacenter: "FSN1-DC1", Description: []string{"2x HDD SATA 4,0 TB"}},
		{ID: 2, CPU: "AMD Ryzen 7 3700X", MemorySize: 64, Price: "35.5000", Datacenter: "HEL1-DC2", Description: []string{"2x SSD U.2 NVMe 1,92 TB"}},
		{ID: 3, CPU: "AMD Ryzen 5 3600", MemorySize: 32, Price: "30.0000", Datacenter: "FSN1-DC5", Description: []string{"2x SSD SATA 512 GB", "1x HDD SATA 8,0 TB"}},
	}
	ids := func(ps []client.MarketProduct) string {
		var s []string
		for _, p := range ps {
			s = append(s, fmt.Sprint(p.ID))
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		f    marketFilter
		want string
	}{
		{marketFilter{}, "3,2,1"},
		{marketFilter{minRAM: 64}, "2,1"},
		{marketFilter{maxPrice: 36}, "3,2"},
		{marketFilter{cpu: "ryzen"}, "3,2"},
		{marketFilter{diskType: "nvme"}, "2"},
		{marketFilter{diskType: "ssd"}, "3"},
		{marketFilter{diskType: "hdd"}, "3,1"},
		{marketFilter{dc: "fsn1"}, "3,1"},
		{marketFilter{limit: 1}, "3"},
	}
	for _, tc := range cases {
		if got := ids(tc.f.apply(offers)); got != tc.want {
			t.Errorf("%+v: got %s want %s", tc.f, got, tc.want)
		}
	}
}

func TestCommandTree(t *testing.T) {
	cmd := NewCommand(&cli.CLI{})
	for _, path := range [][]string{
		{"server", "list"}, {"server", "cancellation"}, {"reset", "execute"}, {"rescue", "enable"},
		{"linux", "enable"}, {"wol"}, {"ip", "describe"}, {"subnet", "list"}, {"rdns", "set"},
		{"failover", "switch"}, {"traffic"}, {"firewall", "set"}, {"firewall", "template", "list"},
		{"vswitch", "remove-server"}, {"ssh-key", "create"}, {"order", "server-market", "list"},
		{"order", "server", "describe"}, {"order", "transaction", "list"},
	} {
		found, _, err := cmd.Find(path)
		if err != nil || found.Name() != path[len(path)-1] {
			t.Errorf("missing command %v (%v)", path, err)
		}
	}
	// Purchasing and cancelling must not exist.
	for _, path := range [][]string{{"order", "server", "buy"}, {"server", "cancel"}} {
		if found, _, _ := cmd.Find(path); found != nil && found.Name() == path[len(path)-1] {
			t.Errorf("command %v must not exist", path)
		}
	}
}
