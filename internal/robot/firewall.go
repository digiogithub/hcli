package robot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

func newFirewallCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "firewall", Aliases: []string{"fw"}, Short: "Robot (switch port) firewall of dedicated servers"}
	tmpl := &cobra.Command{Use: "template", Aliases: []string{"templates"}, Short: "Firewall templates"}
	tmpl.AddCommand(newFirewallTemplateListCommand(c), newFirewallTemplateDescribeCommand(c))
	cmd.AddCommand(newFirewallDescribeCommand(c), newFirewallSetCommand(c), tmpl)
	return cmd
}

func rulesTable(r client.FirewallRules) *output.Table {
	t := &output.Table{Headers: []string{"dir", "#", "name", "ip_version", "protocol", "src_ip", "src_port", "dst_ip", "dst_port", "tcp_flags", "action"}}
	add := func(dir string, rules []client.FirewallRule) {
		for i, x := range rules {
			t.AddRow(dir, i+1, x.Name, x.IPVersion, x.Protocol, x.SrcIP, x.SrcPort, x.DstIP, x.DstPort, x.TCPFlags, x.Action)
		}
	}
	add("in", r.Input)
	add("out", r.Output)
	return t
}

func printFirewall(c *cli.CLI, p *output.Printer, fw *client.Firewall) error {
	if p.Format != output.FormatTable {
		return p.Print(fw, nil)
	}
	if err := p.KeyValues(fw, [][2]string{
		{"Server", strconv.Itoa(fw.ServerNumber)},
		{"Server IP", fw.ServerIP},
		{"Status", fw.Status},
		{"Port", fw.Port},
		{"Filter IPv6", yesNo(fw.FilterIPv6)},
		{"Whitelist Hetzner services", yesNo(fw.WhitelistHOS)},
	}); err != nil {
		return err
	}
	fmt.Fprintln(c.Out)
	return p.Table(rulesTable(fw.Rules))
}

func newFirewallDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "describe <server>",
		Aliases:           []string{"show", "get"},
		Short:             "Show the firewall of a server (-o json output is valid input for \"firewall set --rules-file\")",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeServers, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			n, err := serverNumber(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			fw, err := cl.GetFirewall(cmd.Context(), n)
			if err != nil {
				return err
			}
			return printFirewall(c, p, fw)
		},
	}
}

var (
	fwProtocols  = []string{"tcp", "udp", "gre", "icmp", "ipip", "ah", "esp"}
	fwActions    = []string{"accept", "discard"}
	fwIPVersions = []string{"ipv4", "ipv6"}
	fwStatuses   = []string{"active", "disabled"}
)

// parseFirewallFile reads a rules file. Accepted shapes:
//
//	{"status":"active","filter_ipv6":false,"whitelist_hos":true,"rules":{"input":[...],"output":[...]}}
//	{"firewall":{...same...}}                  (raw API answer)
//	{"input":[...],"output":[...]}             (rules only)
//
// Unknown fields are rejected so typos do not silently become wildcards.
func parseFirewallFile(data []byte) (*client.Firewall, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("rules file is not a JSON object: %w", err)
	}
	if inner, ok := probe["firewall"]; ok && len(probe) == 1 {
		data = inner
		probe = nil
		if err := json.Unmarshal(data, &probe); err != nil {
			return nil, err
		}
	}
	fw := &client.Firewall{}
	_, hasRules := probe["rules"]
	var target any = fw
	if !hasRules {
		target = &fw.Rules
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return nil, fmt.Errorf("invalid rules file: %w", err)
	}
	if err := validateFirewall(fw); err != nil {
		return nil, err
	}
	return fw, nil
}

func validateFirewall(fw *client.Firewall) error {
	var errs []error
	if fw.Status != "" && !slices.Contains(fwStatuses, fw.Status) {
		errs = append(errs, fmt.Errorf("status %q must be active or disabled", fw.Status))
	}
	check := func(dir string, rules []client.FirewallRule) {
		for i, r := range rules {
			at := fmt.Sprintf("rules.%s[%d]", dir, i)
			if r.Name != "" {
				at += " (" + r.Name + ")"
			}
			if !slices.Contains(fwActions, r.Action) {
				errs = append(errs, fmt.Errorf("%s: action %q must be accept or discard", at, r.Action))
			}
			if r.IPVersion != "" && !slices.Contains(fwIPVersions, r.IPVersion) {
				errs = append(errs, fmt.Errorf("%s: ip_version %q must be ipv4 or ipv6", at, r.IPVersion))
			}
			if r.Protocol != "" && !slices.Contains(fwProtocols, r.Protocol) {
				errs = append(errs, fmt.Errorf("%s: protocol %q must be one of %s", at, r.Protocol, join(fwProtocols)))
			}
			if r.Protocol != "" && r.IPVersion == "" {
				errs = append(errs, fmt.Errorf("%s: filtering on protocol requires ip_version", at))
			}
			for _, f := range []struct{ name, v string }{{"src_ip", r.SrcIP}, {"dst_ip", r.DstIP}} {
				if f.v == "" {
					continue
				}
				if r.IPVersion != "ipv4" {
					errs = append(errs, fmt.Errorf("%s: %s is only allowed with ip_version ipv4", at, f.name))
				}
				if _, _, err := net.ParseCIDR(f.v); err != nil && net.ParseIP(f.v) == nil {
					errs = append(errs, fmt.Errorf("%s: %s %q is not an IP or CIDR", at, f.name, f.v))
				}
			}
			for _, f := range []struct{ name, v string }{{"src_port", r.SrcPort}, {"dst_port", r.DstPort}} {
				if f.v != "" && !validPortSpec(f.v) {
					errs = append(errs, fmt.Errorf("%s: %s %q must be a port or range (e.g. 80 or 1024-65535)", at, f.name, f.v))
				}
			}
		}
	}
	check("input", fw.Rules.Input)
	check("output", fw.Rules.Output)
	if len(fw.Rules.Input)+len(fw.Rules.Output) == 0 {
		errs = append(errs, errors.New("rules file has no rules (input/output)"))
	}
	return errors.Join(errs...)
}

