package cloud

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/spf13/cobra"
)

var serverRes = resource[hcloud.Server]{
	Kind: "server",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Server, *hcloud.Response, error) {
		return cl.Server.Get(ctx, s)
	},
	Names:  serverNames,
	Ident:  func(s *hcloud.Server) (int64, string) { return s.ID, s.Name },
	Schema: func(s *hcloud.Server) any { return hcloud.SchemaFromServer(s) },
}

func newServerCommand(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "server",
		Aliases: []string{"servers", "srv"},
		Short:   "Manage servers",
	}
	cmd.AddCommand(
		newListCmd(a, serverRes, listSpec[hcloud.Server]{
			Selector: true,
			Headers:  []string{"id", "name", "status", "ipv4", "type", "location", "age"},
			All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Server, error) {
				return cl.Server.AllWithOpts(ctx, hcloud.ServerListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}, Sort: []string{"id:asc"}})
			},
			Row: func(s *hcloud.Server) []any {
				return []any{s.ID, s.Name, s.Status, ipString(s.PublicNet.IPv4.IP), serverTypeName(s.ServerType), locationName(s.Location), age(s.Created)}
			},
		}),
		newDescribeCmd(a, serverRes, describeServer),
		newServerCreateCmd(a),
		newDeleteCmd(a, serverRes, func(ctx context.Context, cl *hcloud.Client, s *hcloud.Server) ([]*hcloud.Action, error) {
			res, _, err := cl.Server.DeleteWithResult(ctx, s)
			if err != nil {
				return nil, err
			}
			return []*hcloud.Action{res.Action}, nil
		}),
		serverAction(a, "poweron", "Power on a server", "started", false, (func(cl *hcloud.Client) serverActionFn { return cl.Server.Poweron })),
		serverAction(a, "poweroff", "Power off a server (hard, like pulling the plug)", "stopped", true, (func(cl *hcloud.Client) serverActionFn { return cl.Server.Poweroff })),
		serverAction(a, "reboot", "Reboot a server (soft, via ACPI)", "rebooted", false, (func(cl *hcloud.Client) serverActionFn { return cl.Server.Reboot })),
		serverAction(a, "reset", "Reset a server (hard reboot)", "reset", true, (func(cl *hcloud.Client) serverActionFn { return cl.Server.Reset })),
		serverAction(a, "shutdown", "Shut down a server gracefully (via ACPI)", "shut down", false, (func(cl *hcloud.Client) serverActionFn { return cl.Server.Shutdown })),
		serverAction(a, "disable-rescue", "Disable rescue mode", "rescue disabled", false, (func(cl *hcloud.Client) serverActionFn { return cl.Server.DisableRescue })),
		newServerRebuildCmd(a),
		newServerChangeTypeCmd(a),
		newServerEnableRescueCmd(a),
		newServerCreateImageCmd(a),
		newServerSetRDNSCmd(a),
		newServerIPCmd(a),
		newServerSSHCmd(a),
	)
	cmd.AddCommand(newLabelCmds(a, serverRes,
		func(s *hcloud.Server) map[string]string { return s.Labels },
		func(ctx context.Context, cl *hcloud.Client, s *hcloud.Server, l map[string]string) error {
			_, _, err := cl.Server.Update(ctx, s, hcloud.ServerUpdateOpts{Labels: l})
			return err
		})...)
	return cmd
}

func serverTypeName(t *hcloud.ServerType) string {
	if t == nil {
		return ""
	}
	return t.Name
}

