package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/imops"
	"bacnet-gw/internal/simulator"
)

func main() {
	var (
		imopsAddr = flag.String("imops-listen", "127.0.0.1:8085", "Mock IMOPS HTTP API listen address")
		gwTarget  = flag.String("gateway-target", "127.0.0.1:47808", "Target gateway UDP address for spontaneous I-Am and COV broadcasts")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Start mock IMOPS HTTP receiver
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/bacnet/devices", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env imops.DeviceEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			logger.Info("[MOCK-IMOPS] device registered",
				"device_id", env.Device.DeviceID,
				"name", env.Device.ObjectName,
				"model", env.Device.ModelName,
				"role", env.Device.ControllerRole,
				"points", env.Device.PointCount,
				"auth_hdr", maskHeader(r),
			)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})
	mux.HandleFunc("/api/v1/bacnet/telemetry", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env imops.TelemetryBatchEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			logger.Info("[MOCK-IMOPS] telemetry batch received",
				"batch_id", env.BatchID,
				"count", env.Count,
				"auth_hdr", maskHeader(r),
			)
			for i := 0; i < len(env.Readings) && i < 3; i++ {
				rd := env.Readings[i]
				logger.Info("  -> sample point",
					"device", rd.DeviceName,
					"equipment", rd.EquipmentProfile,
					"point", rd.PointName,
					"value", rd.ValueText,
					"source", rd.SourceType,
				)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})
	mux.HandleFunc("/api/v1/bacnet/packets", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env imops.PacketBatchEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			logger.Info("[MOCK-IMOPS] BACnet packet batch received", "count", env.Count)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"accepted"}`))
	})

	srv := &http.Server{Addr: *imopsAddr, Handler: mux}
	go func() {
		logger.Info("mock IMOPS API server listening", "url", "http://"+*imopsAddr)
		_ = srv.ListenAndServe()
	}()

	// 2. Start simulated Optergy AXON, P864, and P442 controllers
	ctrls := simulator.NewExhibitionControllers()
	var addrs []string
	for i, sc := range ctrls {
		if err := sc.Start(ctx, "127.0.0.1", 47810+i, logger); err != nil {
			logger.Error("failed to start simulated controller", "name", sc.ObjectName, "err", err)
			os.Exit(1)
		}
		defer sc.Close()
		addrs = append(addrs, sc.LocalAddr().String())
		logger.Info("started simulated BACnet controller",
			"device_id", sc.DeviceID,
			"name", sc.ObjectName,
			"model", sc.ModelName,
			"udp_addr", sc.LocalAddr().String(),
		)
	}

	fmt.Printf("\n--- Ready! Run the gateway in another terminal with: ---\n")
	fmt.Printf("  $env:IMOPS_BASE_URL=\"http://%s\"\n", *imopsAddr)
	fmt.Printf("  $env:BACNET_BROADCAST_OVERRIDES=\"%s\"\n", strings.Join(addrs, ","))
	fmt.Printf("  go run ./cmd/bacnet-gw\n\n")

	targetUDP, _ := net.ResolveUDPAddr("udp4", *gwTarget)
	if targetUDP != nil {
		for _, sc := range ctrls {
			_ = sc.BroadcastIAm(targetUDP)
		}
	}

	// Periodically emit COV change on P442 Dwyer WD3 / T18XAIR CO2
	covTicker := time.NewTicker(20 * time.Second)
	defer covTicker.Stop()
	toggle := false
	for {
		select {
		case <-ctx.Done():
			_ = srv.Close()
			return
		case <-covTicker.C:
			if targetUDP != nil && len(ctrls) >= 3 {
				toggle = !toggle
				co2 := float32(620.0)
				if toggle {
					co2 = 685.5
				}
				_ = ctrls[2].EmitCOV(targetUDP, bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 10}, co2, bacnet.StatusFlags{})
			}
		}
	}
}

func maskHeader(r *http.Request) string {
	if r.Header.Get("Authorization") != "" {
		return "Authorization(present)"
	}
	if r.Header.Get("X-API-Key") != "" {
		return "X-API-Key(present)"
	}
	return "none"
}

