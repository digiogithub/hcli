package robot

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

var resetTypes = []string{"sw", "hw", "man", "power", "power_long"}

const resetTypesHelp = `Reset types (availability depends on the server, see "reset options"):
  sw          send CTRL+ALT+DEL to the server
  hw          execute a hardware reset
  man         order a manual power cycle by a technician
  power       press the power button (power on / soft power off)
  power_long  long press of the power button (forced power off)`

func newResetCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset (reboot / power) dedicated servers",
	}
	cmd.AddCommand(newResetOptionsCommand(c), newResetExecuteCommand(c))
	return cmd
}

func newResetOptionsCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "options [server]",
		Short:             "Show the reset types available (for one server or all)",
		Args:              cobra.MaximumNArgs(1),
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
			if len(args) == 0 {
				resets, err := cl.ListResets(cmd.Context())
				if err != nil {
					return err
				}
				t := &output.Table{Headers: []string{"number", "ip", "types"}}
				for _, r := range resets {
					t.AddRow(r.ServerNumber, r.ServerIP, join(r.Type))
				}
				return p.Print(resets, t)
			}
			n, err := serverNumber(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			r, err := cl.GetReset(cmd.Context(), n)
			if err != nil {
				return err
			}
			return p.KeyValues(r, [][2]string{
				{"Number", strconv.Itoa(r.ServerNumber)},
				{"IP", r.ServerIP},
				{"IPv6 net", r.ServerIPv6Net},
				{"Types", join(r.Type)},
				{"Operating status", r.OperatingStatus},
			})
		},
	}
}

func newResetExecuteCommand(c *cli.CLI) *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:               "execute <server> --type sw|hw|man|power|power_long",
		Aliases:           []string{"exec", "run"},
		Short:             "Reset a server (asks for confirmation)",
		Long:              "Execute a reset on a server. This interrupts the running system.\n\n" + resetTypesHelp,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeServers, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !slices.Contains(resetTypes, typ) {
				return fmt.Errorf("invalid --type %q (use one of %s)", typ, join(resetTypes))
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
			if err := c.Confirm(fmt.Sprintf("execute a %q reset on server %d (%s)", typ, n, args[0])); err != nil {
				return err
			}
			r, err := cl.ExecuteReset(cmd.Context(), n, typ)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(r, nil)
			}
			fmt.Fprintf(c.Out, "Reset %q sent to server %d (%s)\n", join(r.Type), n, r.ServerIP)
			return nil
		},
	}
	cmd.Flags().StringVarP(&typ, "type", "t", "", "reset type: "+join(resetTypes))
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.RegisterFlagCompletionFunc("type", cobra.FixedCompletions(resetTypes, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newWOLCommand(c *cli.CLI) *cobra.Command {
	var status bool
	cmd := &cobra.Command{
		Use:               "wol <server>",
		Short:             "Send a Wake on LAN packet to a server (asks for confirmation)",
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
			var w *client.WOL
			if status {
				w, err = cl.GetWOL(cmd.Context(), n)
				if err != nil {
					return err
				}
				return p.KeyValues(w, [][2]string{
					{"Number", strconv.Itoa(w.ServerNumber)},
					{"IP", w.ServerIP},
					{"IPv6 net", w.ServerIPv6Net},
					{"Wake on LAN", "available"},
				})
			}
			if err := c.Confirm(fmt.Sprintf("send Wake on LAN to server %d (%s)", n, args[0])); err != nil {
				return err
			}
			if w, err = cl.SendWOL(cmd.Context(), n); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(w, nil)
			}
			fmt.Fprintf(c.Out, "Wake on LAN packet sent to server %d (%s)\n", w.ServerNumber, w.ServerIP)
			return nil
		},
	}
	cmd.Flags().BoolVar(&status, "status", false, "only check whether Wake on LAN is available")
	return cmd
}