func describeServer(s *hcloud.Server) [][2]string {
	kv := [][2]string{
		{"ID", strconv.FormatInt(s.ID, 10)},
		{"Name", s.Name},
		{"Status", string(s.Status)},
		{"Created", created(s.Created)},
	}
	if t := s.ServerType; t != nil {
		kv = append(kv, [2]string{"Server type", fmt.Sprintf("%s (%d vCPU %s, %.0f GB RAM, %d GB disk, %s)", t.Name, t.Cores, t.CPUType, t.Memory, t.Disk, t.Architecture)})
	}
	if l := s.Location; l != nil {
		kv = append(kv, [2]string{"Location", fmt.Sprintf("%s (%s, %s)", l.Name, l.City, l.NetworkZone)})
	}
	if img := s.Image; img != nil {
		name := img.Name
		if name == "" {
			name = img.Description
		}
		kv = append(kv, [2]string{"Image", fmt.Sprintf("%s (%d, %s)", name, img.ID, img.Type)})
	}
	if s.PrimaryDiskSize > 0 {
		kv = append(kv, [2]string{"Disk", fmt.Sprintf("%d GB", s.PrimaryDiskSize)})
	}
	kv = append(kv,
		[2]string{"Public IPv4", joinNonEmpty(ipString(s.PublicNet.IPv4.IP), s.PublicNet.IPv4.DNSPtr)},
		[2]string{"Public IPv6", netString(s.PublicNet.IPv6.Network)},
	)
	for _, fip := range s.PublicNet.FloatingIPs {
		kv = append(kv, [2]string{"Floating IP", strconv.FormatInt(fip.ID, 10)})
	}
	for _, pn := range s.PrivateNet {
		net := ""
		if pn.Network != nil {
			net = strconv.FormatInt(pn.Network.ID, 10)
			if pn.Network.Name != "" {
				net = pn.Network.Name
			}
		}
		kv = append(kv, [2]string{"Private network", fmt.Sprintf("%s: %s", net, ipString(pn.IP))})
	}
	for _, fw := range s.PublicNet.Firewalls {
		kv = append(kv, [2]string{"Firewall", fmt.Sprintf("%d (%s)", fw.Firewall.ID, fw.Status)})
	}
	for _, v := range s.Volumes {
		kv = append(kv, [2]string{"Volume", strconv.FormatInt(v.ID, 10)})
	}
	if s.PlacementGroup != nil {
		kv = append(kv, [2]string{"Placement group", s.PlacementGroup.Name})
	}
	if s.ISO != nil {
		kv = append(kv, [2]string{"ISO", s.ISO.Name})
	}
	kv = append(kv,
		[2]string{"Rescue enabled", yesNo(s.RescueEnabled)},
		[2]string{"Locked", yesNo(s.Locked)},
		[2]string{"Backup window", s.BackupWindow},
		[2]string{"Protection", fmt.Sprintf("delete=%s rebuild=%s", yesNo(s.Protection.Delete), yesNo(s.Protection.Rebuild))},
		[2]string{"Traffic", fmt.Sprintf("out %s / in %s / included %s", bytesHuman(s.OutgoingTraffic), bytesHuman(s.IngoingTraffic), bytesHuman(s.IncludedTraffic))},
		[2]string{"Labels", formatLabels(s.Labels)},
	)
	return kv
}

func joinNonEmpty(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

type serverActionFn func(context.Context, *hcloud.Server) (*hcloud.Action, *hcloud.Response, error)

// serverAction builds a simple "<verb> <server>" command backed by one action.
func serverAction(a *app, use, short, past string, confirm bool, pick func(*hcloud.Client) serverActionFn) *cobra.Command {
	return &cobra.Command{
		Use:               use + " <server>",
		Short:             short,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			if confirm {
				if err := a.c.Confirm(fmt.Sprintf("%s server %s", use, s.Name)); err != nil {
					return err
				}
			}
			action, _, err := pick(cl)(ctx, s)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("%s on server %s", use, s.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Server %s %s", s.Name, past)
		}),
	}
}

type serverCreateFlags struct {
	name, serverType, image, location, datacenter string
	sshKeys, networks, firewalls, volumes         []string
	labels                                        []string
	userDataFile, placementGroup                  string
	primaryIPv4, primaryIPv6                      string
	withoutIPv4, withoutIPv6, noStart, automount  bool
}

