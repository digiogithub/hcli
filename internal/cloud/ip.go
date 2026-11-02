package cloud

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var floatingIPRes = resource[hcloud.FloatingIP]{
	Kind: "floating-ip",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.FloatingIP, *hcloud.Response, error) {
		return cl.FloatingIP.Get(ctx, s)
	},
	Names:  floatingIPNames,
	Ident:  func(f *hcloud.FloatingIP) (int64, string) { return f.ID, f.Name },
	Schema: func(f *hcloud.FloatingIP) any { return hcloud.SchemaFromFloatingIP(f) },
}

var primaryIPRes = resource[hcloud.PrimaryIP]{
	Kind: "primary-ip",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.PrimaryIP, *hcloud.Response, error) {
		return cl.PrimaryIP.Get(ctx, s)
	},
	Names:  primaryIPNames,
	Ident:  func(p *hcloud.PrimaryIP) (int64, string) { return p.ID, p.Name },
	Schema: func(p *hcloud.PrimaryIP) any { return hcloud.SchemaFromPrimaryIP(p) },
}

func formatDNSPtr(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + ": " + m[k]
	}
	return strings.Join(parts, ", ")
}

func floatingIPAddr(f *hcloud.FloatingIP) string {
	if f.Type == hcloud.FloatingIPTypeIPv6 && f.Network != nil {
		return f.Network.String()
	}
	return ipString(f.IP)
}

func primaryIPAddr(p *hcloud.PrimaryIP) string {
	if p.Type == hcloud.PrimaryIPTypeIPv6 && p.Network != nil {
		return p.Network.String()
	}
	return ipString(p.IP)
}

