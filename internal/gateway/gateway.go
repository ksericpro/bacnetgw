package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bacnet-gw/internal/bacnet"
	"bacnet-gw/internal/config"
	"bacnet-gw/internal/imops"
)

// PointMeta stores cached static metadata and latest live reading for a BACnet object.
type PointMeta struct {
	ObjectID         bacnet.ObjectID    `json:"object_id"`
	Name             string             `json:"name"`
	Description      string             `json:"description,omitempty"`
	UnitID           *uint32            `json:"unit_id,omitempty"`
	UnitSymbol       string             `json:"unit_symbol,omitempty"`
	InactiveText     string             `json:"inactive_text,omitempty"`
	ActiveText       string             `json:"active_text,omitempty"`
	StateTexts       []string           `json:"state_texts,omitempty"`
	EquipmentProfile string             `json:"equipment_profile,omitempty"`
	ConnectionBus    string             `json:"connection_bus,omitempty"`
	LastValue        any                `json:"last_value,omitempty"`
	LastValueText    string             `json:"last_value_text,omitempty"`
	LastStatusFlags  bacnet.StatusFlags `json:"last_status_flags"`
	LastUpdated      time.Time          `json:"last_updated,omitempty"`
}

// ManagedDevice tracks a discovered BACnet controller and its telemetry points.
type ManagedDevice struct {
	mu               sync.RWMutex
	DeviceID         uint32
	Address          bacnet.Address
	MaxAPDULength    uint32
	Segmentation     uint32
	VendorID         uint32
	ObjectName       string
	ModelName        string
	VendorName       string
	FirmwareRevision string
	Description      string
	Location         string
	ControllerRole   string
	SupportsRPM      bool
	DiscoveredAt     time.Time
	LastSeenAt       time.Time
	LastMetaSync     time.Time
	LastCOVSync      time.Time
	SyncInProgress   bool
	Points           map[bacnet.ObjectID]*PointMeta
}

// Gateway coordinates subnet discovery, BACnet listener/polling, and IMOPS delivery.
type Gateway struct {
	cfg        config.Config
	logger     *slog.Logger
	bacnetCli  *bacnet.Client
	imopsCli   *imops.Client
	httpServer *http.Server

	subnets          []bacnet.SubnetInfo
	broadcastTargets []*net.UDPAddr

	devMu   sync.RWMutex
	devices map[uint32]*ManagedDevice

	pktMu         sync.Mutex
	recentPackets []bacnet.PacketEvent
	packetsSeen   atomic.Uint64
	iamSeen       atomic.Uint64
	covSeen       atomic.Uint64

	startedAt time.Time
	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// New creates and initializes a Gateway instance.
func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Gateway, error) {
	if logger == nil {
		logger = slog.Default()
	}

	subnets, err := bacnet.DetectSubnets(cfg.Network.Interface, cfg.Network.BindIP)
	if err != nil {
		logger.Warn("auto subnet detection returned warning", "err", err)
	}

	bcastTargets := buildBroadcastTargets(cfg.Network, subnets)
	imopsCli := imops.NewClient(cfg.GatewayID, cfg.SiteName, cfg.IMOPS, logger)

	gw := &Gateway{
		cfg:              cfg,
		logger:           logger,
		imopsCli:         imopsCli,
		subnets:          subnets,
		broadcastTargets: bcastTargets,
		devices:          make(map[uint32]*ManagedDevice),
		recentPackets:    make([]bacnet.PacketEvent, 0, 50),
		startedAt:        time.Now().UTC(),
		closed:           make(chan struct{}),
	}

	bCli, err := bacnet.NewClient(ctx, bacnet.ClientConfig{
		BindIP:         cfg.Network.BindIP,
		Port:           cfg.Network.Port,
		ReusePort:      cfg.Network.ReusePort,
		IncludeRawHex:  cfg.Network.IncludeRawHex,
		RequestTimeout: cfg.RequestTimeout(),
		MaxRetries:     cfg.Network.MaxRetries,
		BBMDAddress:    cfg.Network.BBMDAddress,
		BBMDTTLSeconds: cfg.Network.BBMDTTLSeconds,
		Logger:         logger,
		OnIAm:          gw.handleIAm,
		OnCOV:          gw.handleCOV,
		OnPacket:       gw.handlePacket,
	})
	if err != nil {
		imopsCli.Close()
		return nil, err
	}
	gw.bacnetCli = bCli

	return gw, nil
}

