package robot

import (
	"fmt"
	"strconv"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

// Boot configuration passwords: the API returns the generated root password
// in the answer of POST /boot/{n}/rescue|linux (and while active, on GET).
// hcli never writes it to stdout (which may be piped, logged or used with
// -o json): it is removed from the printed object and written once to stderr.

const redacted = "(hidden)"

func takePassword(p **string) string {
	pw := deref(*p)
	if pw != "" {
		r := redacted
		*p = &r
	}
	return pw
}

func passwordLabel(p *string) string {
	if p == nil || *p == "" {
		return ""
	}
	return *p
}

func newRescueCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rescue",
		Short: "Rescue system boot configuration",
	}
	cmd.AddCommand(newRescueStatusCommand(c, false), newRescueStatusCommand(c, true), newRescueEnableCommand(c), newRescueDisableCommand(c))
	return cmd
}

func rescuePairs(r *client.Rescue, osLabel string) [][2]string {
	return [][2]string{
		{"Number", strconv.Itoa(r.ServerNumber)},
		{"IP", r.ServerIP},
		{"Active", yesNo(r.Active)},
		{osLabel, join(r.OS)},
		{"Password", passwordLabel(r.Password)},
		{"Authorized keys", keyFingerprints(r.AuthorizedKey)},
		{"Host keys", keyFingerprints(r.HostKey)},
	}
}

func newRescueStatusCommand(c *cli.CLI, last bool) *cobra.Command {
	var showPassword bool
	use, short := "status <server>", "Show the rescue system status and available OSes"
	if last {
		use, short = "last <server>", "Show the data of the last rescue activation"
	}
	cmd := &cobra.Command{
		Use:               use,
		Short:             short,
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
			var r *client.Rescue
			if last {
				r, err = cl.GetLastRescue(cmd.Context(), n)
			} else {
				r, err = cl.GetRescue(cmd.Context(), n)
			}
			if err != nil {
				return err
			}
			if pw := takePassword(&r.Password); pw != "" && showPassword {
				fmt.Fprintf(c.Err, "Rescue root password: %s\n", pw)
			}
			label := "Available OS"
			if r.Active || last {
				label = "OS"
			}
			return p.KeyValues(r, rescuePairs(r, label))
		},
	}
	cmd.Flags().BoolVar(&showPassword, "show-password", false, "print the current root password to stderr")
	return cmd
}

func newRescueEnableCommand(c *cli.CLI) *cobra.Command {
	var (
		osName, keyboard string
		keys             []string
	)
	cmd := &cobra.Command{
		Use:     "enable <server>",
		Aliases: []string{"activate"},
		Short:   "Activate the rescue system for the next boot",
		Long: `Activate the rescue system. It boots on the next reboot (see "hcli robot reset
execute"). The generated root password, if any, is printed once on stderr;
pass --ssh-key to log in with keys instead.`,
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
			r, err := cl.EnableRescue(cmd.Context(), n, osName, keyboard, keys)
			if err != nil {
				return err
			}
			if pw := takePassword(&r.Password); pw != "" {
				fmt.Fprintf(c.Err, "Rescue root password (shown only once): %s\n", pw)
			}
			if p.Format != output.FormatTable {
				return p.Print(r, nil)
			}
			fmt.Fprintf(c.Out, "Rescue system %q activated for server %d; reboot it to enter rescue.\n", join(r.OS), n)
			return nil
		},
	}
	cmd.Flags().StringVar(&osName, "os", "linux", "rescue operating system (see \"rescue status\")")
	cmd.Flags().StringVar(&keyboard, "keyboard", "", "keyboard layout (default us)")
	cmd.Flags().StringArrayVar(&keys, "ssh-key", nil, "SSH key fingerprint to authorize (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("ssh-key", flagCompletion(c, completeKeys))
	_ = cmd.RegisterFlagCompletionFunc("os", cobra.FixedCompletions([]string{"linux", "vkvm"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newRescueDisableCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "disable <server>",
		Aliases:           []string{"deactivate"},
		Short:             "Deactivate the rescue system",
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
			r, err := cl.DisableRescue(cmd.Context(), n)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(r, nil)
			}
			fmt.Fprintf(c.Out, "Rescue system deactivated for server %d\n", n)
			return nil
		},
	}
}

func newLinuxCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "linux",
		Short: "Linux installation boot configuration (automatic OS install on next boot)",
	}
	cmd.AddCommand(newLinuxStatusCommand(c), newLinuxEnableCommand(c), newLinuxDisableCommand(c))
	return cmd
}

func linuxPairs(l *client.Linux) [][2]string {
	return [][2]string{
		{"Number", strconv.Itoa(l.ServerNumber)},
		{"IP", l.ServerIP},
		{"Active", yesNo(l.Active)},
		{"Distributions", join(l.Dist)},
		{"Languages", join(l.Lang)},
		{"Password", passwordLabel(l.Password)},
		{"Authorized keys", keyFingerprints(l.AuthorizedKey)},
		{"Host keys", keyFingerprints(l.HostKey)},
	}
}

func newLinuxStatusCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "status <server>",
		Short:             "Show the Linux installation status and available distributions",
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
			l, err := cl.GetLinux(cmd.Context(), n)
			if err != nil {
				return err
			}
			takePassword(&l.Password)
			return p.KeyValues(l, linuxPairs(l))
		},
	}
}

func newLinuxEnableCommand(c *cli.CLI) *cobra.Command {
	var (
		dist, lang string
		keys       []string
	)
	cmd := &cobra.Command{
		Use:     "enable <server> --dist <distribution>",
		Aliases: []string{"activate"},
		Short:   "Activate an automatic Linux installation on next boot (asks for confirmation)",
		Long: `Activate the Linux installation: on the next reboot the server installs the
chosen distribution, ERASING ITS DISKS. The generated root password, if any,
is printed once on stderr.`,
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
			if err := c.Confirm(fmt.Sprintf("schedule a %q installation (wipes the disks on next boot) on server %d (%s)", dist, n, args[0])); err != nil {
				return err
			}
			l, err := cl.EnableLinux(cmd.Context(), n, dist, lang, keys)
			if err != nil {
				return err
			}
			if pw := takePassword(&l.Password); pw != "" {
				fmt.Fprintf(c.Err, "Installation root password (shown only once): %s\n", pw)
			}
			if p.Format != output.FormatTable {
				return p.Print(l, nil)
			}
			fmt.Fprintf(c.Out, "Linux installation %q activated for server %d; reboot it to start.\n", join(l.Dist), n)
			return nil
		},
	}
	cmd.Flags().StringVar(&dist, "dist", "", "distribution, exactly as listed by \"linux status\"")
	cmd.Flags().StringVar(&lang, "lang", "en", "language")
	cmd.Flags().StringArrayVar(&keys, "ssh-key", nil, "SSH key fingerprint to authorize (repeatable)")
	_ = cmd.MarkFlagRequired("dist")
	_ = cmd.RegisterFlagCompletionFunc("ssh-key", flagCompletion(c, completeKeys))
	return cmd
}

func newLinuxDisableCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:               "disable <server>",
		Aliases:           []string{"deactivate"},
		Short:             "Deactivate a pending Linux installation",
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
			l, err := cl.DisableLinux(cmd.Context(), n)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(l, nil)
			}
			fmt.Fprintf(c.Out, "Linux installation deactivated for server %d\n", n)
			return nil
		},
	}
}
