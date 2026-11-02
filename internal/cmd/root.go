// Package cmd assembles the cobra command tree.
package cmd

import (
	"errors"
	"fmt"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/cloud"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot"
	"github.com/digiogithub/hcli/internal/version"
	"github.com/spf13/cobra"
)

// NewRootCommand builds the full command tree around c.
func NewRootCommand(c *cli.CLI) *cobra.Command {
	root := &cobra.Command{
		Use:   "hcli",
		Short: "Hetzner Cloud and Robot from one command line",
		Long: `hcli manages Hetzner Cloud projects (api.hetzner.cloud) and Hetzner
dedicated servers through the Robot webservice (robot-ws.your-server.de).

Credentials are kept in named contexts (hcli context create) or taken from
HCLOUD_TOKEN, HROBOT_USER and HROBOT_PASSWORD.`,
		Version:       version.Get(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			_, err := output.Parse(c.OutputFlag)
			return err
		},
	}
	root.SetIn(c.In)
	root.SetOut(c.Out)
	root.SetErr(c.Err)

	pf := root.PersistentFlags()
	pf.StringVar(&c.ConfigPath, "config", "", "config file (default $XDG_CONFIG_HOME/hcli/config.toml, env HCLI_CONFIG)")
	pf.StringVar(&c.ContextName, "context", "", "context to use (env HCLI_CONTEXT)")
	pf.StringVarP(&c.OutputFlag, "output", "o", "table", "output format: table, json, yaml")
	pf.BoolVar(&c.NoHeader, "no-header", false, "omit the table header")
	pf.BoolVarP(&c.Yes, "yes", "y", false, "do not ask for confirmation on destructive or billable actions")
	pf.BoolVar(&c.Debug, "debug", false, "log API requests to stderr")

	_ = root.RegisterFlagCompletionFunc("output", cobra.FixedCompletions(output.Formats, cobra.ShellCompDirectiveNoFileComp))
	_ = root.RegisterFlagCompletionFunc("context", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return contextNames(c), cobra.ShellCompDirectiveNoFileComp
	})

	root.AddGroup(
		&cobra.Group{ID: "api", Title: "API commands:"},
		&cobra.Group{ID: "config", Title: "Configuration:"},
	)

	cloudCmd := cloud.NewCommand(c)
	cloudCmd.GroupID = "api"
	robotCmd := robot.NewCommand(c)
	robotCmd.GroupID = "api"
	ctxCmd := newContextCommand(c)
	ctxCmd.GroupID = "config"
	compCmd := newCompletionCommand()
	compCmd.GroupID = "config"
	verCmd := newVersionCommand(c)
	verCmd.GroupID = "config"

	root.AddCommand(cloudCmd, robotCmd, ctxCmd, compCmd, verCmd)
	root.SetHelpCommandGroupID("config")
	root.CompletionOptions.DisableDefaultCmd = true
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	c := cli.New()
	root := NewRootCommand(c)
	if err := root.Execute(); err != nil {
		var exit *ExitError
		if errors.As(err, &exit) {
			return exit.Code
		}
		fmt.Fprintln(c.Err, "Error:", err)
		return 1
	}
	return 0
}

// ExitError lets a command choose the exit code.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

func contextNames(c *cli.CLI) []string {
	cfg, err := c.Config()
	if err != nil {
		return nil
	}
	return cfg.Names()
}
