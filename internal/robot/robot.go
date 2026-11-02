// Package robot implements `hcli robot ...` on top of the Robot webservice.
//
// The HTTP client lives in internal/robot/client (standard library only).
// Commands accept a server as its number, its main IP (or any of its single
// IPs / its IPv6 net) or its name; names and IPs are resolved through
// GET /server. Numbers are used as is, which saves a request: Robot rate
// limits are per endpoint and per hour (e.g. 200 req/h for /server).
package robot

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

// NewCommand returns the `robot` command tree.
func NewCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "robot",
		Aliases: []string{"r"},
		Short:   "Hetzner Robot webservice (dedicated servers, IPs, firewall, vSwitch, ...)",
		Long: `Manage Hetzner dedicated servers through the Robot webservice
(robot-ws.your-server.de), authenticated with the webservice user created in
Robot under Settings > Web service and app settings.

Servers can be given by number, main IP or name. Robot rate limits are per
endpoint and per hour; hcli retries briefly when it is rate limited.

Storage Boxes are not covered here: Hetzner moved them to the Hetzner API
(api.hetzner.com) in 2025.`,
	}
	cmd.AddCommand(
		newServerCommand(c),
		newResetCommand(c),
		newRescueCommand(c),
		newLinuxCommand(c),
		newWOLCommand(c),
		newIPCommand(c),
		newSubnetCommand(c),
		newRDNSCommand(c),
		newFailoverCommand(c),
		newTrafficCommand(c),
		newFirewallCommand(c),
		newVSwitchCommand(c),
		newSSHKeyCommand(c),
		newOrderCommand(c),
	)
	return cmd
}

// newClient builds a Robot client from the effective context.
func newClient(c *cli.CLI, opts ...client.Option) (*client.Client, error) {
	ctx, err := c.Context()
	if err != nil {
		return nil, err
	}
	if ctx.RobotUser == "" || ctx.RobotPassword == "" {
		return nil, cli.ErrNoRobotCredentials
	}
	opts = append([]client.Option{client.WithDebug(c.Debugf)}, opts...)
	return client.New(ctx.RobotEndpoint, ctx.RobotUser, ctx.RobotPassword, opts...), nil
}

// ---- server references ----

// serverNumber resolves ref (number, IP or name) to a server number. Numbers
// are returned without calling the API.
func serverNumber(ctx context.Context, cl *client.Client, ref string) (int, error) {
	if n, err := strconv.Atoi(ref); err == nil && n > 0 {
		return n, nil
	}
	s, err := findServer(ctx, cl, ref)
	if err != nil {
		return 0, err
	}
	return s.ServerNumber, nil
}

// serverByRef returns the full server object for ref.
func serverByRef(ctx context.Context, cl *client.Client, ref string) (*client.Server, error) {
	if n, err := strconv.Atoi(ref); err == nil && n > 0 {
		return cl.GetServer(ctx, n)
	}
	return findServer(ctx, cl, ref)
}

func findServer(ctx context.Context, cl *client.Client, ref string) (*client.Server, error) {
	servers, err := cl.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	return matchServer(servers, ref)
}

