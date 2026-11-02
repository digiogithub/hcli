package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/version"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

// pollInterval is how often running actions are polled. Tests lower it.
var pollInterval = 500 * time.Millisecond

// completionTimeout bounds API calls made during shell completion.
const completionTimeout = 10 * time.Second

// app is the per-process state of the cloud commands: the shared CLI and a
// lazily built hcloud client.
type app struct {
	c      *cli.CLI
	client *hcloud.Client
}

// Client returns the hcloud client for the active context. It fails with
// cli.ErrNoCloudToken when no token is configured.
func (a *app) Client() (*hcloud.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	ctx, err := a.c.Context()
	if err != nil {
		return nil, err
	}
	if ctx.CloudToken == "" {
		return nil, cli.ErrNoCloudToken
	}
	opts := []hcloud.ClientOption{
		hcloud.WithToken(ctx.CloudToken),
		hcloud.WithApplication("hcli", version.Get()),
		hcloud.WithPollOpts(hcloud.PollOpts{BackoffFunc: hcloud.ConstantBackoff(pollInterval)}),
	}
	if ctx.CloudEndpoint != "" {
		opts = append(opts, hcloud.WithEndpoint(ctx.CloudEndpoint))
	}
	if a.c.Debug {
		// hcloud-go's debug handler replaces the Authorization header with
		// "REDACTED" before dumping requests, so the token is never printed.
		opts = append(opts, hcloud.WithDebugWriter(a.c.Err))
		a.c.Debugf("cloud endpoint: %s", ctx.CloudEndpoint)
	}
	a.client = hcloud.NewClient(opts...)
	return a.client, nil
}

// runFunc is the body of a command that talks to the API.
type runFunc func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error

// run adapts a runFunc to cobra's RunE, resolving the client and printer.
func (a *app) run(fn runFunc) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		p, err := a.c.Printer()
		if err != nil {
			return err
		}
		cl, err := a.Client()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		return fn(ctx, cl, p, args)
	}
}

// wait blocks until the actions finish, with a progress line on stderr.
func (a *app) wait(ctx context.Context, cl *hcloud.Client, what string, actions ...*hcloud.Action) error {
	var pending []*hcloud.Action
	for _, act := range actions {
		if act != nil {
			pending = append(pending, act)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	fmt.Fprintf(a.c.Err, "Waiting for %s ... ", what)
	if err := cl.Action.WaitFor(ctx, pending...); err != nil {
		fmt.Fprintln(a.c.Err, "failed")
		return err
	}
	fmt.Fprintln(a.c.Err, "done")
	return nil
}

// done reports the result of an action command: a sentence for table output,
// the action object for json/yaml.
func (a *app) done(p *output.Printer, action *hcloud.Action, format string, args ...any) error {
	if p.Format != output.FormatTable {
		if action == nil {
			return p.Print(map[string]string{"result": fmt.Sprintf(format, args...)}, nil)
		}
		return p.Print(hcloud.SchemaFromAction(action), nil)
	}
	fmt.Fprintf(p.Out, format+"\n", args...)
	return nil
}
