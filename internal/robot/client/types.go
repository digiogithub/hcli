package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// StringList decodes fields documented as "Array|String": an array of
// options before activation, the chosen value afterwards (rescue os, linux
// dist, cancellation_reason...). null decodes to an empty list.
type StringList []string

// UnmarshalJSON accepts a string, an array of strings/numbers or null.
func (s *StringList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || string(b) == "null":
		*s = nil
		return nil
	case b[0] == '[':
		var raw []json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
		out := make([]string, 0, len(raw))
		for _, r := range raw {
			var f FlexString
			if err := f.UnmarshalJSON(r); err != nil {
				return err
			}
			out = append(out, string(f))
		}
		*s = out
		return nil
	}
	var f FlexString
	if err := f.UnmarshalJSON(b); err != nil {
		return err
	}
	*s = StringList{string(f)}
	return nil
}

// FlexString decodes a JSON string or number (the API is inconsistent, e.g.
// subnet "mask" is "64" in /server and 64 in /subnet; product ids are strings
// for standard products and integers for the server market).
type FlexString string

// UnmarshalJSON accepts a string, number, bool or null.
func (f *FlexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = FlexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*f = FlexString(n.String())
		return nil
	}
	var v bool
	if err := json.Unmarshal(b, &v); err == nil {
		*f = FlexString(strconv.FormatBool(v))
		return nil
	}
	return fmt.Errorf("cannot decode %s as string", string(b))
}

// Server is an entry of GET /server and GET /server/{number}.
type Server struct {
	ServerIP         string         `json:"server_ip"`
	ServerIPv6Net    string         `json:"server_ipv6_net"`
	ServerNumber     int            `json:"server_number"`
	ServerName       string         `json:"server_name"`
	Product          string         `json:"product"`
	DC               string         `json:"dc"`
	Traffic          string         `json:"traffic"`
	Status           string         `json:"status"`
	Cancelled        bool           `json:"cancelled"`
	PaidUntil        string         `json:"paid_until"`
	IP               []string       `json:"ip"`
	Subnet           []ServerSubnet `json:"subnet"`
	Reset            *bool          `json:"reset,omitempty"`
	Rescue           *bool          `json:"rescue,omitempty"`
	VNC              *bool          `json:"vnc,omitempty"`
	Windows          *bool          `json:"windows,omitempty"`
	Plesk            *bool          `json:"plesk,omitempty"`
	CPanel           *bool          `json:"cpanel,omitempty"`
	WOL              *bool          `json:"wol,omitempty"`
	HotSwap          *bool          `json:"hot_swap,omitempty"`
	LinkedStoragebox *int           `json:"linked_storagebox,omitempty"`
}

// ServerSubnet is a subnet assigned to a server.
type ServerSubnet struct {
	IP   string     `json:"ip"`
	Mask FlexString `json:"mask"`
}

// Cancellation is GET /server/{number}/cancellation.
type Cancellation struct {
	ServerIP                 string     `json:"server_ip"`
	ServerIPv6Net            string     `json:"server_ipv6_net"`
	ServerNumber             int        `json:"server_number"`
	ServerName               string     `json:"server_name"`
	EarliestCancellationDate string     `json:"earliest_cancellation_date"`
	Cancelled                bool       `json:"cancelled"`
	ReservationPossible      bool       `json:"reservation_possible"`
	Reserved                 bool       `json:"reserved"`
	CancellationDate         *string    `json:"cancellation_date"`
	CancellationReason       StringList `json:"cancellation_reason"`
}

// Reset is GET /reset/{number} (Type lists the options) and the answer of
// POST /reset/{number} (Type holds the executed option).
type Reset struct {
	ServerIP        string     `json:"server_ip"`
	ServerIPv6Net   string     `json:"server_ipv6_net"`
	ServerNumber    int        `json:"server_number,omitempty"`
	Type            StringList `json:"type"`
	OperatingStatus string     `json:"operating_status,omitempty"`
}