func newFloatingIPCommand(a *app) *cobra.Command {
	cmd := group("floating-ip", "Manage floating IPs", []string{"floating-ips", "fip"},
		newListCmd(a, floatingIPRes, listSpec[hcloud.FloatingIP]{
			Selector: true,
			Headers:  []string{"id", "type", "name", "description", "ip", "home", "server", "dns", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.FloatingIP, error) {
				return cl.FloatingIP.AllWithOpts(ctx, hcloud.FloatingIPListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(f *hcloud.FloatingIP) []any {
				server := ""
				if f.Server != nil {
					server = strconv.FormatInt(f.Server.ID, 10)
				}
				return []any{f.ID, f.Type, f.Name, f.Description, floatingIPAddr(f), locationName(f.HomeLocation), server, formatDNSPtr(f.DNSPtr), age(f.Created)}
			},
		}),
		newDescribeCmd(a, floatingIPRes, func(f *hcloud.FloatingIP) [][2]string {
			server := ""
			if f.Server != nil {
				server = strconv.FormatInt(f.Server.ID, 10)
			}
			return [][2]string{
				{"ID", strconv.FormatInt(f.ID, 10)},
				{"Name", f.Name},
				{"Type", string(f.Type)},
				{"Description", f.Description},
				{"Created", created(f.Created)},
				{"IP", floatingIPAddr(f)},
				{"Blocked", yesNo(f.Blocked)},
				{"Home location", locationName(f.HomeLocation)},
				{"Server", server},
				{"DNS", formatDNSPtr(f.DNSPtr)},
				{"Protection", "delete=" + yesNo(f.Protection.Delete)},
				{"Labels", formatLabels(f.Labels)},
			}
		}),
		newFloatingIPCreateCmd(a),
		newDeleteCmd(a, floatingIPRes, func(ctx context.Context, cl *hcloud.Client, f *hcloud.FloatingIP) ([]*hcloud.Action, error) {
			_, err := cl.FloatingIP.Delete(ctx, f)
			return nil, err
		}),
		&cobra.Command{
			Use:               "assign <floating-ip> <server>",
			Short:             "Assign a floating IP to a server",
			Args:              cobra.ExactArgs(2),
			ValidArgsFunction: a.complete(floatingIPNames, serverNames),
			RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
				f, err := floatingIPRes.fetch(ctx, cl, args[0])
				if err != nil {
					return err
				}
				s, err := serverRes.fetch(ctx, cl, args[1])
				if err != nil {
					return err
				}
				action, _, err := cl.FloatingIP.Assign(ctx, f, s)
				if err != nil {
					return err
				}
				if err := a.wait(ctx, cl, fmt.Sprintf("floating IP %s to be assigned", floatingIPRes.label(f)), action); err != nil {
					return err
				}
				return a.done(p, action, "Floating IP %s assigned to server %s", floatingIPRes.label(f), s.Name)
			}),
		},
		&cobra.Command{
			Use:               "unassign <floating-ip>",
			Short:             "Unassign a floating IP",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: a.complete(floatingIPNames),
			RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
				f, err := floatingIPRes.fetch(ctx, cl, args[0])
				if err != nil {
					return err
				}
				action, _, err := cl.FloatingIP.Unassign(ctx, f)
				if err != nil {
					return err
				}
				if err := a.wait(ctx, cl, fmt.Sprintf("floating IP %s to be unassigned", floatingIPRes.label(f)), action); err != nil {
					return err
				}
				return a.done(p, action, "Floating IP %s unassigned", floatingIPRes.label(f))
			}),
		},
	)
	cmd.AddCommand(newLabelCmds(a, floatingIPRes,
		func(f *hcloud.FloatingIP) map[string]string { return f.Labels },
		func(ctx context.Context, cl *hcloud.Client, f *hcloud.FloatingIP, l map[string]string) error {
			_, _, err := cl.FloatingIP.Update(ctx, f, hcloud.FloatingIPUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

func newFloatingIPCreateCmd(a *app) *cobra.Command {
	var typ, home, server, name, description string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --type ipv4|ipv6 (--home-location <loc> | --server <server>)",
		Short: "Create a floating IP (billable)",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			opts := hcloud.FloatingIPCreateOpts{Type: hcloud.FloatingIPType(typ)}
			var err error
			if home == "" && server == "" {
				return fmt.Errorf("--home-location or --server is required")
			}
			if home != "" {
				if opts.HomeLocation, err = locationRes.fetch(ctx, cl, home); err != nil {
					return err
				}
			}
			if server != "" {
				if opts.Server, err = serverRes.fetch(ctx, cl, server); err != nil {
					return err
				}
			}
			if name != "" {
				opts.Name = &name
			}
			if description != "" {
				opts.Description = &description
			}
			if opts.Labels, err = parseLabels(labels); err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("create a %s floating IP — this is billable", typ)); err != nil {
				return err
			}
			res, _, err := cl.FloatingIP.Create(ctx, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, "floating IP to be created", res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromFloatingIP(res.FloatingIP), nil)
			}
			fmt.Fprintf(p.Out, "Floating IP %d created: %s\n", res.FloatingIP.ID, floatingIPAddr(res.FloatingIP))
			return nil
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&typ, "type", "ipv4", "ipv4 or ipv6")
	fl.StringVar(&home, "home-location", "", "home location")
	fl.StringVar(&server, "server", "", "server to assign to")
	fl.StringVar(&name, "name", "", "name")
	fl.StringVar(&description, "description", "", "description")
	addLabelFlag(cmd, &labels)
	fixedCompletion(cmd, "type", "ipv4", "ipv6")
	a.flagCompletion(cmd, "home-location", locationNames)
	a.flagCompletion(cmd, "server", serverNames)
	return cmd
}

func newPrimaryIPCommand(a *app) *cobra.Command {
	cmd := group("primary-ip", "Manage primary IPs", []string{"primary-ips", "pip"},
		newListCmd(a, primaryIPRes, listSpec[hcloud.PrimaryIP]{
			Selector: true,
			Headers:  []string{"id", "type", "name", "ip", "assignee", "dns", "auto delete", "location", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.PrimaryIP, error) {
				return cl.PrimaryIP.AllWithOpts(ctx, hcloud.PrimaryIPListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(p *hcloud.PrimaryIP) []any {
				return []any{p.ID, p.Type, p.Name, primaryIPAddr(p), primaryIPAssignee(p), formatDNSPtr(p.DNSPtr), yesNo(p.AutoDelete), locationName(p.Location), age(p.Created)}
			},
		}),
		newDescribeCmd(a, primaryIPRes, func(p *hcloud.PrimaryIP) [][2]string {
			return [][2]string{
				{"ID", strconv.FormatInt(p.ID, 10)},
				{"Name", p.Name},
				{"Type", string(p.Type)},
				{"Created", created(p.Created)},
				{"IP", primaryIPAddr(p)},
				{"Blocked", yesNo(p.Blocked)},
				{"Assignee", primaryIPAssignee(p)},
				{"Auto delete", yesNo(p.AutoDelete)},
				{"Location", locationName(p.Location)},
				{"DNS", formatDNSPtr(p.DNSPtr)},
				{"Protection", "delete=" + yesNo(p.Protection.Delete)},
				{"Labels", formatLabels(p.Labels)},
			}
		}),
		newPrimaryIPCreateCmd(a),
		newDeleteCmd(a, primaryIPRes, func(ctx context.Context, cl *hcloud.Client, p *hcloud.PrimaryIP) ([]*hcloud.Action, error) {
			_, err := cl.PrimaryIP.Delete(ctx, p)
			return nil, err
		}),
		newPrimaryIPAssignCmd(a),
		&cobra.Command{
			Use:               "unassign <primary-ip>",
			Short:             "Unassign a primary IP (the server must be off)",
			Args:              cobra.ExactArgs(1),
			ValidArgsFunction: a.complete(primaryIPNames),
			RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
				pip, err := primaryIPRes.fetch(ctx, cl, args[0])
				if err != nil {
					return err
				}
				action, _, err := cl.PrimaryIP.Unassign(ctx, pip.ID)
				if err != nil {
					return err
				}
				if err := a.wait(ctx, cl, fmt.Sprintf("primary IP %s to be unassigned", primaryIPRes.label(pip)), action); err != nil {
					return err
				}
				return a.done(p, action, "Primary IP %s unassigned", primaryIPRes.label(pip))
			}),
		},
	)
	cmd.AddCommand(newLabelCmds(a, primaryIPRes,
		func(p *hcloud.PrimaryIP) map[string]string { return p.Labels },
		func(ctx context.Context, cl *hcloud.Client, p *hcloud.PrimaryIP, l map[string]string) error {
			_, _, err := cl.PrimaryIP.Update(ctx, p, hcloud.PrimaryIPUpdateOpts{Labels: &l})
			return err
		})...)
	return cmd
}

func primaryIPAssignee(p *hcloud.PrimaryIP) string {
	if p.AssigneeID == 0 {
		return ""
	}
	return fmt.Sprintf("%s %d", p.AssigneeType, p.AssigneeID)
}

func newPrimaryIPCreateCmd(a *app) *cobra.Command {
	var typ, name, location, server string
	var autoDelete bool
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --type ipv4|ipv6 --name <name> (--location <loc> | --server <server>)",
		Short: "Create a primary IP (billable)",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			opts := hcloud.PrimaryIPCreateOpts{Type: hcloud.PrimaryIPType(typ), Name: name, AssigneeType: "server", Location: location}
			if location == "" && server == "" {
				return fmt.Errorf("--location or --server is required")
			}
			if server != "" {
				s, err := serverRes.fetch(ctx, cl, server)
				if err != nil {
					return err
				}
				opts.AssigneeID = &s.ID
			}
			if autoDelete {
				opts.AutoDelete = &autoDelete
			}
			var err error
			if opts.Labels, err = parseLabels(labels); err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("create %s primary IP %s — this is billable", typ, name)); err != nil {
				return err
			}
			res, _, err := cl.PrimaryIP.Create(ctx, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("primary IP %s to be created", name), res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromPrimaryIP(res.PrimaryIP), nil)
			}
			fmt.Fprintf(p.Out, "Primary IP %d created: %s\n", res.PrimaryIP.ID, primaryIPAddr(res.PrimaryIP))
			return nil
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&typ, "type", "ipv4", "ipv4 or ipv6")
	fl.StringVar(&name, "name", "", "name (required)")
	fl.StringVar(&location, "location", "", "location")
	fl.StringVar(&server, "server", "", "server to assign to (must be off)")
	fl.BoolVar(&autoDelete, "auto-delete", false, "delete the IP when the assigned server is deleted")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	cmd.MarkFlagsMutuallyExclusive("location", "server")
	fixedCompletion(cmd, "type", "ipv4", "ipv6")
	a.flagCompletion(cmd, "location", locationNames)
	a.flagCompletion(cmd, "server", serverNames)
	return cmd
}

func newPrimaryIPAssignCmd(a *app) *cobra.Command {
	var server string
	cmd := &cobra.Command{
		Use:               "assign <primary-ip> --server <server>",
		Short:             "Assign a primary IP to a server (the server must be off)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(primaryIPNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			pip, err := primaryIPRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			s, err := serverRes.fetch(ctx, cl, server)
			if err != nil {
				return err
			}
			action, _, err := cl.PrimaryIP.Assign(ctx, hcloud.PrimaryIPAssignOpts{ID: pip.ID, AssigneeID: s.ID, AssigneeType: "server"})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("primary IP %s to be assigned", primaryIPRes.label(pip)), action); err != nil {
				return err
			}
			return a.done(p, action, "Primary IP %s assigned to server %s", primaryIPRes.label(pip), s.Name)
		}),
	}
	cmd.Flags().StringVar(&server, "server", "", "server (required)")
	_ = cmd.MarkFlagRequired("server")
	a.flagCompletion(cmd, "server", serverNames)
	return cmd
}
