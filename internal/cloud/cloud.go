// Package cloud implements `hcli cloud ...` on top of hcloud-go.
package cloud

import (
	"github.com/digiogithub/hcli/internal/cli"
	"github.com/spf13/cobra"
)

// NewCommand returns the `cloud` command tree.
func NewCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cloud",
		Aliases: []string{"c"},
		Short:   "Hetzner Cloud API (servers, volumes, networks, firewalls, ...)",
	}
	_ = c
	return cmd
}
