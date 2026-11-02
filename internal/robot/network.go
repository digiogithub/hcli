package robot

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

// serverIPFilter resolves the --server flag of list commands to the server
// main IP expected by the API's server_ip filter.
func serverIPFilter(ctx context.Context, cl *client.Client, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if ip := net.ParseIP(ref); ip != nil {
		return ref, nil
	}
	s, err := serverByRef(ctx, cl, ref)
	if err != nil {
		return "", err
	}
	if s.ServerIP == "" {
		return "", fmt.Errorf("server %d has no main IPv4 address to filter by", s.ServerNumber)
	}
	return s.ServerIP, nil
}

// ---- ip ----

func newIPCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "ip", Aliases: []string{"ips"}, Short: "Single IP addresses"}
	var server string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List single IP addresses",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			filter, err := serverIPFilter(cmd.Context(), cl, server)
			if err != nil {
				return err
			}
			ips, err := cl.ListIPs(cmd.Context(), filter)
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"ip", "server", "server_ip", "locked", "separate_mac"}}
			for _, ip := range ips {
				t.AddRow(ip.IP, ip.ServerNumber, ip.ServerIP, yesNo(ip.Locked), deref(ip.SeparateMAC))
			}
			return p.Print(ips, t)
		},
	}
	list.Flags().StringVar(&server, "server", "", "only IPs of this server (number, IP or name)")
	_ = list.RegisterFlagCompletionFunc("server", flagCompletion(c, completeServers))

	describe := &cobra.Command{
		Use:               "describe <ip>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a single IP address",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeIPs, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			ip, err := cl.GetIP(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			mask := ""
			if ip.Mask > 0 {
				mask = strconv.Itoa(ip.Mask)
			}
			return p.KeyValues(ip, [][2]string{
				{"IP", ip.IP},
				{"Mask", mask},
				{"Gateway", ip.Gateway},
				{"Broadcast", ip.Broadcast},
				{"Server", strconv.Itoa(ip.ServerNumber)},
				{"Server IP", ip.ServerIP},
				{"Locked", yesNo(ip.Locked)},
				{"Separate MAC", deref(ip.SeparateMAC)},
			})
		},
	}
	cmd.AddCommand(list, describe)
	return cmd
}

// ---- subnet ----

func newSubnetCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "subnet", Aliases: []string{"subnets"}, Short: "Subnets"}
	var server string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List subnets",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			filter, err := serverIPFilter(cmd.Context(), cl, server)
			if err != nil {
				return err
			}
			subs, err := cl.ListSubnets(cmd.Context(), filter)
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"subnet", "gateway", "server", "server_ip", "failover", "locked"}}
			for _, s := range subs {
				t.AddRow(fmt.Sprintf("%s/%d", s.IP, s.Mask), s.Gateway, s.ServerNumber, s.ServerIP, yesNo(s.Failover), yesNo(s.Locked))
			}
			return p.Print(subs, t)
		},
	}
	list.Flags().StringVar(&server, "server", "", "only subnets of this server (number, IP or name)")
	_ = list.RegisterFlagCompletionFunc("server", flagCompletion(c, completeServers))

	describe := &cobra.Command{
		Use:               "describe <net-ip>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a subnet",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeSubnets, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			s, err := cl.GetSubnet(cmd.Context(), strings.Split(args[0], "/")[0])
			if err != nil {
				return err
			}
			return p.KeyValues(s, [][2]string{
				{"Subnet", fmt.Sprintf("%s/%d", s.IP, s.Mask)},
				{"Gateway", s.Gateway},
				{"Server", strconv.Itoa(s.ServerNumber)},
				{"Server IP", s.ServerIP},
				{"Failover", yesNo(s.Failover)},
				{"Locked", yesNo(s.Locked)},
			})
		},
	}
	cmd.AddCommand(list, describe)
	return cmd
}

// ---- rdns ----

func newRDNSCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "rdns", Aliases: []string{"ptr"}, Short: "Reverse DNS (PTR) entries"}
	var server string
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List reverse DNS entries",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			filter, err := serverIPFilter(cmd.Context(), cl, server)
			if err != nil {
				return err
			}
			entries, err := cl.ListRDNS(cmd.Context(), filter)
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"ip", "ptr"}}
			for _, e := range entries {
				t.AddRow(e.IP, e.PTR)
			}
			return p.Print(entries, t)
		},
	}
	list.Flags().StringVar(&server, "server", "", "only entries of this server (number, IP or name)")
	_ = list.RegisterFlagCompletionFunc("server", flagCompletion(c, completeServers))

	get := &cobra.Command{
		Use:               "get <ip>",
		Aliases:           []string{"describe", "show"},
		Short:             "Show the PTR of an IP",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeIPs, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			e, err := cl.GetRDNS(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return p.KeyValues(e, [][2]string{{"IP", e.IP}, {"PTR", e.PTR}})
		},
	}
	set := &cobra.Command{
		Use:               "set <ip> <ptr>",
		Short:             "Create or update the PTR of an IP",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: complete(c, completeIPs, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			e, err := cl.SetRDNS(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(e, nil)
			}
			fmt.Fprintf(c.Out, "%s -> %s\n", e.IP, e.PTR)
			return nil
		},
	}
	del := &cobra.Command{
		Use:               "delete <ip>",
		Aliases:           []string{"rm"},
		Short:             "Delete the PTR of an IP (asks for confirmation)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeIPs, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			if err := c.Confirm("delete the reverse DNS entry of " + args[0]); err != nil {
				return err
			}
			if err := cl.DeleteRDNS(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "Reverse DNS entry of %s deleted\n", args[0])
			return nil
		},
	}
	cmd.AddCommand(list, get, set, del)
	return cmd
}

// ---- failover ----

func newFailoverCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "failover", Aliases: []string{"fo"}, Short: "Failover IPs"}
	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List failover IPs and where they are routed",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			fos, err := cl.ListFailovers(cmd.Context())
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"ip", "netmask", "server", "server_ip", "active_server_ip"}}
			for _, f := range fos {
				t.AddRow(f.IP, f.Netmask, f.ServerNumber, f.ServerIP, deref(f.ActiveServerIP))
			}
			return p.Print(fos, t)
		},
	}
	describe := &cobra.Command{
		Use:               "describe <failover-ip>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a failover IP",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeFailovers, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			f, err := cl.GetFailover(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return p.KeyValues(f, failoverPairs(f))
		},
	}
	var to string
	sw := &cobra.Command{
		Use:   "switch <failover-ip> --to <server-ip|server>",
		Short: "Route a failover IP to another server (asks for confirmation)",
		Long: `Route a failover IP to another server. --to is the destination server main
IP (for IPv6 failover nets, its main IPv6 net), or a server number/name which
is resolved to it.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeFailovers, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			dest := to
			if net.ParseIP(strings.Split(to, "/")[0]) == nil {
				s, err := serverByRef(cmd.Context(), cl, to)
				if err != nil {
					return err
				}
				dest = s.ServerIP
				if fip := net.ParseIP(strings.Split(args[0], "/")[0]); fip != nil && fip.To4() == nil {
					dest = s.ServerIPv6Net
				}
				if dest == "" {
					return fmt.Errorf("server %d has no main IP of the failover's family", s.ServerNumber)
				}
			}
			dest = strings.Split(dest, "/")[0]
			if err := c.Confirm(fmt.Sprintf("route failover IP %s to %s", args[0], dest)); err != nil {
				return err
			}
			f, err := cl.SwitchFailover(cmd.Context(), args[0], dest)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(f, nil)
			}
			fmt.Fprintf(c.Out, "Failover %s now routed to %s\n", f.IP, deref(f.ActiveServerIP))
			return nil
		},
	}
	sw.Flags().StringVar(&to, "to", "", "destination server main IP (or server number/name)")
	_ = sw.MarkFlagRequired("to")
	_ = sw.RegisterFlagCompletionFunc("to", flagCompletion(c, completeServerIPs))
	cmd.AddCommand(list, describe, sw)
	return cmd
}

func failoverPairs(f *client.Failover) [][2]string {
	return [][2]string{
		{"IP", f.IP},
		{"Netmask", f.Netmask},
		{"Server", strconv.Itoa(f.ServerNumber)},
		{"Server IP", f.ServerIP},
		{"Server IPv6 net", f.ServerIPv6Net},
		{"Active server IP", deref(f.ActiveServerIP)},
	}
}

// ---- traffic ----

var trafficTypes = []string{"day", "month", "year"}

func newTrafficCommand(c *cli.CLI) *cobra.Command {
	var (
		typ, from, to string
		subnets       []string
		single        bool
	)
	cmd := &cobra.Command{
		Use:   "traffic <ip|subnet>... [--type day|month|year] [--from ..] [--to ..]",
		Short: "Show traffic (GB) of IPs and subnets",
		Long: `Query traffic in GB. Arguments containing "/" are treated as subnets (the
mask is dropped), others as IPs; --subnet adds subnets explicitly.

Date formats depend on --type:
  day    YYYY-MM-DDTHH  (hourly data within a day)
  month  YYYY-MM-DD     (daily data within a month)   [default]
  year   YYYY-MM        (monthly data within a year)
Defaults cover the current day/month/year. Data only exists for periods that
have already finished. --single-values groups the result by hour/day/month.`,
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: complete(c, completeIPsAndSubnets),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(trafficTypes, typ) {
				return fmt.Errorf("invalid --type %q (use day, month or year)", typ)
			}
			q := client.TrafficQuery{Type: typ, From: from, To: to, SingleValues: single}
			for _, a := range args {
				if strings.Contains(a, "/") {
					q.Subnets = append(q.Subnets, strings.Split(a, "/")[0])
				} else {
					q.IPs = append(q.IPs, a)
				}
			}
			for _, s := range subnets {
				q.Subnets = append(q.Subnets, strings.Split(s, "/")[0])
			}
			if len(q.IPs)+len(q.Subnets) == 0 {
				return fmt.Errorf("give at least one IP or subnet")
			}
			defFrom, defTo := trafficDefaults(typ, time.Now())
			if q.From == "" {
				q.From = defFrom
			}
			if q.To == "" {
				q.To = defTo
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			tr, err := cl.GetTraffic(cmd.Context(), q)
			if err != nil {
				return err
			}
			t, err := trafficTable(tr)
			if err != nil {
				return err
			}
			return p.Print(tr, t)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "month", "query type: day, month, year")
	cmd.Flags().StringVar(&from, "from", "", "start (format depends on --type)")
	cmd.Flags().StringVar(&to, "to", "", "end (format depends on --type)")
	cmd.Flags().StringArrayVar(&subnets, "subnet", nil, "subnet address (repeatable)")
	cmd.Flags().BoolVar(&single, "single-values", false, "group by hour/day/month instead of one sum")
	_ = cmd.RegisterFlagCompletionFunc("type", cobra.FixedCompletions(trafficTypes, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("subnet", flagCompletion(c, completeSubnets))
	return cmd
}

func trafficDefaults(typ string, now time.Time) (string, string) {
	switch typ {
	case "day":
		return now.Format("2006-01-02") + "T00", now.Format("2006-01-02") + "T23"
	case "year":
		return now.Format("2006") + "-01", now.Format("2006-01")
	}
	return now.Format("2006-01") + "-01", now.Format("2006-01-02")
}

func trafficTable(tr *client.Traffic) (*output.Table, error) {
	t := &output.Table{Headers: []string{"address", "period", "in_gb", "out_gb", "sum_gb"}}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
	for _, addr := range sortedKeys(tr.Data) {
		raw := tr.Data[addr]
		var sum client.TrafficValue
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("decode traffic of %s: %w", addr, err)
		}
		if _, ok := probe["sum"]; ok {
			if err := json.Unmarshal(raw, &sum); err != nil {
				return nil, err
			}
			t.AddRow(addr, tr.From+".."+tr.To, f(sum.In), f(sum.Out), f(sum.Sum))
			continue
		}
		var series map[string]client.TrafficValue
		if err := json.Unmarshal(raw, &series); err != nil {
			return nil, fmt.Errorf("decode traffic of %s: %w", addr, err)
		}
		for _, period := range sortedKeys(series) {
			v := series[period]
			t.AddRow(addr, period, f(v.In), f(v.Out), f(v.Sum))
		}
	}
	return t, nil
}
