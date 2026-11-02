package robot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

func newVSwitchCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "vswitch", Aliases: []string{"vswitches", "vs"}, Short: "vSwitches (VLANs between dedicated servers and Cloud networks)"}
	cmd.AddCommand(
		newVSwitchListCommand(c), newVSwitchDescribeCommand(c), newVSwitchCreateCommand(c),
		newVSwitchDeleteCommand(c), newVSwitchServersCommand(c, true), newVSwitchServersCommand(c, false),
	)
	return cmd
}

func vswitchID(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid vSwitch ID %q", s)
	}
	return n, nil
}

func newVSwitchListCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List vSwitches",
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
			vs, err := cl.ListVSwitches(cmd.Context())
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"id", "name", "vlan", "cancelled"}}
			for _, v := range vs {
				t.AddRow(v.ID, v.Name, v.VLAN, yesNo(v.Cancelled))
			}
			return p.Print(vs, t)
		},
	}
}

func newVSwitchDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "describe <vswitch-id>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a vSwitch with its servers, subnets and Cloud networks",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeVSwitches, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := vswitchID(args[0])
			if err != nil {
				return err
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			v, err := cl.GetVSwitch(cmd.Context(), id)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(v, nil)
			}
			if err := p.KeyValues(v, vswitchPairs(v)); err != nil {
				return err
			}
			if len(v.Server) > 0 {
				fmt.Fprintln(c.Out)
				t := &output.Table{Headers: []string{"server", "server_ip", "server_ipv6_net", "status"}}
				for _, s := range v.Server {
					t.AddRow(s.ServerNumber, s.ServerIP, s.ServerIPv6Net, s.Status)
				}
				return p.Table(t)
			}
			return nil
		},
	}
}

func vswitchPairs(v *client.VSwitch) [][2]string {
	subnets := make([]string, 0, len(v.Subnet))
	for _, s := range v.Subnet {
		subnets = append(subnets, fmt.Sprintf("%s/%d (gw %s)", s.IP, s.Mask, s.Gateway))
	}
	nets := make([]string, 0, len(v.CloudNetwork))
	for _, n := range v.CloudNetwork {
		nets = append(nets, fmt.Sprintf("%d: %s/%d (gw %s)", n.ID, n.IP, n.Mask, n.Gateway))
	}
	return [][2]string{
		{"ID", strconv.Itoa(v.ID)},
		{"Name", v.Name},
		{"VLAN", strconv.Itoa(v.VLAN)},
		{"Cancelled", yesNo(v.Cancelled)},
		{"Servers", strconv.Itoa(len(v.Server))},
		{"Subnets", join(subnets)},
		{"Cloud networks", join(nets)},
	}
}

func newVSwitchCreateCommand(c *cli.CLI) *cobra.Command {
	var vlan int
	cmd := &cobra.Command{
		Use:   "create <name> --vlan <4000-4091>",
		Short: "Create a vSwitch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if vlan < 4000 || vlan > 4091 {
				return fmt.Errorf("--vlan must be between 4000 and 4091")
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			v, err := cl.CreateVSwitch(cmd.Context(), args[0], vlan)
			if err != nil {
				return err
			}
			return p.KeyValues(v, vswitchPairs(v))
		},
	}
	cmd.Flags().IntVar(&vlan, "vlan", 0, "VLAN ID (4000-4091)")
	_ = cmd.MarkFlagRequired("vlan")
	return cmd
}

func newVSwitchDeleteCommand(c *cli.CLI) *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:               "delete <vswitch-id> [--cancellation-date now|YYYY-MM-DD]",
		Aliases:           []string{"rm", "cancel"},
		Short:             "Cancel a vSwitch (asks for confirmation)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeVSwitches, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := vswitchID(args[0])
			if err != nil {
				return err
			}
			if date != "now" {
				if _, err := time.Parse("2006-01-02", date); err != nil {
					return fmt.Errorf("--cancellation-date must be now or YYYY-MM-DD")
				}
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			if err := c.Confirm(fmt.Sprintf("cancel vSwitch %d (effective %s)", id, date)); err != nil {
				return err
			}
			if err := cl.CancelVSwitch(cmd.Context(), id, date); err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "vSwitch %d cancelled (effective %s)\n", id, date)
			return nil
		},
	}
	cmd.Flags().StringVar(&date, "cancellation-date", "now", "now or YYYY-MM-DD")
	_ = cmd.RegisterFlagCompletionFunc("cancellation-date", cobra.FixedCompletions([]string{"now"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newVSwitchServersCommand(c *cli.CLI, add bool) *cobra.Command {
	use, short, verb, prep := "add-server", "Attach servers to a vSwitch (asks for confirmation)", "attach", "to"
	if !add {
		use, short, verb, prep = "remove-server", "Detach servers from a vSwitch (asks for confirmation)", "detach", "from"
	}
	return &cobra.Command{
		Use:   use + " <vswitch-id> <server>...",
		Short: short,
		Args:  cobra.MinimumNArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return complete(c, completeVSwitches)(cmd, args, toComplete)
			}
			return complete(c, completeServers)(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := vswitchID(args[0])
			if err != nil {
				return err
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			var servers []string
			for _, ref := range args[1:] {
				n, err := serverNumber(cmd.Context(), cl, ref)
				if err != nil {
					return err
				}
				servers = append(servers, strconv.Itoa(n))
			}
			if err := c.Confirm(fmt.Sprintf("%s server(s) %s %s vSwitch %d", verb, strings.Join(servers, ", "), prep, id)); err != nil {
				return err
			}
			if add {
				err = cl.AddVSwitchServers(cmd.Context(), id, servers)
			} else {
				err = cl.RemoveVSwitchServers(cmd.Context(), id, servers)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "Requested to %s server(s) %s %s vSwitch %d; it is applied asynchronously (see \"vswitch describe %d\")\n", verb, strings.Join(servers, ", "), prep, id, id)
			return nil
		},
	}
}
