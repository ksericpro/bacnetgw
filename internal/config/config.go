package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bacnet-gw/internal/bacnet"
)

// Config holds the complete configuration for the BACnet-to-IMOPS gateway.
type Config struct {
	GatewayID string        `json:"gateway_id"`
	SiteName  string        `json:"site_name"`
	LogLevel  string        `json:"log_level"`
	StatusAPI StatusAPICfg  `json:"status_api"`
	Network   NetworkConfig `json:"network"`
	Polling   PollingConfig `json:"polling"`
	Points    []PointConfig `json:"points,omitempty"`
	IMOPS     IMOPSConfig   `json:"imops"`
}

// PointConfig maps a monitored BACnet object in config.yaml without recompiling.
type PointConfig struct {
	DeviceID        uint32 `json:"device_id"`                   // Target BACnet Device ID (e.g. 864077 or 300; 0 = match default controller)
	ControllerIP    string `json:"controller_ip,omitempty"`     // Optional controller IP for direct unicast Who-Is (e.g. "192.168.0.160")
	Object          string `json:"object"`                      // BACnet object shorthand, e.g. "AI-101", "AV-27126", "BI-1"
	SensorName      string `json:"sensor_name,omitempty"`       // Equipment / sensor name, e.g. "WD3 (WATER LEAK)"
	DashboardLabel  string `json:"dashboard_label,omitempty"`   // Short card title on dashboard, e.g. "WD3"
	PropertyLabel   string `json:"property_label,omitempty"`    // Monitored property label, e.g. "Water detected"
	PhysicalInput   string `json:"physical_input,omitempty"`    // Wiring / comm bus, e.g. "IN 1 • Dry Contact (LED 1)"
	Unit            string `json:"unit,omitempty"`              // Optional unit symbol or ASHRAE unit ID, e.g. "°C", "%RH", "ppm", "%"
	InactiveText    string `json:"inactive_text,omitempty"`     // Optional binary state=0 text, e.g. "Normal"
	ActiveText      string `json:"active_text,omitempty"`       // Optional binary state=1 text, e.g. "Water Detected"
	AlarmOnActive   bool   `json:"alarm_on_active"`             // Trigger critical alarm when binary input is Active (1)
	TriggerOnActive *bool  `json:"trigger_on_active,omitempty"` // Optional override for triggered state when Active
}

// StatusAPICfg configures the local diagnostic/health HTTP endpoint.
type StatusAPICfg struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listen_addr"`
}

// NetworkConfig configures BACnet/IP subnet binding, broadcast discovery, and optional BBMD.
type NetworkConfig struct {
	Subnet             string   `json:"subnet"`               // Optional target subnet CIDR, IP, prefix, or NIC name (e.g. "192.168.0.0/24")
	Interface          string   `json:"interface"`            // Optional NIC name (empty = auto-detect all IPv4 subnets)
	BindIP             string   `json:"bind_ip"`              // Default "0.0.0.0"
	Port               int      `json:"port"`                 // Default 47808 (0xBAC0)
	ReusePort          bool     `json:"reuse_port"`           // Allow sharing port 47808 via SO_REUSEADDR
	BroadcastOverrides []string `json:"broadcast_overrides"`  // Optional explicit broadcast targets e.g. ["192.168.1.255:47808"]
	BBMDAddress        string   `json:"bbmd_address"`         // Optional BBMD "ip:47808" for cross-VLAN Foreign Device mode
	BBMDTTLSeconds     uint16   `json:"bbmd_ttl_seconds"`     // Foreign device TTL (default 120s)
	RequestTimeoutMS   int      `json:"request_timeout_ms"`   // Per-request APDU timeout (default 3000ms)
	MaxRetries         int      `json:"max_retries"`          // APDU retries (default 2)
	IncludeRawHex      bool     `json:"include_raw_hex"`      // Attach raw hex payload to packet events
}

// EffectiveSubnet returns the configured subnet selector (preferring Subnet over legacy Interface).
func (n NetworkConfig) EffectiveSubnet() string {
	if s := strings.TrimSpace(n.Subnet); s != "" {
		return s
	}
	return strings.TrimSpace(n.Interface)
}

