package cloud

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

// resource describes how to look up one kind of API object by ID or name.
// It is the glue for the generic list/describe/delete/label commands.
type resource[T any] struct {
	// Kind is the singular noun used in messages and command names ("server").
	Kind string
	// Get resolves an ID or name.
	Get func(ctx context.Context, cl *hcloud.Client, idOrName string) (*T, *hcloud.Response, error)
	// Names lists the names offered by shell completion.
	Names func(ctx context.Context, cl *hcloud.Client) ([]string, error)
	// Ident returns ID and name of an object.
	Ident func(*T) (int64, string)
	// Schema converts to the API schema for json/yaml output.
	Schema func(*T) any
}

// fetch resolves idOrName and returns a "not found" error when nothing matches.
func (r resource[T]) fetch(ctx context.Context, cl *hcloud.Client, idOrName string) (*T, error) {
	obj, _, err := r.Get(ctx, cl, idOrName)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, fmt.Errorf("%s not found: %s", r.Kind, idOrName)
	}
	return obj, nil
}

// label returns "name" or the ID when the object has no name.
func (r resource[T]) label(obj *T) string {
	id, name := r.Ident(obj)
	if name != "" {
		return name
	}
	return strconv.FormatInt(id, 10)
}

func (r resource[T]) title() string {
	return strings.ToUpper(r.Kind[:1]) + r.Kind[1:]
}

// listSpec configures a generic list command.
type listSpec[T any] struct {
	// All returns every object; selector is empty unless Selector is set.
	All func(ctx context.Context, cl *hcloud.Client, selector string, cmd *cobra.Command) ([]*T, error)
	// Selector enables -l/--selector.
	Selector bool
	Headers  []string
	Row      func(*T) []any
	// Flags adds extra filter flags.
	Flags func(cmd *cobra.Command)
}

func newListCmd[T any](a *app, r resource[T], spec listSpec[T]) *cobra.Command {
	var selector string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   fmt.Sprintf("List %ss", strings.ReplaceAll(r.Kind, "-", " ")),
		Args:    cobra.NoArgs,
	}
	cmd.RunE = a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
		items, err := spec.All(ctx, cl, selector, cmd)
		if err != nil {
			return err
		}
		raw := make([]any, 0, len(items))
		t := &output.Table{Headers: spec.Headers}
		for _, it := range items {
			raw = append(raw, r.Schema(it))
			t.AddRow(spec.Row(it)...)
		}
		return p.Print(raw, t)
	})
	if spec.Selector {
		cmd.Flags().StringVarP(&selector, "selector", "l", "", "label selector (e.g. env=prod,team!=ops)")
	}
	if spec.Flags != nil {
		spec.Flags(cmd)
	}
	return cmd
}

func newDescribeCmd[T any](a *app, r resource[T], details func(*T) [][2]string) *cobra.Command {
	return &cobra.Command{
		Use:               fmt.Sprintf("describe <%s>", r.Kind),
		Aliases:           []string{"show", "get"},
		Short:             fmt.Sprintf("Describe a %s", strings.ReplaceAll(r.Kind, "-", " ")),
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.complete(r.Names),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			obj, err := r.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			return p.KeyValues(r.Schema(obj), details(obj))
		}),
	}
}

// newDeleteCmd deletes one or more objects, asking for confirmation for each.
// del may return actions to wait for.
func newDeleteCmd[T any](a *app, r resource[T], del func(ctx context.Context, cl *hcloud.Client, obj *T) ([]*hcloud.Action, error)) *cobra.Command {
	return &cobra.Command{
		Use:               fmt.Sprintf("delete <%s>...", r.Kind),
		Aliases:           []string{"rm"},
		Short:             fmt.Sprintf("Delete %ss", strings.ReplaceAll(r.Kind, "-", " ")),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeRepeat(r.Names),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			for _, arg := range args {
				obj, err := r.fetch(ctx, cl, arg)
				if err != nil {
					return err
				}
				name := r.label(obj)
				if err := a.c.Confirm(fmt.Sprintf("delete %s %s", r.Kind, name)); err != nil {
					return err
				}
				actions, err := del(ctx, cl, obj)
				if err != nil {
					return err
				}
				if err := a.wait(ctx, cl, fmt.Sprintf("%s %s to be deleted", r.Kind, name), actions...); err != nil {
					return err
				}
				if err := a.done(p, nil, "%s %s deleted", r.title(), name); err != nil {
					return err
				}
			}
			return nil
		}),
	}
}

