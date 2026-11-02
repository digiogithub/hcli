package robot

// Ordering is READ ONLY on purpose.
//
// The Robot webservice can buy servers and addons (POST /order/server/transaction,
// POST /order/server_market/transaction, POST /order/server_addon/transaction).
// hcli does not implement those calls: an order is a binding purchase that is
// billed immediately (at least the first month, plus setup fees), it cannot be
// undone from the API, and the documented limit is only 20 orders per day, so
// a scripting mistake or a mistyped product ID is expensive. Browsing offers
// and following existing transactions is safe and useful; the purchase itself
// is left to the Robot web interface, where the price and terms are shown.

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

func newOrderCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Browse server offers and order transactions (read only, no purchasing)",
		Long: `Browse standard server products, the server auction (server market) and the
order transactions of the last 30 days.

Purchasing is intentionally not implemented: orders are binding, billed and
cannot be undone through the API. Order in the Robot web interface.`,
	}
	market := &cobra.Command{Use: "server-market", Aliases: []string{"auction", "market"}, Short: "Server auction offers"}
	market.AddCommand(newMarketListCommand(c), newMarketDescribeCommand(c))
	server := &cobra.Command{Use: "server", Aliases: []string{"product", "products"}, Short: "Standard server products"}
	server.AddCommand(newProductListCommand(c), newProductDescribeCommand(c))
	tx := &cobra.Command{Use: "transaction", Aliases: []string{"transactions", "tx"}, Short: "Server order transactions (last 30 days)"}
	tx.AddCommand(newTransactionListCommand(c), newTransactionDescribeCommand(c))
	cmd.AddCommand(market, server, tx)
	return cmd
}

func price(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return math.Inf(1)
	}
	return f
}

