package client

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// Typed endpoints. Paths use url.PathEscape for user-supplied segments (IPv6
// addresses and fingerprints contain ':' which is valid in a path segment).
//
// List endpoints answer 404 (NOT_FOUND, SERVER_NOT_FOUND...) when there is
// nothing to list; list helpers turn that into an empty slice.
//
// Deliberately NOT implemented (see internal/robot/order.go):
//   - POST /order/*/transaction (purchasing servers/addons),
//   - POST /server/{n}/cancellation (cancelling servers),
//   - IP/subnet cancellation and storage boxes (moved to the Hetzner API).

func esc(s string) string { return url.PathEscape(s) }

func list[T any](ctx context.Context, c *Client, path string, q url.Values, key string) ([]T, error) {
	var out []T
	if err := c.Get(ctx, path, q, key, &out); err != nil {
		if IsNotFound(err) {
			return []T{}, nil
		}
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

func one[T any](ctx context.Context, c *Client, method, path string, form url.Values, key string) (*T, error) {
	out := new(T)
	if err := c.Do(ctx, method, path, form, key, out); err != nil {
		return nil, err
	}
	return out, nil
}

func serverFilter(serverIP string) url.Values {
	if serverIP == "" {
		return nil
	}
	return url.Values{"server_ip": {serverIP}}
}

// ---- servers ----

// ListServers is GET /server.
func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	return list[Server](ctx, c, "/server", nil, "server")
}

// GetServer is GET /server/{number}.
func (c *Client) GetServer(ctx context.Context, number int) (*Server, error) {
	return one[Server](ctx, c, "GET", fmt.Sprintf("/server/%d", number), nil, "server")
}

// RenameServer is POST /server/{number} server_name=...
func (c *Client) RenameServer(ctx context.Context, number int, name string) (*Server, error) {
	return one[Server](ctx, c, "POST", fmt.Sprintf("/server/%d", number), url.Values{"server_name": {name}}, "server")
}

// GetCancellation is GET /server/{number}/cancellation (read only).
func (c *Client) GetCancellation(ctx context.Context, number int) (*Cancellation, error) {
	return one[Cancellation](ctx, c, "GET", fmt.Sprintf("/server/%d/cancellation", number), nil, "cancellation")
}

// ---- reset / wol ----

// ListResets is GET /reset.
func (c *Client) ListResets(ctx context.Context) ([]Reset, error) {
	return list[Reset](ctx, c, "/reset", nil, "reset")
}

// GetReset is GET /reset/{number}.
func (c *Client) GetReset(ctx context.Context, number int) (*Reset, error) {
	return one[Reset](ctx, c, "GET", fmt.Sprintf("/reset/%d", number), nil, "reset")
}

// ExecuteReset is POST /reset/{number} type=...
func (c *Client) ExecuteReset(ctx context.Context, number int, typ string) (*Reset, error) {
	return one[Reset](ctx, c, "POST", fmt.Sprintf("/reset/%d", number), url.Values{"type": {typ}}, "reset")
}

// GetWOL is GET /wol/{number}.
func (c *Client) GetWOL(ctx context.Context, number int) (*WOL, error) {
	return one[WOL](ctx, c, "GET", fmt.Sprintf("/wol/%d", number), nil, "wol")
}

// SendWOL is POST /wol/{number}.
func (c *Client) SendWOL(ctx context.Context, number int) (*WOL, error) {
	return one[WOL](ctx, c, "POST", fmt.Sprintf("/wol/%d", number), nil, "wol")
}

// ---- boot: rescue / linux ----

// GetRescue is GET /boot/{number}/rescue.
func (c *Client) GetRescue(ctx context.Context, number int) (*Rescue, error) {
	return one[Rescue](ctx, c, "GET", fmt.Sprintf("/boot/%d/rescue", number), nil, "rescue")
}

// GetLastRescue is GET /boot/{number}/rescue/last.
func (c *Client) GetLastRescue(ctx context.Context, number int) (*Rescue, error) {
	return one[Rescue](ctx, c, "GET", fmt.Sprintf("/boot/%d/rescue/last", number), nil, "rescue")
}

// EnableRescue is POST /boot/{number}/rescue os=...&authorized_key[]=...
func (c *Client) EnableRescue(ctx context.Context, number int, os, keyboard string, keys []string) (*Rescue, error) {
	v := url.Values{"os": {os}}
	if keyboard != "" {
		v.Set("keyboard", keyboard)
	}
	AddArray(v, "authorized_key", keys...)
	return one[Rescue](ctx, c, "POST", fmt.Sprintf("/boot/%d/rescue", number), v, "rescue")
}

// DisableRescue is DELETE /boot/{number}/rescue.
func (c *Client) DisableRescue(ctx context.Context, number int) (*Rescue, error) {
	return one[Rescue](ctx, c, "DELETE", fmt.Sprintf("/boot/%d/rescue", number), nil, "rescue")
}

// GetLinux is GET /boot/{number}/linux.
func (c *Client) GetLinux(ctx context.Context, number int) (*Linux, error) {
	return one[Linux](ctx, c, "GET", fmt.Sprintf("/boot/%d/linux", number), nil, "linux")
}

// EnableLinux is POST /boot/{number}/linux dist=...&lang=...&authorized_key[]=...
func (c *Client) EnableLinux(ctx context.Context, number int, dist, lang string, keys []string) (*Linux, error) {
	v := url.Values{"dist": {dist}}
	if lang != "" {
		v.Set("lang", lang)
	}
	AddArray(v, "authorized_key", keys...)
	return one[Linux](ctx, c, "POST", fmt.Sprintf("/boot/%d/linux", number), v, "linux")
}

// DisableLinux is DELETE /boot/{number}/linux.
func (c *Client) DisableLinux(ctx context.Context, number int) (*Linux, error) {
	return one[Linux](ctx, c, "DELETE", fmt.Sprintf("/boot/%d/linux", number), nil, "linux")
}

// ---- IPs, subnets, rDNS, failover, traffic ----

// ListIPs is GET /ip (optionally filtered by server main IP).
func (c *Client) ListIPs(ctx context.Context, serverIP string) ([]IP, error) {
	return list[IP](ctx, c, "/ip", serverFilter(serverIP), "ip")
}

// GetIP is GET /ip/{ip}.
func (c *Client) GetIP(ctx context.Context, ip string) (*IP, error) {
	return one[IP](ctx, c, "GET", "/ip/"+esc(ip), nil, "ip")
}

// ListSubnets is GET /subnet.
func (c *Client) ListSubnets(ctx context.Context, serverIP string) ([]Subnet, error) {
	return list[Subnet](ctx, c, "/subnet", serverFilter(serverIP), "subnet")
}

// GetSubnet is GET /subnet/{net-ip}.
func (c *Client) GetSubnet(ctx context.Context, netIP string) (*Subnet, error) {
	return one[Subnet](ctx, c, "GET", "/subnet/"+esc(netIP), nil, "subnet")
}

// ListRDNS is GET /rdns.
func (c *Client) ListRDNS(ctx context.Context, serverIP string) ([]RDNS, error) {
	return list[RDNS](ctx, c, "/rdns", serverFilter(serverIP), "rdns")
}

// GetRDNS is GET /rdns/{ip}.
func (c *Client) GetRDNS(ctx context.Context, ip string) (*RDNS, error) {
	return one[RDNS](ctx, c, "GET", "/rdns/"+esc(ip), nil, "rdns")
}

// SetRDNS is POST /rdns/{ip} ptr=... (create or update).
func (c *Client) SetRDNS(ctx context.Context, ip, ptr string) (*RDNS, error) {
	return one[RDNS](ctx, c, "POST", "/rdns/"+esc(ip), url.Values{"ptr": {ptr}}, "rdns")
}

// DeleteRDNS is DELETE /rdns/{ip}.
func (c *Client) DeleteRDNS(ctx context.Context, ip string) error {
	return c.Delete(ctx, "/rdns/"+esc(ip), nil, "", nil)
}

// ListFailovers is GET /failover.
func (c *Client) ListFailovers(ctx context.Context) ([]Failover, error) {
	return list[Failover](ctx, c, "/failover", nil, "failover")
}

// GetFailover is GET /failover/{ip}.
func (c *Client) GetFailover(ctx context.Context, ip string) (*Failover, error) {
	return one[Failover](ctx, c, "GET", "/failover/"+esc(ip), nil, "failover")
}

// SwitchFailover is POST /failover/{ip} active_server_ip=...
func (c *Client) SwitchFailover(ctx context.Context, ip, activeServerIP string) (*Failover, error) {
	return one[Failover](ctx, c, "POST", "/failover/"+esc(ip), url.Values{"active_server_ip": {activeServerIP}}, "failover")
}

// TrafficQuery are the inputs of POST /traffic.
type TrafficQuery struct {
	IPs          []string
	Subnets      []string
	Type         string // day, month, year
	From, To     string
	SingleValues bool
}

// GetTraffic is POST /traffic (a read-only query despite the verb).
func (c *Client) GetTraffic(ctx context.Context, q TrafficQuery) (*Traffic, error) {
	v := url.Values{"type": {q.Type}, "from": {q.From}, "to": {q.To}}
	AddArray(v, "ip", q.IPs...)
	AddArray(v, "subnet", q.Subnets...)
	if q.SingleValues {
		v.Set("single_values", "true")
	}
	return one[Traffic](ctx, c, "POST", "/traffic", v, "traffic")
}

// ---- SSH keys ----

// ListKeys is GET /key.
func (c *Client) ListKeys(ctx context.Context) ([]SSHKey, error) {
	return list[SSHKey](ctx, c, "/key", nil, "key")
}

// GetKey is GET /key/{fingerprint}.
func (c *Client) GetKey(ctx context.Context, fp string) (*SSHKey, error) {
	return one[SSHKey](ctx, c, "GET", "/key/"+esc(fp), nil, "key")
}

// CreateKey is POST /key name=...&data=...
func (c *Client) CreateKey(ctx context.Context, name, data string) (*SSHKey, error) {
	return one[SSHKey](ctx, c, "POST", "/key", url.Values{"name": {name}, "data": {data}}, "key")
}

// DeleteKey is DELETE /key/{fingerprint}.
func (c *Client) DeleteKey(ctx context.Context, fp string) error {
	return c.Delete(ctx, "/key/"+esc(fp), nil, "", nil)
}

// ---- firewall ----

// GetFirewall is GET /firewall/{number}.
func (c *Client) GetFirewall(ctx context.Context, number int) (*Firewall, error) {
	return one[Firewall](ctx, c, "GET", fmt.Sprintf("/firewall/%d", number), nil, "firewall")
}

// FirewallUpdate are the inputs of POST /firewall/{number}. Either Rules (with
// WhitelistHOS) or TemplateID is used.
type FirewallUpdate struct {
	Status       string // active, disabled
	FilterIPv6   *bool
	WhitelistHOS *bool
	Rules        *FirewallRules
	TemplateID   string
}

// Values encodes the update as rules[input][0][name]=... form fields.
func (u FirewallUpdate) Values() url.Values {
	v := url.Values{}
	if u.Status != "" {
		v.Set("status", u.Status)
	}
	if u.FilterIPv6 != nil {
		v.Set("filter_ipv6", strconv.FormatBool(*u.FilterIPv6))
	}
	if u.TemplateID != "" {
		v.Set("template_id", u.TemplateID)
		return v
	}
	if u.WhitelistHOS != nil {
		v.Set("whitelist_hos", strconv.FormatBool(*u.WhitelistHOS))
	}
	if u.Rules != nil {
		encodeRules(v, "input", u.Rules.Input)
		encodeRules(v, "output", u.Rules.Output)
	}
	return v
}

func encodeRules(v url.Values, dir string, rules []FirewallRule) {
	for i, r := range rules {
		p := fmt.Sprintf("rules[%s][%d]", dir, i)
		set := func(k, val string) {
			if val != "" {
				v.Set(p+"["+k+"]", val)
			}
		}
		set("name", r.Name)
		set("ip_version", r.IPVersion)
		set("dst_ip", r.DstIP)
		set("src_ip", r.SrcIP)
		set("dst_port", r.DstPort)
		set("src_port", r.SrcPort)
		set("protocol", r.Protocol)
		set("tcp_flags", r.TCPFlags)
		set("action", r.Action)
	}
}

// SetFirewall is POST /firewall/{number}.
func (c *Client) SetFirewall(ctx context.Context, number int, u FirewallUpdate) (*Firewall, error) {
	return one[Firewall](ctx, c, "POST", fmt.Sprintf("/firewall/%d", number), u.Values(), "firewall")
}

// ListFirewallTemplates is GET /firewall/template.
func (c *Client) ListFirewallTemplates(ctx context.Context) ([]FirewallTemplate, error) {
	return list[FirewallTemplate](ctx, c, "/firewall/template", nil, "firewall_template")
}

// GetFirewallTemplate is GET /firewall/template/{id}.
func (c *Client) GetFirewallTemplate(ctx context.Context, id string) (*FirewallTemplate, error) {
	return one[FirewallTemplate](ctx, c, "GET", "/firewall/template/"+esc(id), nil, "firewall_template")
}

// ---- vSwitch (not wrapped) ----

// ListVSwitches is GET /vswitch.
func (c *Client) ListVSwitches(ctx context.Context) ([]VSwitch, error) {
	return list[VSwitch](ctx, c, "/vswitch", nil, "")
}

// GetVSwitch is GET /vswitch/{id}.
func (c *Client) GetVSwitch(ctx context.Context, id int) (*VSwitch, error) {
	return one[VSwitch](ctx, c, "GET", fmt.Sprintf("/vswitch/%d", id), nil, "")
}

// CreateVSwitch is POST /vswitch name=...&vlan=...
func (c *Client) CreateVSwitch(ctx context.Context, name string, vlan int) (*VSwitch, error) {
	return one[VSwitch](ctx, c, "POST", "/vswitch", url.Values{"name": {name}, "vlan": {strconv.Itoa(vlan)}}, "")
}

// CancelVSwitch is DELETE /vswitch/{id} cancellation_date=now|yyyy-MM-dd.
func (c *Client) CancelVSwitch(ctx context.Context, id int, date string) error {
	return c.Delete(ctx, fmt.Sprintf("/vswitch/%d", id), url.Values{"cancellation_date": {date}}, "", nil)
}

// AddVSwitchServers is POST /vswitch/{id}/server server[]=...
func (c *Client) AddVSwitchServers(ctx context.Context, id int, servers []string) error {
	v := url.Values{}
	AddArray(v, "server", servers...)
	return c.Post(ctx, fmt.Sprintf("/vswitch/%d/server", id), v, "", nil)
}

// RemoveVSwitchServers is DELETE /vswitch/{id}/server server[]=...
func (c *Client) RemoveVSwitchServers(ctx context.Context, id int, servers []string) error {
	v := url.Values{}
	AddArray(v, "server", servers...)
	return c.Delete(ctx, fmt.Sprintf("/vswitch/%d/server", id), v, "", nil)
}

// ---- ordering (read only) ----

// ListServerProducts is GET /order/server/product.
func (c *Client) ListServerProducts(ctx context.Context, q url.Values) ([]ServerProduct, error) {
	return list[ServerProduct](ctx, c, "/order/server/product", q, "product")
}

// GetServerProduct is GET /order/server/product/{id}.
func (c *Client) GetServerProduct(ctx context.Context, id string) (*ServerProduct, error) {
	return one[ServerProduct](ctx, c, "GET", "/order/server/product/"+esc(id), nil, "product")
}

// ListMarketProducts is GET /order/server_market/product.
func (c *Client) ListMarketProducts(ctx context.Context) ([]MarketProduct, error) {
	return list[MarketProduct](ctx, c, "/order/server_market/product", nil, "product")
}

// GetMarketProduct is GET /order/server_market/product/{id}.
func (c *Client) GetMarketProduct(ctx context.Context, id string) (*MarketProduct, error) {
	return one[MarketProduct](ctx, c, "GET", "/order/server_market/product/"+esc(id), nil, "product")
}

// ListTransactions is GET /order/{kind}/transaction, kind = server | server_market.
func (c *Client) ListTransactions(ctx context.Context, kind string) ([]Transaction, error) {
	out, err := list[Transaction](ctx, c, "/order/"+kind+"/transaction", nil, "transaction")
	for i := range out {
		out[i].Kind = kind
	}
	return out, err
}

// GetTransaction is GET /order/{kind}/transaction/{id}.
func (c *Client) GetTransaction(ctx context.Context, kind, id string) (*Transaction, error) {
	t, err := one[Transaction](ctx, c, "GET", "/order/"+kind+"/transaction/"+esc(id), nil, "transaction")
	if t != nil {
		t.Kind = kind
	}
	return t, err
}
