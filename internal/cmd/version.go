package cmd

import (
	"runtime"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/version"
	"github.com/spf13/cobra"
)

type versionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
	Go      string `json:"go"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

func newVersionCommand(c *cli.CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			p, err := c.Printer()
			if err != nil {
				return err
			}
			v := versionInfo{
				Version: version.Get(), Commit: version.Commit, Date: version.Date,
				Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH,
			}
			return p.KeyValues(v, [][2]string{
				{"Version", v.Version}, {"Commit", v.Commit}, {"Built", v.Date},
				{"Go", v.Go}, {"Platform", v.OS + "/" + v.Arch},
			})
		},
	}
}