func newServerCreateCmd(a *app) *cobra.Command {
	var f serverCreateFlags
	cmd := &cobra.Command{
		Use:   "create --name <name> --type <type> --image <image> [flags]",
		Short: "Create a server (billable)",
		Args:  cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			opts, err := a.serverCreateOpts(ctx, cl, &f)
			if err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("create server %s (%s, %s, %s) — this is billable", f.name, f.serverType, f.image, locationName(opts.Location))); err != nil {
				return err
			}
			res, _, err := cl.Server.Create(ctx, opts)
			if err != nil {
				return err
			}
			actions := append([]*hcloud.Action{res.Action}, res.NextActions...)
			if err := a.wait(ctx, cl, fmt.Sprintf("server %s to be created", f.name), actions...); err != nil {
				return err
			}
			srv := res.Server
			if fresh, _, err := cl.Server.GetByID(ctx, srv.ID); err == nil && fresh != nil {
				srv = fresh
			}
			if p.Format != output.FormatTable {
				return p.Print(struct {
					Server       schema.Server `json:"server"`
					RootPassword string        `json:"root_password,omitempty"`
				}{hcloud.SchemaFromServer(srv), res.RootPassword}, nil)
			}
			fmt.Fprintf(p.Out, "Server %d created\n", srv.ID)
			if ip := ipString(srv.PublicNet.IPv4.IP); ip != "" {
				fmt.Fprintf(p.Out, "IPv4: %s\n", ip)
			}
			if n := netString(srv.PublicNet.IPv6.Network); n != "" {
				fmt.Fprintf(p.Out, "IPv6: %s\n", n)
			}
			if res.RootPassword != "" {
				fmt.Fprintf(p.Out, "Root password: %s\n", res.RootPassword)
			}
			return nil
		}),
	}
	fl := cmd.Flags()
	fl.StringVar(&f.name, "name", "", "server name (required)")
	fl.StringVar(&f.serverType, "type", "", "server type name or ID (required)")
	fl.StringVar(&f.image, "image", "", "image name or ID (required)")
	fl.StringVar(&f.location, "location", "", "location name or ID")
	fl.StringVar(&f.datacenter, "datacenter", "", "datacenter name or ID (its location is used; deprecated upstream, prefer --location)")
	fl.StringArrayVar(&f.sshKeys, "ssh-key", nil, "SSH key name or ID (repeatable)")
	fl.StringArrayVar(&f.networks, "network", nil, "network to attach (repeatable)")
	fl.StringArrayVar(&f.firewalls, "firewall", nil, "firewall to apply (repeatable)")
	fl.StringArrayVar(&f.volumes, "volume", nil, "volume to attach (repeatable)")
	fl.BoolVar(&f.automount, "automount", false, "automount attached volumes")
	addLabelFlag(cmd, &f.labels)
	fl.StringVar(&f.userDataFile, "user-data-from-file", "", "cloud-init user data file (\"-\" for stdin)")
	fl.StringVar(&f.placementGroup, "placement-group", "", "placement group name or ID")
	fl.StringVar(&f.primaryIPv4, "primary-ipv4", "", "existing primary IPv4 to use")
	fl.StringVar(&f.primaryIPv6, "primary-ipv6", "", "existing primary IPv6 to use")
	fl.BoolVar(&f.withoutIPv4, "without-ipv4", false, "create without public IPv4")
	fl.BoolVar(&f.withoutIPv6, "without-ipv6", false, "create without public IPv6")
	fl.BoolVar(&f.noStart, "no-start", false, "do not start the server after creation")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("image")
	cmd.MarkFlagsMutuallyExclusive("location", "datacenter")
	_ = cmd.MarkFlagFilename("user-data-from-file")
	a.flagCompletion(cmd, "type", serverTypeNames)
	a.flagCompletion(cmd, "image", imageNames)
	a.flagCompletion(cmd, "location", locationNames)
	a.flagCompletion(cmd, "datacenter", datacenterNames)
	a.flagCompletion(cmd, "ssh-key", sshKeyNames)
	a.flagCompletion(cmd, "network", networkNames)
	a.flagCompletion(cmd, "firewall", firewallNames)
	a.flagCompletion(cmd, "volume", volumeNames)
	a.flagCompletion(cmd, "placement-group", placementGroupNames)
	a.flagCompletion(cmd, "primary-ipv4", primaryIPNames)
	a.flagCompletion(cmd, "primary-ipv6", primaryIPNames)
	return cmd
}

