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

func TestLoadPointsFromProductionAndExampleYAML(t *testing.T) {
	for _, rel := range []string{"../../config.production.yaml", "../../config.example.yaml", "../../config.sim.yaml"} {
		cfg, err := Load(rel, "")
		if err != nil {
			t.Fatalf("Load(%s) failed: %v", rel, err)
		}
		if len(cfg.Points) != 12 {
			t.Fatalf("%s: expected 12 configured points, got %d", rel, len(cfg.Points))
		}
		if cfg.Points[0].DeviceID != 864077 || cfg.Points[0].Object != "AI-101" || cfg.Points[0].Unit != "°C" {
			t.Fatalf("%s: unexpected points[0]: %+v", rel, cfg.Points[0])
		}
		if cfg.Points[5].Object != "BI-1" || !cfg.Points[5].AlarmOnActive || cfg.Points[5].ActiveText != "Water Detected" {
			t.Fatalf("%s: unexpected points[5] (BI-1): %+v", rel, cfg.Points[5])
		}
		if cfg.Points[6].Object != "BI-2" || cfg.Points[6].AlarmOnActive {
			t.Fatalf("%s: expected BI-2 alarm_on_active=false, got %+v", rel, cfg.Points[6])
		}
		if cfg.Points[11].Object != "BI-7" || cfg.Points[11].InactiveText != "Closed" {
			t.Fatalf("%s: unexpected points[11] (BI-7): %+v", rel, cfg.Points[11])
		}
	}
}

func TestLoadCDEKongAliasYAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `
cde:
  enabled: true
  base_url: "http://192.168.0.50:8088"
  telemetry_endpoint: "/api/v1/bacnet/telemetry"
  devices_endpoint: "/api/v1/bacnet/devices"
  api_key: "zaarr-bacnet-gw01-key-2026"
  auth_header: "X-API-Key"
  timeout_seconds: 10
  verify_tls: false
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cfg, err := Load(yamlPath, "")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !cfg.IMOPS.Enabled || cfg.IMOPS.BaseURL != "http://192.168.0.50:8088" {
		t.Fatalf("unexpected CDE enabled/base_url: %+v", cfg.IMOPS)
	}
	if cfg.IMOPS.AuthType != "api_key" || cfg.IMOPS.APIKey != "zaarr-bacnet-gw01-key-2026" || cfg.IMOPS.APIKeyHeader != "X-API-Key" {
		t.Fatalf("unexpected CDE auth config: %+v", cfg.IMOPS)
	}
	if cfg.IMOPS.HTTPTimeoutMS != 10000 || !cfg.IMOPS.InsecureSkipTLS() {
		t.Fatalf("unexpected timeout/verify_tls: timeout=%d skipTLS=%v", cfg.IMOPS.HTTPTimeoutMS, cfg.IMOPS.InsecureSkipTLS())
	}
}


