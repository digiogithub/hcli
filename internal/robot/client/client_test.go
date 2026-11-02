package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	opts = append([]Option{WithRetry(2, time.Millisecond)}, opts...)
	return New(srv.URL, "user", "s3cret", opts...)
}

func TestUnwrapListAndObject(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server":
			fmt.Fprint(w, `[{"server":{"server_number":321,"server_name":"a","subnet":[{"ip":"2a01::","mask":"64"}]}},{"server":{"server_number":421,"server_name":"b","subnet":null}}]`)
		case "/server/321":
			fmt.Fprint(w, `{"server":{"server_number":321,"server_name":"a","reset":true,"linked_storagebox":7}}`)
		case "/vswitch":
			fmt.Fprint(w, `[{"id":1,"name":"v","vlan":4000,"cancelled":false}]`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	servers, err := cl.ListServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ServerNumber != 321 || servers[1].ServerName != "b" {
		t.Fatalf("unexpected servers: %+v", servers)
	}
	if string(servers[0].Subnet[0].Mask) != "64" {
		t.Fatalf("mask: %q", servers[0].Subnet[0].Mask)
	}
	s, err := cl.GetServer(ctx, 321)
	if err != nil {
		t.Fatal(err)
	}
	if s.Reset == nil || !*s.Reset || deref(s.LinkedStoragebox) != 7 {
		t.Fatalf("unexpected server: %+v", s)
	}
	vs, err := cl.ListVSwitches(ctx)
	if err != nil || len(vs) != 1 || vs[0].VLAN != 4000 {
		t.Fatalf("vswitches: %+v %v", vs, err)
	}
}

func deref[T any](p *T) T {
	var z T
	if p == nil {
		return z
	}
	return *p
}

func TestUnwrapHelper(t *testing.T) {
	got, err := Unwrap([]byte(`[{"key":{"a":1}},{"key":{"a":2}}]`), "key")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[{"a":1},{"a":2}]` {
		t.Fatalf("got %s", got)
	}
	got, _ = Unwrap([]byte(`{"key":{"a":1}}`), "key")
	if string(got) != `{"a":1}` {
		t.Fatalf("got %s", got)
	}
	// Objects with other keys are left alone.
	got, _ = Unwrap([]byte(`{"id":1,"name":"x"}`), "key")
	if string(got) != `{"id":1,"name":"x"}` {
		t.Fatalf("got %s", got)
	}
}

func TestRequestHeadersAndFormArrays(t *testing.T) {
	var gotForm map[string][]string
	var gotCT, gotUA, gotMethod string
	var user, pass string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotCT, gotUA = r.Method, r.Header.Get("Content-Type"), r.Header.Get("User-Agent")
		user, pass, _ = r.BasicAuth()
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		gotForm = r.PostForm
	})
	if err := cl.AddVSwitchServers(context.Background(), 4321, []string{"321", "123.123.123.124"}); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotCT != "application/x-www-form-urlencoded" {
		t.Fatalf("method %s content-type %s", gotMethod, gotCT)
	}
	if !strings.HasPrefix(gotUA, "hcli/") {
		t.Fatalf("user agent %q", gotUA)
	}
	if user != "user" || pass != "s3cret" {
		t.Fatalf("basic auth %q/%q", user, pass)
	}
	if got := gotForm["server[]"]; len(got) != 2 || got[0] != "321" || got[1] != "123.123.123.124" {
		t.Fatalf("server[] = %v (form %v)", got, gotForm)
	}
}

func TestDeleteSendsFormBody(t *testing.T) {
	var date, method string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		// net/http only parses bodies of POST/PUT/PATCH: read it by hand.
		body, _ := io.ReadAll(r.Body)
		v, _ := url.ParseQuery(string(body))
		date = v.Get("cancellation_date")
	})
	if err := cl.CancelVSwitch(context.Background(), 1, "now"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodDelete || date != "now" {
		t.Fatalf("%s cancellation_date=%q", method, date)
	}
}

func TestFirewallFormEncoding(t *testing.T) {
	wl := true
	u := FirewallUpdate{
		Status:       "active",
		WhitelistHOS: &wl,
		Rules: &FirewallRules{
			Input:  []FirewallRule{{Name: "ssh", IPVersion: "ipv4", Protocol: "tcp", DstPort: "22", Action: "accept"}, {Name: "rest", Action: "discard"}},
			Output: []FirewallRule{{Name: "all", Action: "accept"}},
		},
	}
	v := u.Values()
	want := map[string]string{
		"status":                      "active",
		"whitelist_hos":               "true",
		"rules[input][0][name]":       "ssh",
		"rules[input][0][ip_version]": "ipv4",
		"rules[input][0][protocol]":   "tcp",
		"rules[input][0][dst_port]":   "22",
		"rules[input][0][action]":     "accept",
		"rules[input][1][action]":     "discard",
		"rules[output][0][action]":    "accept",
	}
	for k, val := range want {
		if v.Get(k) != val {
			t.Errorf("%s = %q, want %q", k, v.Get(k), val)
		}
	}
	if _, ok := v["rules[input][1][src_ip]"]; ok {
		t.Error("empty fields must be omitted (wildcards)")
	}
	tv := FirewallUpdate{Status: "active", TemplateID: "7", WhitelistHOS: &wl}.Values()
	if tv.Get("template_id") != "7" || tv.Has("whitelist_hos") {
		t.Errorf("template update: %v", tv)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/server/1":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"status":404,"code":"SERVER_NOT_FOUND","message":"Server with id 1 not found"}}`)
		case "/rdns/1.2.3.4":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"status":400,"code":"INVALID_INPUT","message":"invalid input","missing":["ptr"],"invalid":null}}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "<html>oops</html>")
		}
	})
	ctx := context.Background()
	_, err := cl.GetServer(ctx, 1)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 404 || ae.Code != "SERVER_NOT_FOUND" || ae.Message == "" {
		t.Fatalf("got %#v", err)
	}
	if !IsNotFound(err) || !HasCode(err, "SERVER_NOT_FOUND") {
		t.Fatal("helpers")
	}
	_, err = cl.SetRDNS(ctx, "1.2.3.4", "")
	if !errors.As(err, &ae) || ae.Code != "INVALID_INPUT" || len(ae.Missing) != 1 || !strings.Contains(err.Error(), "missing: ptr") {
		t.Fatalf("got %v", err)
	}
	_, err = cl.GetIP(ctx, "9.9.9.9")
	if !errors.As(err, &ae) || ae.Status != 500 || !strings.Contains(ae.Message, "oops") {
		t.Fatalf("non-JSON error: %v", err)
	}
}