// PollingConfig configures BACnet discovery, object polling, COV subscriptions, and packet forwarding.
type PollingConfig struct {
	DiscoveryIntervalSec   int      `json:"discovery_interval_sec"`    // Who-Is broadcast interval (default 60s)
	MetadataRefreshSec     int      `json:"metadata_refresh_sec"`      // Re-scan Object_List interval (default 600s)
	PollIntervalSec        int      `json:"poll_interval_sec"`         // Present_Value read interval (default 5s)
	RPMBatchSize           int      `json:"rpm_batch_size"`            // Max objects per ReadPropertyMultiple request (default 8)
	SubscribeCOV           bool     `json:"subscribe_cov"`             // Subscribe to Unconfirmed COV on binary/analog inputs
	COVLifetimeSec         uint32   `json:"cov_lifetime_sec"`          // COV subscription lifetime (default 300s)
	ForwardPacketEvents    bool     `json:"forward_packet_events"`     // Forward decoded BACnet packet stream to IMOPS events endpoint
	OnlyCatalogPoints      bool     `json:"only_catalog_points"`       // Monitor only production points from Optergy BACnet Points.xlsx (default true)
	ExtraObjectIDs         []string `json:"extra_object_ids"`          // Additional BACnet objects to poll, e.g. ["AI-301", "BI-8"]
	AllowedDeviceIDs       []uint32 `json:"allowed_device_ids"`        // Empty = allow all discovered controllers (or e.g. [864077])
}

// IMOPSConfig configures the external IMOPS HTTP API client.
type IMOPSConfig struct {
	Enabled           bool              `json:"enabled"`            // true = push to IMOPS; false = disable IMOPS completely
	BaseURL           string            `json:"base_url"`
	TelemetryPath     string            `json:"telemetry_path"`
	DevicesPath       string            `json:"devices_path"`
	PacketEventsPath  string            `json:"packet_events_path"`
	LoginPath         string            `json:"login_path"`         // Token login path when auth_type="login" (default "/api/v1/auth/login")
	AuthType          string            `json:"auth_type"`          // "basic" (username+password) | "login" (POST username+password -> token) | "bearer" | "api_key" | "none"
	Username          string            `json:"username"`           // IMOPS username
	Password          string            `json:"password"`           // IMOPS password
	APIKeyHeader      string            `json:"api_key_header"`     // Default "X-API-Key"
	APIKey            string            `json:"api_key"`
	BearerToken       string            `json:"bearer_token"`
	BasicUsername     string            `json:"basic_username"`     // Legacy alias for username
	BasicPassword     string            `json:"basic_password"`     // Legacy alias for password
	ExtraHeaders      map[string]string `json:"extra_headers"`
	BatchMaxPoints    int               `json:"batch_max_points"`   // Max readings per HTTP POST (default 100)
	FlushIntervalMS   int               `json:"flush_interval_ms"`
	HTTPTimeoutMS     int               `json:"http_timeout_ms"`
	MaxRetries        int               `json:"max_retries"`
	RetryBaseBackoffM int               `json:"retry_base_backoff_ms"`
	DryRunLogPayloads bool              `json:"dry_run_log_payloads"` // Log outbound JSON when BaseURL is not set yet
}

// EffectiveUsername returns Username (falling back to legacy BasicUsername).
func (c IMOPSConfig) EffectiveUsername() string {
	if c.Username != "" {
		return c.Username
	}
	return c.BasicUsername
}

// EffectivePassword returns Password (falling back to legacy BasicPassword).
func (c IMOPSConfig) EffectivePassword() string {
	if c.Password != "" {
		return c.Password
	}
	return c.BasicPassword
}