func money(s string) string {
	f := price(s)
	if math.IsInf(f, 0) {
		return s
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

var diskLine = regexp.MustCompile(`(?i)\b(hdd|ssd|nvme)\b`)

// diskType classifies one description line: nvme, ssd, hdd or "".
func diskType(line string) string {
	l := strings.ToLower(line)
	switch {
	case !diskLine.MatchString(l):
		return ""
	case strings.Contains(l, "nvme"):
		return "nvme"
	case strings.Contains(l, "ssd"):
		return "ssd"
	}
	return "hdd"
}

// marketDisks returns the disk lines of an offer and the set of disk types.
func marketDisks(p *client.MarketProduct) ([]string, []string) {
	var lines, types []string
	for _, d := range p.Description {
		if t := diskType(d); t != "" {
			lines = append(lines, strings.TrimSpace(d))
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
	}
	return lines, types
}

type marketFilter struct {
	minRAM, maxPrice float64
	cpu, diskType    string
	dc               string
	limit            int
}

func (f marketFilter) apply(in []client.MarketProduct) []client.MarketProduct {
	out := make([]client.MarketProduct, 0, len(in))
	for i := range in {
		p := &in[i]
		if f.minRAM > 0 && p.MemorySize < f.minRAM {
			continue
		}
		if f.maxPrice > 0 && price(p.Price) > f.maxPrice {
			continue
		}
		if f.cpu != "" && !strings.Contains(strings.ToLower(p.CPU), strings.ToLower(f.cpu)) {
			continue
		}
		if f.dc != "" && !strings.HasPrefix(strings.ToUpper(p.Datacenter), strings.ToUpper(f.dc)) {
			continue
		}
		if f.diskType != "" {
			if _, types := marketDisks(p); !slices.Contains(types, f.diskType) {
				continue
			}
		}
		out = append(out, *p)
	}
	sort.SliceStable(out, func(i, j int) bool { return price(out[i].Price) < price(out[j].Price) })
	if f.limit > 0 && len(out) > f.limit {
		out = out[:f.limit]
	}
	return out
}

var diskTypes = []string{"nvme", "ssd", "hdd"}

func newMarketListCommand(c *cli.CLI) *cobra.Command {
	var f marketFilter
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List auction offers, cheapest first",
		Long: `List server auction offers sorted by monthly price (net, without VAT).
Filters are evaluated locally on the full offer list; --disk-type looks at the
drive lines of the description (an offer matches if it has any drive of that
type).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if f.diskType != "" && !slices.Contains(diskTypes, f.diskType) {
				return fmt.Errorf("invalid --disk-type %q (nvme, ssd or hdd)", f.diskType)
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			all, err := cl.ListMarketProducts(cmd.Context())
			if err != nil {
				return err
			}
			offers := f.apply(all)
			t := &output.Table{Headers: []string{"id", "cpu", "ram_gb", "disks", "dc", "price", "setup", "fixed"}}
			for i := range offers {
				o := &offers[i]
				lines, _ := marketDisks(o)
				t.AddRow(o.ID, o.CPU, num(o.MemorySize), strings.Join(lines, " + "), o.Datacenter, money(o.Price), money(o.PriceSetup), yesNo(o.FixedPrice))
			}
			return p.Print(offers, t)
		},
	}
	fl := cmd.Flags()
	fl.Float64Var(&f.minRAM, "min-ram", 0, "minimum RAM in GB")
	fl.Float64Var(&f.maxPrice, "max-price", 0, "maximum monthly net price")
	fl.StringVar(&f.cpu, "cpu", "", "CPU model substring (case insensitive), e.g. ryzen, xeon, \"i7-8700\"")
	fl.StringVar(&f.diskType, "disk-type", "", "only offers with this drive type: nvme, ssd, hdd")
	fl.StringVar(&f.dc, "dc", "", "datacenter prefix, e.g. FSN1, NBG1, HEL1")
	fl.IntVar(&f.limit, "limit", 0, "show at most N offers")
	_ = cmd.RegisterFlagCompletionFunc("disk-type", cobra.FixedCompletions(diskTypes, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("dc", cobra.FixedCompletions([]string{"FSN1", "NBG1", "HEL1"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newMarketDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "describe <offer-id>",
		Aliases: []string{"show", "get"},
		Short:   "Show an auction offer",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			o, err := cl.GetMarketProduct(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			hourly := ""
			if o.PriceHourly != nil {
				hourly = money(*o.PriceHourly)
			}
			return p.KeyValues(o, [][2]string{
				{"ID", strconv.Itoa(o.ID)},
				{"Name", o.Name},
				{"CPU", fmt.Sprintf("%s (benchmark %d)", o.CPU, o.CPUBenchmark)},
				{"RAM", num(o.MemorySize) + " GB"},
				{"Drives", fmt.Sprintf("%d x %s GB %s", o.HDDCount, num(o.HDDSize), o.HDDText)},
				{"Description", strings.Join(o.Description, "; ")},
				{"Datacenter", o.Datacenter},
				{"Network", o.NetworkSpeed},
				{"Traffic", o.Traffic},
				{"Price (net/month)", money(o.Price)},
				{"Price (gross/month)", money(o.PriceVAT)},
				{"Price (net/hour)", hourly},
				{"Setup (net)", money(o.PriceSetup)},
				{"Fixed price", yesNo(o.FixedPrice)},
				{"Next price reduction", o.NextReduceDate},
				{"Addons", addonNames(o.OrderableAddons)},
			})
		},
	}
}

func addonNames(as []client.Addon) string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}
	return join(out)
}

func minPrice(prices []client.LocationPrice) (net, setup string) {
	best := math.Inf(1)
	for _, lp := range prices {
		if v := price(lp.Price.Net); v < best {
			best, net, setup = v, lp.Price.Net, lp.PriceSetup.Net
		}
	}
	return net, setup
}

func newProductListCommand(c *cli.CLI) *cobra.Command {
	var (
		location           string
		minPriceF, maxPriF float64
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List standard server products, cheapest first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			q := url.Values{}
			if location != "" {
				q.Set("location", location)
			}
			if minPriceF > 0 {
				q.Set("min_price", num(minPriceF))
			}
			if maxPriF > 0 {
				q.Set("max_price", num(maxPriF))
			}
			products, err := cl.ListServerProducts(cmd.Context(), q)
			if err != nil {
				return err
			}
			sort.SliceStable(products, func(i, j int) bool {
				a, _ := minPrice(products[i].Prices)
				b, _ := minPrice(products[j].Prices)
				return price(a) < price(b)
			})
			t := &output.Table{Headers: []string{"id", "name", "traffic", "locations", "price_from", "setup"}}
			for _, pr := range products {
				net, setup := minPrice(pr.Prices)
				t.AddRow(pr.ID, pr.Name, pr.Traffic, join(pr.Location), money(net), money(setup))
			}
			return p.Print(products, t)
		},
	}
	cmd.Flags().StringVar(&location, "location", "", "only products available in this location (e.g. FSN1)")
	cmd.Flags().Float64Var(&minPriceF, "min-price", 0, "minimum monthly net price")
	cmd.Flags().Float64Var(&maxPriF, "max-price", 0, "maximum monthly net price")
	return cmd
}

func newProductDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "describe <product-id>",
		Aliases: []string{"show", "get"},
		Short:   "Show a standard product with prices per location",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			pr, err := cl.GetServerProduct(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if p.Format != output.FormatTable {
				return p.Print(pr, nil)
			}
			if err := p.KeyValues(pr, [][2]string{
				{"ID", pr.ID},
				{"Name", pr.Name},
				{"Description", strings.Join(pr.Description, "; ")},
				{"Traffic", pr.Traffic},
				{"Locations", join(pr.Location)},
				{"Distributions", join(pr.Dist)},
				{"Addons", addonNames(pr.OrderableAddons)},
			}); err != nil {
				return err
			}
			fmt.Fprintln(c.Out)
			t := &output.Table{Headers: []string{"location", "net", "gross", "hourly_net", "setup_net"}}
			for _, lp := range pr.Prices {
				t.AddRow(lp.Location, money(lp.Price.Net), money(lp.Price.Gross), money(deref(lp.Price.HourlyNet)), money(lp.PriceSetup.Net))
			}
			return p.Table(t)
		},
	}
}

var transactionKinds = []string{"server", "server_market"}

func newTransactionListCommand(c *cli.CLI) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List order transactions of the last 30 days",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kinds := transactionKinds
			if kind != "all" {
				if !slices.Contains(transactionKinds, kind) {
					return fmt.Errorf("invalid --kind %q (server, server_market or all)", kind)
				}
				kinds = []string{kind}
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			all := []client.Transaction{}
			for _, k := range kinds {
				txs, err := cl.ListTransactions(cmd.Context(), k)
				if err != nil {
					return err
				}
				all = append(all, txs...)
			}
			sort.SliceStable(all, func(i, j int) bool { return all[i].Date > all[j].Date })
			t := &output.Table{Headers: []string{"id", "kind", "date", "status", "product", "server", "server_ip"}}
			for _, x := range all {
				server := ""
				if x.ServerNumber != nil {
					server = strconv.Itoa(*x.ServerNumber)
				}
				t.AddRow(x.ID, x.Kind, x.Date, x.Status, string(x.Product.ID)+" "+x.Product.Name, server, deref(x.ServerIP))
			}
			return p.Print(all, t)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "all", "server, server_market or all")
	_ = cmd.RegisterFlagCompletionFunc("kind", cobra.FixedCompletions(append([]string{"all"}, transactionKinds...), cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func newTransactionDescribeCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:     "describe <transaction-id>",
		Aliases: []string{"show", "get"},
		Short:   "Show an order transaction (standard or server market)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			var x *client.Transaction
			for _, k := range transactionKinds {
				x, err = cl.GetTransaction(cmd.Context(), k, args[0])
				if err == nil || !client.IsNotFound(err) {
					break
				}
			}
			if err != nil {
				if client.IsNotFound(err) {
					return errors.New("transaction " + args[0] + " not found (only the last 30 days are available)")
				}
				return err
			}
			server := ""
			if x.ServerNumber != nil {
				server = strconv.Itoa(*x.ServerNumber)
			}
			return p.KeyValues(x, [][2]string{
				{"ID", x.ID},
				{"Kind", x.Kind},
				{"Date", x.Date},
				{"Status", x.Status},
				{"Product", strings.TrimSpace(string(x.Product.ID) + " " + x.Product.Name)},
				{"Description", strings.Join(x.Product.Description, "; ")},
				{"Distribution", string(x.Product.Dist)},
				{"Location", deref(x.Product.Location) + x.Product.Datacenter},
				{"Addons", join(x.Addons)},
				{"Server", server},
				{"Server IP", deref(x.ServerIP)},
				{"Authorized keys", keyFingerprints(x.AuthorizedKey)},
				{"Host keys", keyFingerprints(x.HostKey)},
				{"Comment", deref(x.Comment)},
			})
		},
	}
}
