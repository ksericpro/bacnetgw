package imops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
)

// DeviceRegistration describes a discovered BACnet controller sent to IMOPS.
type DeviceRegistration struct {
	DeviceID            uint32    `json:"device_id"`
	ObjectName          string    `json:"object_name"`
	ModelName           string    `json:"model_name,omitempty"`
	VendorName          string    `json:"vendor_name,omitempty"`
	VendorID            uint32    `json:"vendor_id"`
	FirmwareRevision    string    `json:"firmware_revision,omitempty"`
	Description         string    `json:"description,omitempty"`
	Location            string    `json:"location,omitempty"`
	ControllerRole      string    `json:"controller_role,omitempty"`
	Address             string    `json:"address"`
	Online              bool      `json:"online"`
	CommunicationStatus string    `json:"communication_status,omitempty"` // "online" | "communication_failure"
	PointCount          int       `json:"point_count"`
	DiscoveredAt        time.Time `json:"discovered_at"`
}

// DeviceEnvelope wraps a device registration POST body.
type DeviceEnvelope struct {
	GatewayID string             `json:"gateway_id"`
	SiteName  string             `json:"site_name"`
	SentAt    time.Time          `json:"sent_at"`
	Device    DeviceRegistration `json:"device"`
}

// TelemetryReading represents a normalized BACnet point reading ready for IMOPS ingestion.
type TelemetryReading struct {
	Timestamp        time.Time          `json:"timestamp"`
	SourceType       string             `json:"source_type"` // "poll" | "cov"
	DeviceID         uint32             `json:"device_id"`
	DeviceName       string             `json:"device_name"`
	ControllerModel  string             `json:"controller_model,omitempty"`
	DeviceAddress    string             `json:"device_address"`
	ObjectType       string             `json:"object_type"`
	ObjectInstance   uint32             `json:"object_instance"`
	ObjectID         string             `json:"object_id"`
	ShortObjectID    string             `json:"short_object_id"`            // e.g. "AI-101", "AV-27126", "BI-1".."BI-7"
	PointName        string             `json:"point_name"`
	PropertyLabel    string             `json:"property_label,omitempty"`   // e.g. "Temperature", "Water detected"
	DashboardLabel   string             `json:"dashboard_label,omitempty"`  // e.g. "BW-INT-M", "WD3", "Fire Detection Sensor"
	Description      string             `json:"description,omitempty"`
	EquipmentProfile string             `json:"equipment_profile,omitempty"`
	ConnectionBus    string             `json:"connection_bus,omitempty"`
	Value            any                `json:"value"`
	ValueText        string             `json:"value_text,omitempty"`
	Unit             string             `json:"unit,omitempty"`
	UnitID           *uint32            `json:"unit_id,omitempty"`
	IsTriggered      bool               `json:"is_triggered"`               // True when dry contact / threshold is active
	IsAlarm          bool               `json:"is_alarm"`                   // True when alarm dry contact (BI-1, BI-3..BI-7) or StatusFlags.InAlarm is active
	Quality          string             `json:"quality"`                    // "good" | "alarm" | "fault" | "overridden" | "out_of_service"
	StatusFlags      bacnet.StatusFlags `json:"status_flags"`
}

// TelemetryBatchEnvelope wraps a batch of telemetry readings posted to IMOPS.
type TelemetryBatchEnvelope struct {
	GatewayID string             `json:"gateway_id"`
	SiteName  string             `json:"site_name"`
	BatchID   string             `json:"batch_id"`
	SentAt    time.Time          `json:"sent_at"`
	Count     int                `json:"count"`
	Readings  []TelemetryReading `json:"readings"`
}

// PacketBatchEnvelope wraps decoded BACnet wire packet events posted to IMOPS.
type PacketBatchEnvelope struct {
	GatewayID string               `json:"gateway_id"`
	SiteName  string               `json:"site_name"`
	SentAt    time.Time            `json:"sent_at"`
	Count     int                  `json:"count"`
	Packets   []bacnet.PacketEvent `json:"packets"`
}

