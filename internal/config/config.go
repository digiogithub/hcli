// Package config loads and stores hcli contexts.
//
// A context bundles the credentials for one Hetzner account: a Cloud API
// token (per Cloud project) and/or a Robot webservice user. The file lives at
// $XDG_CONFIG_HOME/hcli/config.toml (or the OS equivalent returned by
// os.UserConfigDir) and is written with 0600 permissions.
//
// Environment variables override the active context:
//
//	HCLI_CONFIG       path of the config file
//	HCLI_CONTEXT      name of the context to use
//	HCLOUD_TOKEN      Cloud API token (same name as the official hcloud CLI)
//	HCLOUD_ENDPOINT   Cloud API endpoint
//	HROBOT_USER       Robot webservice user
//	HROBOT_PASSWORD   Robot webservice password
//	HROBOT_ENDPOINT   Robot webservice endpoint
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	DefaultCloudEndpoint = "https://api.hetzner.cloud/v1"
	DefaultRobotEndpoint = "https://robot-ws.your-server.de"
)

// Context is one named set of credentials.
type Context struct {
	Name          string `toml:"name"`
	CloudToken    string `toml:"cloud_token,omitempty"`
	CloudEndpoint string `toml:"cloud_endpoint,omitempty"`
	RobotUser     string `toml:"robot_user,omitempty"`
	RobotPassword string `toml:"robot_password,omitempty"`
	RobotEndpoint string `toml:"robot_endpoint,omitempty"`
}

// Config is the on-disk file.
type Config struct {
	ActiveContext string     `toml:"active_context"`
	Contexts      []*Context `toml:"contexts"`

	path string
}

// Path returns the config file location.
func Path() (string, error) {
	if p := os.Getenv("HCLI_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hcli", "config.toml"), nil
}

// Load reads the config file from path ("" = default). A missing file is not
// an error: an empty config is returned.
func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		if path, err = Path(); err != nil {
			return nil, err
		}
	}
	cfg := &Config{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes the config file with 0600 permissions.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// FilePath is where the config is read from / written to.
func (c *Config) FilePath() string { return c.path }

// Get returns the named context or nil.
func (c *Config) Get(name string) *Context {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx
		}
	}
	return nil
}

// Names returns the sorted context names.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Contexts))
	for _, ctx := range c.Contexts {
		names = append(names, ctx.Name)
	}
	sort.Strings(names)
	return names
}

// Upsert adds or replaces a context.
func (c *Config) Upsert(ctx *Context) {
	for i, existing := range c.Contexts {
		if existing.Name == ctx.Name {
			c.Contexts[i] = ctx
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
}

// Delete removes a context and clears it as active.
func (c *Config) Delete(name string) bool {
	for i, ctx := range c.Contexts {
		if ctx.Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			if c.ActiveContext == name {
				c.ActiveContext = ""
			}
			return true
		}
	}
	return false
}

// Resolve returns the effective credentials: the selected context (flag,
// HCLI_CONTEXT or active_context) with environment variables applied on top.
// The returned value is a copy and never nil.
func (c *Config) Resolve(contextFlag string) (*Context, error) {
	name := contextFlag
	if name == "" {
		name = os.Getenv("HCLI_CONTEXT")
	}
	if name == "" {
		name = c.ActiveContext
	}
	eff := &Context{}
	if name != "" {
		ctx := c.Get(name)
		if ctx == nil {
			return nil, fmt.Errorf("context %q not found (see `hcli context list`)", name)
		}
		copied := *ctx
		eff = &copied
	}
	if v := os.Getenv("HCLOUD_TOKEN"); v != "" {
		eff.CloudToken = v
	}
	if v := os.Getenv("HCLOUD_ENDPOINT"); v != "" {
		eff.CloudEndpoint = v
	}
	if v := os.Getenv("HROBOT_USER"); v != "" {
		eff.RobotUser = v
	}
	if v := os.Getenv("HROBOT_PASSWORD"); v != "" {
		eff.RobotPassword = v
	}
	if v := os.Getenv("HROBOT_ENDPOINT"); v != "" {
		eff.RobotEndpoint = v
	}
	if eff.CloudEndpoint == "" {
		eff.CloudEndpoint = DefaultCloudEndpoint
	}
	if eff.RobotEndpoint == "" {
		eff.RobotEndpoint = DefaultRobotEndpoint
	}
	return eff, nil
}