func buildBroadcastTargets(netCfg config.NetworkConfig, subnets []bacnet.SubnetInfo) []*net.UDPAddr {
	port := netCfg.Port
	if port <= 0 {
		port = bacnet.DefaultPort
	}

	var targets []*net.UDPAddr
	for _, raw := range netCfg.BroadcastOverrides {
		if addr, err := parseTargetAddr(raw, port); err == nil {
			targets = append(targets, addr)
		}
	}

	for _, sn := range subnets {
		targets = append(targets, &net.UDPAddr{
			IP:   sn.BroadcastIP,
			Port: port,
		})
	}

	// Always include limited broadcast fallback unless explicit overrides were the only desired targets
	if len(netCfg.BroadcastOverrides) == 0 {
		targets = append(targets, &net.UDPAddr{
			IP:   net.IPv4bcast,
			Port: port,
		})
	}
	return targets
}

func parseTargetAddr(s string, defaultPort int) (*net.UDPAddr, error) {
	if strings.Contains(s, ":") {
		return net.ResolveUDPAddr("udp4", s)
	}
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %q", s)
	}
	return &net.UDPAddr{IP: ip, Port: defaultPort}, nil
}

// Start launches discovery, telemetry polling, and the optional status HTTP API.
func (g *Gateway) Start(ctx context.Context) error {
	for _, sn := range g.subnets {
		g.logger.Info("detected local subnet for BACnet discovery",
			"interface", sn.InterfaceName,
			"local_ip", sn.LocalIP.String(),
			"cidr", sn.CIDR,
			"directed_broadcast", sn.BroadcastIP.String(),
		)
	}
	g.logger.Info("BACnet/IP listener bound",
		"listen", g.bacnetCli.LocalAddr().String(),
		"broadcast_targets", formatUDPTargets(g.broadcastTargets),
	)

	if g.cfg.StatusAPI.Enabled && g.cfg.StatusAPI.ListenAddr != "" {
		mux := http.NewServeMux()
		mux.HandleFunc("/", g.handleDashboard)
		mux.HandleFunc("/assets/dashboard.jpeg", g.handleDashboardImage)
		mux.HandleFunc("/assets/bacnet-devices.jpeg", g.handleTopologyImage)
		mux.HandleFunc("/healthz", g.handleHealthz)
		mux.HandleFunc("/status", g.handleStatus)
		mux.HandleFunc("/discover", g.handleTriggerDiscover)
		g.httpServer = &http.Server{
			Addr:              g.cfg.StatusAPI.ListenAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		ln, err := net.Listen("tcp", g.cfg.StatusAPI.ListenAddr)
		if err != nil {
			g.logger.Warn("could not start local status HTTP endpoint", "addr", g.cfg.StatusAPI.ListenAddr, "err", err)
		} else {
			g.logger.Info("live web dashboard & status API active",
				"dashboard", "http://"+ln.Addr().String()+"/",
				"status_json", "http://"+ln.Addr().String()+"/status",
			)
			g.wg.Add(1)
			go func() {
				defer g.wg.Done()
				_ = g.httpServer.Serve(ln)
			}()
		}
	}

	// Trigger immediate Who-Is discovery on startup
	g.TriggerDiscovery()

	g.wg.Add(2)
	go g.discoveryLoop(ctx)
	go g.pollLoop(ctx)

	return nil
}

// LocalBACnetAddr returns the UDP address the BACnet listener bound to.
func (g *Gateway) LocalBACnetAddr() *net.UDPAddr {
	return g.bacnetCli.LocalAddr()
}

// TriggerDiscovery sends an active Who-Is broadcast across all target subnets.
func (g *Gateway) TriggerDiscovery() {
	if err := g.bacnetCli.SendWhoIs(g.broadcastTargets, nil, nil); err != nil {
		g.logger.Warn("Who-Is broadcast failed", "err", err)
	} else {
		g.logger.Info("broadcasted BACnet Who-Is discovery", "targets", formatUDPTargets(g.broadcastTargets))
	}
}

// Close gracefully stops the gateway, flushes IMOPS batches, and closes sockets.
func (g *Gateway) Close() error {
	var err error
	g.closeOnce.Do(func() {
		close(g.closed)
		if g.httpServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = g.httpServer.Shutdown(ctx)
			cancel()
		}
		err = g.bacnetCli.Close()
		g.wg.Wait()
		g.imopsCli.Close()
	})
	return err
}

func (g *Gateway) discoveryLoop(ctx context.Context) {
	defer g.wg.Done()
	ticker := time.NewTicker(g.cfg.DiscoveryInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.closed:
			return
		case <-ticker.C:
			g.TriggerDiscovery()
		}
	}
}

func (g *Gateway) pollLoop(ctx context.Context) {
	defer g.wg.Done()
	ticker := time.NewTicker(g.cfg.PollInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.closed:
			return
		case <-ticker.C:
			g.PollAllDevices(ctx)
		}
	}
}

func (g *Gateway) isDeviceAllowed(deviceID uint32) bool {
	if len(g.cfg.Polling.AllowedDeviceIDs) == 0 {
		return true
	}
	for _, id := range g.cfg.Polling.AllowedDeviceIDs {
		if id == deviceID {
			return true
		}
	}
	return false
}