// Default returns production-ready defaults for same-subnet Optergy BACnet discovery & IMOPS delivery.
func Default() Config {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "bacnet-gw-01"
	}
	return Config{
		GatewayID: hostname,
		SiteName:  "Optergy Exhibition System",
		LogLevel:  "info",
		StatusAPI: StatusAPICfg{
			Enabled:    true,
			ListenAddr: "127.0.0.1:8090",
		},
		Network: NetworkConfig{
			Interface:        "",
			BindIP:           "0.0.0.0",
			Port:             47808,
			ReusePort:        true,
			BBMDTTLSeconds:   120,
			RequestTimeoutMS: 3000,
			MaxRetries:       2,
			IncludeRawHex:    false,
		},
		Polling: PollingConfig{
			DiscoveryIntervalSec: 60,
			MetadataRefreshSec:   600,
			PollIntervalSec:      5,
			RPMBatchSize:         8,
			SubscribeCOV:         true,
			COVLifetimeSec:       300,
			ForwardPacketEvents:  false,
			OnlyCatalogPoints:    true,
		},
		IMOPS: IMOPSConfig{
			Enabled:           true,
			BaseURL:           "",
			TelemetryPath:     "/api/v1/bacnet/telemetry",
			DevicesPath:       "/api/v1/bacnet/devices",
			PacketEventsPath:  "/api/v1/bacnet/packets",
			LoginPath:         "/api/v1/auth/login",
			AuthType:          "basic",
			APIKeyHeader:      "X-API-Key",
			ExtraHeaders:      map[string]string{},
			BatchMaxPoints:    100,
			FlushIntervalMS:   2000,
			HTTPTimeoutMS:     10000,
			MaxRetries:        3,
			RetryBaseBackoffM: 500,
			DryRunLogPayloads: true,
		},
	}
}

// Load reads an optional .env file, an optional YAML/JSON config file, and environment overrides.
func Load(configPath string, envFilePath string) (Config, error) {
	cfg := Default()

	if envFilePath != "" {
		if _, err := os.Stat(envFilePath); err == nil {
			if err := loadDotEnv(envFilePath); err != nil {
				return cfg, fmt.Errorf("load env file %s: %w", envFilePath, err)
			}
		}
	} else if _, err := os.Stat(".env"); err == nil {
		_ = loadDotEnv(".env")
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", configPath, err)
		}
		ext := strings.ToLower(filepath.Ext(configPath))
		switch ext {
		case ".json":
			if err := json.Unmarshal(data, &cfg); err != nil {
				return cfg, fmt.Errorf("parse JSON config %s: %w", configPath, err)
			}
		case ".yaml", ".yml":
			if err := applySimpleYAML(string(data), &cfg); err != nil {
				return cfg, fmt.Errorf("parse YAML config %s: %w", configPath, err)
			}
		default:
			// Try JSON first, then YAML
			if jsonErr := json.Unmarshal(data, &cfg); jsonErr != nil {
				if yamlErr := applySimpleYAML(string(data), &cfg); yamlErr != nil {
					return cfg, fmt.Errorf("unsupported config format in %s: %v", configPath, yamlErr)
				}
			}
		}
	}

	applyEnvOverrides(&cfg)
	return cfg, cfg.Validate()
}

// Validate checks configuration values and fills safe bounds.
func (c *Config) Validate() error {
	if c.Network.Port < 0 || c.Network.Port > 65535 {
		return fmt.Errorf("invalid network.port: %d", c.Network.Port)
	}
	if c.Polling.DiscoveryIntervalSec <= 0 {
		c.Polling.DiscoveryIntervalSec = 60
	}
	if c.Polling.PollIntervalSec <= 0 {
		c.Polling.PollIntervalSec = 15
	}
	if c.Polling.RPMBatchSize <= 0 {
		c.Polling.RPMBatchSize = 8
	}
	if c.IMOPS.BatchMaxPoints <= 0 {
		c.IMOPS.BatchMaxPoints = 100
	}
	if c.IMOPS.HTTPTimeoutMS <= 0 {
		c.IMOPS.HTTPTimeoutMS = 10000
	}
	for i, pt := range c.Points {
		if strings.TrimSpace(pt.Object) == "" {
			return fmt.Errorf("points[%d]: missing required 'object' (e.g. \"AI-101\" or \"BI-1\")", i)
		}
		if _, err := bacnet.ParseShortObjectID(pt.Object); err != nil {
			return fmt.Errorf("points[%d] (%s): %w", i, pt.Object, err)
		}
	}
	return nil
}