// WOL is the Wake on LAN object.
type WOL struct {
	ServerIP      string `json:"server_ip"`
	ServerIPv6Net string `json:"server_ipv6_net"`
	ServerNumber  int    `json:"server_number"`
}

// KeyRef is an SSH key referenced from boot configs and transactions. The
// API wraps it as {"key":{...}}; a flat object or a bare fingerprint string
// are accepted too.
type KeyRef struct {
	Name        string `json:"name,omitempty"`
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type,omitempty"`
	Size        int    `json:"size,omitempty"`
}

// UnmarshalJSON accepts {"key":{...}}, {...} or "fingerprint".
func (k *KeyRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &k.Fingerprint)
	}
	type plain KeyRef
	var env struct {
		Key *plain `json:"key"`
	}
	if err := json.Unmarshal(b, &env); err == nil && env.Key != nil {
		*k = KeyRef(*env.Key)
		return nil
	}
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*k = KeyRef(p)
	return nil
}

// Rescue is the rescue system boot configuration.
type Rescue struct {
	ServerIP      string     `json:"server_ip"`
	ServerIPv6Net string     `json:"server_ipv6_net"`
	ServerNumber  int        `json:"server_number"`
	OS            StringList `json:"os"`
	Active        bool       `json:"active"`
	Password      *string    `json:"password"`
	AuthorizedKey []KeyRef   `json:"authorized_key"`
	HostKey       []KeyRef   `json:"host_key"`
}

// Linux is the Linux installation boot configuration.
type Linux struct {
	ServerIP      string     `json:"server_ip"`
	ServerIPv6Net string     `json:"server_ipv6_net"`
	ServerNumber  int        `json:"server_number"`
	Dist          StringList `json:"dist"`
	Lang          StringList `json:"lang"`
	Active        bool       `json:"active"`
	Password      *string    `json:"password"`
	AuthorizedKey []KeyRef   `json:"authorized_key"`
	HostKey       []KeyRef   `json:"host_key"`
}

// IP is a single IP address (GET /ip).
type IP struct {
	IP           string  `json:"ip"`
	Gateway      string  `json:"gateway,omitempty"`
	Mask         int     `json:"mask,omitempty"`
	Broadcast    string  `json:"broadcast,omitempty"`
	ServerIP     string  `json:"server_ip"`
	ServerNumber int     `json:"server_number"`
	Locked       bool    `json:"locked"`
	SeparateMAC  *string `json:"separate_mac"`
	// Deprecated by Hetzner (removed on 2027-01-12).
	TrafficWarnings *bool `json:"traffic_warnings,omitempty"`
	TrafficHourly   *int  `json:"traffic_hourly,omitempty"`
	TrafficDaily    *int  `json:"traffic_daily,omitempty"`
	TrafficMonthly  *int  `json:"traffic_monthly,omitempty"`
}

// Subnet is GET /subnet.
type Subnet struct {
	IP           string `json:"ip"`
	Mask         int    `json:"mask"`
	Gateway      string `json:"gateway"`
	ServerIP     string `json:"server_ip"`
	ServerNumber int    `json:"server_number"`
	Failover     bool   `json:"failover"`
	Locked       bool   `json:"locked"`
	// Deprecated by Hetzner (removed on 2027-01-12).
	TrafficWarnings *bool `json:"traffic_warnings,omitempty"`
	TrafficHourly   *int  `json:"traffic_hourly,omitempty"`
	TrafficDaily    *int  `json:"traffic_daily,omitempty"`
	TrafficMonthly  *int  `json:"traffic_monthly,omitempty"`
}

// RDNS is a reverse DNS entry.
type RDNS struct {
	IP  string `json:"ip"`
	PTR string `json:"ptr"`
}

// Failover is a failover IP or net.
type Failover struct {
	IP             string  `json:"ip"`
	Netmask        string  `json:"netmask"`
	ServerIP       string  `json:"server_ip"`
	ServerIPv6Net  string  `json:"server_ipv6_net"`
	ServerNumber   int     `json:"server_number"`
	ActiveServerIP *string `json:"active_server_ip"`
}

