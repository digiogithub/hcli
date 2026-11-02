package cloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

// Read-only catalog resources: server types, images, locations, datacenters, ISOs.

var serverTypeRes = resource[hcloud.ServerType]{
	Kind: "server-type",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.ServerType, *hcloud.Response, error) {
		return cl.ServerType.Get(ctx, s)
	},
	Names:  serverTypeNames,
	Ident:  func(t *hcloud.ServerType) (int64, string) { return t.ID, t.Name },
	Schema: func(t *hcloud.ServerType) any { return hcloud.SchemaFromServerType(t) },
}

var imageRes = resource[hcloud.Image]{
	Kind: "image",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Image, *hcloud.Response, error) {
		return getImage(ctx, cl, s)
	},
	Names:  imageNames,
	Ident:  func(i *hcloud.Image) (int64, string) { return i.ID, i.Name },
	Schema: func(i *hcloud.Image) any { return hcloud.SchemaFromImage(i) },
}

var locationRes = resource[hcloud.Location]{
	Kind: "location",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Location, *hcloud.Response, error) {
		return cl.Location.Get(ctx, s)
	},
	Names:  locationNames,
	Ident:  func(l *hcloud.Location) (int64, string) { return l.ID, l.Name },
	Schema: func(l *hcloud.Location) any { return hcloud.SchemaFromLocation(l) },
}

// Datacenters were deprecated by Hetzner (changelog 2026-06-02) and the list
// endpoint now answers deprecated_api_endpoint, so there is no `datacenter`
// command; this resource only backs the deprecated `server create --datacenter`.
//
//nolint:staticcheck // deprecated upstream, see above
var datacenterRes = resource[hcloud.Datacenter]{
	Kind: "datacenter",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.Datacenter, *hcloud.Response, error) {
		return cl.Datacenter.Get(ctx, s)
	},
	Ident:  func(d *hcloud.Datacenter) (int64, string) { return d.ID, d.Name },
	Schema: func(d *hcloud.Datacenter) any { return hcloud.SchemaFromDatacenter(d) },
}

var isoRes = resource[hcloud.ISO]{
	Kind: "iso",
	Get: func(ctx context.Context, cl *hcloud.Client, s string) (*hcloud.ISO, *hcloud.Response, error) {
		return cl.ISO.Get(ctx, s)
	},
	Names:  isoNames,
	Ident:  func(i *hcloud.ISO) (int64, string) { return i.ID, i.Name },
	Schema: func(i *hcloud.ISO) any { return hcloud.SchemaFromISO(i) },
}

// getImage resolves an image by ID, or by name trying x86 first and then arm
// (image names are unique per architecture).
func getImage(ctx context.Context, cl *hcloud.Client, idOrName string) (*hcloud.Image, *hcloud.Response, error) {
	if id, err := strconv.ParseInt(idOrName, 10, 64); err == nil {
		return cl.Image.GetByID(ctx, id)
	}
	img, resp, err := cl.Image.GetByNameAndArchitecture(ctx, idOrName, hcloud.ArchitectureX86)
	if err != nil || img != nil {
		return img, resp, err
	}
	return cl.Image.GetByNameAndArchitecture(ctx, idOrName, hcloud.ArchitectureARM)
}

func group(use, short string, aliases []string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Aliases: aliases}
	cmd.AddCommand(children...)
	return cmd
}

func deprecated(d *hcloud.DeprecationInfo) string {
	if d == nil {
		return ""
	}
	return "unavailable after " + d.UnavailableAfter.Format("2006-01-02")
}

func newServerTypeCommand(a *app) *cobra.Command {
	return group("server-type", "List server types", []string{"server-types", "st"},
		newListCmd(a, serverTypeRes, listSpec[hcloud.ServerType]{
			Headers: []string{"id", "name", "cores", "cpu type", "architecture", "memory", "disk", "storage type"},
			All: func(ctx context.Context, cl *hcloud.Client, _ string, _ *cobra.Command) ([]*hcloud.ServerType, error) {
				return cl.ServerType.All(ctx)
			},
			Row: func(t *hcloud.ServerType) []any {
				return []any{t.ID, t.Name, t.Cores, t.CPUType, t.Architecture, fmt.Sprintf("%.0f GB", t.Memory), fmt.Sprintf("%d GB", t.Disk), t.StorageType}
			},
		}),
		newDescribeCmd(a, serverTypeRes, func(t *hcloud.ServerType) [][2]string {
			kv := [][2]string{
				{"ID", strconv.FormatInt(t.ID, 10)},
				{"Name", t.Name},
				{"Description", t.Description},
				{"Category", t.Category},
				{"Cores", strconv.Itoa(t.Cores)},
				{"CPU type", string(t.CPUType)},
				{"Architecture", string(t.Architecture)},
				{"Memory", fmt.Sprintf("%.1f GB", t.Memory)},
				{"Disk", fmt.Sprintf("%d GB", t.Disk)},
				{"Storage type", string(t.StorageType)},
			}
			for _, l := range t.Locations {
				state := "available"
				if !l.Available {
					state = "unavailable"
				}
				if d := deprecated(l.Deprecation); d != "" {
					state += ", " + d
				}
				kv = append(kv, [2]string{"Location " + locationName(l.Location), state})
			}
			for _, pr := range t.Pricings {
				kv = append(kv, [2]string{"Price " + locationName(pr.Location),
					fmt.Sprintf("%s/h, %s/month (gross, %s), traffic included %s", pr.Hourly.Gross, pr.Monthly.Gross, pr.Monthly.Currency, bytesHuman(pr.IncludedTraffic))})
			}
			return kv
		}),
	)
}