func TestListNotFoundIsEmpty(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"status":404,"code":"NOT_FOUND","message":"No IP addresses found"}}`)
	})
	ips, err := cl.ListIPs(context.Background(), "1.2.3.4")
	if err != nil || ips == nil || len(ips) != 0 {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestRateLimitRetry(t *testing.T) {
	var calls atomic.Int32
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":{"status":403,"code":"RATE_LIMIT_EXCEEDED","max_request":200,"interval":3600,"message":"rate limit exceeded"}}`)
		case 2:
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			_ = r.ParseForm()
			if r.PostForm.Get("type") != "hw" {
				t.Errorf("body lost on retry: %v", r.PostForm)
			}
			fmt.Fprint(w, `{"reset":{"server_ip":"1.2.3.4","type":"hw"}}`)
		}
	})
	r, err := cl.ExecuteReset(context.Background(), 321, "hw")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || len(r.Type) != 1 || r.Type[0] != "hw" {
		t.Fatalf("calls=%d reset=%+v", calls.Load(), r)
	}
}

func TestRateLimitGivesUp(t *testing.T) {
	var calls atomic.Int32
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"status":403,"code":"RATE_LIMIT_EXCEEDED","max_request":200,"interval":3600,"message":"rate limit exceeded"}}`)
	})
	_, err := cl.ListServers(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || !ae.IsRateLimit() || !strings.Contains(err.Error(), "200 requests per 3600s") {
		t.Fatalf("err=%v", err)
	}
	if calls.Load() != 3 { // 1 + 2 retries
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestForbiddenWithoutRateLimitIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"status":403,"code":"FORBIDDEN","message":"no"}}`)
	})
	if _, err := cl.ListServers(context.Background()); err == nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestDebugNeverLogsCredentials(t *testing.T) {
	var logs []string
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"key":{"name":"k","fingerprint":"aa"}}`)
	}, WithDebug(func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }))
	if _, err := cl.CreateKey(context.Background(), "k", "ssh-ed25519 AAAA"); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(logs, "\n")
	if !strings.Contains(all, "POST /key -> 200") {
		t.Fatalf("logs: %q", all)
	}
	for _, secret := range []string{"s3cret", "Authorization", "Basic", "AAAA"} {
		if strings.Contains(all, secret) {
			t.Fatalf("debug log leaks %q: %q", secret, all)
		}
	}
}

func TestStringListAndKeyRef(t *testing.T) {
	var r Rescue
	data := `{"os":["linux","vkvm"],"active":false,"password":null,"authorized_key":[{"key":{"name":"k1","fingerprint":"aa:bb","type":"ED25519","size":256}}],"host_key":[{"fingerprint":"cc","type":"RSA","size":4096}]}`
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.OS) != 2 || r.AuthorizedKey[0].Name != "k1" || r.HostKey[0].Fingerprint != "cc" {
		t.Fatalf("%+v", r)
	}
	if err := json.Unmarshal([]byte(`{"os":"linux","active":true,"password":"x"}`), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.OS) != 1 || r.OS[0] != "linux" {
		t.Fatalf("%+v", r.OS)
	}
	var tp TransactionProduct
	if err := json.Unmarshal([]byte(`{"id":283693,"dist":"Rescue system"}`), &tp); err != nil || tp.ID != "283693" {
		t.Fatalf("%v %+v", err, tp)
	}
}