// TrafficValue is in/out/sum in GB.
type TrafficValue struct {
	In  float64 `json:"in"`
	Out float64 `json:"out"`
	Sum float64 `json:"sum"`
}

// Traffic is the answer of POST /traffic. Each Data value is a TrafficValue,
// or a map interval -> TrafficValue when single_values=true.
type Traffic struct {
	Type string                     `json:"type"`
	From string                     `json:"from"`
	To   string                     `json:"to"`
	Data map[string]json.RawMessage `json:"data"`
}

// SSHKey is a stored public key.
type SSHKey struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"`
	Size        int    `json:"size"`
	Data        string `json:"data"`
	CreatedAt   string `json:"created_at"`
}

// Price is a net/gross price (strings with 4 decimals, as the API sends them).
type Price struct {
	Net         string  `json:"net"`
	Gross       string  `json:"gross"`
	HourlyNet   *string `json:"hourly_net,omitempty"`
	HourlyGross *string `json:"hourly_gross,omitempty"`
}

// LocationPrice is the price of a product in one location.
type LocationPrice struct {
	Location   string `json:"location"`
	Price      Price  `json:"price"`
	PriceSetup Price  `json:"price_setup"`
}

// Addon is an orderable addon (e.g. primary_ipv4).
type Addon struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Location *string         `json:"location,omitempty"`
	Min      int             `json:"min"`
	Max      int             `json:"max"`
	Prices   []LocationPrice `json:"prices"`
}

// ServerProduct is a standard product (GET /order/server/product).
type ServerProduct struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     []string        `json:"description"`
	Traffic         string          `json:"traffic"`
	Dist            []string        `json:"dist"`
	Lang            []string        `json:"lang"`
	Location        []string        `json:"location"`
	Prices          []LocationPrice `json:"prices"`
	OrderableAddons []Addon         `json:"orderable_addons"`
}

// MarketProduct is a server auction offer (GET /order/server_market/product).
type MarketProduct struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Description     []string `json:"description"`
	Traffic         string   `json:"traffic"`
	Dist            []string `json:"dist"`
	Lang            []string `json:"lang"`
	CPU             string   `json:"cpu"`
	CPUBenchmark    int      `json:"cpu_benchmark"`
	MemorySize      float64  `json:"memory_size"`
	HDDSize         float64  `json:"hdd_size"`
	HDDText         string   `json:"hdd_text"`
	HDDCount        int      `json:"hdd_count"`
	Datacenter      string   `json:"datacenter"`
	NetworkSpeed    string   `json:"network_speed"`
	Price           string   `json:"price"`
	PriceHourly     *string  `json:"price_hourly"`
	PriceSetup      string   `json:"price_setup"`
	PriceVAT        string   `json:"price_vat"`
	PriceHourlyVAT  *string  `json:"price_hourly_vat"`
	PriceSetupVAT   string   `json:"price_setup_vat"`
	FixedPrice      bool     `json:"fixed_price"`
	NextReduce      int      `json:"next_reduce"`
	NextReduceDate  string   `json:"next_reduce_date"`
	OrderableAddons []Addon  `json:"orderable_addons"`
}

// TransactionProduct is the product of an order transaction (standard or
// server market; market-only fields are empty for standard orders).
type TransactionProduct struct {
	ID           FlexString `json:"id"`
	Name         string     `json:"name"`
	Description  []string   `json:"description"`
	Traffic      string     `json:"traffic"`
	Dist         FlexString `json:"dist"`
	Lang         FlexString `json:"lang"`
	Location     *string    `json:"location,omitempty"`
	CPU          string     `json:"cpu,omitempty"`
	CPUBenchmark int        `json:"cpu_benchmark,omitempty"`
	MemorySize   float64    `json:"memory_size,omitempty"`
	HDDSize      float64    `json:"hdd_size,omitempty"`
	HDDText      string     `json:"hdd_text,omitempty"`
	HDDCount     int        `json:"hdd_count,omitempty"`
	Datacenter   string     `json:"datacenter,omitempty"`
	NetworkSpeed string     `json:"network_speed,omitempty"`
}

