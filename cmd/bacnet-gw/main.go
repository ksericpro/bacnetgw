package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
	"bacnet-gw/internal/gateway"
)

func main() {
	var (
		configPath  = flag.String("config", "", "Path to config.yaml or config.json (auto-detected if omitted)")
		envPath     = flag.String("env", "", "Optional path to .env file")
		listSubnets = flag.Bool("list-subnets", false, "List detected IPv4 interfaces and BACnet broadcast addresses, then exit")
	)
	flag.Parse()

	if *listSubnets {
		subnets, err := bacnet.DetectSubnets("", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error detecting subnets: %v\n", err)
			os.Exit(1)
		}
		if len(subnets) == 0 {
			fmt.Println("No active non-loopback IPv4 subnets detected.")
			return
		}
		fmt.Println("Detected IPv4 Subnets for BACnet/IP Discovery (UDP 47808):")
		for _, sn := range subnets {
			fmt.Printf("  - Interface: %-24s  IP: %-15s  CIDR: %-18s  Broadcast: %s:47808\n",
				sn.InterfaceName, sn.LocalIP.String(), sn.CIDR, sn.BroadcastIP.String())
		}
		return
	}

	resolvedCfgPath := *configPath
	if resolvedCfgPath == "" {
		for _, candidate := range []string{"config.yaml", "config.yml", "config.json"} {
			if _, err := os.Stat(candidate); err == nil {
				resolvedCfgPath = candidate
				break
			}
		}
	}

	cfg, err := config.Load(resolvedCfgPath, *envPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)
	if resolvedCfgPath != "" {
		logger.Info("loaded gateway configuration", "path", resolvedCfgPath)
	} else {
		logger.Info("starting with default + environment configuration")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	gw, err := gateway.New(ctx, cfg, logger)
	if err != nil {
		logger.Error("failed to initialize BACnet gateway", "err", err)
		os.Exit(1)
	}

	if err := gw.Start(ctx); err != nil {
		logger.Error("failed to start BACnet gateway", "err", err)
		_ = gw.Close()
		os.Exit(1)
	}

	<-ctx.Done()
	logger.Info("shutting down BACnet gateway and flushing IMOPS queue...")
	_ = gw.Close()
	logger.Info("shutdown complete")
}

func newLogger(levelStr string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

