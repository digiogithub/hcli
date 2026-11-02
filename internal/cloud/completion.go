package cloud

import (
	"context"
	"strconv"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

// nameLister returns candidate names for completion.
type nameLister func(ctx context.Context, cl *hcloud.Client) ([]string, error)

// candidates calls list with the active credentials and filters by prefix.
// Any failure (no token, network, API error) yields no completions.
func (a *app) candidates(cmd *cobra.Command, list nameLister, toComplete string) []string {
	if list == nil {
		return nil
	}
	cl, err := a.Client()
	if err != nil {
		return nil
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, completionTimeout)
	defer cancel()
	names, err := list(ctx, cl)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] || !strings.HasPrefix(n, toComplete) {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// complete completes positional arguments: the i-th lister for the i-th arg.
func (a *app) complete(lists ...nameLister) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= len(lists) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return a.candidates(cmd, lists[len(args)], toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeRepeat completes every positional argument with the same lister,
// skipping values already given.
func (a *app) completeRepeat(list nameLister) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		used := map[string]bool{}
		for _, arg := range args {
			used[arg] = true
		}
		var out []string
		for _, n := range a.candidates(cmd, list, toComplete) {
			if !used[n] {
				out = append(out, n)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// flagCompletion registers dynamic completion for a flag.
func (a *app) flagCompletion(cmd *cobra.Command, flag string, list nameLister) {
	_ = cmd.RegisterFlagCompletionFunc(flag, func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return a.candidates(cmd, list, toComplete), cobra.ShellCompDirectiveNoFileComp
	})
}

// fixedCompletion registers static completion for a flag.
func fixedCompletion(cmd *cobra.Command, flag string, values ...string) {
	_ = cmd.RegisterFlagCompletionFunc(flag, cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp))
}

// Name listers used by positional and flag completion.

func serverNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Server.All(ctx)
	return namesOf(items, func(s *hcloud.Server) (int64, string) { return s.ID, s.Name }), err
}

func serverTypeNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.ServerType.All(ctx)
	return namesOf(items, func(s *hcloud.ServerType) (int64, string) { return s.ID, s.Name }), err
}

// imageNames offers system/app image names plus snapshot and backup IDs.
func imageNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Image.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(items))
	for _, img := range items {
		if img.Name != "" {
			out = append(out, img.Name)
		} else {
			out = append(out, strconv.FormatInt(img.ID, 10))
		}
	}
	return out, nil
}

func locationNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Location.All(ctx)
	return namesOf(items, func(l *hcloud.Location) (int64, string) { return l.ID, l.Name }), err
}

//nolint:staticcheck // datacenters are deprecated upstream
func datacenterNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Datacenter.All(ctx) //nolint:staticcheck // deprecated upstream
	return namesOf(items, func(d *hcloud.Datacenter) (int64, string) { return d.ID, d.Name }), err
}

func isoNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.ISO.All(ctx)
	return namesOf(items, func(i *hcloud.ISO) (int64, string) { return i.ID, i.Name }), err
}

func sshKeyNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.SSHKey.All(ctx)
	return namesOf(items, func(k *hcloud.SSHKey) (int64, string) { return k.ID, k.Name }), err
}

func volumeNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Volume.All(ctx)
	return namesOf(items, func(v *hcloud.Volume) (int64, string) { return v.ID, v.Name }), err
}

func networkNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Network.All(ctx)
	return namesOf(items, func(n *hcloud.Network) (int64, string) { return n.ID, n.Name }), err
}

func firewallNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Firewall.All(ctx)
	return namesOf(items, func(f *hcloud.Firewall) (int64, string) { return f.ID, f.Name }), err
}

func floatingIPNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.FloatingIP.All(ctx)
	return namesOf(items, func(f *hcloud.FloatingIP) (int64, string) { return f.ID, f.Name }), err
}

func primaryIPNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.PrimaryIP.All(ctx)
	return namesOf(items, func(p *hcloud.PrimaryIP) (int64, string) { return p.ID, p.Name }), err
}

func loadBalancerNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.LoadBalancer.All(ctx)
	return namesOf(items, func(l *hcloud.LoadBalancer) (int64, string) { return l.ID, l.Name }), err
}

func loadBalancerTypeNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.LoadBalancerType.All(ctx)
	return namesOf(items, func(l *hcloud.LoadBalancerType) (int64, string) { return l.ID, l.Name }), err
}

func certificateNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.Certificate.All(ctx)
	return namesOf(items, func(c *hcloud.Certificate) (int64, string) { return c.ID, c.Name }), err
}

func placementGroupNames(ctx context.Context, cl *hcloud.Client) ([]string, error) {
	items, err := cl.PlacementGroup.All(ctx)
	return namesOf(items, func(p *hcloud.PlacementGroup) (int64, string) { return p.ID, p.Name }), err
}