func (c *Config) RequestTimeout() time.Duration {
	return time.Duration(c.Network.RequestTimeoutMS) * time.Millisecond
}

func (c *Config) DiscoveryInterval() time.Duration {
	return time.Duration(c.Polling.DiscoveryIntervalSec) * time.Second
}

func (c *Config) MetadataRefreshInterval() time.Duration {
	return time.Duration(c.Polling.MetadataRefreshSec) * time.Second
}

func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.Polling.PollIntervalSec) * time.Second
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := unquoteValue(strings.TrimSpace(v))
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
	return scanner.Err()
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("GATEWAY_ID"); v != "" {
		cfg.GatewayID = v
	}
	if v := os.Getenv("SITE_NAME"); v != "" {
		cfg.SiteName = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("STATUS_LISTEN_ADDR"); v != "" {
		cfg.StatusAPI.ListenAddr = v
	}
	if v, ok := envBool("STATUS_ENABLED"); ok {
		cfg.StatusAPI.Enabled = v
	}

	// Network overrides
	if v := os.Getenv("BACNET_SUBNET"); v != "" {
		cfg.Network.Subnet = v
	}
	if v := os.Getenv("BACNET_INTERFACE"); v != "" {
		cfg.Network.Interface = v
	}
	if v := os.Getenv("BACNET_BIND_IP"); v != "" {
		cfg.Network.BindIP = v
	}
	if v, ok := envInt("BACNET_PORT"); ok {
		cfg.Network.Port = v
	}
	if v, ok := envBool("BACNET_REUSE_PORT"); ok {
		cfg.Network.ReusePort = v
	}
	if v := os.Getenv("BACNET_BROADCAST_OVERRIDES"); v != "" {
		cfg.Network.BroadcastOverrides = splitCSV(v)
	}
	if v := os.Getenv("BACNET_BBMD_ADDRESS"); v != "" {
		cfg.Network.BBMDAddress = v
	}
	if v, ok := envBool("BACNET_INCLUDE_RAW_HEX"); ok {
		cfg.Network.IncludeRawHex = v
	}

	// Polling overrides
	if v, ok := envInt("BACNET_DISCOVERY_INTERVAL_SEC"); ok {
		cfg.Polling.DiscoveryIntervalSec = v
	}
	if v, ok := envInt("BACNET_POLL_INTERVAL_SEC"); ok {
		cfg.Polling.PollIntervalSec = v
	}
	if v, ok := envBool("BACNET_SUBSCRIBE_COV"); ok {
		cfg.Polling.SubscribeCOV = v
	}
	if v, ok := envBool("BACNET_FORWARD_PACKET_EVENTS"); ok {
		cfg.Polling.ForwardPacketEvents = v
	}
	if v, ok := envBool("BACNET_ONLY_CATALOG_POINTS"); ok {
		cfg.Polling.OnlyCatalogPoints = v
	}
	if v := os.Getenv("BACNET_EXTRA_OBJECT_IDS"); v != "" {
		cfg.Polling.ExtraObjectIDs = splitCSV(v)
	}

	// IMOPS overrides
	if v, ok := envBool("IMOPS_ENABLED"); ok {
		cfg.IMOPS.Enabled = v
	}
	if v := os.Getenv("IMOPS_BASE_URL"); v != "" {
		cfg.IMOPS.BaseURL = v
	}
	if v := os.Getenv("IMOPS_TELEMETRY_PATH"); v != "" {
		cfg.IMOPS.TelemetryPath = v
	}
	if v := os.Getenv("IMOPS_DEVICES_PATH"); v != "" {
		cfg.IMOPS.DevicesPath = v
	}
	if v := os.Getenv("IMOPS_PACKET_EVENTS_PATH"); v != "" {
		cfg.IMOPS.PacketEventsPath = v
	}
	if v := os.Getenv("IMOPS_LOGIN_PATH"); v != "" {
		cfg.IMOPS.LoginPath = v
	}
	if v := os.Getenv("IMOPS_AUTH_TYPE"); v != "" {
		cfg.IMOPS.AuthType = v
	}
	if v := os.Getenv("IMOPS_USERNAME"); v != "" {
		cfg.IMOPS.Username = v
	}
	if v := os.Getenv("IMOPS_PASSWORD"); v != "" {
		cfg.IMOPS.Password = v
	}
	if v := os.Getenv("IMOPS_API_KEY_HEADER"); v != "" {
		cfg.IMOPS.APIKeyHeader = v
	}
	if v := os.Getenv("IMOPS_API_KEY"); v != "" {
		cfg.IMOPS.APIKey = v
	}
	if v := os.Getenv("IMOPS_BEARER_TOKEN"); v != "" {
		cfg.IMOPS.BearerToken = v
	}
	if v := os.Getenv("IMOPS_BASIC_USERNAME"); v != "" {
		cfg.IMOPS.BasicUsername = v
	}
	if v := os.Getenv("IMOPS_BASIC_PASSWORD"); v != "" {
		cfg.IMOPS.BasicPassword = v
	}
	if v, ok := envInt("IMOPS_BATCH_MAX_POINTS"); ok {
		cfg.IMOPS.BatchMaxPoints = v
	}
	if v, ok := envBool("IMOPS_DRY_RUN_LOG_PAYLOADS"); ok {
		cfg.IMOPS.DryRunLogPayloads = v
	}
}

