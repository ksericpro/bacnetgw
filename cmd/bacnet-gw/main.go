package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
	"bacnet-gw/internal/gateway"
)

func main() {
	var (
		configPath   = flag.String("config", "", "Path to config.yaml or config.json (auto-detected if omitted)")
		envPath      = flag.String("env", "", "Optional path to .env file")
		subnetFlag   = flag.String("subnet", "", "Target IPv4 subnet to use: CIDR (192.168.0.0/24), IP/prefix (192.168.0), or adapter name (\"Ethernet 2\")")
		selectSubnet = flag.Bool("select-subnet", false, "Interactively pick which local IPv4 subnet to use from a numbered menu")
		listSubnets  = flag.Bool("list-subnets", false, "List detected IPv4 interfaces and BACnet broadcast addresses, then exit")
		scanDevices  = flag.Bool("scan", false, "Actively scan the subnet via BACnet Who-Is, print all detected BACnet devices, and exit")
		scanPoints   = flag.Bool("scan-points", false, "Scan the subnet and also print all BACnet points and live values on every detected device")
		scanWait     = flag.Duration("scan-wait", 3*time.Second, "How long to wait for BACnet I-Am replies during -scan")
	)
	flag.Parse()

	if *listSubnets {
		subnets, err := bacnet.DetectSubnets(*subnetFlag, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error detecting subnets: %v\n", err)
			os.Exit(1)
		}
		if len(subnets) == 0 {
			fmt.Println("No matching active non-loopback IPv4 subnets detected.")
			return
		}
		fmt.Println("Detected IPv4 Subnets for BACnet/IP Discovery (UDP 47808):")
		for i, sn := range subnets {
			cidr := sn.NetworkCIDR
			if cidr == "" {
				cidr = sn.CIDR
			}
			fmt.Printf("  [%d] Interface: %-26s  Subnet: %-18s  IP: %-15s  Broadcast: %s:47808\n",
				i+1, sn.InterfaceName, cidr, sn.LocalIP.String(), sn.BroadcastIP.String())
		}
		fmt.Println("\nTip: Use -subnet <Subnet-CIDR-or-Interface> or -select-subnet to lock discovery to one subnet.")
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

	if strings.TrimSpace(*subnetFlag) != "" {
		cfg.Network.Subnet = strings.TrimSpace(*subnetFlag)
	}

	if *selectSubnet {
		chosen, pickErr := promptSubnetSelection(os.Stdin, os.Stdout, cfg.Network.BindIP)
		if pickErr != nil {
			fmt.Fprintf(os.Stderr, "Subnet selection aborted: %v\n", pickErr)
			os.Exit(1)
		}
		cfg.Network.Subnet = chosen
		if chosen == "" {
			cfg.Network.Interface = ""
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *scanDevices || *scanPoints {
		_, scanErr := gateway.RunSubnetScan(ctx, cfg, gateway.ScanOptions{
			WaitDuration:  *scanWait,
			IncludePoints: *scanPoints,
		}, os.Stdout)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "BACnet subnet scan failed: %v\n", scanErr)
			os.Exit(1)
		}
		return
	}

	logger := newLogger(cfg.LogLevel)
	if resolvedCfgPath != "" {
		logger.Info("loaded gateway configuration", "path", resolvedCfgPath)
	} else {
		logger.Info("starting with default + environment configuration")
	}

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

func promptSubnetSelection(in io.Reader, out io.Writer, bindIP string) (string, error) {
	subnets, err := bacnet.DetectSubnets("", bindIP)
	if err != nil {
		return "", err
	}
	if len(subnets) == 0 {
		return "", fmt.Errorf("no active non-loopback IPv4 interfaces detected")
	}

	fmt.Fprintln(out, "=== Select Local IPv4 Subnet for BACnet/IP ===")
	fmt.Fprintln(out, "  [0] All detected subnets (Auto)")
	for i, sn := range subnets {
		cidr := sn.NetworkCIDR
		if cidr == "" {
			cidr = sn.CIDR
		}
		fmt.Fprintf(out, "  [%d] %-26s  Subnet: %-18s  Local IP: %-15s  Broadcast: %s:%d\n",
			i+1, sn.InterfaceName, cidr, sn.LocalIP.String(), sn.BroadcastIP.String(), bacnet.DefaultPort)
	}

	reader := bufio.NewReader(in)
	for {
		fmt.Fprintf(out, "Enter choice [0-%d] (press Enter for 0): ", len(subnets))
		line, readErr := reader.ReadString('\n')
		if readErr != nil && len(line) == 0 {
			return "", readErr
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "0" {
			fmt.Fprintln(out, "-> Using: All detected subnets")
			fmt.Fprintln(out)
			return "", nil
		}
		idx, convErr := strconv.Atoi(trimmed)
		if convErr == nil && idx >= 1 && idx <= len(subnets) {
			chosen := subnets[idx-1]
			sel := chosen.NetworkCIDR
			if sel == "" {
				sel = chosen.CIDR
			}
			fmt.Fprintf(out, "-> Selected: [%d] %s — %s (Broadcast %s:%d)\n\n",
				idx, chosen.InterfaceName, sel, chosen.BroadcastIP.String(), bacnet.DefaultPort)
			return sel, nil
		}
		fmt.Fprintf(out, "Invalid selection %q — please enter a number between 0 and %d.\n", trimmed, len(subnets))
	}
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
