package cloud

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

// SSH keys, certificates, placement groups and load balancers.

var sshKeyRes = resource[hcloud.SSHKey]{
	Kind: "ssh-key",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.SSHKey, *hcloud.Response, error) {
		return cl.SSHKey.Get(ctx, s)
	},
	Names:  sshKeyNames,
	Ident:  func(k *hcloud.SSHKey) (int64, string) { return k.ID, k.Name },
	Schema: func(k *hcloud.SSHKey) any { return hcloud.SchemaFromSSHKey(k) },
}

var certificateRes = resource[hcloud.Certificate]{
	Kind: "certificate",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Certificate, *hcloud.Response, error) {
		return cl.Certificate.Get(ctx, s)
	},
	Names:  certificateNames,
	Ident:  func(c *hcloud.Certificate) (int64, string) { return c.ID, c.Name },
	Schema: func(c *hcloud.Certificate) any { return hcloud.SchemaFromCertificate(c) },
}

var placementGroupRes = resource[hcloud.PlacementGroup]{
	Kind: "placement-group",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.PlacementGroup, *hcloud.Response, error) {
		return cl.PlacementGroup.Get(ctx, s)
	},
	Names:  placementGroupNames,
	Ident:  func(p *hcloud.PlacementGroup) (int64, string) { return p.ID, p.Name },
	Schema: func(p *hcloud.PlacementGroup) any { return hcloud.SchemaFromPlacementGroup(p) },
}

var loadBalancerRes = resource[hcloud.LoadBalancer]{
	Kind: "load-balancer",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.LoadBalancer, *hcloud.Response, error) {
		return cl.LoadBalancer.Get(ctx, s)
	},
	Names:  loadBalancerNames,
	Ident:  func(l *hcloud.LoadBalancer) (int64, string) { return l.ID, l.Name },
	Schema: func(l *hcloud.LoadBalancer) any { return hcloud.SchemaFromLoadBalancer(l) },
}