func matchServer(servers []client.Server, ref string) (*client.Server, error) {
	if ip := net.ParseIP(strings.Split(ref, "/")[0]); ip != nil {
		for i := range servers {
			if serverHasIP(&servers[i], ip) {
				return &servers[i], nil
			}
		}
		return nil, fmt.Errorf("no server with IP %s", ref)
	}
	var matches []*client.Server
	for i := range servers {
		if servers[i].ServerName == ref {
			matches = append(matches, &servers[i])
		}
	}
	if len(matches) == 0 {
		for i := range servers {
			if strings.EqualFold(servers[i].ServerName, ref) {
				matches = append(matches, &servers[i])
			}
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no server named %q (use its number, IP or name; see `hcli robot server list`)", ref)
	case 1:
		return matches[0], nil
	}
	nums := make([]string, len(matches))
	for i, m := range matches {
		nums[i] = strconv.Itoa(m.ServerNumber)
	}
	return nil, fmt.Errorf("server name %q is ambiguous (servers %s): use the number", ref, strings.Join(nums, ", "))
}

func serverHasIP(s *client.Server, ip net.IP) bool {
	eq := func(v string) bool {
		other := net.ParseIP(strings.Split(v, "/")[0])
		return other != nil && other.Equal(ip)
	}
	if eq(s.ServerIP) || eq(s.ServerIPv6Net) {
		return true
	}
	for _, v := range s.IP {
		if eq(v) {
			return true
		}
	}
	return false
}

// ---- shell completion ----

type completer func(ctx context.Context, cl *client.Client) []string

// complete returns a ValidArgsFunction that offers f's values for the
// positional arguments in positions (none given = every position). It fails
// silently (no suggestions) without credentials or when the API errors.
func complete(c *cli.CLI, f completer, positions ...int) cobra.CompletionFunc {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(positions) > 0 {
			ok := false
			for _, p := range positions {
				if p == len(args) {
					ok = true
				}
			}
			if !ok {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return completeValues(c, f), cobra.ShellCompDirectiveNoFileComp
	}
}

func flagCompletion(c *cli.CLI, f completer) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return completeValues(c, f), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeValues(c *cli.CLI, f completer) []string {
	cl, err := newClient(c, client.WithTimeout(5*time.Second), client.WithRetry(0, 0))
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return f(ctx, cl)
}

func completeServers(ctx context.Context, cl *client.Client) []string {
	servers, err := cl.ListServers(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(servers))
	for _, s := range servers {
		out = append(out, fmt.Sprintf("%d\t%s %s", s.ServerNumber, s.ServerName, s.ServerIP))
	}
	return out
}

func completeServerIPs(ctx context.Context, cl *client.Client) []string {
	servers, err := cl.ListServers(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range servers {
		if s.ServerIP != "" {
			out = append(out, fmt.Sprintf("%s\t%d %s", s.ServerIP, s.ServerNumber, s.ServerName))
		}
		if s.ServerIPv6Net != "" {
			out = append(out, fmt.Sprintf("%s\t%d %s (IPv6 net)", s.ServerIPv6Net, s.ServerNumber, s.ServerName))
		}
	}
	return out
}

func completeVSwitches(ctx context.Context, cl *client.Client) []string {
	vs, err := cl.ListVSwitches(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, fmt.Sprintf("%d\t%s vlan %d", v.ID, v.Name, v.VLAN))
	}
	return out
}

func completeKeys(ctx context.Context, cl *client.Client) []string {
	keys, err := cl.ListKeys(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s\t%s %s", k.Fingerprint, k.Name, k.Type))
	}
	return out
}

func completeFailovers(ctx context.Context, cl *client.Client) []string {
	fos, err := cl.ListFailovers(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(fos))
	for _, f := range fos {
		out = append(out, fmt.Sprintf("%s\t-> %s (server %d)", f.IP, deref(f.ActiveServerIP), f.ServerNumber))
	}
	return out
}

func completeIPs(ctx context.Context, cl *client.Client) []string {
	ips, err := cl.ListIPs(ctx, "")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, fmt.Sprintf("%s\tserver %d", ip.IP, ip.ServerNumber))
	}
	return out
}

func completeSubnets(ctx context.Context, cl *client.Client) []string {
	subs, err := cl.ListSubnets(ctx, "")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, fmt.Sprintf("%s\t/%d server %d", s.IP, s.Mask, s.ServerNumber))
	}
	return out
}

func completeIPsAndSubnets(ctx context.Context, cl *client.Client) []string {
	return append(completeIPs(ctx, cl), completeSubnets(ctx, cl)...)
}

func completeTemplates(ctx context.Context, cl *client.Client) []string {
	ts, err := cl.ListFirewallTemplates(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, fmt.Sprintf("%d\t%s", t.ID, t.Name))
	}
	return out
}

// ---- small helpers ----

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func join(s []string) string { return strings.Join(s, ", ") }

func keyFingerprints(keys []client.KeyRef) string {
	fps := make([]string, 0, len(keys))
	for _, k := range keys {
		s := k.Fingerprint
		if k.Name != "" {
			s += " (" + k.Name + ")"
		}
		fps = append(fps, s)
	}
	return strings.Join(fps, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
