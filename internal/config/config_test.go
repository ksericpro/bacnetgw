package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadYAMLAndEnvOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
gateway_id: "exhibition-gw-test"
site_name: "Booth 12"
network:
  subnet: "192.168.0.0/24"
  port: 47809
  reuse_port: true
  broadcast_overrides:
    - "192.168.10.255:47808"
polling:
  poll_interval_sec: 10
  subscribe_cov: true
imops:
  base_url: "https://imops.example.com"
  auth_type: "bearer"
  bearer_token: "yaml-token"
  extra_headers:
    X-Tenant-ID: "zaarr-dte"
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	t.Setenv("IMOPS_BEARER_TOKEN", "env-override-token")

	cfg, err := Load(yamlPath, "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.GatewayID != "exhibition-gw-test" {
		t.Fatalf("unexpected gateway_id %q", cfg.GatewayID)
	}
	if cfg.Network.Port != 47809 {
		t.Fatalf("unexpected port %d", cfg.Network.Port)
	}
	if cfg.Network.EffectiveSubnet() != "192.168.0.0/24" {
		t.Fatalf("unexpected effective subnet %q", cfg.Network.EffectiveSubnet())
	}
	if len(cfg.Network.BroadcastOverrides) != 1 || cfg.Network.BroadcastOverrides[0] != "192.168.10.255:47808" {
		t.Fatalf("unexpected broadcast_overrides: %+v", cfg.Network.BroadcastOverrides)
	}
	if cfg.IMOPS.BearerToken != "env-override-token" {
		t.Fatalf("expected env override token, got %q", cfg.IMOPS.BearerToken)
	}
	if cfg.IMOPS.ExtraHeaders["X-Tenant-ID"] != "zaarr-dte" {
		t.Fatalf("missing extra header X-Tenant-ID: %+v", cfg.IMOPS.ExtraHeaders)
	}
}

