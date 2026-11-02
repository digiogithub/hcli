package robot

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/hcli/internal/cli"
	"github.com/digiogithub/hcli/internal/output"
	"github.com/digiogithub/hcli/internal/robot/client"
	"github.com/spf13/cobra"
)

func newSSHKeyCommand(c *cli.CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "ssh-key", Aliases: []string{"ssh-keys", "key", "keys"}, Short: "SSH keys stored in Robot (for rescue/installs)"}

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List SSH keys",
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
			keys, err := cl.ListKeys(cmd.Context())
			if err != nil {
				return err
			}
			t := &output.Table{Headers: []string{"name", "fingerprint", "type", "size", "created_at"}}
			for _, k := range keys {
				t.AddRow(k.Name, k.Fingerprint, k.Type, k.Size, k.CreatedAt)
			}
			return p.Print(keys, t)
		},
	}

	describe := &cobra.Command{
		Use:               "describe <fingerprint|name>",
		Aliases:           []string{"show", "get"},
		Short:             "Show an SSH key",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeKeys, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			fp, err := keyFingerprint(cmd, cl, args[0])
			if err != nil {
				return err
			}
			k, err := cl.GetKey(cmd.Context(), fp)
			if err != nil {
				return err
			}
			return p.KeyValues(k, keyPairs(k))
		},
	}

	var name, file string
	create := &cobra.Command{
		Use:   "create --name <name> --public-key-from-file <file.pub>",
		Short: "Upload a public SSH key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			key := strings.TrimSpace(string(data))
			if strings.Contains(key, "PRIVATE KEY") {
				return errors.New("refusing to upload: the file looks like a PRIVATE key, pass the .pub file")
			}
			if key == "" {
				return errors.New("the public key file is empty")
			}
			if name == "" {
				// Default to the key comment (third field) like ssh-keygen does.
				if f := strings.Fields(key); len(f) >= 3 {
					name = strings.Join(f[2:], " ")
				} else {
					return errors.New("--name is required (the key has no comment)")
				}
			}
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			p, err := c.Printer()
			if err != nil {
				return err
			}
			k, err := cl.CreateKey(cmd.Context(), name, key)
			if err != nil {
				return err
			}
			return p.KeyValues(k, keyPairs(k))
		},
	}
	create.Flags().StringVar(&name, "name", "", "key name (default: the key comment)")
	create.Flags().StringVar(&file, "public-key-from-file", "", "path of the public key (e.g. ~/.ssh/id_ed25519.pub)")
	_ = create.MarkFlagRequired("public-key-from-file")
	_ = create.MarkFlagFilename("public-key-from-file", "pub")

	del := &cobra.Command{
		Use:               "delete <fingerprint|name>",
		Aliases:           []string{"rm"},
		Short:             "Delete an SSH key (asks for confirmation)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: complete(c, completeKeys, 0),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(c)
			if err != nil {
				return err
			}
			fp, err := keyFingerprint(cmd, cl, args[0])
			if err != nil {
				return err
			}
			if err := c.Confirm("delete SSH key " + fp); err != nil {
				return err
			}
			if err := cl.DeleteKey(cmd.Context(), fp); err != nil {
				return err
			}
			fmt.Fprintf(c.Out, "SSH key %s deleted\n", fp)
			return nil
		},
	}
	cmd.AddCommand(list, describe, create, del)
	return cmd
}

// keyFingerprint accepts an MD5 fingerprint (aa:bb:...) or a key name.
func keyFingerprint(cmd *cobra.Command, cl *client.Client, ref string) (string, error) {
	if strings.Count(ref, ":") == 15 {
		return ref, nil
	}
	keys, err := cl.ListKeys(cmd.Context())
	if err != nil {
		return "", err
	}
	var found []string
	for _, k := range keys {
		if k.Name == ref {
			found = append(found, k.Fingerprint)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no SSH key named %q", ref)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("SSH key name %q is ambiguous: use the fingerprint (%s)", ref, join(found))
}

func keyPairs(k *client.SSHKey) [][2]string {
	return [][2]string{
		{"Name", k.Name},
		{"Fingerprint", k.Fingerprint},
		{"Type", k.Type},
		{"Size", fmt.Sprint(k.Size)},
		{"Created", k.CreatedAt},
		{"Data", k.Data},
	}
}