func (g *Gateway) handlePacket(ev bacnet.PacketEvent) {
	g.packetsSeen.Add(1)

	g.pktMu.Lock()
	if len(g.recentPackets) >= 50 {
		copy(g.recentPackets, g.recentPackets[1:])
		g.recentPackets[len(g.recentPackets)-1] = ev
	} else {
		g.recentPackets = append(g.recentPackets, ev)
	}
	g.pktMu.Unlock()

	if g.cfg.Polling.ForwardPacketEvents {
		g.imopsCli.EnqueuePacketEvent(ev)
	}
}

func (g *Gateway) handleIAm(iam *bacnet.IAmNotification) {
	g.iamSeen.Add(1)
	if !g.isDeviceAllowed(iam.DeviceID) {
		g.logger.Debug("ignoring I-Am from non-whitelisted device", "device_id", iam.DeviceID)
		return
	}

	now := time.Now().UTC()
	var shouldSync bool

	g.devMu.Lock()
	dev, exists := g.devices[iam.DeviceID]
	if !exists {
		dev = &ManagedDevice{
			DeviceID:      iam.DeviceID,
			Address:       iam.Source,
			MaxAPDULength: iam.MaxAPDULength,
			Segmentation:  iam.Segmentation,
			VendorID:      iam.VendorID,
			SupportsRPM:   true,
			DiscoveredAt:  now,
			LastSeenAt:    now,
			Points:        make(map[bacnet.ObjectID]*PointMeta),
		}
		g.devices[iam.DeviceID] = dev
		shouldSync = true
		g.logger.Info("auto-detected new BACnet device on subnet",
			"device_id", iam.DeviceID,
			"address", iam.Source.String(),
			"vendor_id", iam.VendorID,
			"max_apdu", iam.MaxAPDULength,
		)
	} else {
		dev.mu.Lock()
		dev.Address = iam.Source
		dev.MaxAPDULength = iam.MaxAPDULength
		dev.Segmentation = iam.Segmentation
		dev.VendorID = iam.VendorID
		dev.LastSeenAt = now
		refreshDue := time.Since(dev.LastMetaSync) >= g.cfg.MetadataRefreshInterval()
		if !dev.SyncInProgress && (dev.LastMetaSync.IsZero() || refreshDue) {
			dev.SyncInProgress = true
			shouldSync = true
		}
		dev.mu.Unlock()
	}
	if exists == false {
		dev.SyncInProgress = true
	}
	g.devMu.Unlock()

	if shouldSync {
		g.wg.Add(1)
		go func(d *ManagedDevice) {
			defer g.wg.Done()
			syncCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			g.syncDeviceMetadataAndInitialPoll(syncCtx, d)
		}(dev)
	}
}

