package cmd

import (
	"errors"
	"fmt"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/config"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/spf13/cobra"
)

func newContextCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "context",
		Aliases: []string{"ctx"},
		Short:   "Manage credential contexts (Cloud token, Robot webservice user)",
	}
	completeNames := func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return contextNames(c), cobra.ShellCompDirectiveNoFileComp
	}

	// create / update
	var withCloud, withRobot, activate bool
	var robotUser string
	create := &cobra.Command{
		Use:   "create <name>",
		Short: "Create or update a context (secrets are prompted, never passed as flags)",
		Long: `Create or update a context. Secrets are read from the terminal without echo,
or from stdin (one value per line) when piped.

  hcli context create digio --cloud            # asks for the Cloud API token
  hcli context create digio --robot --robot-user '#ws+abc'   # asks for the password
  hcli context create digio --cloud --robot   # both

Cloud tokens are per project: Cloud Console > project > Security > API tokens.
Robot users are per account: robot.hetzner.com > Settings > Webservice and app settings.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if !withCloud && !withRobot {
				return errors.New("pass --cloud and/or --robot")
			}
			cfg, err := c.Config()
			if err != nil {
				return err
			}
			ctx := cfg.Get(args[0])
			if ctx == nil {
				ctx = &config.Context{Name: args[0]}
			} else {
				copied := *ctx
				ctx = &copied
			}
			if withCloud {
				tok, err := c.ReadSecret("Hetzner Cloud API token: ")
				if err != nil {
					return err
				}
				if tok == "" {
					return errors.New("empty token")
				}
				ctx.CloudToken = tok
			}
			if withRobot {
				if robotUser == "" {
					return errors.New("--robot needs --robot-user")
				}
				pw, err := c.ReadSecret("Robot webservice password: ")
				if err != nil {
					return err
				}
				if pw == "" {
					return errors.New("empty password")
				}
				ctx.RobotUser, ctx.RobotPassword = robotUser, pw
			}
			cfg.Upsert(ctx)
			if activate || cfg.ActiveContext == "" {
				cfg.ActiveContext = ctx.Name
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(c.Err, "Context %q saved in %s\n", ctx.Name, cfg.FilePath())
			return nil
		},
	}
	create.Flags().BoolVar(&withCloud, "cloud", false, "prompt for a Hetzner Cloud API token")
	create.Flags().BoolVar(&withRobot, "robot", false, "prompt for a Robot webservice password")
	create.Flags().StringVar(&robotUser, "robot-user", "", "Robot webservice user")
	create.Flags().BoolVar(&activate, "activate", false, "make it the active context")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List contexts",
		Args:    cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := c.Config()
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			type row struct {
				Name      string `json:"name"`
				Active    bool   `json:"active"`
				Cloud     bool   `json:"cloud"`
				RobotUser string `json:"robot_user,omitempty"`
			}
			rows := []row{}
			t := &output.Table{Headers: []string{"active", "name", "cloud", "robot user"}}
			for _, name := range cfg.Names() {
				ctx := cfg.Get(name)
				r := row{Name: name, Active: name == cfg.ActiveContext, Cloud: ctx.CloudToken != "", RobotUser: ctx.RobotUser}
				rows = append(rows, r)
				mark := ""
				if r.Active {
					mark = "*"
				}
				t.AddRow(mark, r.Name, yesNo(r.Cloud), r.RobotUser)
			}
			return p.Print(rows, t)
		},
	}

	use := &cobra.Command{
		Use:               "use <name>",
		Short:             "Set the active context",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := c.Config()
			if err != nil {
				return err
			}
			if cfg.Get(args[0]) == nil {
				return fmt.Errorf("context %q not found", args[0])
			}
			cfg.ActiveContext = args[0]
			return cfg.Save()
		},
	}

	active := &cobra.Command{
		Use:   "active",
		Short: "Print the active context name",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := c.Config()
			if err != nil {
				return err
			}
			name := c.ContextName
			if name == "" {
				name = cfg.ActiveContext
			}
			fmt.Fprintln(c.Out, name)
			return nil
		},
	}

	del := &cobra.Command{
		Use:               "delete <name>",
		Aliases:           []string{"rm"},
		Short:             "Delete a context",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNames,
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := c.Config()
			if err != nil {
				return err
			}
			if !cfg.Delete(args[0]) {
				return fmt.Errorf("context %q not found", args[0])
			}
			return cfg.Save()
		},
	}

	cmd.AddCommand(create, list, use, active, del)
	return cmd
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
