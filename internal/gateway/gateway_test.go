package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
	"bacnet-gw/internal/imops"
	"bacnet-gw/internal/simulator"
)

func TestProductionP864CatalogPointsAndIMOPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var mu sync.Mutex
	registeredDevices := make(map[uint32]imops.DeviceRegistration)
	byShortID := make(map[string]imops.TelemetryReading)
	var fireAlarmTriggered bool

	imopsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					byShortID[rd.ShortObjectID] = rd
					if rd.ShortObjectID == "BI-3" && rd.IsAlarm && rd.Quality == "alarm" {
						fireAlarmTriggered = true
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer imopsSrv.Close()

	// Start simulated Production Optergy P864 (Device Instance 864077)
	p864 := simulator.NewProductionP864Controller()
	if err := p864.Start(ctx, "127.0.0.1", 0, logger); err != nil {
		t.Fatalf("start simulated P864 864077: %v", err)
	}
	defer p864.Close()

	cfg := config.Default() // OnlyCatalogPoints defaults to true
	cfg.StatusAPI.Enabled = false
	cfg.Network.BindIP = "127.0.0.1"
	cfg.Network.Port = 0
	cfg.Network.BroadcastOverrides = []string{p864.LocalAddr().String()}
	cfg.IMOPS.BaseURL = imopsSrv.URL
	cfg.IMOPS.AuthType = "none"
	cfg.IMOPS.FlushIntervalMS = 100

	gw, err := New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}
	defer gw.Close()

	if err := gw.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		cnt := len(byShortID)
		mu.Unlock()
		if cnt >= 12 {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	mu.Lock()
	devReg, hasDev := registeredDevices[864077]
	_, hasSpare := byShortID["AV-999"]
	spReading, hasSP := byShortID["AV-27126"]
	bwThTemp, hasBWTH := byShortID["AI-201"]
	rackDoor, hasRack := byShortID["BI-7"]
	totalMonitored := len(byShortID)
	mu.Unlock()

	if !hasDev || !devReg.Online || devReg.PointCount != 12 {
		t.Fatalf("expected P864 #864077 registered online with 12 points, got %+v", devReg)
	}
	if hasSpare || totalMonitored != 12 {
		t.Fatalf("expected strictly 12 catalog points (AV-999 excluded), got %d (hasSpare=%v)", totalMonitored, hasSpare)
	}
	if !hasSP || spReading.DashboardLabel != "BW-INT-M" || spReading.Unit != "°C" {
		t.Fatalf("unexpected AV-27126 setpoint reading: %+v", spReading)
	}
	if !hasBWTH || bwThTemp.DashboardLabel != "BW-TH-M" || bwThTemp.Unit != "°C" {
		t.Fatalf("unexpected AI-201 BW-TH-M reading: %+v", bwThTemp)
	}
	if !hasRack || rackDoor.PropertyLabel != "Rack door opened without authorization" {
		t.Fatalf("unexpected BI-7 Server Rack Door reading: %+v", rackDoor)
	}

	// Emit dry-contact closure on BI-3 (Fire Sensor) WITHOUT BACnet StatusFlags.InAlarm set,
	// verifying AlarmOnActive triggers critical alarm quality automatically!
	err = p864.EmitCOV(
		gw.LocalBACnetAddr(),
		bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 3},
		1.0,
		bacnet.StatusFlags{InAlarm: false},
	)
	if err != nil {
		t.Fatalf("EmitCOV BI-3 failed: %v", err)
	}

	covDeadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(covDeadline) {
		mu.Lock()
		fired := fireAlarmTriggered
		mu.Unlock()
		if fired {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if !fireAlarmTriggered {
		t.Fatalf("expected BI-3 Fire Sensor dry-contact closure to trigger alarm in IMOPS")
	}
}

func TestFullObjectListDiscoveryMode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	var mu sync.Mutex
	registeredDevices := make(map[uint32]imops.DeviceRegistration)
	readingsByKey := make(map[string]imops.TelemetryReading)

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
					readingsByKey[rd.DeviceName+"/"+rd.ShortObjectID] = rd
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer imopsSrv.Close()

	ctrls := simulator.NewExhibitionControllers()
	ctrls[2].DisableRPM = true // Exercises non-RPM single ReadProperty fallback

	var targetAddrs []string
	for _, sc := range ctrls {
		if err := sc.Start(ctx, "127.0.0.1", 0, logger); err != nil {
			t.Fatalf("start sim controller %s: %v", sc.ObjectName, err)
		}
		defer sc.Close()
		targetAddrs = append(targetAddrs, sc.LocalAddr().String())
	}

	cfg := config.Default()
	cfg.StatusAPI.Enabled = false
	cfg.Network.BindIP = "127.0.0.1"
	cfg.Network.Port = 0
	cfg.Network.BroadcastOverrides = targetAddrs
	cfg.Polling.OnlyCatalogPoints = false // Full Object_List enumeration mode
	cfg.IMOPS.BaseURL = imopsSrv.URL
	cfg.IMOPS.AuthType = "bearer"
	cfg.IMOPS.BearerToken = "test-imops-bearer"
	cfg.IMOPS.FlushIntervalMS = 120

	gw, err := New(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("gateway.New: %v", err)
	}
	defer gw.Close()

	if err := gw.Start(ctx); err != nil {
		t.Fatalf("gateway.Start: %v", err)
	}

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
	defer mu.Unlock()
	if len(registeredDevices) != 3 || len(readingsByKey) != 24 {
		t.Fatalf("expected 3 controllers and 24 points in full discovery mode, got %d devs / %d points", len(registeredDevices), len(readingsByKey))
	}
}

func TestRunSubnetScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p864 := simulator.NewProductionP864Controller()
	if err := p864.Start(ctx, "127.0.0.1", 0, logger); err != nil {
		t.Fatalf("start sim P864: %v", err)
	}
	defer p864.Close()

	cfg := config.Default()
	cfg.Network.BindIP = "127.0.0.1"
	cfg.Network.Port = 0
	cfg.Network.BroadcastOverrides = []string{p864.LocalAddr().String()}

	var buf bytes.Buffer
	found, err := RunSubnetScan(ctx, cfg, ScanOptions{
		WaitDuration:  400 * time.Millisecond,
		IncludePoints: true,
	}, &buf)
	if err != nil {
		t.Fatalf("RunSubnetScan failed: %v", err)
	}
	if len(found) != 1 || found[0].DeviceID != 864077 {
		t.Fatalf("expected Device 864077 in scan, got %+v", found)
	}
	output := buf.String()
	t.Log("\n" + output)
	if !strings.Contains(output, "864077") || !strings.Contains(output, "Optergy-P864") || !strings.Contains(output, "AV-27126") {
		t.Fatalf("scan output missing expected controller/point details:\n%s", output)
	}
}

func TestBuildBroadcastTargetsIsolatesSelectedSubnet(t *testing.T) {
	netCfg := config.NetworkConfig{
		Port:   47808,
		Subnet: "192.168.0.0/24",
	}
	subnets := []bacnet.SubnetInfo{
		{
			InterfaceName: "Ethernet 2",
			CIDR:          "192.168.0.154/24",
			NetworkCIDR:   "192.168.0.0/24",
			BroadcastIP:   []byte{192, 168, 0, 255},
		},
	}

	targets := buildBroadcastTargets(netCfg, netCfg.EffectiveSubnet(), subnets)
	got := formatUDPTargets(targets)
	for _, addr := range got {
		if strings.HasPrefix(addr, "255.255.255.255:") {
			t.Fatalf("expected global 255.255.255.255 broadcast to be omitted when a specific subnet is selected, got %v", got)
		}
	}
	if len(got) == 0 || got[0] != "192.168.0.255:47808" {
		t.Fatalf("expected directed broadcast 192.168.0.255:47808 first, got %v", got)
	}
}