func (g *Gateway) syncDeviceMetadataAndInitialPoll(ctx context.Context, dev *ManagedDevice) {
	defer func() {
		dev.mu.Lock()
		dev.SyncInProgress = false
		dev.mu.Unlock()
	}()

	dev.mu.RLock()
	addr := dev.Address
	devID := dev.DeviceID
	dev.mu.RUnlock()

	devObj := bacnet.ObjectID{Type: bacnet.ObjDevice, Instance: devID}

	// 1. Read Device-level metadata
	var objName, modelName, vendorName, fwRev, descr, location string
	rpmRes, err := g.bacnetCli.ReadPropertyMultiple(ctx, addr, []bacnet.ReadAccessSpec{{
		Object: devObj,
		Properties: []bacnet.PropertyRef{
			{Property: bacnet.PropObjectName},
			{Property: bacnet.PropModelName},
			{Property: bacnet.PropVendorName},
			{Property: bacnet.PropFirmwareRevision},
			{Property: bacnet.PropDescription},
			{Property: bacnet.PropLocation},
		},
	}})
	if err == nil && len(rpmRes) > 0 {
		for _, r := range rpmRes[0].Results {
			if r.HasError || len(r.Values) == 0 {
				continue
			}
			s, _ := r.Values[0].(string)
			switch r.Property {
			case bacnet.PropObjectName:
				objName = s
			case bacnet.PropModelName:
				modelName = s
			case bacnet.PropVendorName:
				vendorName = s
			case bacnet.PropFirmwareRevision:
				fwRev = s
			case bacnet.PropDescription:
				descr = s
			case bacnet.PropLocation:
				location = s
			}
		}
	} else {
		// Fallback to individual ReadProperty calls if controller rejects ReadPropertyMultiple
		dev.mu.Lock()
		dev.SupportsRPM = false
		dev.mu.Unlock()

		objName = g.readStringProp(ctx, addr, devObj, bacnet.PropObjectName)
		modelName = g.readStringProp(ctx, addr, devObj, bacnet.PropModelName)
		vendorName = g.readStringProp(ctx, addr, devObj, bacnet.PropVendorName)
		fwRev = g.readStringProp(ctx, addr, devObj, bacnet.PropFirmwareRevision)
		descr = g.readStringProp(ctx, addr, devObj, bacnet.PropDescription)
		location = g.readStringProp(ctx, addr, devObj, bacnet.PropLocation)
	}

	if objName == "" {
		objName = fmt.Sprintf("Device-%d", devID)
	}
	ctrlRole := ClassifyController(modelName, objName, vendorName, descr)

	// 2. Enumerate Object_List
	objList, err := g.bacnetCli.ReadObjectList(ctx, addr, devID)
	if err != nil {
		g.logger.Warn("failed to read Object_List from device",
			"device_id", devID,
			"name", objName,
			"address", addr.String(),
			"err", err,
		)
		return
	}

	var telemetryObjs []bacnet.ObjectID
	for _, oid := range objList {
		if oid.Type.IsTelemetryObject() {
			telemetryObjs = append(telemetryObjs, oid)
		}
	}

	// 3. Read point metadata (Object_Name, Description, Units / Active_Text / Inactive_Text / State_Text)
	pointMap := g.fetchPointsMetadata(ctx, dev, addr, ctrlRole, telemetryObjs)

	now := time.Now().UTC()
	dev.mu.Lock()
	dev.ObjectName = objName
	dev.ModelName = modelName
	dev.VendorName = vendorName
	dev.FirmwareRevision = fwRev
	dev.Description = descr
	dev.Location = location
	dev.ControllerRole = ctrlRole
	for oid, meta := range pointMap {
		if existing, ok := dev.Points[oid]; ok {
			meta.LastValue = existing.LastValue
			meta.LastValueText = existing.LastValueText
			meta.LastStatusFlags = existing.LastStatusFlags
			meta.LastUpdated = existing.LastUpdated
		}
		dev.Points[oid] = meta
	}
	dev.LastMetaSync = now
	pointCount := len(dev.Points)
	vendorID := dev.VendorID
	discoveredAt := dev.DiscoveredAt
	dev.mu.Unlock()

	g.logger.Info("synchronized BACnet controller metadata",
		"device_id", devID,
		"name", objName,
		"model", modelName,
		"role", ctrlRole,
		"telemetry_points", pointCount,
	)

	// 4. Register discovered device in IMOPS
	if err := g.imopsCli.SendDeviceRegistration(ctx, imops.DeviceRegistration{
		DeviceID:         devID,
		ObjectName:       objName,
		ModelName:        modelName,
		VendorName:       vendorName,
		VendorID:         vendorID,
		FirmwareRevision: fwRev,
		Description:      descr,
		Location:         location,
		ControllerRole:   ctrlRole,
		Address:          addr.String(),
		PointCount:       pointCount,
		DiscoveredAt:     discoveredAt,
	}); err != nil {
		g.logger.Warn("failed to register device with IMOPS", "device_id", devID, "err", err)
	}

	// 5. Subscribe to COV if configured
	if g.cfg.Polling.SubscribeCOV {
		g.renewDeviceCOVSubscriptions(ctx, dev)
	}

	// 6. Run immediate initial poll so IMOPS gets data right away
	g.pollSingleDevice(ctx, dev)
}

