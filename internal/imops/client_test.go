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
	if stats.ReadingsSent != 3 || stats.BatchesSent != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