// Stats tracks IMOPS delivery metrics for diagnostics and health checks.
type Stats struct {
	Enabled         bool   `json:"enabled"`
	AuthType        string `json:"auth_type,omitempty"`
	BatchesSent     uint64 `json:"batches_sent"`
	ReadingsSent    uint64 `json:"readings_sent"`
	DevicesSent     uint64 `json:"devices_sent"`
	PacketBatches   uint64 `json:"packet_batches_sent"`
	FailedRequests  uint64 `json:"failed_requests"`
	DryRunBatches   uint64 `json:"dry_run_batches"`
	LastSuccessTime string `json:"last_success_time,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

// Client manages batched, authenticated HTTP delivery to the external IMOPS API.
type Client struct {
	gatewayID  string
	siteName   string
	cfg        config.IMOPSConfig
	httpClient *http.Client
	logger     *slog.Logger

	batchSeq atomic.Uint64

	tokenMu     sync.Mutex
	cachedToken string

	mu          sync.Mutex
	readingBuf  []TelemetryReading
	packetBuf   []bacnet.PacketEvent
	lastSuccess time.Time
	lastErr     string

	batchesSent    atomic.Uint64
	readingsSent   atomic.Uint64
	devicesSent    atomic.Uint64
	packetBatches  atomic.Uint64
	failedReqs     atomic.Uint64
	dryRunBatches  atomic.Uint64

	flushCh   chan struct{}
	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// NewClient initializes and starts the background IMOPS batch dispatcher.
func NewClient(gatewayID, siteName string, cfg config.IMOPSConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	timeout := time.Duration(cfg.HTTPTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if cfg.BatchMaxPoints <= 0 {
		cfg.BatchMaxPoints = 100
	}
	if cfg.FlushIntervalMS <= 0 {
		cfg.FlushIntervalMS = 2000
	}

	c := &Client{
		gatewayID: gatewayID,
		siteName:  siteName,
		cfg:       cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger:  logger,
		flushCh: make(chan struct{}, 1),
		closed:  make(chan struct{}),
	}

	if !cfg.Enabled {
		logger.Info("IMOPS API forwarding is disabled (set imops.enabled=true or IMOPS_ENABLED=true to enable)")
	}

	c.wg.Add(1)
	go c.flushLoop()
	return c
}

// EnqueueTelemetry appends readings to the outbound buffer and triggers a flush if BatchMaxPoints is reached.
func (c *Client) EnqueueTelemetry(readings ...TelemetryReading) {
	if !c.cfg.Enabled || len(readings) == 0 {
		return
	}
	c.mu.Lock()
	c.readingBuf = append(c.readingBuf, readings...)
	shouldFlush := len(c.readingBuf) >= c.cfg.BatchMaxPoints
	c.mu.Unlock()

	if shouldFlush {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
}

// EnqueuePacketEvent queues a decoded BACnet packet event for forwarding to IMOPS.
func (c *Client) EnqueuePacketEvent(ev bacnet.PacketEvent) {
	if !c.cfg.Enabled {
		return
	}
	c.mu.Lock()
	c.packetBuf = append(c.packetBuf, ev)
	shouldFlush := len(c.packetBuf) >= c.cfg.BatchMaxPoints
	c.mu.Unlock()

	if shouldFlush {
		select {
		case c.flushCh <- struct{}{}:
		default:
		}
	}
}

// SendDeviceRegistration immediately posts a discovered/updated BACnet device profile to IMOPS.
func (c *Client) SendDeviceRegistration(ctx context.Context, reg DeviceRegistration) error {
	if !c.cfg.Enabled {
		return nil
	}
	env := DeviceEnvelope{
		GatewayID: c.gatewayID,
		SiteName:  c.siteName,
		SentAt:    time.Now().UTC(),
		Device:    reg,
	}
	if err := c.postJSON(ctx, c.cfg.DevicesPath, env); err != nil {
		return err
	}
	c.devicesSent.Add(1)
	return nil
}

// Flush drains all buffered telemetry and packet events immediately.
func (c *Client) Flush(ctx context.Context) {
	c.mu.Lock()
	readings := c.readingBuf
	c.readingBuf = nil
	packets := c.packetBuf
	c.packetBuf = nil
	c.mu.Unlock()

	maxBatch := c.cfg.BatchMaxPoints
	for start := 0; start < len(readings); start += maxBatch {
		end := start + maxBatch
		if end > len(readings) {
			end = len(readings)
		}
		chunk := readings[start:end]
		seq := c.batchSeq.Add(1)
		now := time.Now().UTC()
		env := TelemetryBatchEnvelope{
			GatewayID: c.gatewayID,
			SiteName:  c.siteName,
			BatchID:   fmt.Sprintf("%s-%d", now.Format("20060102T150405Z"), seq),
			SentAt:    now,
			Count:     len(chunk),
			Readings:  chunk,
		}
		if err := c.postJSON(ctx, c.cfg.TelemetryPath, env); err != nil {
			c.logger.Error("failed to send telemetry batch to IMOPS", "count", len(chunk), "err", err)
		} else {
			c.batchesSent.Add(1)
			c.readingsSent.Add(uint64(len(chunk)))
		}
	}

	for start := 0; start < len(packets); start += maxBatch {
		end := start + maxBatch
		if end > len(packets) {
			end = len(packets)
		}
		chunk := packets[start:end]
		env := PacketBatchEnvelope{
			GatewayID: c.gatewayID,
			SiteName:  c.siteName,
			SentAt:    time.Now().UTC(),
			Count:     len(chunk),
			Packets:   chunk,
		}
		if err := c.postJSON(ctx, c.cfg.PacketEventsPath, env); err != nil {
			c.logger.Error("failed to send packet batch to IMOPS", "count", len(chunk), "err", err)
		} else {
			c.packetBatches.Add(1)
		}
	}
}

func (c *Client) flushLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(time.Duration(c.cfg.FlushIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.closed:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			c.Flush(ctx)
			cancel()
			return
		case <-c.flushCh:
			c.Flush(context.Background())
		case <-ticker.C:
			c.Flush(context.Background())
		}
	}
}

// Close flushes remaining telemetry and stops the background worker.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.wg.Wait()
	})
}

// SnapshotStats returns current IMOPS delivery statistics.
func (c *Client) SnapshotStats() Stats {
	c.mu.Lock()
	var lastSucc string
	if !c.lastSuccess.IsZero() {
		lastSucc = c.lastSuccess.Format(time.RFC3339)
	}
	lastErr := c.lastErr
	c.mu.Unlock()

	return Stats{
		Enabled:         c.cfg.Enabled,
		AuthType:        c.cfg.AuthType,
		BatchesSent:     c.batchesSent.Load(),
		ReadingsSent:    c.readingsSent.Load(),
		DevicesSent:     c.devicesSent.Load(),
		PacketBatches:   c.packetBatches.Load(),
		FailedRequests:  c.failedReqs.Load(),
		DryRunBatches:   c.dryRunBatches.Load(),
		LastSuccessTime: lastSucc,
		LastError:       lastErr,
	}
}

func (c *Client) postJSON(ctx context.Context, endpointPath string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal IMOPS payload: %w", err)
	}

	base := strings.TrimRight(strings.TrimSpace(c.cfg.BaseURL), "/")
	if base == "" {
		c.dryRunBatches.Add(1)
		if c.cfg.DryRunLogPayloads {
			c.logger.Info("IMOPS dry-run (set imops.base_url / IMOPS_BASE_URL to enable live HTTP POST)",
				"endpoint", endpointPath,
				"bytes", len(payload),
				"payload_preview", truncatePreview(string(payload), 420),
			)
		}
		return nil
	}

	path := endpointPath
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	fullURL := base + path

	attempts := c.cfg.MaxRetries + 1
	if attempts < 1 {
		attempts = 1
	}
	baseBackoff := time.Duration(c.cfg.RetryBaseBackoffM) * time.Millisecond
	if baseBackoff <= 0 {
		baseBackoff = 500 * time.Millisecond
	}

	var lastErr error
	refreshedLogin := false
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := baseBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build IMOPS request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "zaarr-bacnet-gw/1.0")
		if authErr := c.applyAuthHeaders(ctx, base, req); authErr != nil {
			lastErr = authErr
			continue
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.mu.Lock()
			c.lastSuccess = time.Now().UTC()
			c.lastErr = ""
			c.mu.Unlock()
			return nil
		}

		// If using auth_type="login" and token expired (401), clear cached token and retry once immediately
		if resp.StatusCode == http.StatusUnauthorized && isLoginAuth(c.cfg.AuthType) && !refreshedLogin {
			refreshedLogin = true
			c.clearCachedToken()
			attempt--
			continue
		}

		lastErr = fmt.Errorf("IMOPS HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		// Retry only on 429 or 5xx
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			break
		}
	}

	c.failedReqs.Add(1)
	if lastErr != nil {
		c.mu.Lock()
		c.lastErr = lastErr.Error()
		c.mu.Unlock()
	}
	return lastErr
}

func isLoginAuth(authType string) bool {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "login", "jwt", "token_login":
		return true
	default:
		return false
	}
}

func (c *Client) clearCachedToken() {
	c.tokenMu.Lock()
	c.cachedToken = ""
	c.tokenMu.Unlock()
}

func (c *Client) getOrFetchLoginToken(ctx context.Context, baseURL string) (string, error) {
	c.tokenMu.Lock()
	if c.cachedToken != "" {
		tok := c.cachedToken
		c.tokenMu.Unlock()
		return tok, nil
	}
	c.tokenMu.Unlock()

	loginPath := strings.TrimSpace(c.cfg.LoginPath)
	if loginPath == "" {
		loginPath = "/api/v1/auth/login"
	}
	if !strings.HasPrefix(loginPath, "/") {
		loginPath = "/" + loginPath
	}
	bodyBytes, _ := json.Marshal(map[string]string{
		"username": c.cfg.EffectiveUsername(),
		"password": c.cfg.EffectivePassword(),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+loginPath, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("build IMOPS login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "zaarr-bacnet-gw/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("IMOPS login request failed: %w", err)
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	_ = resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("IMOPS login HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse IMOPS login response: %w", err)
	}
	tok := extractTokenString(parsed)
	if tok == "" {
		return "", fmt.Errorf("IMOPS login response did not include token/access_token")
	}

	c.tokenMu.Lock()
	c.cachedToken = tok
	c.tokenMu.Unlock()
	return tok, nil
}

func extractTokenString(m map[string]any) string {
	for _, key := range []string{"token", "access_token", "accessToken", "jwt"} {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if dataMap, ok := m["data"].(map[string]any); ok {
		return extractTokenString(dataMap)
	}
	return ""
}

func (c *Client) applyAuthHeaders(ctx context.Context, baseURL string, req *http.Request) error {
	switch strings.ToLower(strings.TrimSpace(c.cfg.AuthType)) {
	case "basic", "username_password", "userpass":
		user := c.cfg.EffectiveUsername()
		pass := c.cfg.EffectivePassword()
		if user != "" || pass != "" {
			req.SetBasicAuth(user, pass)
		}
	case "login", "jwt", "token_login":
		tok, err := c.getOrFetchLoginToken(ctx, baseURL)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	case "bearer":
		if c.cfg.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.cfg.BearerToken)
		}
	case "api_key", "apikey":
		hdr := c.cfg.APIKeyHeader
		if hdr == "" {
			hdr = "X-API-Key"
		}
		if c.cfg.APIKey != "" {
			req.Header.Set(hdr, c.cfg.APIKey)
		}
	}

	for k, v := range c.cfg.ExtraHeaders {
		if k != "" && v != "" {
			req.Header.Set(k, v)
		}
	}
	return nil
}

func truncatePreview(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

