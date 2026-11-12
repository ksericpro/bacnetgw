package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
)

// ScannedPoint represents a BACnet object discovered during a CLI -scan -scan-points run.
type ScannedPoint struct {
	ShortID     string
	ObjectID    bacnet.ObjectID
	Name        string
	ValueText   string
	Description string
}

// ScannedDevice represents a BACnet controller discovered during a CLI -scan run.
type ScannedDevice struct {
	DeviceID         uint32
	Address          bacnet.Address
	VendorID         uint32
	MaxAPDU          uint32
	ObjectName       string
	ModelName        string
	VendorName       string
	FirmwareRevision string
	Location         string
	Description      string
	ObjectCount      int
	Points           []ScannedPoint
}

// ScanOptions configures a one-shot subnet BACnet device scan.
type ScanOptions struct {
	WaitDuration  time.Duration
	IncludePoints bool
}

// RunSubnetScan broadcasts Who-Is across all detected (or selected) subnets, enriches every responding BACnet device,
// prints a formatted summary table to out, and returns the discovered devices.
func RunSubnetScan(ctx context.Context, cfg config.Config, opts ScanOptions, out io.Writer) ([]ScannedDevice, error) {
	if opts.WaitDuration <= 0 {
		opts.WaitDuration = 3 * time.Second
	}

	quietLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	allSubnets, err := bacnet.DetectSubnets("", cfg.Network.BindIP)
	if err != nil {
		fmt.Fprintf(out, "Warning: subnet auto-detection returned error: %v\n", err)
	}

	selector := normalizeSubnetSelector(cfg.Network.EffectiveSubnet())
	subnets := bacnet.FilterSubnets(allSubnets, selector)
	if selector != "" && len(subnets) == 0 && len(cfg.Network.BroadcastOverrides) == 0 {
		return nil, formatMissingSubnetError(selector, allSubnets)
	}

	if selector != "" {
		fmt.Fprintf(out, "=== 1. Selected IPv4 Subnet (%s) ===\n", selector)
	} else {
		fmt.Fprintln(out, "=== 1. Detected Local IPv4 Subnets ===")
	}
	if len(subnets) == 0 {
		fmt.Fprintln(out, "  (No active non-loopback IPv4 interfaces detected; using broadcast overrides)")
	} else {
		for _, sn := range subnets {
			cidrStr := sn.NetworkCIDR
			if cidrStr == "" {
				cidrStr = sn.CIDR
			}
			fmt.Fprintf(out, "  - %-26s IP: %-15s Subnet: %-18s Broadcast: %s:%d\n",
				sn.InterfaceName, sn.LocalIP.String(), cidrStr, sn.BroadcastIP.String(), bacnet.DefaultPort)
		}
	}

	targets := buildBroadcastTargets(cfg.Network, selector, subnets)
	// Also include common Optergy site controller IPs (192.168.0.158..161) if a 192.168.0.0/24 NIC is active
	for _, sn := range subnets {
		cidrStr := sn.NetworkCIDR
		if cidrStr == "" {
			cidrStr = sn.CIDR
		}
		if _, cidr, parseErr := net.ParseCIDR(cidrStr); parseErr == nil {
			for _, ipStr := range []string{"192.168.0.158", "192.168.0.159", "192.168.0.160", "192.168.0.161"} {
				ip := net.ParseIP(ipStr)
				if ip != nil && cidr.Contains(ip) {
					targets = append(targets, &net.UDPAddr{IP: ip, Port: bacnet.DefaultPort})
				}
			}
		}
	}

	var mu sync.Mutex
	found := make(map[uint32]*bacnet.IAmNotification)
	isolateToSubnet := selector != "" && len(subnets) > 0 && len(cfg.Network.BroadcastOverrides) == 0 && cfg.Network.BBMDAddress == ""

	bindPort := cfg.Network.Port
	cliCfg := bacnet.ClientConfig{
		BindIP:         cfg.Network.BindIP,
		Port:           bindPort,
		ReusePort:      true,
		RequestTimeout: 2 * time.Second,
		MaxRetries:     1,
		BBMDAddress:    cfg.Network.BBMDAddress,
		BBMDTTLSeconds: cfg.Network.BBMDTTLSeconds,
		Logger:         quietLogger,
		OnIAm: func(iam *bacnet.IAmNotification) {
			if isolateToSubnet && !bacnet.SubnetContainsIP(subnets, iam.Source.IP) {
				return
			}
			mu.Lock()
			found[iam.DeviceID] = iam
			mu.Unlock()
		},
	}

	cli, err := bacnet.NewClient(ctx, cliCfg)
	if err != nil && bindPort != 0 {
		// Fallback to an ephemeral local UDP port if port 47808 is exclusively locked by another BMS process
		fmt.Fprintf(out, "\nNote: UDP port %d is busy (%v); falling back to ephemeral UDP port for active scan...\n", bindPort, err)
		cliCfg.Port = 0
		cli, err = bacnet.NewClient(ctx, cliCfg)
	}
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	fmt.Fprintf(out, "\n=== 2. Scanning Subnet via BACnet Who-Is (Listening on %s, waiting %s) ===\n",
		cli.LocalAddr().String(), opts.WaitDuration.Round(100*time.Millisecond))

	// Send initial Who-Is and a second burst halfway through in case of UDP loss
	_ = cli.SendWhoIs(targets, nil, nil)
	halfWait := opts.WaitDuration / 2
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(halfWait):
	}
	_ = cli.SendWhoIs(targets, nil, nil)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(opts.WaitDuration - halfWait):
	}

	mu.Lock()
	iams := make([]*bacnet.IAmNotification, 0, len(found))
	for _, v := range found {
		iams = append(iams, v)
	}
	mu.Unlock()

	sort.Slice(iams, func(i, j int) bool {
		return iams[i].DeviceID < iams[j].DeviceID
	})

	fmt.Fprintf(out, "\n=== 3. Discovered BACnet Devices (%d found) ===\n", len(iams))
	if len(iams) == 0 {
		fmt.Fprintln(out, "  No BACnet devices responded to Who-Is on this subnet.")
		fmt.Fprintln(out, "  Tips: Verify Ethernet cable/VLAN, check Windows Firewall allows UDP 47808, or specify -config.")
		return nil, nil
	}

	results := make([]ScannedDevice, 0, len(iams))
	for _, iam := range iams {
		sd := enrichScannedDevice(ctx, cli, iam, opts.IncludePoints)
		results = append(results, sd)
	}

	// Print clean tabular summary
	fmt.Fprintf(out, "\n  %-10s  %-22s  %-24s  %-12s  %-14s  %-10s  %s\n",
		"DEVICE ID", "ADDRESS", "OBJECT NAME", "MODEL", "VENDOR", "FIRMWARE", "OBJECTS")
	fmt.Fprintf(out, "  %s\n", strings.Repeat("-", 108))
	for _, d := range results {
		objCntStr := "-"
		if d.ObjectCount > 0 {
			objCntStr = fmt.Sprintf("%d", d.ObjectCount)
		}
		fmt.Fprintf(out, "  %-10d  %-22s  %-24s  %-12s  %-14s  %-10s  %s\n",
			d.DeviceID,
			truncateCol(d.Address.String(), 22),
			truncateCol(emptyDash(d.ObjectName), 24),
			truncateCol(emptyDash(d.ModelName), 12),
			truncateCol(emptyDash(d.VendorName), 14),
			truncateCol(emptyDash(d.FirmwareRevision), 10),
			objCntStr,
		)
	}

	if opts.IncludePoints {
		for _, d := range results {
			fmt.Fprintf(out, "\n  [Points on Device %d — %s (%s) — %d telemetry objects]\n",
				d.DeviceID, emptyDash(d.ObjectName), d.Address.String(), len(d.Points))
			if len(d.Points) == 0 {
				fmt.Fprintln(out, "    (No readable telemetry points found)")
				continue
			}
			fmt.Fprintf(out, "    %-12s  %-30s  %-20s  %s\n", "OBJECT", "NAME", "PRESENT VALUE", "DESCRIPTION")
			fmt.Fprintf(out, "    %s\n", strings.Repeat("-", 92))
			for _, pt := range d.Points {
				fmt.Fprintf(out, "    %-12s  %-30s  %-20s  %s\n",
					pt.ShortID,
					truncateCol(pt.Name, 30),
					truncateCol(pt.ValueText, 20),
					truncateCol(pt.Description, 40),
				)
			}
		}
	}

	fmt.Fprintln(out)
	return results, nil
}

