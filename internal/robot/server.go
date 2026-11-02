package robot

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

func newServerCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"servers", "srv"},
		Short:   "Dedicated servers",
	}
	cmd.AddCommand(newServerListCommand(c), newServerDescribeCommand(c), newServerRenameCommand(c), newServerCancellationCommand(c))
	return cmd
}

func newServerListCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List servers",
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
			servers, err := cl.ListServers(cmd.Context())
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"number", "name", "ip", "product", "dc", "status", "paid_until", "cancelled"}}
			for _, s := range servers {
				ip := s.ServerIP
				if ip == "" {
					ip = s.ServerIPv6Net
				}
				t.AddRow(s.ServerNumber, s.ServerName, ip, s.Product, s.DC, s.Status, s.PaidUntil, yesNo(s.Cancelled))
			}
			return p.Print(servers, t)
		},
	}
}

func newServerDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "describe <server>",
		Aliases:           []string{"show", "get"},
		Short:             "Show a server (by number, IP or name)",
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
			s, err := serverByRef(cmd.Context(), cl, args[0])
			if err != nil {
				return err
			}
			// A server found by name/IP comes from the list (no feature flags).
			if s.Reset == nil {
				if full, err := cl.GetServer(cmd.Context(), s.ServerNumber); err == nil {
					s = full
				}
			}
			return p.KeyValues(s, serverPairs(s))
		},
	}
}

func serverPairs(s *client.Server) [][2]string {
	subnets := make([]string, 0, len(s.Subnet))
	for _, sn := range s.Subnet {
		subnets = append(subnets, sn.IP+"/"+string(sn.Mask))
	}
	pairs := [][2]string{
		{"Number", strconv.Itoa(s.ServerNumber)},
		{"Name", s.ServerName},
		{"IPv4", s.ServerIP},
		{"IPv6 net", s.ServerIPv6Net},
		{"Product", s.Product},
		{"Datacenter", s.DC},
		{"Traffic", s.Traffic},
		{"Status", s.Status},
		{"Cancelled", yesNo(s.Cancelled)},
		{"Paid until", s.PaidUntil},
		{"IPs", join(s.IP)},
		{"Subnets", join(subnets)},
	}
	var features []string
	for _, f := range []struct {
		name string
		v    *bool
	}{{"reset", s.Reset}, {"rescue", s.Rescue}, {"vnc", s.VNC}, {"windows", s.Windows}, {"plesk", s.Plesk}, {"cpanel", s.CPanel}, {"wol", s.WOL}, {"hot_swap", s.HotSwap}} {
		if f.v != nil && *f.v {
			features = append(features, f.name)
		}
	}
	if s.Reset != nil {
		pairs = append(pairs, [2]string{"Features", join(features)})
	}
	if s.LinkedStoragebox != nil {
		pairs = append(pairs, [2]string{"Linked Storage Box", strconv.Itoa(*s.LinkedStoragebox)})
	}
	return pairs
}

func newServerRenameCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "rename <server> <new-name>",
		Short:             "Change the server name",
		Args:              cobra.ExactArgs(2),
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
			s, err := cl.RenameServer(cmd.Context(), n, args[1])
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(s, nil)
			}
			fmt.Fprintf(c.Out, "Server %d renamed to %q\n", s.ServerNumber, s.ServerName)
			return nil
		},
	}
}

func newServerCancellationCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "cancellation <server>",
		Short: "Show the cancellation status of a server (read only)",
		Long: `Show the cancellation data of a server: earliest cancellation date, whether
it is cancelled and the possible reasons.

hcli deliberately does not cancel servers (POST /server/{n}/cancellation):
it is irreversible once effective. Use the Robot web interface for that.`,
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
			cn, err := cl.GetCancellation(cmd.Context(), n)
			if err != nil {
				return err
			}
			reasonLabel := "Possible reasons"
			if cn.Cancelled {
				reasonLabel = "Reason"
			}
			return p.KeyValues(cn, [][2]string{
				{"Number", strconv.Itoa(cn.ServerNumber)},
				{"Name", cn.ServerName},
				{"IP", cn.ServerIP},
				{"Cancelled", yesNo(cn.Cancelled)},
				{"Cancellation date", deref(cn.CancellationDate)},
				{"Earliest cancellation", cn.EarliestCancellationDate},
				{"Reservation possible", yesNo(cn.ReservationPossible)},
				{"Location reserved", yesNo(cn.Reserved)},
				{reasonLabel, strings.Join(cn.CancellationReason, "; ")},
			})
		},
	}
}
