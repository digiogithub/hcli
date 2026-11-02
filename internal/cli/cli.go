// Package cli carries the state shared by every command: loaded config,
// effective credentials, output printer and confirmation prompts.
//
// Command packages (internal/cloud, internal/robot, ...) receive a *CLI when
// building their cobra tree and call its methods from RunE. Everything is
// resolved lazily, so `--help` and shell completion work without credentials.
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/digiogithub/hcli/internal/config"
	"github.com/digiogithub/hcli/internal/output"
	"golang.org/x/term"
)

// CLI is the shared runtime state.
type CLI struct {
	// Bound to persistent flags by internal/cmd.
	ConfigPath  string
	ContextName string
	OutputFlag  string
	NoHeader    bool
	Yes         bool
	Debug       bool

	In  io.Reader
	Out io.Writer
	Err io.Writer

	cfg *config.Config
	eff *config.Context
}

// New returns a CLI wired to the process stdio.
func New() *CLI {
	return &CLI{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, OutputFlag: string(output.FormatTable)}
}

// Config loads (once) the config file.
func (c *CLI) Config() (*config.Config, error) {
	if c.cfg != nil {
		return c.cfg, nil
	}
	cfg, err := config.Load(c.ConfigPath)
	if err != nil {
		return nil, err
	}
	c.cfg = cfg
	return cfg, nil
}

// Context returns the effective credentials (context + env overrides).
func (c *CLI) Context() (*config.Context, error) {
	if c.eff != nil {
		return c.eff, nil
	}
	cfg, err := c.Config()
	if err != nil {
		return nil, err
	}
	eff, err := cfg.Resolve(c.ContextName)
	if err != nil {
		return nil, err
	}
	c.eff = eff
	return eff, nil
}

// ErrNoCloudToken / ErrNoRobotCredentials are returned when credentials are missing.
var (
	ErrNoCloudToken       = errors.New("no Hetzner Cloud token: set HCLOUD_TOKEN or run `hcli context create <name> --cloud`")
	ErrNoRobotCredentials = errors.New("no Robot webservice credentials: set HROBOT_USER/HROBOT_PASSWORD or run `hcli context create <name> --robot`")
)

// Printer returns a printer for the --output flag.
func (c *CLI) Printer() (*output.Printer, error) {
	f, err := output.Parse(c.OutputFlag)
	if err != nil {
		return nil, err
	}
	return &output.Printer{Out: c.Out, Format: f, NoHeader: c.NoHeader}, nil
}

// Confirm asks before a destructive or billable action. --yes skips the
// prompt; a non-interactive stdin without --yes refuses.
func (c *CLI) Confirm(action string) error {
	if c.Yes {
		return nil
	}
	if f, ok := c.In.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
		return fmt.Errorf("refusing to %s without confirmation: pass --yes", action)
	}
	fmt.Fprintf(c.Err, "About to %s. Continue? [y/N]: ", action)
	line, _ := bufio.NewReader(c.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "s", "si", "sí":
		return nil
	}
	return errors.New("aborted")
}

// ReadSecret reads a value without echo when stdin is a terminal.
func (c *CLI) ReadSecret(prompt string) (string, error) {
	fmt.Fprint(c.Err, prompt)
	if f, ok := c.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(c.Err)
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(c.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// Debugf prints to stderr when --debug is set.
func (c *CLI) Debugf(format string, args ...any) {
	if c.Debug {
		fmt.Fprintf(c.Err, "[debug] "+format+"\n", args...)
	}
}