func (g *Gateway) fetchPointsMetadata(ctx context.Context, dev *ManagedDevice, addr bacnet.Address, ctrlRole string, objs []bacnet.ObjectID) map[bacnet.ObjectID]*PointMeta {
	out := make(map[bacnet.ObjectID]*PointMeta, len(objs))
	for _, oid := range objs {
		out[oid] = &PointMeta{
			ObjectID: oid,
			Name:     oid.String(),
		}
	}

	dev.mu.RLock()
	supportsRPM := dev.SupportsRPM
	dev.mu.RUnlock()

	batchSize := g.cfg.Polling.RPMBatchSize
	if batchSize <= 0 {
		batchSize = 8
	}

	if supportsRPM {
		for start := 0; start < len(objs); start += batchSize {
			end := start + batchSize
			if end > len(objs) {
				end = len(objs)
			}
			chunk := objs[start:end]
			var specs []bacnet.ReadAccessSpec
			for _, oid := range chunk {
				props := []bacnet.PropertyRef{
					{Property: bacnet.PropObjectName},
					{Property: bacnet.PropDescription},
				}
				if oid.Type.IsAnalogOrNumeric() {
					props = append(props, bacnet.PropertyRef{Property: bacnet.PropUnits})
				} else if oid.Type.IsBinary() {
					props = append(props,
						bacnet.PropertyRef{Property: bacnet.PropInactiveText},
						bacnet.PropertyRef{Property: bacnet.PropActiveText},
					)
				} else {
					props = append(props, bacnet.PropertyRef{Property: bacnet.PropStateText})
				}
				specs = append(specs, bacnet.ReadAccessSpec{Object: oid, Properties: props})
			}

			rpmRes, err := g.bacnetCli.ReadPropertyMultiple(ctx, addr, specs)
			if err != nil {
				supportsRPM = false
				break
			}
			for _, rar := range rpmRes {
				meta := out[rar.Object]
				if meta == nil {
					continue
				}
				for _, pr := range rar.Results {
					if pr.HasError || len(pr.Values) == 0 {
						continue
					}
					applyPointMetaProperty(meta, pr.Property, pr.Values)
				}
			}
		}
	}

	if !supportsRPM {
		for _, oid := range objs {
			meta := out[oid]
			if name := g.readStringProp(ctx, addr, oid, bacnet.PropObjectName); name != "" {
				meta.Name = name
			}
			meta.Description = g.readStringProp(ctx, addr, oid, bacnet.PropDescription)
			if oid.Type.IsAnalogOrNumeric() {
				if res, err := g.bacnetCli.ReadProperty(ctx, addr, oid, bacnet.PropUnits, nil); err == nil && len(res.Values) > 0 {
					applyPointMetaProperty(meta, bacnet.PropUnits, res.Values)
				}
			} else if oid.Type.IsBinary() {
				meta.InactiveText = g.readStringProp(ctx, addr, oid, bacnet.PropInactiveText)
				meta.ActiveText = g.readStringProp(ctx, addr, oid, bacnet.PropActiveText)
			}
		}
	}

	for _, meta := range out {
		eq, bus := ClassifyPoint(ctrlRole, meta.Name, meta.Description, meta.ObjectID, meta.UnitSymbol)
		meta.EquipmentProfile = eq
		meta.ConnectionBus = bus
	}

	return out
}

func applyPointMetaProperty(meta *PointMeta, prop bacnet.PropertyID, vals []any) {
	if len(vals) == 0 {
		return
	}
	switch prop {
	case bacnet.PropObjectName:
		if s, ok := vals[0].(string); ok && s != "" {
			meta.Name = s
		}
	case bacnet.PropDescription:
		if s, ok := vals[0].(string); ok {
			meta.Description = s
		}
	case bacnet.PropUnits:
		switch u := vals[0].(type) {
		case uint32:
			uCopy := u
			meta.UnitID = &uCopy
			meta.UnitSymbol = bacnet.EngineeringUnitSymbol(u)
		case uint64:
			uCopy := uint32(u)
			meta.UnitID = &uCopy
			meta.UnitSymbol = bacnet.EngineeringUnitSymbol(uCopy)
		}
	case bacnet.PropInactiveText:
		if s, ok := vals[0].(string); ok {
			meta.InactiveText = s
		}
	case bacnet.PropActiveText:
		if s, ok := vals[0].(string); ok {
			meta.ActiveText = s
		}
	case bacnet.PropStateText:
		var states []string
		for _, v := range vals {
			if s, ok := v.(string); ok {
				states = append(states, s)
			}
		}
		meta.StateTexts = states
	}
}

func (g *Gateway) readStringProp(ctx context.Context, addr bacnet.Address, obj bacnet.ObjectID, prop bacnet.PropertyID) string {
	res, err := g.bacnetCli.ReadProperty(ctx, addr, obj, prop, nil)
	if err != nil || len(res.Values) == 0 {
		return ""
	}
	s, _ := res.Values[0].(string)
	return s
}

func (g *Gateway) renewDeviceCOVSubscriptions(ctx context.Context, dev *ManagedDevice) {
	dev.mu.RLock()
	addr := dev.Address
	lifetime := g.cfg.Polling.COVLifetimeSec
	if lifetime == 0 {
		lifetime = 300
	}
	if !dev.LastCOVSync.IsZero() && time.Since(dev.LastCOVSync) < time.Duration(lifetime/2)*time.Second {
		dev.mu.RUnlock()
		return
	}
	var objs []bacnet.ObjectID
	for oid := range dev.Points {
		objs = append(objs, oid)
	}
	dev.mu.RUnlock()

	for _, oid := range objs {
		// Ignore errors gently because some BACnet object types (or Modbus virtual points) don't support SubscribeCOV
		_ = g.bacnetCli.SubscribeCOV(ctx, addr, 1, oid, lifetime)
	}

	dev.mu.Lock()
	dev.LastCOVSync = time.Now().UTC()
	dev.mu.Unlock()
}

