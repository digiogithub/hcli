// Command hcli is a command-line client for the Hetzner Cloud API and the
// Hetzner Robot (dedicated servers) webservice.
package main

import (
	"os"

	"github.com/digiogithub/hcli/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
