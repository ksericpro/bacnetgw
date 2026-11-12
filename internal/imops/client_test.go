package imops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"bacnet-gw/internal/config"
)

func TestIMOPSClientBatchingAuthAndRetry(t *testing.T) {
	var attempts atomic.Int32
	var gotAuth string
	var gotReadings int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// Simulate transient 503 on first attempt to verify exponential backoff retry
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporary overload"))
			return
		}
		gotAuth = r.Header.Get("X-API-Key")
		body, _ := io.ReadAll(r.Body)
		var env TelemetryBatchEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			gotReadings += len(env.Readings)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.IMOPSConfig{
		Enabled:           true,
		BaseURL:           srv.URL,
		TelemetryPath:     "/api/v1/bacnet/telemetry",
		AuthType:          "api_key",
		APIKeyHeader:      "X-API-Key",
		APIKey:            "secret-imops-key",
		BatchMaxPoints:    2,
		FlushIntervalMS:   5000,
		HTTPTimeoutMS:     3000,
		MaxRetries:        2,
		RetryBaseBackoffM: 25,
	}

	cli := NewClient("gw-01", "TestSite", cfg, nil)
	defer cli.Close()

	cli.EnqueueTelemetry(
		TelemetryReading{Timestamp: time.Now(), DeviceID: 1001, PointName: "PM1200_kW", Value: 14.5, Quality: "good"},
		TelemetryReading{Timestamp: time.Now(), DeviceID: 8640, PointName: "RHPLC_Temp", Value: 23.4, Quality: "good"},
		TelemetryReading{Timestamp: time.Now(), DeviceID: 4420, PointName: "WD3_Leak", Value: 0, Quality: "good"},
	)
	cli.Flush(context.Background())

	if gotAuth != "secret-imops-key" {
		t.Fatalf("expected X-API-Key header 'secret-imops-key', got %q", gotAuth)
	}
	if gotReadings != 3 {
		t.Fatalf("expected 3 readings delivered across chunks, got %d", gotReadings)
	}
	stats := cli.SnapshotStats()
	if !stats.Enabled || stats.ReadingsSent != 3 || stats.BatchesSent != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestIMOPSUsernamePasswordAndDisabledToggle(t *testing.T) {
	var hitCount atomic.Int32
	var gotUser, gotPass string
	var gotBearer string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		if r.URL.Path == "/api/v1/auth/login" {
			var creds map[string]string
			_ = json.NewDecoder(r.Body).Decode(&creds)
			if creds["username"] == "imops-admin" && creds["password"] == "s3cr3t!" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"token":"jwt-session-xyz"}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		u, p, hasBasic := r.BasicAuth()
		if hasBasic {
			gotUser, gotPass = u, p
		}
		gotBearer = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 1. Disabled mode: zero outbound HTTP calls
	disabledCli := NewClient("gw-01", "Site", config.IMOPSConfig{
		Enabled:       false,
		BaseURL:       srv.URL,
		TelemetryPath: "/api/v1/bacnet/telemetry",
		AuthType:      "basic",
		Username:      "imops-admin",
		Password:      "s3cr3t!",
	}, nil)
	disabledCli.EnqueueTelemetry(TelemetryReading{Timestamp: time.Now(), DeviceID: 864077, PointName: "AI-101", Value: 22.5})
	disabledCli.Flush(context.Background())
	disabledCli.Close()
	if hitCount.Load() != 0 {
		t.Fatalf("expected 0 HTTP requests when imops.enabled=false, got %d", hitCount.Load())
	}

	// 2. Enabled with username + password Basic Auth
	basicCli := NewClient("gw-01", "Site", config.IMOPSConfig{
		Enabled:       true,
		BaseURL:       srv.URL,
		TelemetryPath: "/api/v1/bacnet/telemetry",
		AuthType:      "basic",
		Username:      "imops-admin",
		Password:      "s3cr3t!",
	}, nil)
	basicCli.EnqueueTelemetry(TelemetryReading{Timestamp: time.Now(), DeviceID: 864077, PointName: "AI-101", Value: 22.5})
	basicCli.Flush(context.Background())
	basicCli.Close()
	if gotUser != "imops-admin" || gotPass != "s3cr3t!" {
		t.Fatalf("expected BasicAuth imops-admin/s3cr3t!, got %q / %q", gotUser, gotPass)
	}

	// 3. Enabled with username + password Token Login Auth
	loginCli := NewClient("gw-01", "Site", config.IMOPSConfig{
		Enabled:       true,
		BaseURL:       srv.URL,
		TelemetryPath: "/api/v1/bacnet/telemetry",
		LoginPath:     "/api/v1/auth/login",
		AuthType:      "login",
		Username:      "imops-admin",
		Password:      "s3cr3t!",
	}, nil)
	loginCli.EnqueueTelemetry(TelemetryReading{Timestamp: time.Now(), DeviceID: 864077, PointName: "AI-101", Value: 22.5})
	loginCli.Flush(context.Background())
	loginCli.Close()
	if gotBearer != "Bearer jwt-session-xyz" {
		t.Fatalf("expected Bearer jwt-session-xyz from login flow, got %q", gotBearer)
	}
}