// PollAllDevices polls Present_Value and Status_Flags across all discovered BACnet controllers.
func (g *Gateway) PollAllDevices(ctx context.Context) {
	g.devMu.RLock()
	devs := make([]*ManagedDevice, 0, len(g.devices))
	for _, d := range g.devices {
		devs = append(devs, d)
	}
	g.devMu.RUnlock()

	if len(devs) == 0 {
		return
	}

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, dev := range devs {
		wg.Add(1)
		sem <- struct{}{}
		go func(d *ManagedDevice) {
			defer wg.Done()
			defer func() { <-sem }()
			if g.cfg.Polling.SubscribeCOV {
				g.renewDeviceCOVSubscriptions(ctx, d)
			}
			g.pollSingleDevice(ctx, d)
		}(dev)
	}
	wg.Wait()
}

func (g *Gateway) pollSingleDevice(ctx context.Context, dev *ManagedDevice) {
	dev.mu.RLock()
	addr := dev.Address
	supportsRPM := dev.SupportsRPM
	objs := make([]bacnet.ObjectID, 0, len(dev.Points))
	for oid := range dev.Points {
		objs = append(objs, oid)
	}
	dev.mu.RUnlock()

	if len(objs) == 0 {
		return
	}

	sort.Slice(objs, func(i, j int) bool {
		if objs[i].Type != objs[j].Type {
			return objs[i].Type < objs[j].Type
		}
		return objs[i].Instance < objs[j].Instance
	})

	type rawPointRead struct {
		val   any
		flags bacnet.StatusFlags
		ok    bool
	}
	collected := make(map[bacnet.ObjectID]rawPointRead, len(objs))

	batchSize := g.cfg.Polling.RPMBatchSize
	if batchSize <= 0 {
		batchSize = 8
	}

	if supportsRPM {
		for start := 0; start < len(objs); start += batchSize {
			end := start + batchSize
			if end > len(objs) {
				end = len(objs)
			}
			chunk := objs[start:end]
			specs := make([]bacnet.ReadAccessSpec, 0, len(chunk))
			for _, oid := range chunk {
				specs = append(specs, bacnet.ReadAccessSpec{
					Object: oid,
					Properties: []bacnet.PropertyRef{
						{Property: bacnet.PropPresentValue},
						{Property: bacnet.PropStatusFlags},
					},
				})
			}

			rpmRes, err := g.bacnetCli.ReadPropertyMultiple(ctx, addr, specs)
			if err != nil {
				supportsRPM = false
				break
			}
			for _, rar := range rpmRes {
				var r rawPointRead
				for _, pr := range rar.Results {
					if pr.HasError || len(pr.Values) == 0 {
						continue
					}
					switch pr.Property {
					case bacnet.PropPresentValue:
						r.val = pr.Values[0]
						r.ok = true
					case bacnet.PropStatusFlags:
						if sf, ok := pr.Values[0].(bacnet.StatusFlags); ok {
							r.flags = sf
						}
					}
				}
				if r.ok {
					collected[rar.Object] = r
				}
			}
		}
	}

	// Individual ReadProperty fallback for any unread objects when RPM is unsupported
	if !supportsRPM {
		for _, oid := range objs {
			if _, already := collected[oid]; already {
				continue
			}
			pvRes, err := g.bacnetCli.ReadProperty(ctx, addr, oid, bacnet.PropPresentValue, nil)
			if err != nil || len(pvRes.Values) == 0 {
				continue
			}
			var sf bacnet.StatusFlags
			if sfRes, sfErr := g.bacnetCli.ReadProperty(ctx, addr, oid, bacnet.PropStatusFlags, nil); sfErr == nil && len(sfRes.Values) > 0 {
				if parsedSF, ok := sfRes.Values[0].(bacnet.StatusFlags); ok {
					sf = parsedSF
				}
			}
			collected[oid] = rawPointRead{
				val:   pvRes.Values[0],
				flags: sf,
				ok:    true,
			}
		}
	}

	if len(collected) == 0 {
		return
	}

	now := time.Now().UTC()
	var readings []imops.TelemetryReading

	dev.mu.Lock()
	dev.LastSeenAt = now
	for oid, item := range collected {
		meta, exists := dev.Points[oid]
		if !exists {
			continue
		}
		normVal, valText := formatPointValue(meta, item.val)
		meta.LastValue = normVal
		meta.LastValueText = valText
		meta.LastStatusFlags = item.flags
		meta.LastUpdated = now

		readings = append(readings, buildTelemetryReading(dev, meta, normVal, valText, item.flags, "poll", now))
	}
	dev.mu.Unlock()

	g.imopsCli.EnqueueTelemetry(readings...)
}