func newSSHKeyCommand(a *app) *cobra.Command {
	cmd := group("ssh-key", "Manage SSH keys", []string{"ssh-keys", "key"},
		newListCmd(a, sshKeyRes, listSpec[hcloud.SSHKey]{
			Selector: true,
			Headers:  []string{"id", "name", "fingerprint", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.SSHKey, error) {
				return cl.SSHKey.AllWithOpts(ctx, hcloud.SSHKeyListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(k *hcloud.SSHKey) []any { return []any{k.ID, k.Name, k.Fingerprint, age(k.Created)} },
		}),
		newDescribeCmd(a, sshKeyRes, func(k *hcloud.SSHKey) [][2]string {
			return [][2]string{
				{"ID", strconv.FormatInt(k.ID, 10)},
				{"Name", k.Name},
				{"Created", created(k.Created)},
				{"Fingerprint", k.Fingerprint},
				{"Public key", strings.TrimSpace(k.PublicKey)},
				{"Labels", formatLabels(k.Labels)},
			}
		}),
		newSSHKeyCreateCmd(a),
		newDeleteCmd(a, sshKeyRes, func(ctx context.Context, cl *hcloud.Client, k *hcloud.SSHKey) ([]*hcloud.Action, error) {
			_, err := cl.SSHKey.Delete(ctx, k)
			return nil, err
		}),
	)
	cmd.AddCommand(newLabelCmds(a, sshKeyRes,
		func(k *hcloud.SSHKey) map[string]string { return k.Labels },
		func(ctx context.Context, cl *hcloud.Client, k *hcloud.SSHKey, l map[string]string) error {
			_, _, err := cl.SSHKey.Update(ctx, k, hcloud.SSHKeyUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

func newSSHKeyCreateCmd(a *app) *cobra.Command {
	var name, key, file string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --name <name> (--public-key <key> | --public-key-from-file <file>)",
		Short: "Upload an SSH public key",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			if file != "" {
				data, err := a.readFileOrStdin(file)
				if err != nil {
					return err
				}
				key = string(data)
			}
			key = strings.TrimSpace(key)
			if key == "" {
				return errors.New("--public-key or --public-key-from-file is required")
			}
			l, err := parseLabels(labels)
			if err != nil {
				return err
			}
			k, _, err := cl.SSHKey.Create(ctx, hcloud.SSHKeyCreateOpts{Name: name, PublicKey: key, Labels: l})
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromSSHKey(k), nil)
			}
			fmt.Fprintf(p.Out, "SSH key %d created (%s)\n", k.ID, k.Fingerprint)
			return nil
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "key name (required)")
	cmd.Flags().StringVar(&key, "public-key", "", "public key")
	cmd.Flags().StringVar(&file, "public-key-from-file", "", "read the public key from a file (\"-\" for stdin)")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	cmd.MarkFlagsMutuallyExclusive("public-key", "public-key-from-file")
	_ = cmd.MarkFlagFilename("public-key-from-file", "pub")
	return cmd
}

func newCertificateCommand(a *app) *cobra.Command {
	return group("certificate", "Manage TLS certificates", []string{"certificates", "cert"},
		newListCmd(a, certificateRes, listSpec[hcloud.Certificate]{
			Selector: true,
			Headers:  []string{"id", "name", "type", "domain names", "not valid after", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Certificate, error) {
				return cl.Certificate.AllWithOpts(ctx, hcloud.CertificateListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(c *hcloud.Certificate) []any {
				return []any{c.ID, c.Name, c.Type, strings.Join(c.DomainNames, ", "), dateOnly(c.NotValidAfter), age(c.Created)}
			},
		}),
		newDescribeCmd(a, certificateRes, func(c *hcloud.Certificate) [][2]string {
			kv := [][2]string{
				{"ID", strconv.FormatInt(c.ID, 10)},
				{"Name", c.Name},
				{"Type", string(c.Type)},
				{"Created", created(c.Created)},
				{"Domain names", strings.Join(c.DomainNames, ", ")},
				{"Fingerprint", c.Fingerprint},
				{"Not valid before", dateOnly(c.NotValidBefore)},
				{"Not valid after", dateOnly(c.NotValidAfter)},
				{"Labels", formatLabels(c.Labels)},
			}
			if c.Status != nil {
				kv = append(kv, [2]string{"Status", fmt.Sprintf("issuance=%s renewal=%s", c.Status.Issuance, c.Status.Renewal)})
			}
			for _, u := range c.UsedBy {
				kv = append(kv, [2]string{"Used by", fmt.Sprintf("%s %d", u.Type, u.ID)})
			}
			return kv
		}),
		newDeleteCmd(a, certificateRes, func(ctx context.Context, cl *hcloud.Client, c *hcloud.Certificate) ([]*hcloud.Action, error) {
			_, err := cl.Certificate.Delete(ctx, c)
			return nil, err
		}),
	)
}

func dateOnly(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func newPlacementGroupCommand(a *app) *cobra.Command {
	var name string
	var labels []string
	create := &cobra.Command{
		Use:   "create --name <name>",
		Short: "Create a spread placement group",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			l, err := parseLabels(labels)
			if err != nil {
				return err
			}
			res, _, err := cl.PlacementGroup.Create(ctx, hcloud.PlacementGroupCreateOpts{Name: name, Labels: l, Type: hcloud.PlacementGroupTypeSpread})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("placement group %s", name), res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromPlacementGroup(res.PlacementGroup), nil)
			}
			fmt.Fprintf(p.Out, "Placement group %d created\n", res.PlacementGroup.ID)
			return nil
		}),
	}
	create.Flags().StringVar(&name, "name", "", "name (required)")
	addLabelFlag(create, &labels)
	_ = create.MarkFlagRequired("name")

	return group("placement-group", "Manage placement groups", []string{"placement-groups", "pg"},
		newListCmd(a, placementGroupRes, listSpec[hcloud.PlacementGroup]{
			Selector: true,
			Headers:  []string{"id", "name", "servers", "type", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.PlacementGroup, error) {
				return cl.PlacementGroup.AllWithOpts(ctx, hcloud.PlacementGroupListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(p *hcloud.PlacementGroup) []any {
				return []any{p.ID, p.Name, len(p.Servers), p.Type, age(p.Created)}
			},
		}),
		newDescribeCmd(a, placementGroupRes, func(p *hcloud.PlacementGroup) [][2]string {
			ids := make([]string, len(p.Servers))
			for i, id := range p.Servers {
				ids[i] = strconv.FormatInt(id, 10)
			}
			return [][2]string{
				{"ID", strconv.FormatInt(p.ID, 10)},
				{"Name", p.Name},
				{"Type", string(p.Type)},
				{"Created", created(p.Created)},
				{"Servers", strings.Join(ids, ", ")},
				{"Labels", formatLabels(p.Labels)},
			}
		}),
		create,
		newDeleteCmd(a, placementGroupRes, func(ctx context.Context, cl *hcloud.Client, p *hcloud.PlacementGroup) ([]*hcloud.Action, error) {
			_, err := cl.PlacementGroup.Delete(ctx, p)
			return nil, err
		}),
	)
}

func newLoadBalancerCommand(a *app) *cobra.Command {
	cmd := group("load-balancer", "Manage load balancers", []string{"load-balancers", "lb"},
		newListCmd(a, loadBalancerRes, listSpec[hcloud.LoadBalancer]{
			Selector: true,
			Headers:  []string{"id", "name", "ipv4", "ipv6", "type", "location", "services", "targets", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.LoadBalancer, error) {
				return cl.LoadBalancer.AllWithOpts(ctx, hcloud.LoadBalancerListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(l *hcloud.LoadBalancer) []any {
				return []any{l.ID, l.Name, ipString(l.PublicNet.IPv4.IP), ipString(l.PublicNet.IPv6.IP), lbTypeName(l), locationName(l.Location), len(l.Services), len(l.Targets), age(l.Created)}
			},
		}),
		newDescribeCmd(a, loadBalancerRes, describeLoadBalancer),
		newLoadBalancerCreateCmd(a),
		newDeleteCmd(a, loadBalancerRes, func(ctx context.Context, cl *hcloud.Client, l *hcloud.LoadBalancer) ([]*hcloud.Action, error) {
			res, _, err := cl.LoadBalancer.DeleteWithResult(ctx, l)
			if err != nil {
				return nil, err
			}
			return []*hcloud.Action{res.Action}, nil
		}),
	)
	cmd.AddCommand(newLabelCmds(a, loadBalancerRes,
		func(l *hcloud.LoadBalancer) map[string]string { return l.Labels },
		func(ctx context.Context, cl *hcloud.Client, l *hcloud.LoadBalancer, labels map[string]string) error {
			_, _, err := cl.LoadBalancer.Update(ctx, l, hcloud.LoadBalancerUpdateOpts{Labels: labels})
			return err
		})...)
	return cmd
}

func lbTypeName(l *hcloud.LoadBalancer) string {
	if l.LoadBalancerType == nil {
		return ""
	}
	return l.LoadBalancerType.Name
}

func describeLoadBalancer(l *hcloud.LoadBalancer) [][2]string {
	kv := [][2]string{
		{"ID", strconv.FormatInt(l.ID, 10)},
		{"Name", l.Name},
		{"Created", created(l.Created)},
		{"Type", lbTypeName(l)},
		{"Location", locationName(l.Location)},
		{"Algorithm", string(l.Algorithm.Type)},
		{"Public interface", yesNo(l.PublicNet.Enabled)},
		{"Public IPv4", joinNonEmpty(ipString(l.PublicNet.IPv4.IP), l.PublicNet.IPv4.DNSPtr)},
		{"Public IPv6", joinNonEmpty(ipString(l.PublicNet.IPv6.IP), l.PublicNet.IPv6.DNSPtr)},
	}
	for _, pn := range l.PrivateNet {
		id := int64(0)
		if pn.Network != nil {
			id = pn.Network.ID
		}
		kv = append(kv, [2]string{"Private network", fmt.Sprintf("%d: %s", id, ipString(pn.IP))})
	}
	for _, s := range l.Services {
		kv = append(kv, [2]string{"Service", fmt.Sprintf("%s %d -> %d", s.Protocol, s.ListenPort, s.DestinationPort)})
	}
	for _, t := range l.Targets {
		v := string(t.Type)
		switch {
		case t.Server != nil && t.Server.Server != nil:
			v += " " + strconv.FormatInt(t.Server.Server.ID, 10)
		case t.LabelSelector != nil:
			v += " " + t.LabelSelector.Selector
		case t.IP != nil:
			v += " " + t.IP.IP
		}
		kv = append(kv, [2]string{"Target", v})
	}
	kv = append(kv,
		[2]string{"Traffic", fmt.Sprintf("out %s / in %s / included %s", bytesHuman(l.OutgoingTraffic), bytesHuman(l.IngoingTraffic), bytesHuman(l.IncludedTraffic))},
		[2]string{"Protection", "delete=" + yesNo(l.Protection.Delete)},
		[2]string{"Labels", formatLabels(l.Labels)},
	)
	return kv
}

func newLoadBalancerCreateCmd(a *app) *cobra.Command {
	var name, typ, location, zone, algorithm string
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --name <name> --type <type> (--location <loc> | --network-zone <zone>)",
		Short: "Create a load balancer (billable)",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			if location == "" && zone == "" {
				return errors.New("--location or --network-zone is required")
			}
			lbt, _, err := cl.LoadBalancerType.Get(ctx, typ)
			if err != nil {
				return err
			}
			if lbt == nil {
				return fmt.Errorf("load balancer type not found: %s", typ)
			}
			opts := hcloud.LoadBalancerCreateOpts{
				Name:             name,
				LoadBalancerType: lbt,
				NetworkZone:      hcloud.NetworkZone(zone),
				Algorithm:        &hcloud.LoadBalancerAlgorithm{Type: hcloud.LoadBalancerAlgorithmType(algorithm)},
			}
			if location != "" {
				if opts.Location, err = locationRes.fetch(ctx, cl, location); err != nil {
					return err
				}
			}
			if opts.Labels, err = parseLabels(labels); err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("create load balancer %s (%s) — this is billable", name, lbt.Name)); err != nil {
				return err
			}
			res, _, err := cl.LoadBalancer.Create(ctx, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("load balancer %s to be created", name), res.Action); err != nil {
				return err
			}
			lb := res.LoadBalancer
			if fresh, _, err := cl.LoadBalancer.GetByID(ctx, lb.ID); err == nil && fresh != nil {
				lb = fresh
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromLoadBalancer(lb), nil)
			}
			fmt.Fprintf(p.Out, "Load balancer %d created\n", lb.ID)
			if ip := ipString(lb.PublicNet.IPv4.IP); ip != "" {
				fmt.Fprintf(p.Out, "IPv4: %s\n", ip)
			}
			return nil
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "name (required)")
	fl.StringVar(&typ, "type", "", "load balancer type, e.g. lb11 (required)")
	fl.StringVar(&location, "location", "", "location")
	fl.StringVar(&zone, "network-zone", "", "network zone")
	fl.StringVar(&algorithm, "algorithm", string(hcloud.LoadBalancerAlgorithmTypeRoundRobin), "round_robin or least_connections")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	cmd.MarkFlagsMutuallyExclusive("location", "network-zone")
	a.flagCompletion(cmd, "type", loadBalancerTypeNames)
	a.flagCompletion(cmd, "location", locationNames)
	fixedCompletion(cmd, "network-zone", networkZones...)
	fixedCompletion(cmd, "algorithm", string(hcloud.LoadBalancerAlgorithmTypeRoundRobin), string(hcloud.LoadBalancerAlgorithmTypeLeastConnections))
	return cmd
}