func (a *app) serverCreateOpts(ctx context.Context, cl *hcloud.Client, f *serverCreateFlags) (hcloud.ServerCreateOpts, error) {
	opts := hcloud.ServerCreateOpts{Name: f.name}
	st, err := serverTypeRes.fetch(ctx, cl, f.serverType)
	if err != nil {
		return opts, err
	}
	opts.ServerType = st
	img, _, err := cl.Image.GetForArchitecture(ctx, f.image, st.Architecture)
	if err != nil {
		return opts, err
	}
	if img == nil {
		return opts, fmt.Errorf("image not found for architecture %s: %s", st.Architecture, f.image)
	}
	opts.Image = img
	switch {
	case f.location != "":
		if opts.Location, err = locationRes.fetch(ctx, cl, f.location); err != nil {
			return opts, err
		}
	case f.datacenter != "":
		dc, err := datacenterRes.fetch(ctx, cl, f.datacenter)
		if err != nil {
			return opts, err
		}
		opts.Location = dc.Location
	}
	for _, k := range f.sshKeys {
		key, err := sshKeyRes.fetch(ctx, cl, k)
		if err != nil {
			return opts, err
		}
		opts.SSHKeys = append(opts.SSHKeys, key)
	}
	for _, n := range f.networks {
		nw, err := networkRes.fetch(ctx, cl, n)
		if err != nil {
			return opts, err
		}
		opts.Networks = append(opts.Networks, nw)
	}
	for _, n := range f.firewalls {
		fw, err := firewallRes.fetch(ctx, cl, n)
		if err != nil {
			return opts, err
		}
		opts.Firewalls = append(opts.Firewalls, &hcloud.ServerCreateFirewall{Firewall: *fw})
	}
	for _, n := range f.volumes {
		v, err := volumeRes.fetch(ctx, cl, n)
		if err != nil {
			return opts, err
		}
		opts.Volumes = append(opts.Volumes, v)
	}
	if len(f.volumes) > 0 {
		opts.Automount = new(f.automount)
	}
	if f.placementGroup != "" {
		if opts.PlacementGroup, err = placementGroupRes.fetch(ctx, cl, f.placementGroup); err != nil {
			return opts, err
		}
	}
	if opts.Labels, err = parseLabels(f.labels); err != nil {
		return opts, err
	}
	if f.userDataFile != "" {
		data, err := a.readFileOrStdin(f.userDataFile)
		if err != nil {
			return opts, fmt.Errorf("read user data: %w", err)
		}
		opts.UserData = string(data)
	}
	if f.noStart {
		opts.StartAfterCreate = new(false)
	}
	if f.withoutIPv4 || f.withoutIPv6 || f.primaryIPv4 != "" || f.primaryIPv6 != "" {
		pn := &hcloud.ServerCreatePublicNet{EnableIPv4: !f.withoutIPv4, EnableIPv6: !f.withoutIPv6}
		if f.primaryIPv4 != "" {
			if pn.IPv4, err = primaryIPRes.fetch(ctx, cl, f.primaryIPv4); err != nil {
				return opts, err
			}
		}
		if f.primaryIPv6 != "" {
			if pn.IPv6, err = primaryIPRes.fetch(ctx, cl, f.primaryIPv6); err != nil {
				return opts, err
			}
		}
		opts.PublicNet = pn
	}
	return opts, nil
}

func newServerRebuildCmd(a *app) *cobra.Command {
	var image string
	cmd := &cobra.Command{
		Use:               "rebuild <server> --image <image>",
		Short:             "Rebuild a server from an image (wipes the disk)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			arch := hcloud.ArchitectureX86
			if s.ServerType != nil && s.ServerType.Architecture != "" {
				arch = s.ServerType.Architecture
			}
			img, _, err := cl.Image.GetForArchitecture(ctx, image, arch)
			if err != nil {
				return err
			}
			if img == nil {
				return fmt.Errorf("image not found for architecture %s: %s", arch, image)
			}
			if err := a.c.Confirm(fmt.Sprintf("rebuild server %s from image %s (all data on its disk is lost)", s.Name, image)); err != nil {
				return err
			}
			res, _, err := cl.Server.RebuildWithResult(ctx, s, hcloud.ServerRebuildOpts{Image: img})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("server %s to be rebuilt", s.Name), res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(struct {
					Action       schema.Action `json:"action"`
					RootPassword string        `json:"root_password,omitempty"`
				}{hcloud.SchemaFromAction(res.Action), res.RootPassword}, nil)
			}
			fmt.Fprintf(p.Out, "Server %s rebuilt with image %s\n", s.Name, image)
			if res.RootPassword != "" {
				fmt.Fprintf(p.Out, "Root password: %s\n", res.RootPassword)
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&image, "image", "", "image name or ID (required)")
	_ = cmd.MarkFlagRequired("image")
	a.flagCompletion(cmd, "image", imageNames)
	return cmd
}

