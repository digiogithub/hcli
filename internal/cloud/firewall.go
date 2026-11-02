package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/spf13/cobra"
)

var firewallRes = resource[hcloud.Firewall]{
	Kind: "firewall",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Firewall, *hcloud.Response, error) {
		return cl.Firewall.Get(ctx, s)
	},
	Names:  firewallNames,
	Ident:  func(f *hcloud.Firewall) (int64, string) { return f.ID, f.Name },
	Schema: func(f *hcloud.Firewall) any { return hcloud.SchemaFromFirewall(f) },
}

func newFirewallCommand(a *app) *cobra.Command {
	cmd := group("firewall", "Manage firewalls", []string{"firewalls", "fw"},
		newListCmd(a, firewallRes, listSpec[hcloud.Firewall]{
			Selector: true,
			Headers:  []string{"id", "name", "rules", "applied to", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Firewall, error) {
				return cl.Firewall.AllWithOpts(ctx, hcloud.FirewallListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(f *hcloud.Firewall) []any {
				return []any{f.ID, f.Name, len(f.Rules), len(f.AppliedTo), age(f.Created)}
			},
		}),
		newDescribeCmd(a, firewallRes, func(f *hcloud.Firewall) [][2]string {
			kv := [][2]string{
				{"ID", strconv.FormatInt(f.ID, 10)},
				{"Name", f.Name},
				{"Created", created(f.Created)},
				{"Labels", formatLabels(f.Labels)},
			}
			for _, r := range f.Rules {
				kv = append(kv, [2]string{"Rule", formatRule(r)})
			}
			for _, res := range f.AppliedTo {
				switch res.Type {
				case hcloud.FirewallResourceTypeServer:
					if res.Server != nil {
						kv = append(kv, [2]string{"Applied to", fmt.Sprintf("server %d", res.Server.ID)})
					}
				case hcloud.FirewallResourceTypeLabelSelector:
					if res.LabelSelector != nil {
						kv = append(kv, [2]string{"Applied to", fmt.Sprintf("label selector %q (%d servers)", res.LabelSelector.Selector, len(res.AppliedToResources))})
					}
				}
			}
			return kv
		}),
		newFirewallCreateCmd(a),
		newDeleteCmd(a, firewallRes, func(ctx context.Context, cl *hcloud.Client, f *hcloud.Firewall) ([]*hcloud.Action, error) {
			_, err := cl.Firewall.Delete(ctx, f)
			return nil, err
		}),
		newFirewallRuleCmd(a, true),
		newFirewallRuleCmd(a, false),
		newFirewallResourceCmd(a, true),
		newFirewallResourceCmd(a, false),
	)
	cmd.AddCommand(newLabelCmds(a, firewallRes,
		func(f *hcloud.Firewall) map[string]string { return f.Labels },
		func(ctx context.Context, cl *hcloud.Client, f *hcloud.Firewall, l map[string]string) error {
			_, _, err := cl.Firewall.Update(ctx, f, hcloud.FirewallUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

func formatRule(r hcloud.FirewallRule) string {
	ips := r.SourceIPs
	dir := "from"
	if r.Direction == hcloud.FirewallRuleDirectionOut {
		ips, dir = r.DestinationIPs, "to"
	}
	parts := make([]string, len(ips))
	for i, n := range ips {
		parts[i] = n.String()
	}
	s := fmt.Sprintf("%s %s", r.Direction, r.Protocol)
	if r.Port != nil {
		s += " port " + *r.Port
	}
	s += fmt.Sprintf(" %s %s", dir, strings.Join(parts, ","))
	if r.Description != nil && *r.Description != "" {
		s += fmt.Sprintf(" (%s)", *r.Description)
	}
	return s
}

func parseNets(in []string) ([]net.IPNet, error) {
	out := make([]net.IPNet, 0, len(in))
	for _, s := range in {
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil {
				if ip.To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid IP network %q: %w", s, err)
		}
		out = append(out, *n)
	}
	return out, nil
}

// ruleFromSchema converts a rules-file entry (API JSON format).
func ruleFromSchema(in schema.FirewallRule) (hcloud.FirewallRule, error) {
	r := hcloud.FirewallRule{
		Direction:   hcloud.FirewallRuleDirection(in.Direction),
		Protocol:    hcloud.FirewallRuleProtocol(in.Protocol),
		Port:        in.Port,
		Description: in.Description,
	}
	var err error
	if r.SourceIPs, err = parseNets(in.SourceIPs); err != nil {
		return r, err
	}
	if r.DestinationIPs, err = parseNets(in.DestinationIPs); err != nil {
		return r, err
	}
	return r, nil
}

func newFirewallCreateCmd(a *app) *cobra.Command {
	var name, rulesFile string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --name <name> [--rules-file rules.json]",
		Short: "Create a firewall",
		Long: `Create a firewall, optionally with rules from a JSON file in the API format:

  [{"direction": "in", "protocol": "tcp", "port": "22",
    "source_ips": ["0.0.0.0/0", "::/0"], "description": "ssh"}]`,
		Args: cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			opts := hcloud.FirewallCreateOpts{Name: name}
			var err error
			if opts.Labels, err = parseLabels(labels); err != nil {
				return err
			}
			if rulesFile != "" {
				data, err := a.readFileOrStdin(rulesFile)
				if err != nil {
					return err
				}
				var raw []schema.FirewallRule
				if err := json.Unmarshal(data, &raw); err != nil {
					return fmt.Errorf("parse %s: %w", rulesFile, err)
				}
				for _, in := range raw {
					r, err := ruleFromSchema(in)
					if err != nil {
						return err
					}
					opts.Rules = append(opts.Rules, r)
				}
			}
			res, _, err := cl.Firewall.Create(ctx, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("firewall %s", name), res.Actions...); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromFirewall(res.Firewall), nil)
			}
			fmt.Fprintf(p.Out, "Firewall %d created\n", res.Firewall.ID)
			return nil
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "firewall name (required)")
	cmd.Flags().StringVar(&rulesFile, "rules-file", "", "JSON file with rules (\"-\" for stdin)")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagFilename("rules-file", "json")
	return cmd
}

func sameNets(x, y []net.IPNet) bool {
	norm := func(in []net.IPNet) []string {
		out := make([]string, len(in))
		for i, n := range in {
			out[i] = n.String()
		}
		slices.Sort(out)
		return out
	}
	return slices.Equal(norm(x), norm(y))
}

func sameRule(x, y hcloud.FirewallRule) bool {
	strp := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return x.Direction == y.Direction && x.Protocol == y.Protocol && strp(x.Port) == strp(y.Port) &&
		sameNets(x.SourceIPs, y.SourceIPs) && sameNets(x.DestinationIPs, y.DestinationIPs)
}

// newFirewallRuleCmd builds add-rule (add=true) or delete-rule.
func newFirewallRuleCmd(a *app, add bool) *cobra.Command {
	var direction, protocol, port, description string
	var sources, destinations []string
	use, short := "add-rule", "Add a rule to a firewall"
	if !add {
		use, short = "delete-rule", "Delete a matching rule from a firewall"
	}
	cmd := &cobra.Command{
		Use:               use + " <firewall> --direction in|out --protocol <proto> [--port <port>] [--source-ips <cidr>...]",
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(firewallNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			fw, err := firewallRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			rule := hcloud.FirewallRule{
				Direction: hcloud.FirewallRuleDirection(direction),
				Protocol:  hcloud.FirewallRuleProtocol(protocol),
			}
			if port != "" {
				rule.Port = &port
			}
			if description != "" {
				rule.Description = &description
			}
			if rule.SourceIPs, err = parseNets(sources); err != nil {
				return err
			}
			if rule.DestinationIPs, err = parseNets(destinations); err != nil {
				return err
			}
			switch rule.Direction {
			case hcloud.FirewallRuleDirectionIn:
				if len(rule.SourceIPs) == 0 {
					return errors.New("--source-ips is required for direction in")
				}
			case hcloud.FirewallRuleDirectionOut:
				if len(rule.DestinationIPs) == 0 {
					return errors.New("--destination-ips is required for direction out")
				}
			default:
				return fmt.Errorf("--direction must be in or out")
			}
			rules := slices.Clone(fw.Rules)
			if add {
				rules = append(rules, rule)
			} else {
				idx := slices.IndexFunc(rules, func(r hcloud.FirewallRule) bool { return sameRule(r, rule) })
				if idx < 0 {
					return fmt.Errorf("no matching rule in firewall %s", fw.Name)
				}
				rules = slices.Delete(rules, idx, idx+1)
			}
			actions, _, err := cl.Firewall.SetRules(ctx, fw, hcloud.FirewallSetRulesOpts{Rules: rules})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("rules of firewall %s", fw.Name), actions...); err != nil {
				return err
			}
			if add {
				return a.done(p, nil, "Rule added to firewall %s", fw.Name)
			}
			return a.done(p, nil, "Rule deleted from firewall %s", fw.Name)
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&direction, "direction", "in", "in or out")
	fl.StringVar(&protocol, "protocol", "", "tcp, udp, icmp, esp or gre (required)")
	fl.StringVar(&port, "port", "", "port or range (tcp/udp), e.g. 80 or 8000-9000")
	fl.StringSliceVar(&sources, "source-ips", nil, "source networks (direction in)")
	fl.StringSliceVar(&destinations, "destination-ips", nil, "destination networks (direction out)")
	if add {
		fl.StringVar(&description, "description", "", "rule description")
	}
	_ = cmd.MarkFlagRequired("protocol")
	fixedCompletion(cmd, "direction", "in", "out")
	fixedCompletion(cmd, "protocol", "tcp", "udp", "icmp", "esp", "gre")
	return cmd
}

// newFirewallResourceCmd builds apply-to-resource (apply=true) or remove-from-resource.
func newFirewallResourceCmd(a *app, apply bool) *cobra.Command {
	var typ, server, selector string
	use, short := "apply-to-resource", "Apply a firewall to a server or label selector"
	if !apply {
		use, short = "remove-from-resource", "Remove a firewall from a server or label selector"
	}
	cmd := &cobra.Command{
		Use:               use + " <firewall> --type server|label_selector (--server <server> | --label-selector <sel>)",
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(firewallNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			fw, err := firewallRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			res := hcloud.FirewallResource{Type: hcloud.FirewallResourceType(typ)}
			target := ""
			switch res.Type {
			case hcloud.FirewallResourceTypeServer:
				if server == "" {
					return errors.New("--server is required for type server")
				}
				s, err := serverRes.fetch(ctx, cl, server)
				if err != nil {
					return err
				}
				res.Server = &hcloud.FirewallResourceServer{ID: s.ID}
				target = "server " + s.Name
			case hcloud.FirewallResourceTypeLabelSelector:
				if selector == "" {
					return errors.New("--label-selector is required for type label_selector")
				}
				res.LabelSelector = &hcloud.FirewallResourceLabelSelector{Selector: selector}
				target = fmt.Sprintf("label selector %q", selector)
			default:
				return fmt.Errorf("--type must be server or label_selector")
			}
			var actions []*hcloud.Action
			if apply {
				actions, _, err = cl.Firewall.ApplyResources(ctx, fw, []hcloud.FirewallResource{res})
			} else {
				actions, _, err = cl.Firewall.RemoveResources(ctx, fw, []hcloud.FirewallResource{res})
			}
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("firewall %s on %s", fw.Name, target), actions...); err != nil {
				return err
			}
			if apply {
				return a.done(p, nil, "Firewall %s applied to %s", fw.Name, target)
			}
			return a.done(p, nil, "Firewall %s removed from %s", fw.Name, target)
		}),
	}
	cmd.Flags().StringVar(&typ, "type", "server", "resource type: server or label_selector")
	cmd.Flags().StringVar(&server, "server", "", "server name or ID")
	cmd.Flags().StringVar(&selector, "label-selector", "", "label selector")
	fixedCompletion(cmd, "type", "server", "label_selector")
	a.flagCompletion(cmd, "server", serverNames)
	return cmd
}