// newLabelCmds returns add-label and remove-label commands.
func newLabelCmds[T any](a *app, r resource[T], labels func(*T) map[string]string,
	update func(ctx context.Context, cl *hcloud.Client, obj *T, labels map[string]string) error) []*cobra.Command {

	var overwrite bool
	add := &cobra.Command{
		Use:               fmt.Sprintf("add-label <%s> <key=value>...", r.Kind),
		Short:             fmt.Sprintf("Add labels to a %s", strings.ReplaceAll(r.Kind, "-", " ")),
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: a.complete(r.Names),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			obj, err := r.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			add, err := parseLabels(args[1:])
			if err != nil {
				return err
			}
			merged := map[string]string{}
			for k, v := range labels(obj) {
				merged[k] = v
			}
			for k, v := range add {
				if _, exists := merged[k]; exists && !overwrite {
					return fmt.Errorf("label %q already exists on %s %s (use --overwrite)", k, r.Kind, r.label(obj))
				}
				merged[k] = v
			}
			if err := update(ctx, cl, obj, merged); err != nil {
				return err
			}
			return a.done(p, nil, "Labels added to %s %s", r.Kind, r.label(obj))
		}),
	}
	add.Flags().BoolVar(&overwrite, "overwrite", false, "replace existing labels with the same key")

	var all bool
	remove := &cobra.Command{
		Use:               fmt.Sprintf("remove-label <%s> <key>...", r.Kind),
		Short:             fmt.Sprintf("Remove labels from a %s", strings.ReplaceAll(r.Kind, "-", " ")),
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.complete(r.Names),
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, args []string) error {
			if !all && len(args) < 2 {
				return fmt.Errorf("specify label keys to remove or --all")
			}
			obj, err := r.fetch(ctx, cl, args[0])
			if err != nil {
				return err
			}
			kept := map[string]string{}
			if !all {
				for k, v := range labels(obj) {
					kept[k] = v
				}
				for _, k := range args[1:] {
					if _, ok := kept[k]; !ok {
						return fmt.Errorf("label %q does not exist on %s %s", k, r.Kind, r.label(obj))
					}
					delete(kept, k)
				}
			}
			if err := update(ctx, cl, obj, kept); err != nil {
				return err
			}
			return a.done(p, nil, "Labels removed from %s %s", r.Kind, r.label(obj))
		}),
	}
	remove.Flags().BoolVar(&all, "all", false, "remove all labels")
	return []*cobra.Command{add, remove}
}

// parseLabels turns ["k=v", ...] into a map.
func parseLabels(in []string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for _, kv := range in {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid label %q (expected key=value)", kv)
		}
		out[k] = v
	}
	return out, nil
}

// formatLabels renders labels sorted as "k=v, k2=v2".
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + labels[k]
	}
	return strings.Join(parts, ", ")
}

// age renders the time since t like hcloud ("3d", "5h", "12m", "40s").
func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "0s"
	case d >= 365*24*time.Hour:
		return fmt.Sprintf("%dy", int(d.Hours()/24/365))
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}

// created renders a timestamp plus its age.
func created(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s (%s ago)", t.Format(time.RFC3339), age(t))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func ipString(ip net.IP) string {
	if ip == nil || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

func netString(n *net.IPNet) string {
	if n == nil {
		return ""
	}
	return n.String()
}

func locationName(l *hcloud.Location) string {
	if l == nil {
		return ""
	}
	return l.Name
}

func bytesHuman(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// readFileOrStdin reads path, or stdin when path is "-".
func (a *app) readFileOrStdin(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(a.c.In)
	}
	return os.ReadFile(path)
}

// namesOf extracts names (or IDs for unnamed objects).
func namesOf[T any](items []*T, ident func(*T) (int64, string)) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		id, name := ident(it)
		if name == "" {
			name = strconv.FormatInt(id, 10)
		}
		out = append(out, name)
	}
	return out
}

// addLabelFlag registers a repeatable --label key=value flag.
func addLabelFlag(cmd *cobra.Command, dst *[]string) {
	cmd.Flags().StringArrayVar(dst, "label", nil, "label key=value (repeatable)")
}
