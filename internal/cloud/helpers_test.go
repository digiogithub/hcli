package cloud

import (
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
)

func TestParseLabels(t *testing.T) {
	got, err := parseLabels([]string{"env=prod", "empty="})
	if err != nil || got["env"] != "prod" || got["empty"] != "" || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseLabels([]string{"novalue"}); err == nil {
		t.Fatal("want error for label without =")
	}
}

func TestRuleFromSchemaAndMatch(t *testing.T) {
	port := "22"
	r, err := ruleFromSchema(schema.FirewallRule{Direction: "in", Protocol: "tcp", Port: &port, SourceIPs: []string{"0.0.0.0/0", "192.0.2.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.SourceIPs) != 2 || r.SourceIPs[1].String() != "192.0.2.1/32" {
		t.Fatalf("source ips = %v", r.SourceIPs)
	}
	nets, _ := parseNets([]string{"192.0.2.1/32", "0.0.0.0/0"})
	other := hcloud.FirewallRule{Direction: "in", Protocol: "tcp", Port: &port, SourceIPs: nets}
	if !sameRule(r, other) {
		t.Error("rules with the same IPs in another order should match")
	}
	if _, err := ruleFromSchema(schema.FirewallRule{SourceIPs: []string{"bogus"}}); err == nil {
		t.Error("want error for invalid CIDR")
	}
}