func envBool(key string) (bool, bool) {
	v, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(v) == "" {
		return false, false
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	return b, err == nil
}

func envInt(key string) (int, bool) {
	v, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(v) == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func stripInlineComment(s string) string {
	var inSingle, inDouble bool
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return strings.TrimSpace(s)
}

func unquoteValue(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// applySimpleYAML parses standard 2-level/3-level YAML configuration files into Config without external dependencies.
func applySimpleYAML(content string, cfg *Config) error {
	type stackEntry struct {
		indent int
		key    string
	}
	var stack []stackEntry
	var activeListPath string
	var inPointsList bool
	var pointsIndent int

	lines := strings.Split(content, "\n")
	for lineNo, rawLine := range lines {
		trimmedRight := strings.TrimRight(rawLine, "\r\n ")
		if strings.TrimSpace(trimmedRight) == "" || strings.HasPrefix(strings.TrimSpace(trimmedRight), "#") {
			continue
		}

		indent := len(trimmedRight) - len(strings.TrimLeft(trimmedRight, " "))
		clean := stripInlineComment(strings.TrimSpace(trimmedRight))
		if clean == "" {
			continue
		}

		if inPointsList {
			if indent <= pointsIndent && !strings.HasPrefix(clean, "-") {
				inPointsList = false
			} else {
				if clean == "-" || strings.HasPrefix(clean, "- ") {
					cfg.Points = append(cfg.Points, PointConfig{})
					rest := strings.TrimSpace(strings.TrimPrefix(clean, "-"))
					if rest != "" {
						pk, pv, ok := strings.Cut(rest, ":")
						if !ok {
							return fmt.Errorf("line %d: expected key: value in points item, got %q", lineNo+1, rest)
						}
						if err := setPointConfigField(&cfg.Points[len(cfg.Points)-1], strings.TrimSpace(pk), unquoteValue(strings.TrimSpace(pv))); err != nil {
							return fmt.Errorf("line %d (points.%s): %w", lineNo+1, strings.TrimSpace(pk), err)
						}
					}
					continue
				}
				if len(cfg.Points) == 0 {
					return fmt.Errorf("line %d: expected '- ' list entry under points, got %q", lineNo+1, clean)
				}
				pk, pv, ok := strings.Cut(clean, ":")
				if !ok {
					return fmt.Errorf("line %d: expected key: value in points item, got %q", lineNo+1, clean)
				}
				if err := setPointConfigField(&cfg.Points[len(cfg.Points)-1], strings.TrimSpace(pk), unquoteValue(strings.TrimSpace(pv))); err != nil {
					return fmt.Errorf("line %d (points.%s): %w", lineNo+1, strings.TrimSpace(pk), err)
				}
				continue
			}
		}

		// List item "- val"
		if strings.HasPrefix(clean, "- ") && activeListPath != "" {
			itemVal := unquoteValue(strings.TrimSpace(strings.TrimPrefix(clean, "- ")))
			if err := appendYAMLListItem(cfg, activeListPath, itemVal); err != nil {
				return fmt.Errorf("line %d: %w", lineNo+1, err)
			}
			continue
		}

		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}

		k, v, hasColon := strings.Cut(clean, ":")
		if !hasColon {
			return fmt.Errorf("line %d: expected key: value, got %q", lineNo+1, clean)
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)

		pathParts := make([]string, 0, len(stack)+1)
		for _, se := range stack {
			pathParts = append(pathParts, se.key)
		}
		pathParts = append(pathParts, key)
		fullPath := strings.Join(pathParts, ".")

		if val == "" {
			if fullPath == "points" || fullPath == "polling.points" {
				inPointsList = true
				pointsIndent = indent
				cfg.Points = []PointConfig{}
				activeListPath = ""
				continue
			}
			stack = append(stack, stackEntry{indent: indent, key: key})
			activeListPath = fullPath
			continue
		}

		activeListPath = ""
		if strings.HasPrefix(val, "[") && strings.HasSuffix(val, "]") {
			inner := strings.TrimSpace(val[1 : len(val)-1])
			resetYAMLList(cfg, fullPath)
			if inner != "" {
				for _, item := range strings.Split(inner, ",") {
					if err := appendYAMLListItem(cfg, fullPath, unquoteValue(strings.TrimSpace(item))); err != nil {
						return fmt.Errorf("line %d: %w", lineNo+1, err)
					}
				}
			}
			continue
		}
		if val == "{}" {
			continue
		}

		if err := setYAMLField(cfg, fullPath, unquoteValue(val)); err != nil {
			return fmt.Errorf("line %d (%s): %w", lineNo+1, fullPath, err)
		}
	}
	return nil
}

func resetYAMLList(cfg *Config, path string) {
	switch path {
	case "network.broadcast_overrides":
		cfg.Network.BroadcastOverrides = nil
	case "polling.allowed_device_ids":
		cfg.Polling.AllowedDeviceIDs = nil
	case "polling.extra_object_ids":
		cfg.Polling.ExtraObjectIDs = nil
	case "points", "polling.points":
		cfg.Points = []PointConfig{}
	}
}

func setPointConfigField(pt *PointConfig, key string, val string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "device_id":
		if val == "" {
			pt.DeviceID = 0
			return nil
		}
		u, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid device_id %q: %w", val, err)
		}
		pt.DeviceID = uint32(u)
	case "controller_ip":
		pt.ControllerIP = val
	case "object", "object_id", "short_id":
		pt.Object = val
	case "sensor_name", "equipment", "equipment_profile":
		pt.SensorName = val
	case "dashboard_label", "label":
		pt.DashboardLabel = val
	case "property_label", "property":
		pt.PropertyLabel = val
	case "physical_input", "connection_bus", "input":
		pt.PhysicalInput = val
	case "unit", "units":
		pt.Unit = val
	case "inactive_text":
		pt.InactiveText = val
	case "active_text":
		pt.ActiveText = val
	case "alarm_on_active":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid alarm_on_active %q: %w", val, err)
		}
		pt.AlarmOnActive = b
	case "trigger_on_active":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid trigger_on_active %q: %w", val, err)
		}
		pt.TriggerOnActive = &b
	default:
		return fmt.Errorf("unknown point field %q", key)
	}
	return nil
}