func newServerChangeTypeCmd(a *app) *cobra.Command {
	var typ string
	var keepDisk bool
	cmd := &cobra.Command{
		Use:               "change-type <server> --type <type>",
		Short:             "Change the server type (the server must be off; may change billing)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			st, err := serverTypeRes.fetch(ctx, cl, typ)
			if err != nil {
				return err
			}
			if err := a.c.Confirm(fmt.Sprintf("change type of server %s from %s to %s", s.Name, serverTypeName(s.ServerType), st.Name)); err != nil {
				return err
			}
			action, _, err := cl.Server.ChangeType(ctx, s, hcloud.ServerChangeTypeOpts{ServerType: st, UpgradeDisk: !keepDisk})
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("server %s to change type", s.Name), action); err != nil {
				return err
			}
			return a.done(p, action, "Server %s changed to type %s", s.Name, st.Name)
		}),
	}
	cmd.Flags().StringVar(&typ, "type", "", "new server type (required)")
	cmd.Flags().BoolVar(&keepDisk, "keep-disk", false, "keep the current disk size (allows downgrading later)")
	_ = cmd.MarkFlagRequired("type")
	a.flagCompletion(cmd, "type", serverTypeNames)
	return cmd
}

func newServerEnableRescueCmd(a *app) *cobra.Command {
	var typ string
	var keys []string
	cmd := &cobra.Command{
		Use:               "enable-rescue <server>",
		Short:             "Enable rescue mode for the next boot",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			opts := hcloud.ServerEnableRescueOpts{Type: hcloud.ServerRescueType(typ)}
			for _, k := range keys {
				key, err := sshKeyRes.fetch(ctx, cl, k)
				if err != nil {
					return err
				}
				opts.SSHKeys = append(opts.SSHKeys, key)
			}
			res, _, err := cl.Server.EnableRescue(ctx, s, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("rescue mode on server %s", s.Name), res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(struct {
					Action       schema.Action `json:"action"`
					RootPassword string        `json:"root_password,omitempty"`
				}{hcloud.SchemaFromAction(res.Action), res.RootPassword}, nil)
			}
			fmt.Fprintf(p.Out, "Rescue enabled for server %s (reboot or reset it to boot into rescue)\n", s.Name)
			if res.RootPassword != "" {
				fmt.Fprintf(p.Out, "Root password: %s\n", res.RootPassword)
			}
			return nil
		}),
	}
	cmd.Flags().StringVar(&typ, "type", string(hcloud.ServerRescueTypeLinux64), "rescue type")
	cmd.Flags().StringArrayVar(&keys, "ssh-key", nil, "SSH key to inject (repeatable)")
	fixedCompletion(cmd, "type", string(hcloud.ServerRescueTypeLinux64))
	a.flagCompletion(cmd, "ssh-key", sshKeyNames)
	return cmd
}

func newServerCreateImageCmd(a *app) *cobra.Command {
	var typ, description string
	var labels []string
	cmd := &cobra.Command{
		Use:               "create-image <server>",
		Short:             "Create a snapshot or backup image of a server (billable)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			l, err := parseLabels(labels)
			if err != nil {
				return err
			}
			opts := &hcloud.ServerCreateImageOpts{Type: hcloud.ImageType(typ), Labels: l}
			if description != "" {
				opts.Description = &description
			}
			if err := a.c.Confirm(fmt.Sprintf("create a %s image of server %s (billable)", typ, s.Name)); err != nil {
				return err
			}
			res, _, err := cl.Server.CreateImage(ctx, s, opts)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("image of server %s", s.Name), res.Action); err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromImage(res.Image), nil)
			}
			fmt.Fprintf(p.Out, "Image %d created from server %s\n", res.Image.ID, s.Name)
			return nil
		}),
	}
	cmd.Flags().StringVar(&typ, "type", string(hcloud.ImageTypeSnapshot), "image type: snapshot or backup")
	cmd.Flags().StringVar(&description, "description", "", "image description")
	addLabelFlag(cmd, &labels)
	fixedCompletion(cmd, "type", string(hcloud.ImageTypeSnapshot), string(hcloud.ImageTypeBackup))
	return cmd
}