func enrichScannedDevice(ctx context.Context, cli *bacnet.Client, iam *bacnet.IAmNotification, includePoints bool) ScannedDevice {
	sd := ScannedDevice{
		DeviceID: iam.DeviceID,
		Address:  iam.Source,
		VendorID: iam.VendorID,
		MaxAPDU:  iam.MaxAPDULength,
	}
	devObj := bacnet.ObjectID{Type: bacnet.ObjDevice, Instance: iam.DeviceID}

	rpmRes, err := cli.ReadPropertyMultiple(ctx, iam.Source, []bacnet.ReadAccessSpec{{
		Object: devObj,
		Properties: []bacnet.PropertyRef{
			{Property: bacnet.PropObjectName},
			{Property: bacnet.PropModelName},
			{Property: bacnet.PropVendorName},
			{Property: bacnet.PropFirmwareRevision},
			{Property: bacnet.PropLocation},
			{Property: bacnet.PropDescription},
		},
	}})
	supportsRPM := err == nil
	if supportsRPM && len(rpmRes) > 0 {
		for _, r := range rpmRes[0].Results {
			if r.HasError || len(r.Values) == 0 {
				continue
			}
			s, _ := r.Values[0].(string)
			switch r.Property {
			case bacnet.PropObjectName:
				sd.ObjectName = s
			case bacnet.PropModelName:
				sd.ModelName = s
			case bacnet.PropVendorName:
				sd.VendorName = s
			case bacnet.PropFirmwareRevision:
				sd.FirmwareRevision = s
			case bacnet.PropLocation:
				sd.Location = s
			case bacnet.PropDescription:
				sd.Description = s
			}
		}
	} else {
		sd.ObjectName = readStrSingle(ctx, cli, iam.Source, devObj, bacnet.PropObjectName)
		sd.ModelName = readStrSingle(ctx, cli, iam.Source, devObj, bacnet.PropModelName)
		sd.VendorName = readStrSingle(ctx, cli, iam.Source, devObj, bacnet.PropVendorName)
		sd.FirmwareRevision = readStrSingle(ctx, cli, iam.Source, bacnet.ObjectID{Type: bacnet.ObjDevice, Instance: iam.DeviceID}, bacnet.PropFirmwareRevision)
	}

	if !includePoints {
		// Read Object_List[0] quickly for object count if supported, else full Object_List length
		idxZero := uint32(0)
		if cntRes, cntErr := cli.ReadProperty(ctx, iam.Source, devObj, bacnet.PropObjectList, &idxZero); cntErr == nil && len(cntRes.Values) > 0 {
			if cnt, ok := cntRes.Values[0].(uint64); ok {
				sd.ObjectCount = int(cnt)
			}
		} else if objList, listErr := cli.ReadObjectList(ctx, iam.Source, iam.DeviceID); listErr == nil {
			sd.ObjectCount = len(objList)
		}
		return sd
	}

	objList, err := cli.ReadObjectList(ctx, iam.Source, iam.DeviceID)
	if err != nil {
		return sd
	}
	sd.ObjectCount = len(objList)

	for _, oid := range objList {
		if !oid.Type.IsTelemetryObject() {
			continue
		}
		meta := &PointMeta{
			ObjectID: oid,
			ShortID:  oid.ShortCode(),
			Name:     oid.ShortCode(),
		}
		if cp, ok := LookupCatalogPoint(iam.DeviceID, sd.ModelName, sd.ObjectName, oid); ok {
			applyCatalogDefaults(meta, cp)
		}

		var rawPV any
		var hasPV bool
		if supportsRPM {
			props := []bacnet.PropertyRef{
				{Property: bacnet.PropObjectName},
				{Property: bacnet.PropDescription},
				{Property: bacnet.PropPresentValue},
			}
			if oid.Type.IsAnalogOrNumeric() {
				props = append(props, bacnet.PropertyRef{Property: bacnet.PropUnits})
			} else if oid.Type.IsBinary() {
				props = append(props, bacnet.PropertyRef{Property: bacnet.PropInactiveText}, bacnet.PropertyRef{Property: bacnet.PropActiveText})
			}
			if res, rErr := cli.ReadPropertyMultiple(ctx, iam.Source, []bacnet.ReadAccessSpec{{Object: oid, Properties: props}}); rErr == nil && len(res) > 0 {
				for _, pr := range res[0].Results {
					if pr.HasError || len(pr.Values) == 0 {
						continue
					}
					if pr.Property == bacnet.PropPresentValue {
						rawPV = pr.Values[0]
						hasPV = true
					} else {
						applyPointMetaProperty(meta, pr.Property, pr.Values)
					}
				}
			}
		}
		if !hasPV {
			if n := readStrSingle(ctx, cli, iam.Source, oid, bacnet.PropObjectName); n != "" {
				meta.Name = n
			}
			meta.Description = readStrSingle(ctx, cli, iam.Source, oid, bacnet.PropDescription)
			if oid.Type.IsAnalogOrNumeric() {
				if uRes, uErr := cli.ReadProperty(ctx, iam.Source, oid, bacnet.PropUnits, nil); uErr == nil && len(uRes.Values) > 0 {
					applyPointMetaProperty(meta, bacnet.PropUnits, uRes.Values)
				}
			}
			if pvRes, pvErr := cli.ReadProperty(ctx, iam.Source, oid, bacnet.PropPresentValue, nil); pvErr == nil && len(pvRes.Values) > 0 {
				rawPV = pvRes.Values[0]
				hasPV = true
			}
		}

		valText := "-"
		if hasPV {
			_, valText, _, _ = evaluatePointValue(meta, rawPV, bacnet.StatusFlags{})
		}
		sd.Points = append(sd.Points, ScannedPoint{
			ShortID:     meta.ShortID,
			ObjectID:    oid,
			Name:        meta.Name,
			ValueText:   valText,
			Description: meta.Description,
		})
	}
	return sd
}

func readStrSingle(ctx context.Context, cli *bacnet.Client, addr bacnet.Address, obj bacnet.ObjectID, prop bacnet.PropertyID) string {
	res, err := cli.ReadProperty(ctx, addr, obj, prop, nil)
	if err != nil || len(res.Values) == 0 {
		return ""
	}
	s, _ := res.Values[0].(string)
	return s
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}

func truncateCol(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