func appendYAMLListItem(cfg *Config, path string, val string) error {
	switch path {
	case "network.broadcast_overrides":
		cfg.Network.BroadcastOverrides = append(cfg.Network.BroadcastOverrides, val)
	case "polling.allowed_device_ids":
		u, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return err
		}
		cfg.Polling.AllowedDeviceIDs = append(cfg.Polling.AllowedDeviceIDs, uint32(u))
	case "polling.extra_object_ids":
		cfg.Polling.ExtraObjectIDs = append(cfg.Polling.ExtraObjectIDs, val)
	}
	return nil
}

func setYAMLField(cfg *Config, path string, val string) error {
	if strings.HasPrefix(path, "imops.extra_headers.") {
		hdrName := strings.TrimPrefix(path, "imops.extra_headers.")
		if cfg.IMOPS.ExtraHeaders == nil {
			cfg.IMOPS.ExtraHeaders = make(map[string]string)
		}
		cfg.IMOPS.ExtraHeaders[hdrName] = val
		return nil
	}

	switch path {
	case "gateway_id":
		cfg.GatewayID = val
	case "site_name":
		cfg.SiteName = val
	case "log_level":
		cfg.LogLevel = val
	case "status_api.enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.StatusAPI.Enabled = b
	case "status_api.listen_addr":
		cfg.StatusAPI.ListenAddr = val

	case "network.subnet":
		cfg.Network.Subnet = val
	case "network.interface":
		cfg.Network.Interface = val
	case "network.bind_ip":
		cfg.Network.BindIP = val
	case "network.port":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Network.Port = n
	case "network.reuse_port":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.Network.ReusePort = b
	case "network.bbmd_address":
		cfg.Network.BBMDAddress = val
	case "network.bbmd_ttl_seconds":
		u, err := strconv.ParseUint(val, 10, 16)
		if err != nil {
			return err
		}
		cfg.Network.BBMDTTLSeconds = uint16(u)
	case "network.request_timeout_ms":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Network.RequestTimeoutMS = n
	case "network.max_retries":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Network.MaxRetries = n
	case "network.include_raw_hex":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.Network.IncludeRawHex = b

	case "polling.discovery_interval_sec":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Polling.DiscoveryIntervalSec = n
	case "polling.metadata_refresh_sec":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Polling.MetadataRefreshSec = n
	case "polling.poll_interval_sec":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Polling.PollIntervalSec = n
	case "polling.rpm_batch_size":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.Polling.RPMBatchSize = n
	case "polling.subscribe_cov":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.Polling.SubscribeCOV = b
	case "polling.cov_lifetime_sec":
		u, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return err
		}
		cfg.Polling.COVLifetimeSec = uint32(u)
	case "polling.forward_packet_events":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.Polling.ForwardPacketEvents = b
	case "polling.only_catalog_points":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.Polling.OnlyCatalogPoints = b

	case "imops.enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.Enabled = b
	case "imops.base_url":
		cfg.IMOPS.BaseURL = val
	case "imops.telemetry_path":
		cfg.IMOPS.TelemetryPath = val
	case "imops.devices_path":
		cfg.IMOPS.DevicesPath = val
	case "imops.packet_events_path":
		cfg.IMOPS.PacketEventsPath = val
	case "imops.login_path":
		cfg.IMOPS.LoginPath = val
	case "imops.auth_type":
		cfg.IMOPS.AuthType = val
	case "imops.username":
		cfg.IMOPS.Username = val
	case "imops.password":
		cfg.IMOPS.Password = val
	case "imops.api_key_header":
		cfg.IMOPS.APIKeyHeader = val
	case "imops.api_key":
		cfg.IMOPS.APIKey = val
	case "imops.bearer_token":
		cfg.IMOPS.BearerToken = val
	case "imops.basic_username":
		cfg.IMOPS.BasicUsername = val
	case "imops.basic_password":
		cfg.IMOPS.BasicPassword = val
	case "imops.batch_max_points":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.BatchMaxPoints = n
	case "imops.flush_interval_ms":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.FlushIntervalMS = n
	case "imops.http_timeout_ms":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.HTTPTimeoutMS = n
	case "imops.max_retries":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.MaxRetries = n
	case "imops.retry_base_backoff_ms":
		n, err := strconv.Atoi(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.RetryBaseBackoffM = n
	case "imops.dry_run_log_payloads":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		cfg.IMOPS.DryRunLogPayloads = b
	}
	return nil
}