func (g *Gateway) handleCOV(cov *bacnet.COVNotification) {
	g.covSeen.Add(1)
	if !g.isDeviceAllowed(cov.InitiatingDeviceID) {
		return
	}

	var pv any
	var hasPV bool
	var sf bacnet.StatusFlags
	for _, v := range cov.Values {
		switch v.PropertyID {
		case bacnet.PropPresentValue:
			pv = v.Value
			hasPV = true
		case bacnet.PropStatusFlags:
			if parsed, ok := v.Value.(bacnet.StatusFlags); ok {
				sf = parsed
			}
		}
	}
	if !hasPV {
		return
	}

	g.devMu.RLock()
	dev, exists := g.devices[cov.InitiatingDeviceID]
	g.devMu.RUnlock()

	now := time.Now().UTC()
	if !exists {
		// Trigger discovery for unknown COV sender and still forward reading
		_ = g.bacnetCli.SendWhoIs([]*net.UDPAddr{cov.Source.UDPAddr()}, &cov.InitiatingDeviceID, &cov.InitiatingDeviceID)
		g.imopsCli.EnqueueTelemetry(imops.TelemetryReading{
			Timestamp:      now,
			SourceType:     "cov",
			DeviceID:       cov.InitiatingDeviceID,
			DeviceName:     fmt.Sprintf("Device-%d", cov.InitiatingDeviceID),
			DeviceAddress:  cov.Source.String(),
			ObjectType:     cov.MonitoredObject.Type.String(),
			ObjectInstance: cov.MonitoredObject.Instance,
			ObjectID:       cov.MonitoredObject.String(),
			PointName:      cov.MonitoredObject.String(),
			Value:          pv,
			Quality:        deriveQuality(sf),
			StatusFlags:    sf,
		})
		return
	}

	dev.mu.Lock()
	dev.LastSeenAt = now
	meta, ok := dev.Points[cov.MonitoredObject]
	if !ok {
		meta = &PointMeta{
			ObjectID: cov.MonitoredObject,
			Name:     cov.MonitoredObject.String(),
		}
		dev.Points[cov.MonitoredObject] = meta
	}
	normVal, valText := formatPointValue(meta, pv)
	meta.LastValue = normVal
	meta.LastValueText = valText
	meta.LastStatusFlags = sf
	meta.LastUpdated = now
	reading := buildTelemetryReading(dev, meta, normVal, valText, sf, "cov", now)
	dev.mu.Unlock()

	g.imopsCli.EnqueueTelemetry(reading)
}

func buildTelemetryReading(dev *ManagedDevice, meta *PointMeta, normVal any, valText string, sf bacnet.StatusFlags, sourceType string, ts time.Time) imops.TelemetryReading {
	return imops.TelemetryReading{
		Timestamp:        ts,
		SourceType:       sourceType,
		DeviceID:         dev.DeviceID,
		DeviceName:       dev.ObjectName,
		ControllerModel:  dev.ModelName,
		DeviceAddress:    dev.Address.String(),
		ObjectType:       meta.ObjectID.Type.String(),
		ObjectInstance:   meta.ObjectID.Instance,
		ObjectID:         meta.ObjectID.String(),
		PointName:        meta.Name,
		Description:      meta.Description,
		EquipmentProfile: meta.EquipmentProfile,
		ConnectionBus:    meta.ConnectionBus,
		Value:            normVal,
		ValueText:        valText,
		Unit:             meta.UnitSymbol,
		UnitID:           meta.UnitID,
		Quality:          deriveQuality(sf),
		StatusFlags:      sf,
	}
}

func formatPointValue(meta *PointMeta, raw any) (any, string) {
	if meta.ObjectID.Type.IsBinary() {
		var active bool
		switch v := raw.(type) {
		case uint32:
			active = v != 0
		case uint64:
			active = v != 0
		case bool:
			active = v
		}
		if active {
			if meta.ActiveText != "" {
				return 1, meta.ActiveText
			}
			return 1, "Active"
		}
		if meta.InactiveText != "" {
			return 0, meta.InactiveText
		}
		return 0, "Inactive"
	}

	switch v := raw.(type) {
	case float64:
		// Round float32->float64 artifacts cleanly to 4 decimal places
		rounded, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 4, 64), 64)
		if meta.UnitSymbol != "" {
			return rounded, fmt.Sprintf("%g %s", rounded, meta.UnitSymbol)
		}
		return rounded, fmt.Sprintf("%g", rounded)
	case uint32:
		if len(meta.StateTexts) > 0 && int(v) >= 1 && int(v) <= len(meta.StateTexts) {
			return v, meta.StateTexts[v-1]
		}
		return v, fmt.Sprintf("%d", v)
	case uint64:
		if len(meta.StateTexts) > 0 && int(v) >= 1 && int(v) <= len(meta.StateTexts) {
			return v, meta.StateTexts[v-1]
		}
		if meta.UnitSymbol != "" {
			return v, fmt.Sprintf("%d %s", v, meta.UnitSymbol)
		}
		return v, fmt.Sprintf("%d", v)
	default:
		return raw, fmt.Sprintf("%v", raw)
	}
}

