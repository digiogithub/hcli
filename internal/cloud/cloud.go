// Package cloud implements `hcli cloud ...` on top of hcloud-go.
//
// The command layout mirrors the official hcloud CLI: one sub-command per
// resource (singular noun) with list/describe/create/delete and the resource
// actions. Objects are referenced by name or ID; positional arguments and
// resource flags complete dynamically from the API with the active context.
package cloud

import (
	"github.com/digiogithub/hcli/internal/cli"
	"github.com/spf13/cobra"
)

// NewCommand returns the `cloud` command tree.
func NewCommand(c *cli.CLI) *cobra.Command {
	a := &app{c: c}
	cmd := &cobra.Command{
		Use:     "cloud",
		Aliases: []string{"c"},
		Short:   "Hetzner Cloud API (servers, volumes, networks, firewalls, ...)",
		Long: `Manage Hetzner Cloud resources through api.hetzner.cloud.

The token comes from the active context (hcli context create <name> --cloud)
or from HCLOUD_TOKEN. Resources are referenced by name or ID.`,
	}
	cmd.AddGroup(
		&cobra.Group{ID: "compute", Title: "Compute:"},
		&cobra.Group{ID: "network", Title: "Networking:"},
		&cobra.Group{ID: "storage", Title: "Storage and images:"},
		&cobra.Group{ID: "catalog", Title: "Catalog:"},
	)
	add := func(groupID string, cmds ...*cobra.Command) {
		for _, sub := range cmds {
			sub.GroupID = groupID
			cmd.AddCommand(sub)
		}
	}
	add("compute",
		newServerCommand(a),
		newSSHKeyCommand(a),
		newPlacementGroupCommand(a),
	)
	add("network",
		newNetworkCommand(a),
		newFirewallCommand(a),
		newFloatingIPCommand(a),
		newPrimaryIPCommand(a),
		newLoadBalancerCommand(a),
		newCertificateCommand(a),
	)
	add("storage",
		newVolumeCommand(a),
		newImageCommand(a),
		newISOCommand(a),
	)
	add("catalog",
		newServerTypeCommand(a),
		newLocationCommand(a),
		newPricingCommand(a),
	)
	return cmd
}