// Transaction is a server order (standard or market) of the last 30 days.
type Transaction struct {
	ID            string             `json:"id"`
	Date          string             `json:"date"`
	Status        string             `json:"status"`
	ServerNumber  *int               `json:"server_number"`
	ServerIP      *string            `json:"server_ip"`
	AuthorizedKey []KeyRef           `json:"authorized_key"`
	HostKey       []KeyRef           `json:"host_key"`
	Comment       *string            `json:"comment"`
	Product       TransactionProduct `json:"product"`
	Addons        []string           `json:"addons,omitempty"`
	// Kind is set by hcli ("server" or "server_market"), not by the API.
	Kind string `json:"kind,omitempty"`
}

// FirewallRule is one firewall rule. Empty fields act as wildcards.
type FirewallRule struct {
	IPVersion string `json:"ip_version,omitempty"`
	Name      string `json:"name,omitempty"`
	DstIP     string `json:"dst_ip,omitempty"`
	SrcIP     string `json:"src_ip,omitempty"`
	DstPort   string `json:"dst_port,omitempty"`
	SrcPort   string `json:"src_port,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	TCPFlags  string `json:"tcp_flags,omitempty"`
	Action    string `json:"action"`
}

// FirewallRules groups rules by direction.
type FirewallRules struct {
	Input  []FirewallRule `json:"input,omitempty"`
	Output []FirewallRule `json:"output,omitempty"`
}

// Firewall is the firewall of a server.
type Firewall struct {
	ServerIP     string        `json:"server_ip,omitempty"`
	ServerNumber int           `json:"server_number,omitempty"`
	Status       string        `json:"status"`
	FilterIPv6   bool          `json:"filter_ipv6"`
	WhitelistHOS bool          `json:"whitelist_hos"`
	Port         string        `json:"port,omitempty"`
	Rules        FirewallRules `json:"rules"`
}

// FirewallTemplate is a stored firewall template.
type FirewallTemplate struct {
	ID           int            `json:"id"`
	Name         string         `json:"name"`
	FilterIPv6   bool           `json:"filter_ipv6"`
	WhitelistHOS bool           `json:"whitelist_hos"`
	IsDefault    bool           `json:"is_default"`
	Rules        *FirewallRules `json:"rules,omitempty"`
}

// VSwitch is a vSwitch (responses are not wrapped in an envelope).
type VSwitch struct {
	ID           int                   `json:"id"`
	Name         string                `json:"name"`
	VLAN         int                   `json:"vlan"`
	Cancelled    bool                  `json:"cancelled"`
	Server       []VSwitchServer       `json:"server,omitempty"`
	Subnet       []VSwitchSubnet       `json:"subnet,omitempty"`
	CloudNetwork []VSwitchCloudNetwork `json:"cloud_network,omitempty"`
}

// VSwitchServer is a server attached to a vSwitch.
type VSwitchServer struct {
	ServerIP      string `json:"server_ip"`
	ServerIPv6Net string `json:"server_ipv6_net"`
	ServerNumber  int    `json:"server_number"`
	Status        string `json:"status"`
}

// VSwitchSubnet is a subnet routed to a vSwitch.
type VSwitchSubnet struct {
	IP      string `json:"ip"`
	Mask    int    `json:"mask"`
	Gateway string `json:"gateway"`
}

// VSwitchCloudNetwork is a Hetzner Cloud network coupled with a vSwitch.
type VSwitchCloudNetwork struct {
	ID      int    `json:"id"`
	IP      string `json:"ip"`
	Mask    int    `json:"mask"`
	Gateway string `json:"gateway"`
}
