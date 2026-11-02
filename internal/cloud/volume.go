package cloud

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

var volumeRes = resource[hcloud.Volume]{
	Kind: "volume",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Volume, *hcloud.Response, error) {
		return cl.Volume.Get(ctx, s)
	},
	Names:  volumeNames,
	Ident:  func(v *hcloud.Volume) (int64, string) { return v.ID, v.Name },
	Schema: func(v *hcloud.Volume) any { return hcloud.SchemaFromVolume(v) },
}

func volumeServer(v *hcloud.Volume) string {
	if v.Server == nil {
		return ""
	}
	if v.Server.Name != "" {
		return v.Server.Name
	}
	return strconv.FormatInt(v.Server.ID, 10)
}

func newVolumeCommand(a *app) *cobra.Command {
	cmd := group("volume", "Manage volumes", []string{"volumes", "vol"},
		newListCmd(a, volumeRes, listSpec[hcloud.Volume]{
			Selector: true,
			Headers:  []string{"id", "name", "size", "server", "location", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Volume, error) {
				vols, err := cl.Volume.AllWithOpts(ctx, hcloud.VolumeListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}, Sort: []string{"id:asc"}})
				if err != nil {
					return nil, err
				}
				fillVolumeServers(ctx, cl, vols)
				return vols, nil
			},
			Row: func(v *hcloud.Volume) []any {
				return []any{v.ID, v.Name, fmt.Sprintf("%d GB", v.Size), volumeServer(v), locationName(v.Location), age(v.Created)}
			},
		}),
		newDescribeCmd(a, volumeRes, func(v *hcloud.Volume) [][2]string {
			format := ""
			if v.Format != nil {
				format = *v.Format
			}
			return [][2]string{
				{"ID", strconv.FormatInt(v.ID, 10)},
				{"Name", v.Name},
				{"Status", string(v.Status)},
				{"Created", created(v.Created)},
				{"Size", fmt.Sprintf("%d GB", v.Size)},
				{"Linux device", v.LinuxDevice},
				{"Format", format},
				{"Location", locationName(v.Location)},
				{"Server", volumeServer(v)},
				{"Protection", "delete=" + yesNo(v.Protection.Delete)},
				{"Labels", formatLabels(v.Labels)},
			}
		}),
		newVolumeCreateCmd(a),
		newDeleteCmd(a, volumeRes, func(ctx context.Context, cl *hcloud.Client, v *hcloud.Volume) ([]*hcloud.Action, error) {
			_, err := cl.Volume.Delete(ctx, v)
			return nil, err
		}),
		newVolumeAttachCmd(a),
		newVolumeDetachCmd(a),
		newVolumeResizeCmd(a),
	)
	cmd.AddCommand(newLabelCmds(a, volumeRes,
		func(v *hcloud.Volume) map[string]string { return v.Labels },
		func(ctx context.Context, cl *hcloud.Client, v *hcloud.Volume, l map[string]string) error {
			_, _, err := cl.Volume.Update(ctx, v, hcloud.VolumeUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

// fillVolumeServers resolves server names for the list view (the API only
// returns the server ID).
func fillVolumeServers(ctx context.Context, cl *hcloud.Client, vols []*hcloud.Volume) {
	need := false
	for _, v := range vols {
		if v.Server != nil {
			need = true
			break
		}
	}
	if !need {
		return
	}
	servers, err := cl.Server.All(ctx)
	if err != nil {
		return
	}
	names := map[int64]string{}
	for _, s := range servers {
		names[s.ID] = s.Name
	}
	for _, v := range vols {
		if v.Server != nil && v.Server.Name == "" {
			v.Server.Name = names[v.Server.ID]
		}
	}
}

func newVolumeCreateCmd(a *app) *cobra.Command {
	var name, location, server, format string
	var size int
	var automount bool
	var labels []string
	cmd := &cobra.Command{
		Use:   "create --name <name> --size <GB> (--location <loc> | --server <server>)",
		Short: "Create a volume (billable)",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			if location == "" && server == "" {
				return errors.New("--location or --server is required")
			}
			opts := hcloud.VolumeCreateOpts{Name: name, Size: size}
			var err error
			if location != "" {
				if opts.Location, err = locationRes.fetch(ctx, cl, location); err != nil {
					return err
				}
			}
			if server != "" {
				if opts.Server, err = serverRes.fetch(ctx, cl, server); err != nil {
					return err
				}
				opts.Automount = new(automount)
			}
			if format != "" {
				opts.Format = &format
			}
			if opts.Labels, err = parseLabels(labels); err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("create volume %s (%d GB) — this is billable", name, size)); err != nil {
				return err
			}
			res, _, err := cl.Volume.Create(ctx, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("volume %s to be created", name), append([]*hcloud.Action{res.Action}, res.NextActions...)...); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromVolume(res.Volume), nil)
			}
			fmt.Fprintf(p.Out, "Volume %d created\n", res.Volume.ID)
			return nil
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&name, "name", "", "volume name (required)")
	fl.IntVar(&size, "size", 0, "size in GB (required, min 10)")
	fl.StringVar(&location, "location", "", "location")
	fl.StringVar(&server, "server", "", "server to attach to")
	fl.StringVar(&format, "format", "", "filesystem: ext4 or xfs")
	fl.BoolVar(&automount, "automount", false, "automount on the server (with --server)")
	addLabelFlag(cmd, &labels)
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("size")
	cmd.MarkFlagsMutuallyExclusive("location", "server")
	a.flagCompletion(cmd, "location", locationNames)
	a.flagCompletion(cmd, "server", serverNames)
	fixedCompletion(cmd, "format", "ext4", "xfs")
	return cmd
}

func newVolumeAttachCmd(a *app) *cobra.Command {
	var server string
	var automount bool
	cmd := &cobra.Command{
		Use:               "attach <volume> --server <server>",
		Short:             "Attach a volume to a server",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(volumeNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			v, err := volumeRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			s, err := serverRes.fetch(ctx, cl, server)
			if err != nil {
				return err
			}
			action, _, err := cl.Volume.AttachWithOpts(ctx, v, hcloud.VolumeAttachOpts{Server: s, Automount: new(automount)})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("volume %s to attach", v.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Volume %s attached to server %s", v.Name, s.Name)
		}),
	}
	cmd.Flags().StringVar(&server, "server", "", "server (required)")
	cmd.Flags().BoolVar(&automount, "automount", false, "automount the volume")
	_ = cmd.MarkFlagRequired("server")
	a.flagCompletion(cmd, "server", serverNames)
	return cmd
}

func newVolumeDetachCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "detach <volume>",
		Short:             "Detach a volume from its server",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(volumeNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			v, err := volumeRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			action, _, err := cl.Volume.Detach(ctx, v)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("volume %s to detach", v.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Volume %s detached", v.Name)
		}),
	}
}

func newVolumeResizeCmd(a *app) *cobra.Command {
	var size int
	cmd := &cobra.Command{
		Use:               "resize <volume> --size <GB>",
		Short:             "Grow a volume (billable; volumes cannot shrink)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(volumeNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			v, err := volumeRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("resize volume %s from %d GB to %d GB (billable)", v.Name, v.Size, size)); err != nil {
				return err
			}
			action, _, err := cl.Volume.Resize(ctx, v, size)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("volume %s to resize", v.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Volume %s resized to %d GB", v.Name, size)
		}),
	}
	cmd.Flags().IntVar(&size, "size", 0, "new size in GB (required)")
	_ = cmd.MarkFlagRequired("size")
	return cmd
}