func validPortSpec(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}

func newFirewallSetCommand(c *cli.CLI) *cobra.Command {
	var (
		rulesFile, template, status string
		whitelist, filterIPv6       bool
	)
	cmd := &cobra.Command{
		Use:   "set <server> (--rules-file rules.json | --template <id>)",
		Short: "Replace the firewall configuration of a server (asks for confirmation)",
		Long: `Replace the whole firewall configuration of a server, either from a JSON file
or from a firewall template. The file uses the API field names, e.g. the
output of "hcli robot firewall describe <server> -o json":

  {
    "status": "active",
    "filter_ipv6": false,
    "whitelist_hos": true,
    "rules": {
      "input": [
        {"name": "ssh", "ip_version": "ipv4", "protocol": "tcp", "dst_port": "22", "action": "accept"},
        {"name": "established", "ip_version": "ipv4", "protocol": "tcp", "dst_port": "32768-65535", "tcp_flags": "ack", "action": "accept"}
      ],
      "output": [{"name": "allow all", "action": "accept"}]
    }
  }

Omitted rule fields act as wildcards. --status/--whitelist-hos/--filter-ipv6
override the file. A wrong rule set can lock you out of the server.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeServers, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (rulesFile == "") == (template == "") {
				return errors.New("pass exactly one of --rules-file or --template")
			}
			if status != "" && !slices.Contains(fwStatuses, status) {
				return fmt.Errorf("invalid --status %q (active or disabled)", status)
			}
			u := client.FirewallUpdate{Status: status, TemplateID: template}
			if rulesFile != "" {
				data, err := os.ReadFile(rulesFile)
				if err != nil {
					return err
				}
				fw, err := parseFirewallFile(data)
				if err != nil {
					return err
				}
				if u.Status == "" {
					u.Status = fw.Status
				}
				wl, f6 := fw.WhitelistHOS, fw.FilterIPv6
				u.WhitelistHOS, u.FilterIPv6, u.Rules = &wl, &f6, &fw.Rules
			}
			if cmd.Flags().Changed("whitelist-hos") {
				if template != "" {
					return errors.New("--whitelist-hos cannot be combined with --template")
				}
				u.WhitelistHOS = &whitelist
			}
			if cmd.Flags().Changed("filter-ipv6") {
				u.FilterIPv6 = &filterIPv6
			}
			if u.Status == "" {
				u.Status = "active"
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			n, err := serverNumber(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			what := "the rules in " + rulesFile
			if template != "" {
				what = "firewall template " + template
			} else {
				what += fmt.Sprintf(" (%d input, %d output rules)", len(u.Rules.Input), len(u.Rules.Output))
			}
			if err := c.Confirm(fmt.Sprintf("replace the firewall of server %d (%s) with %s, status %s", n, args[0], what, u.Status)); err != nil {
				return err
			}
			fw, err := cl.SetFirewall(cmd.Context(), n, u)
			if err != nil {
				return err
			}
			return printFirewall(c, p, fw)
		},
	}
	f := cmd.Flags()
	f.StringVarP(&rulesFile, "rules-file", "f", "", "JSON file with the firewall configuration")
	f.StringVar(&template, "template", "", "firewall template ID to apply")
	f.StringVar(&status, "status", "", "active or disabled (default: from file, else active)")
	f.BoolVar(&whitelist, "whitelist-hos", true, "allow Hetzner services (overrides the file)")
	f.BoolVar(&filterIPv6, "filter-ipv6", false, "filter IPv6 traffic too (overrides the file)")
	_ = cmd.RegisterFlagCompletionFunc("template", flagCompletion(c, completeTemplates))
	_ = cmd.RegisterFlagCompletionFunc("status", cobra.FixedCompletions(fwStatuses, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.MarkFlagFilename("rules-file", "json")
	return cmd
}

func newFirewallTemplateListCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List firewall templates",
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
			ts, err := cl.ListFirewallTemplates(cmd.Context())
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"id", "name", "filter_ipv6", "whitelist_hos", "default"}}
			for _, x := range ts {
				t.AddRow(x.ID, x.Name, yesNo(x.FilterIPv6), yesNo(x.WhitelistHOS), yesNo(x.IsDefault))
			}
			return p.Print(ts, t)
		},
	}
}

func newFirewallTemplateDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "describe <template-id>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a firewall template and its rules",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeTemplates, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			x, err := cl.GetFirewallTemplate(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(x, nil)
			}
			if err := p.KeyValues(x, [][2]string{
				{"ID", strconv.Itoa(x.ID)},
				{"Name", x.Name},
				{"Filter IPv6", yesNo(x.FilterIPv6)},
				{"Whitelist Hetzner services", yesNo(x.WhitelistHOS)},
				{"Default", yesNo(x.IsDefault)},
			}); err != nil {
				return err
			}
			if x.Rules == nil {
				return nil
			}
			fmt.Fprintln(c.Out)
			return p.Table(rulesTable(*x.Rules))
		},
	}
}
