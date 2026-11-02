package cloud

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var networkRes = resource[hcloud.Network]{
	Kind: "network",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Network, *hcloud.Response, error) {
		return cl.Network.Get(ctx, s)
	},
	Names:  networkNames,
	Ident:  func(n *hcloud.Network) (int64, string) { return n.ID, n.Name },
	Schema: func(n *hcloud.Network) any { return hcloud.SchemaFromNetwork(n) },
}

var networkZones = []string{
	string(hcloud.NetworkZoneEUCentral), string(hcloud.NetworkZoneUSEast),
	string(hcloud.NetworkZoneUSWest), string(hcloud.NetworkZoneAPSouthEast),
}

func newNetworkCommand(a *app) *cobra.Command {
	cmd := group("network", "Manage private networks", []string{"networks", "net"},
		newListCmd(a, networkRes, listSpec[hcloud.Network]{
			Selector: true,
			Headers:  []string{"id", "name", "ip range", "subnets", "servers", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Network, error) {
				return cl.Network.AllWithOpts(ctx, hcloud.NetworkListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}})
			},
			Row: func(n *hcloud.Network) []any {
				return []any{n.ID, n.Name, netString(n.IPRange), len(n.Subnets), len(n.Servers), age(n.Created)}
			},
		}),
		newDescribeCmd(a, networkRes, func(n *hcloud.Network) [][2]string {
			kv := [][2]string{
				{"ID", strconv.FormatInt(n.ID, 10)},
				{"Name", n.Name},
				{"Created", created(n.Created)},
				{"IP range", netString(n.IPRange)},
				{"Expose routes to vSwitch", yesNo(n.ExposeRoutesToVSwitch)},
				{"Protection", "delete=" + yesNo(n.Protection.Delete)},
				{"Labels", formatLabels(n.Labels)},
			}
			for _, s := range n.Subnets {
				v := fmt.Sprintf("%s %s zone=%s gateway=%s", s.Type, netString(s.IPRange), s.NetworkZone, ipString(s.Gateway))
				if s.VSwitchID != 0 {
					v += fmt.Sprintf(" vswitch=%d", s.VSwitchID)
				}
				kv = append(kv, [2]string{"Subnet", v})
			}
			for _, r := range n.Routes {
				kv = append(kv, [2]string{"Route", fmt.Sprintf("%s via %s", netString(r.Destination), ipString(r.Gateway))})
			}
			for _, s := range n.Servers {
				kv = append(kv, [2]string{"Server", strconv.FormatInt(s.ID, 10)})
			}
			return kv
		}),
		newNetworkCreateCmd(a),
		newDeleteCmd(a, networkRes, func(ctx context.Context, cl *hcloud.Client, n *hcloud.Network) ([]*hcloud.Action, error) {
			_, err := cl.Network.Delete(ctx, n)
			return nil, err
		}),
		newNetworkAddSubnetCmd(a),
		newNetworkAddRouteCmd(a),
	)
	cmd.AddCommand(newLabelCmds(a, networkRes,
		func(n *hcloud.Network) map[string]string { return n.Labels },
		func(ctx context.Context, cl *hcloud.Client, n *hcloud.Network, l map[string]string) error {
			_, _, err := cl.Network.Update(ctx, n, hcloud.NetworkUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

func parseCIDR(flag, s string) (*net.IPNet, error) {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return nil, fmt.Errorf("--%s: %w", flag, err)
	}
	return n, nil
}

func newNetworkCreateCmd(a *app) *cobra.Command {
	var name, ipRange string
	var expose bool
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --name <name> --ip-range <cidr>",
		Short: "Create a network",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			r, err := parseCIDR("ip-range", ipRange)
			if err != nil {
				return err
			}
			l, err := parseLabels(labels)
			if err != nil {
				return err
			}
			n, _, err := cl.Network.Create(ctx, hcloud.NetworkCreateOpts{Name: name, IPRange: r, Labels: l, ExposeRoutesToVSwitch: expose})
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromNetwork(n), nil)
			}
			fmt.Fprintf(p.Out, "Network %d created\n", n.ID)
			return nil
		}),
	}
	cmd.Flags().StringVar(&name, "name", "", "network name (required)")
	cmd.Flags().StringVar(&ipRange, "ip-range", "", "IP range in CIDR notation, e.g. 10.0.0.0/16 (required)")
	cmd.Flags().BoolVar(&expose, "expose-routes-to-vswitch", false, "expose routes to a connected vSwitch")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("ip-range")
	return cmd
}

func newNetworkAddSubnetCmd(a *app) *cobra.Command {
	var typ, zone, ipRange string
	var vswitch int64
	cmd := &cobra.Command{
		Use:               "add-subnet <network> --type <cloud|server|vswitch> --network-zone <zone> [--ip-range <cidr>]",
		Short:             "Add a subnet to a network",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(networkNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			n, err := networkRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			sub := hcloud.NetworkSubnet{Type: hcloud.NetworkSubnetType(typ), NetworkZone: hcloud.NetworkZone(zone), VSwitchID: vswitch}
			if ipRange != "" {
				if sub.IPRange, err = parseCIDR("ip-range", ipRange); err != nil {
					return err
				}
			}
			action, _, err := cl.Network.AddSubnet(ctx, n, hcloud.NetworkAddSubnetOpts{Subnet: sub})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("subnet on network %s", n.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Subnet added to network %s", n.Name)
		}),
	}
	cmd.Flags().StringVar(&typ, "type", string(hcloud.NetworkSubnetTypeCloud), "subnet type: cloud, server, vswitch")
	cmd.Flags().StringVar(&zone, "network-zone", string(hcloud.NetworkZoneEUCentral), "network zone")
	cmd.Flags().StringVar(&ipRange, "ip-range", "", "subnet range (default: chosen by the API)")
	cmd.Flags().Int64Var(&vswitch, "vswitch-id", 0, "Robot vSwitch ID (type vswitch)")
	fixedCompletion(cmd, "type", "cloud", "server", "vswitch")
	fixedCompletion(cmd, "network-zone", networkZones...)
	return cmd
}

func newNetworkAddRouteCmd(a *app) *cobra.Command {
	var dest, gw string
	cmd := &cobra.Command{
		Use:               "add-route <network> --destination <cidr> --gateway <ip>",
		Short:             "Add a route to a network",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(networkNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			n, err := networkRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			d, err := parseCIDR("destination", dest)
			if err != nil {
				return err
			}
			g := net.ParseIP(gw)
			if g == nil {
				return fmt.Errorf("--gateway: invalid IP %q", gw)
			}
			action, _, err := cl.Network.AddRoute(ctx, n, hcloud.NetworkAddRouteOpts{Route: hcloud.NetworkRoute{Destination: d, Gateway: g}})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("route on network %s", n.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Route %s via %s added to network %s", d, g, n.Name)
		}),
	}
	cmd.Flags().StringVar(&dest, "destination", "", "destination CIDR (required)")
	cmd.Flags().StringVar(&gw, "gateway", "", "gateway IP (required)")
	_ = cmd.MarkFlagRequired("destination")
	_ = cmd.MarkFlagRequired("gateway")
	return cmd
}
