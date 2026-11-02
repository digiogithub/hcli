package cloud

import (
	"context"
	"fmt"
	"sort"

	"github.com/digiogithub/hcli/internal/output"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/spf13/cobra"
)

func newPricingCommand(a *app) *cobra.Command {
	var net bool
	var location string
	cmd := &cobra.Command{
		Use:     "pricing",
		Aliases: []string{"prices"},
		Short:   "Show prices for server types, volumes, IPs and load balancers",
		Args:    cobra.NoArgs,
		RunE: a.run(func(ctx context.Context, cl *hcloud.Client, p *output.Printer, _ []string) error {
			pr, _, err := cl.Pricing.Get(ctx)
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(hcloud.SchemaFromPricing(pr), nil)
			}
			amount := func(pp hcloud.Price) string {
				if net {
					return pp.Net
				}
				return pp.Gross
			}
			kind := "gross"
			if net {
				kind = "net"
			}
			fmt.Fprintf(p.Out, "Prices in %s, %s (VAT %s%%)\n\n", pr.Currency, kind, pr.VATRate)
			match := func(l string) bool { return location == "" || l == location }

			servers := &output.Table{Headers: []string{"server type", "location", "hourly", "monthly", "included traffic", "per TB"}}
			types := pr.ServerTypes
			sort.SliceStable(types, func(i, j int) bool { return types[i].ServerType.Name < types[j].ServerType.Name })
			for _, st := range types {
				for _, lp := range st.Pricings {
					if !match(locationName(lp.Location)) {
						continue
					}
					servers.AddRow(st.ServerType.Name, locationName(lp.Location), amount(lp.Hourly), amount(lp.Monthly), bytesHuman(lp.IncludedTraffic), amount(lp.PerTBTraffic))
				}
			}
			if err := p.Table(servers); err != nil {
				return err
			}

			fmt.Fprintln(p.Out)
			other := &output.Table{Headers: []string{"resource", "location", "hourly", "monthly"}}
			other.AddRow("volume (per GB)", "", "", amount(pr.Volume.PerGBMonthly))
			other.AddRow("image (per GB)", "", "", amount(pr.Image.PerGBMonth))
			other.AddRow("server backup", "", "", pr.ServerBackup.Percentage+"% of server")
			for _, ip := range pr.PrimaryIPs {
				for _, lp := range ip.Pricings {
					if !match(lp.Location) {
						continue
					}
					h, m := lp.Hourly.Gross, lp.Monthly.Gross
					if net {
						h, m = lp.Hourly.Net, lp.Monthly.Net
					}
					other.AddRow("primary ip "+ip.Type, lp.Location, h, m)
				}
			}
			for _, ip := range pr.FloatingIPs {
				for _, lp := range ip.Pricings {
					if !match(locationName(lp.Location)) {
						continue
					}
					other.AddRow("floating ip "+string(ip.Type), locationName(lp.Location), "", amount(lp.Monthly))
				}
			}
			for _, lb := range pr.LoadBalancerTypes {
				for _, lp := range lb.Pricings {
					if !match(locationName(lp.Location)) {
						continue
					}
					name := ""
					if lb.LoadBalancerType != nil {
						name = lb.LoadBalancerType.Name
					}
					other.AddRow("load balancer "+name, locationName(lp.Location), amount(lp.Hourly), amount(lp.Monthly))
				}
			}
			return p.Table(other)
		}),
	}
	cmd.Flags().BoolVar(&net, "net", false, "show net prices instead of gross")
	cmd.Flags().StringVar(&location, "location", "", "only show this location")
	a.flagCompletion(cmd, "location", locationNames)
	return cmd
}
