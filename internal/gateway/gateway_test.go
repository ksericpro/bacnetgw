package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
	"bacnet-gw/internal/imops"
	"bacnet-gw/internal/simulator"
)

func TestEndToEndExhibitionDiscoveryPollingAndIMOPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// 1. Start mock external IMOPS API server
	var mu sync.Mutex
	registeredDevices := make(map[uint32]imops.DeviceRegistration)
	readingsByKey := make(map[string]imops.TelemetryReading)
	var covAlarmReceived bool
	var packetEventsReceived int

	imopsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-imops-bearer" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/api/v1/bacnet/devices":
			var env imops.DeviceEnvelope
			if err := json.Unmarshal(body, &env); err == nil {
				registeredDevices[env.Device.DeviceID] = env.Device
			}
		case "/api/v1/bacnet/telemetry":
			var env imops.TelemetryBatchEnvelope
			if err := json.Unmarshal(body, &env); err == nil {
				for _, rd := range env.Readings {
					key := rd.DeviceName + "/" + rd.PointName
					readingsByKey[key] = rd
					if rd.SourceType == "cov" && rd.PointName == "IN1_WD3_Water_Leak" && rd.Quality == "alarm" {
						covAlarmReceived = true
					}
				}
			}
		case "/api/v1/bacnet/packets":
			var env imops.PacketBatchEnvelope
			if err := json.Unmarshal(body, &env); err == nil {
				packetEventsReceived += env.Count
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer imopsSrv.Close()

	// 2. Start simulated Optergy AXON (1001), P864 (8640), and P442 (4420) on ephemeral localhost UDP ports
	ctrls := simulator.NewExhibitionControllers()
	// Also exercise non-RPM fallback on P442 to test both RPM and single ReadProperty codepaths!
	ctrls[2].DisableRPM = true

	var targetAddrs []string
	for _, sc := range ctrls {
		if err := sc.Start(ctx, "127.0.0.1", 0, logger); err != nil {
			t.Fatalf("start sim controller %s: %v", sc.ObjectName, err)
		}
		defer sc.Close()
		targetAddrs = append(targetAddrs, sc.LocalAddr().String())
	}

	// 3. Configure and launch the BACnet Gateway
	cfg := config.Default()
	cfg.GatewayID = "gw-integration-test"
	cfg.StatusAPI.Enabled = false
	cfg.Network.BindIP = "127.0.0.1"
	cfg.Network.Port = 0 // Ephemeral port for isolated test execution
	cfg.Network.BroadcastOverrides = targetAddrs
	cfg.Network.IncludeRawHex = true
	cfg.Polling.PollIntervalSec = 2
	cfg.Polling.SubscribeCOV = true
	cfg.Polling.ForwardPacketEvents = true
	cfg.IMOPS.BaseURL = imopsSrv.URL
	cfg.IMOPS.AuthType = "bearer"
	cfg.IMOPS.BearerToken = "test-imops-bearer"
	cfg.IMOPS.FlushIntervalMS = 150
	cfg.IMOPS.BatchMaxPoints = 50

	gw, err := New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}
	defer gw.Close()

	if err := gw.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}

	// Wait for discovery + initial metadata sync + initial poll delivery to IMOPS
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		devCnt := len(registeredDevices)
		rdCnt := len(readingsByKey)
		mu.Unlock()
		if devCnt == 3 && rdCnt >= 24 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	devCnt := len(registeredDevices)
	rdCnt := len(readingsByKey)
	pmPower, hasPM := readingsByKey["Optergy-AXON-Main/PM1200_Active_Power_kW"]
	rhplcTemp, hasRHPLC := readingsByKey["Optergy-P864-IO/UI1_RHPLC_Room_Temp"]
	t18CO2, hasCO2 := readingsByKey["Optergy-P442-IO/T18XAIR_CO2"]
	wd3Init, hasWD3 := readingsByKey["Optergy-P442-IO/IN1_WD3_Water_Leak"]
	mu.Unlock()

	if devCnt != 3 {
		t.Fatalf("expected 3 Optergy controllers registered in IMOPS, got %d", devCnt)
	}
	if rdCnt != 24 {
		t.Fatalf("expected all 24 Exhibition telemetry points in IMOPS, got %d", rdCnt)
	}
	if !hasPM || pmPower.EquipmentProfile != "Schneider PM1200" || pmPower.Unit != "kW" {
		t.Fatalf("unexpected Schneider PM1200 reading: %+v", pmPower)
	}
	if !hasRHPLC || rhplcTemp.EquipmentProfile != "Dwyer RHPLC" || rhplcTemp.Unit != "°C" {
		t.Fatalf("unexpected Dwyer RHPLC reading: %+v", rhplcTemp)
	}
	if !hasCO2 || t18CO2.EquipmentProfile != "T18XAIR-485-WIFI(TY)" || t18CO2.Unit != "ppm" {
		t.Fatalf("unexpected T18XAIR CO2 reading: %+v", t18CO2)
	}
	if !hasWD3 || wd3Init.ValueText != "Normal" {
		t.Fatalf("expected initial Dwyer WD3 state 'Normal', got %+v", wd3Init)
	}

	// 4. Simulate real-time Unconfirmed COV Water Leak Alarm from P442 Dwyer WD3
	err = ctrls[2].EmitCOV(
		gw.LocalBACnetAddr(),
		bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 1},
		1.0,
		bacnet.StatusFlags{InAlarm: true},
	)
	if err != nil {
		t.Fatalf("EmitCOV failed: %v", err)
	}

	covDeadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(covDeadline) {
		mu.Lock()
		gotAlarm := covAlarmReceived
		pkts := packetEventsReceived
		mu.Unlock()
		if gotAlarm && pkts > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !covAlarmReceived {
		t.Fatalf("expected real-time COV alarm for IN1_WD3_Water_Leak to arrive at IMOPS")
	}
	if packetEventsReceived == 0 {
		t.Fatalf("expected decoded BACnet packet events to be delivered to IMOPS")
	}
}

