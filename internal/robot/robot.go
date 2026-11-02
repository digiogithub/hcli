// Package robot implements `hcli robot ...` on top of the Robot webservice.
package robot

import (
	"github.com/digiogithub/hcli/internal/cli"
	"github.com/spf13/cobra"
)

// NewCommand returns the `robot` command tree.
func NewCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "robot",
		Aliases: []string{"r"},
		Short:   "Hetzner Robot webservice (dedicated servers, IPs, firewall, vSwitch, ...)",
	}
	_ = c
	return cmd
}