func deriveQuality(sf bacnet.StatusFlags) string {
	switch {
	case sf.Fault:
		return "fault"
	case sf.OutOfService:
		return "out_of_service"
	case sf.InAlarm:
		return "alarm"
	case sf.Overridden:
		return "overridden"
	default:
		return "good"
	}
}

func formatUDPTargets(targets []*net.UDPAddr) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		if t != nil {
			out = append(out, t.String())
		}
	}
	return out
}

// DeviceSummary represents a discovered controller in the /status response.
type DeviceSummary struct {
	DeviceID         uint32       `json:"device_id"`
	ObjectName       string       `json:"object_name"`
	ModelName        string       `json:"model_name"`
	VendorName       string       `json:"vendor_name"`
	ControllerRole   string       `json:"controller_role"`
	Address          string       `json:"address"`
	DiscoveredAt     time.Time    `json:"discovered_at"`
	LastSeenAt       time.Time    `json:"last_seen_at"`
	Points           []*PointMeta `json:"points"`
}

// StatusSnapshot is returned by GET /status (and usable directly in tests).
type StatusSnapshot struct {
	GatewayID        string                `json:"gateway_id"`
	SiteName         string                `json:"site_name"`
	UptimeSeconds    int64                 `json:"uptime_seconds"`
	BACnetListenAddr string                `json:"bacnet_listen_addr"`
	DetectedSubnets  []bacnet.SubnetInfo   `json:"detected_subnets"`
	BroadcastTargets []string              `json:"broadcast_targets"`
	PacketsSeen      uint64                `json:"packets_seen"`
	IAmReceived      uint64                `json:"iam_received"`
	COVReceived      uint64                `json:"cov_received"`
	DeviceCount      int                   `json:"device_count"`
	Devices          []DeviceSummary       `json:"devices"`
	IMOPS            imops.Stats           `json:"imops"`
	RecentPackets    []bacnet.PacketEvent  `json:"recent_packets"`
}

// Snapshot returns the current operational state of the gateway.
func (g *Gateway) Snapshot() StatusSnapshot {
	g.devMu.RLock()
	devs := make([]DeviceSummary, 0, len(g.devices))
	for _, d := range g.devices {
		d.mu.RLock()
		pts := make([]*PointMeta, 0, len(d.Points))
		for _, p := range d.Points {
			cp := *p
			pts = append(pts, &cp)
		}
		sort.Slice(pts, func(i, j int) bool {
			if pts[i].ObjectID.Type != pts[j].ObjectID.Type {
				return pts[i].ObjectID.Type < pts[j].ObjectID.Type
			}
			return pts[i].ObjectID.Instance < pts[j].ObjectID.Instance
		})
		devs = append(devs, DeviceSummary{
			DeviceID:       d.DeviceID,
			ObjectName:     d.ObjectName,
			ModelName:      d.ModelName,
			VendorName:     d.VendorName,
			ControllerRole: d.ControllerRole,
			Address:        d.Address.String(),
			DiscoveredAt:   d.DiscoveredAt,
			LastSeenAt:     d.LastSeenAt,
			Points:         pts,
		})
		d.mu.RUnlock()
	}
	g.devMu.RUnlock()

	sort.Slice(devs, func(i, j int) bool {
		return devs[i].DeviceID < devs[j].DeviceID
	})

	g.pktMu.Lock()
	pkts := make([]bacnet.PacketEvent, len(g.recentPackets))
	copy(pkts, g.recentPackets)
	g.pktMu.Unlock()

	return StatusSnapshot{
		GatewayID:        g.cfg.GatewayID,
		SiteName:         g.cfg.SiteName,
		UptimeSeconds:    int64(time.Since(g.startedAt).Seconds()),
		BACnetListenAddr: g.bacnetCli.LocalAddr().String(),
		DetectedSubnets:  g.subnets,
		BroadcastTargets: formatUDPTargets(g.broadcastTargets),
		PacketsSeen:      g.packetsSeen.Load(),
		IAmReceived:      g.iamSeen.Load(),
		COVReceived:      g.covSeen.Load(),
		DeviceCount:      len(devs),
		Devices:          devs,
		IMOPS:            g.imopsCli.SnapshotStats(),
		RecentPackets:    pkts,
	}
}

func (g *Gateway) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"gateway_id":   g.cfg.GatewayID,
		"device_count": g.Snapshot().DeviceCount,
	})
}

func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(g.Snapshot())
}

func (g *Gateway) handleTriggerDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST /discover", http.StatusMethodNotAllowed)
		return
	}
	g.TriggerDiscovery()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "discovery_triggered"})
}