func newImageCommand(a *app) *cobra.Command {
	var types, archs []string
	list := newListCmd(a, imageRes, listSpec[hcloud.Image]{
		Selector: true,
		Headers:  []string{"id", "type", "name", "description", "architecture", "image size", "disk size", "age", "deprecated"},
		All: func(ctx context.Context, cl *hcloud.Client, sel string, _ *cobra.Command) ([]*hcloud.Image, error) {
			opts := hcloud.ImageListOpts{ListOpts: hcloud.ListOpts{LabelSelector: sel}, IncludeDeprecated: true, Sort: []string{"id:asc"}}
			for _, t := range types {
				opts.Type = append(opts.Type, hcloud.ImageType(t))
			}
			for _, ar := range archs {
				opts.Architecture = append(opts.Architecture, hcloud.Architecture(ar))
			}
			return cl.Image.AllWithOpts(ctx, opts)
		},
		Row: func(i *hcloud.Image) []any {
			size := ""
			if i.ImageSize > 0 {
				size = fmt.Sprintf("%.2f GB", i.ImageSize)
			}
			return []any{i.ID, i.Type, i.Name, i.Description, i.Architecture, size, fmt.Sprintf("%.0f GB", i.DiskSize), age(i.Created), deprecated(i.Deprecation)}
		},
		Flags: func(cmd *cobra.Command) {
			cmd.Flags().StringSliceVarP(&types, "type", "t", nil, "filter by type: system, app, snapshot, backup")
			cmd.Flags().StringSliceVarP(&archs, "architecture", "a", nil, "filter by architecture: x86, arm")
			fixedCompletion(cmd, "type", "system", "app", "snapshot", "backup")
			fixedCompletion(cmd, "architecture", "x86", "arm")
		},
	})
	return group("image", "Manage images and snapshots", []string{"images", "img"},
		list,
		newDescribeCmd(a, imageRes, func(i *hcloud.Image) [][2]string {
			kv := [][2]string{
				{"ID", strconv.FormatInt(i.ID, 10)},
				{"Type", string(i.Type)},
				{"Status", string(i.Status)},
				{"Name", i.Name},
				{"Description", i.Description},
				{"Created", created(i.Created)},
				{"OS", strings.TrimSpace(i.OSFlavor + " " + i.OSVersion)},
				{"Architecture", string(i.Architecture)},
				{"Disk size", fmt.Sprintf("%.0f GB", i.DiskSize)},
				{"Rapid deploy", yesNo(i.RapidDeploy)},
				{"Protection", "delete=" + yesNo(i.Protection.Delete)},
				{"Deprecated", deprecated(i.Deprecation)},
				{"Labels", formatLabels(i.Labels)},
			}
			if i.ImageSize > 0 {
				kv = append(kv, [2]string{"Image size", fmt.Sprintf("%.2f GB", i.ImageSize)})
			}
			if i.CreatedFrom != nil {
				kv = append(kv, [2]string{"Created from", fmt.Sprintf("%s (%d)", i.CreatedFrom.Name, i.CreatedFrom.ID)})
			}
			if i.BoundTo != nil {
				kv = append(kv, [2]string{"Bound to", strconv.FormatInt(i.BoundTo.ID, 10)})
			}
			return kv
		}),
		newDeleteCmd(a, imageRes, func(ctx context.Context, cl *hcloud.Client, i *hcloud.Image) ([]*hcloud.Action, error) {
			_, err := cl.Image.Delete(ctx, i)
			return nil, err
		}),
	)
}

func newLocationCommand(a *app) *cobra.Command {
	return group("location", "List locations", []string{"locations", "loc"},
		newListCmd(a, locationRes, listSpec[hcloud.Location]{
			Headers: []string{"id", "name", "description", "network zone", "country", "city"},
			All: func(ctx context.Context, cl *hcloud.Client, _ string, _ *cobra.Command) ([]*hcloud.Location, error) {
				return cl.Location.All(ctx)
			},
			Row: func(l *hcloud.Location) []any {
				return []any{l.ID, l.Name, l.Description, l.NetworkZone, l.Country, l.City}
			},
		}),
		newDescribeCmd(a, locationRes, func(l *hcloud.Location) [][2]string {
			return [][2]string{
				{"ID", strconv.FormatInt(l.ID, 10)},
				{"Name", l.Name},
				{"Description", l.Description},
				{"Network zone", string(l.NetworkZone)},
				{"Country", l.Country},
				{"City", l.City},
				{"Latitude", fmt.Sprintf("%f", l.Latitude)},
				{"Longitude", fmt.Sprintf("%f", l.Longitude)},
			}
		}),
	)
}

func newISOCommand(a *app) *cobra.Command {
	return group("iso", "List ISOs", []string{"isos"},
		newListCmd(a, isoRes, listSpec[hcloud.ISO]{
			Headers: []string{"id", "name", "description", "type", "architecture"},
			All: func(ctx context.Context, cl *hcloud.Client, _ string, _ *cobra.Command) ([]*hcloud.ISO, error) {
				return cl.ISO.All(ctx)
			},
			Row: func(i *hcloud.ISO) []any {
				arch := ""
				if i.Architecture != nil {
					arch = string(*i.Architecture)
				}
				return []any{i.ID, i.Name, i.Description, i.Type, arch}
			},
		}),
		newDescribeCmd(a, isoRes, func(i *hcloud.ISO) [][2]string {
			arch := ""
			if i.Architecture != nil {
				arch = string(*i.Architecture)
			}
			return [][2]string{
				{"ID", strconv.FormatInt(i.ID, 10)},
				{"Name", i.Name},
				{"Description", i.Description},
				{"Type", string(i.Type)},
				{"Architecture", arch},
				{"Deprecated", deprecated(i.Deprecation)},
			}
		}),
	)
}
