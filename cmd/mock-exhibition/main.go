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
	"strconv"
	"syscall"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/imops"
	"bacnet-gw/internal/simulator"
)

func main() {
	var (
		imopsAddr = flag.String("imops-listen", "127.0.0.1:8085", "Mock IMOPS HTTP API listen address")
		simPort   = flag.Int("bacnet-port", 47810, "Simulated Optergy P864 (864077) UDP port")
		gwTarget  = flag.String("gateway-target", "127.0.0.1:47809", "Target gateway UDP address for spontaneous I-Am and COV broadcasts")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p864 := simulator.NewProductionP864Controller()
	if err := p864.Start(ctx, "127.0.0.1", *simPort, logger); err != nil {
		logger.Error("failed to start simulated P864 controller", "err", err)
		os.Exit(1)
	}
	defer p864.Close()

	targetUDP, _ := net.ResolveUDPAddr("udp4", *gwTarget)

	// Start mock IMOPS HTTP receiver + interactive simulation trigger API
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/bacnet/devices", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env imops.DeviceEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			logger.Info("[MOCK-IMOPS] controller registered",
				"device_id", env.Device.DeviceID,
				"name", env.Device.ObjectName,
				"model", env.Device.ModelName,
				"online", env.Device.Online,
				"points", env.Device.PointCount,
				"auth", maskHeader(r),
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
				"auth", maskHeader(r),
			)
			for i := 0; i < len(env.Readings) && i < 4; i++ {
				rd := env.Readings[i]
				logger.Info("  -> reading",
					"short_id", rd.ShortObjectID,
					"sensor", rd.DashboardLabel,
					"property", rd.PropertyLabel,
					"value", rd.ValueText,
					"alarm", rd.IsAlarm,
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
	// Interactive endpoint to trigger simulated COV alarms/values on P864 #864077:
	// e.g. Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=BI-1&val=1"
	mux.HandleFunc("/sim/set", func(w http.ResponseWriter, r *http.Request) {
		ptStr := r.URL.Query().Get("point")
		valStr := r.URL.Query().Get("val")
		oid, err := bacnet.ParseShortObjectID(ptStr)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fv, err := strconv.ParseFloat(valStr, 32)
		if err != nil {
			http.Error(w, "invalid val", http.StatusBadRequest)
			return
		}
		flags := bacnet.StatusFlags{InAlarm: oid.Type.IsBinary() && oid.Instance != 2 && fv > 0}
		if targetUDP != nil {
			_ = p864.EmitCOV(targetUDP, oid, float32(fv), flags)
		}
		logger.Info("[SIMULATOR] updated P864 point & emitted COV", "point", oid.ShortCode(), "val", fv)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok","point":%q,"val":%v}`, oid.ShortCode(), fv)
	})

	srv := &http.Server{Addr: *imopsAddr, Handler: mux}
	go func() {
		logger.Info("mock IMOPS API & simulator control listening", "url", "http://"+*imopsAddr)
		_ = srv.ListenAndServe()
	}()

	logger.Info("started simulated Production Optergy P864 controller",
		"device_id", p864.DeviceID,
		"name", p864.ObjectName,
		"udp_addr", p864.LocalAddr().String(),
	)

	fmt.Printf("\n--- Ready! Start the gateway in another terminal with: ---\n")
	fmt.Printf("  $env:IMOPS_BASE_URL=\"http://%s\"\n", *imopsAddr)
	fmt.Printf("  $env:BACNET_PORT=\"47809\"\n")
	fmt.Printf("  $env:BACNET_BROADCAST_OVERRIDES=\"%s\"\n", p864.LocalAddr().String())
	fmt.Printf("  .\\bacnet-gw.exe\n\n")
	fmt.Printf("--- Trigger a simulated alarm anytime with: ---\n")
	fmt.Printf("  Invoke-RestMethod -Method Post \"http://%s/sim/set?point=BI-1&val=1\"  # WD3 Water Leak\n", *imopsAddr)
	fmt.Printf("  Invoke-RestMethod -Method Post \"http://%s/sim/set?point=BI-3&val=1\"  # Fire Alarm\n\n", *imopsAddr)

	if targetUDP != nil {
		_ = p864.BroadcastIAm(targetUDP)
	}

	// Subtle live drift on BW-TH-M temperature so you see live readings changing
	driftTicker := time.NewTicker(15 * time.Second)
	defer driftTicker.Stop()
	step := 0
	for {
		select {
		case <-ctx.Done():
			_ = srv.Close()
			return
		case <-driftTicker.C:
			step++
			temp := float32(24.1) + float32(step%4)*0.2
			if targetUDP != nil {
				_ = p864.EmitCOV(targetUDP, bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 201}, temp, bacnet.StatusFlags{})
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