func newServerSetRDNSCmd(a *app) *cobra.Command {
	var ip, hostname string
	var reset bool
	cmd := &cobra.Command{
		Use:               "set-rdns <server> --hostname <ptr> [--ip <ip>]",
		Short:             "Set the reverse DNS entry of a server IP",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			if hostname == "" && !reset {
				return errors.New("--hostname or --reset is required")
			}
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			if ip == "" {
				ip = ipString(s.PublicNet.IPv4.IP)
				if ip == "" {
					return errors.New("server has no public IPv4: pass --ip")
				}
			}
			var ptr *string
			if !reset {
				ptr = &hostname
			}
			action, _, err := cl.Server.ChangeDNSPtr(ctx, s, ip, ptr)
			if err != nil {
				return err
			}
			if err := a.wait(ctx, cl, fmt.Sprintf("reverse DNS of %s", ip), action); err != nil {
				return err
			}
			if reset {
				return a.done(p, action, "Reverse DNS of %s reset", ip)
			}
			return a.done(p, action, "Reverse DNS of %s set to %s", ip, hostname)
		}),
	}
	cmd.Flags().StringVar(&ip, "ip", "", "IP address (default: primary IPv4)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "PTR hostname")
	cmd.Flags().BoolVar(&reset, "reset", false, "reset to the default PTR")
	return cmd
}

func newServerIPCmd(a *app) *cobra.Command {
	var v6 bool
	cmd := &cobra.Command{
		Use:               "ip <server>",
		Short:             "Print the public IPv4 (or IPv6 with -6) of a server",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			ip := serverIP(s, v6)
			if ip == "" {
				return fmt.Errorf("server %s has no public IPv%s", s.Name, map[bool]string{false: "4", true: "6"}[v6])
			}
			fmt.Fprintln(p.Out, ip)
			return nil
		}),
	}
	cmd.Flags().BoolVarP(&v6, "ipv6", "6", false, "print the IPv6 address (<network>1)")
	return cmd
}

// serverIP returns the public IPv4, or the first address of the IPv6 /64.
func serverIP(s *hcloud.Server, v6 bool) string {
	if !v6 {
		return ipString(s.PublicNet.IPv4.IP)
	}
	n := s.PublicNet.IPv6.Network
	if n == nil {
		if ip := ipString(s.PublicNet.IPv6.IP); ip != "" {
			n = &net.IPNet{IP: s.PublicNet.IPv6.IP}
		} else {
			return ""
		}
	}
	ip := make(net.IP, len(n.IP.To16()))
	copy(ip, n.IP.To16())
	ip[len(ip)-1] |= 1
	return ip.String()
}

func newServerSSHCmd(a *app) *cobra.Command {
	var user string
	var port int
	var v6 bool
	cmd := &cobra.Command{
		Use:   "ssh <server> [ssh options] [command...]",
		Short: "SSH into a server (runs the local ssh binary)",
		Long: `Run the local ssh client against the server's public IP.

Flags for hcli go before the server name; everything after it is passed to
ssh unchanged, e.g.:

  hcli cloud server ssh web1 -L 8080:localhost:80 uptime`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.complete(serverNames),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, _ *output.Printer, args []string) error {
			s, err := serverRes.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			ip := serverIP(s, v6)
			if ip == "" {
				return fmt.Errorf("server %s has no public IP", s.Name)
			}
			bin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh client not found in PATH: %w", err)
			}
			sshArgs := append([]string{"-l", user, "-p", strconv.Itoa(port), ip}, args[1:]...)
			a.c.Debugf("exec %s %s", bin, strings.Join(sshArgs, " "))
			sh := exec.CommandContext(ctx, bin, sshArgs...) //nolint:gosec // running the user's ssh with their arguments is the point
			sh.Stdin, sh.Stdout, sh.Stderr = a.c.In, a.c.Out, a.c.Err
			if err := sh.Run(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					return &cli.ExitError{Code: exitErr.ExitCode()}
				}
				return err
			}
			return nil
		}),
	}
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().StringVarP(&user, "user", "u", "root", "remote user")
	cmd.Flags().IntVarP(&port, "port", "p", 22, "SSH port")
	cmd.Flags().BoolVarP(&v6, "ipv6", "6", false, "connect over IPv6")
	return cmd
}
