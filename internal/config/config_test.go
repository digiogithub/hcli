package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadAndResolve(t *testing.T) {
	for _, k := range []string{"HCLI_CONTEXT", "HCLOUD_TOKEN", "HCLOUD_ENDPOINT", "HROBOT_USER", "HROBOT_PASSWORD", "HROBOT_ENDPOINT"} {
		t.Setenv(k, "")
	}
	path := filepath.Join(t.TempDir(), "hcli", "config.toml")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Upsert(&Context{Name: "a", CloudToken: "tok-a"})
	cfg.Upsert(&Context{Name: "b", RobotUser: "#ws+b", RobotPassword: "pw"})
	cfg.ActiveContext = "a"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("config mode = %v, want 0600", st.Mode().Perm())
		}
	}

	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("names = %v", got)
	}

	eff, err := cfg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if eff.CloudToken != "tok-a" || eff.CloudEndpoint != DefaultCloudEndpoint || eff.RobotEndpoint != DefaultRobotEndpoint {
		t.Fatalf("active context not resolved: %+v", eff)
	}

	// flag beats active context, env beats the file
	t.Setenv("HROBOT_PASSWORD", "from-env")
	eff, err = cfg.Resolve("b")
	if err != nil {
		t.Fatal(err)
	}
	if eff.RobotUser != "#ws+b" || eff.RobotPassword != "from-env" || eff.CloudToken != "" {
		t.Fatalf("override not applied: %+v", eff)
	}
	if cfg.Get("b").RobotPassword != "pw" {
		t.Fatal("Resolve must not modify the stored context")
	}

	if _, err := cfg.Resolve("missing"); err == nil {
		t.Fatal("expected error for unknown context")
	}
	if !cfg.Delete("a") || cfg.ActiveContext != "" {
		t.Fatal("delete should clear the active context")
	}
}
