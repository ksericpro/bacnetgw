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
)

// Config holds the complete configuration for the BACnet-to-IMOPS gateway.
type Config struct {
	GatewayID string        `json:"gateway_id"`
	SiteName  string        `json:"site_name"`
	LogLevel  string        `json:"log_level"`
	StatusAPI StatusAPICfg  `json:"status_api"`
	Network   NetworkConfig `json:"network"`
	Polling   PollingConfig `json:"polling"`
	IMOPS     IMOPSConfig   `json:"imops"`
}

// StatusAPICfg configures the local diagnostic/health HTTP endpoint.
type StatusAPICfg struct {
	Enabled    bool   `json:"enabled"`
	ListenAddr string `json:"listen_addr"`
}

// NetworkConfig configures BACnet/IP subnet binding, broadcast discovery, and optional BBMD.
type NetworkConfig struct {
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

// PollingConfig configures BACnet discovery, object polling, COV subscriptions, and packet forwarding.
type PollingConfig struct {
	DiscoveryIntervalSec   int      `json:"discovery_interval_sec"`    // Who-Is broadcast interval (default 60s)
	MetadataRefreshSec     int      `json:"metadata_refresh_sec"`      // Re-scan Object_List interval (default 600s)
	PollIntervalSec        int      `json:"poll_interval_sec"`         // Present_Value read interval (default 15s)
	RPMBatchSize           int      `json:"rpm_batch_size"`            // Max objects per ReadPropertyMultiple request (default 8)
	SubscribeCOV           bool     `json:"subscribe_cov"`             // Subscribe to Unconfirmed COV on binary/analog inputs
	COVLifetimeSec         uint32   `json:"cov_lifetime_sec"`          // COV subscription lifetime (default 300s)
	ForwardPacketEvents    bool     `json:"forward_packet_events"`     // Forward decoded BACnet packet stream to IMOPS events endpoint
	AllowedDeviceIDs       []uint32 `json:"allowed_device_ids"`        // Empty = integrate all discovered BACnet devices on subnet
}

// IMOPSConfig configures the external IMOPS HTTP API client.
type IMOPSConfig struct {
	Enabled           bool              `json:"enabled"`
	BaseURL           string            `json:"base_url"`
	TelemetryPath     string            `json:"telemetry_path"`
	DevicesPath       string            `json:"devices_path"`
	PacketEventsPath  string            `json:"packet_events_path"`
	AuthType          string            `json:"auth_type"`        // "none" | "bearer" | "api_key" | "basic"
	APIKeyHeader      string            `json:"api_key_header"`   // Default "X-API-Key"
	APIKey            string            `json:"api_key"`
	BearerToken       string            `json:"bearer_token"`
	BasicUsername     string            `json:"basic_username"`
	BasicPassword     string            `json:"basic_password"`
	ExtraHeaders      map[string]string `json:"extra_headers"`
	BatchMaxPoints    int               `json:"batch_max_points"` // Max readings per HTTP POST (default 100)
	FlushIntervalMS   int               `json:"flush_interval_ms"`
	HTTPTimeoutMS     int               `json:"http_timeout_ms"`
	MaxRetries        int               `json:"max_retries"`
	RetryBaseBackoffM int               `json:"retry_base_backoff_ms"`
	DryRunLogPayloads bool              `json:"dry_run_log_payloads"` // Log outbound JSON when BaseURL is not set yet
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
			PollIntervalSec:      15,
			RPMBatchSize:         8,
			SubscribeCOV:         true,
			COVLifetimeSec:       300,
			ForwardPacketEvents:  false,
		},
		IMOPS: IMOPSConfig{
			Enabled:           true,
			BaseURL:           "",
			TelemetryPath:     "/api/v1/bacnet/telemetry",
			DevicesPath:       "/api/v1/bacnet/devices",
			PacketEventsPath:  "/api/v1/bacnet/packets",
			AuthType:          "api_key",
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
	if v := os.Getenv("IMOPS_AUTH_TYPE"); v != "" {
		cfg.IMOPS.AuthType = v
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
	}
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
	case "imops.auth_type":
		cfg.IMOPS.AuthType = val
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

