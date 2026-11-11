package bacnet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ClientConfig configures the BACnet/IP UDP listener and active client.
type ClientConfig struct {
	BindIP            string
	Port              int
	ReusePort         bool
	IncludeRawHex     bool
	RequestTimeout    time.Duration
	MaxRetries        int
	BBMDAddress       string
	BBMDTTLSeconds    uint16
	Logger            *slog.Logger
	OnIAm             func(*IAmNotification)
	OnCOV             func(*COVNotification)
	OnPacket          func(PacketEvent)
	OnConfirmedServer func(frame *DecodedFrame, rawConn net.PacketConn, udpSrc *net.UDPAddr) // Optional hook for simulator/responder
	OnWhoIsServer     func(low, high *uint32, udpSrc *net.UDPAddr)                            // Optional hook for simulator/responder
}

type pendingKey struct {
	ipPort string
	invoke byte
}

type txResult struct {
	serviceChoice byte
	payload       []byte
	simpleACK     bool
	err           error
}

type pendingTx struct {
	dest          Address
	segmentedBuf  []byte
	expectedSeq   byte
	inSegmented   bool
	serviceChoice byte
	done          chan txResult
	once          sync.Once
}

func (p *pendingTx) finish(res txResult) {
	p.once.Do(func() {
		p.done <- res
	})
}

// Client is a concurrent BACnet/IP UDP listener and active client.
type Client struct {
	cfg       ClientConfig
	conn      net.PacketConn
	localAddr *net.UDPAddr
	logger    *slog.Logger

	invokeSeq atomic.Uint32
	mu        sync.Mutex
	pending   map[pendingKey]*pendingTx

	bbmdAddr  *net.UDPAddr
	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// NewClient binds the UDP socket and starts the BACnet/IP listener loop.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
	if cfg.BindIP == "" {
		cfg.BindIP = "0.0.0.0"
	}
	if cfg.Port < 0 {
		cfg.Port = DefaultPort
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 3 * time.Second
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 2
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	lc := net.ListenConfig{
		Control: socketControl(cfg.ReusePort),
	}
	bindStr := fmt.Sprintf("%s:%d", cfg.BindIP, cfg.Port)
	pktConn, err := lc.ListenPacket(ctx, "udp4", bindStr)
	if err != nil {
		return nil, fmt.Errorf("bind BACnet UDP %s: %w", bindStr, err)
	}

	localUDP, _ := pktConn.LocalAddr().(*net.UDPAddr)

	var bbmdUDP *net.UDPAddr
	if cfg.BBMDAddress != "" {
		resolved, err := net.ResolveUDPAddr("udp4", cfg.BBMDAddress)
		if err != nil {
			_ = pktConn.Close()
			return nil, fmt.Errorf("resolve BBMD address %q: %w", cfg.BBMDAddress, err)
		}
		bbmdUDP = resolved
	}

	c := &Client{
		cfg:       cfg,
		conn:      pktConn,
		localAddr: localUDP,
		logger:    logger,
		pending:   make(map[pendingKey]*pendingTx),
		bbmdAddr:  bbmdUDP,
		closed:    make(chan struct{}),
	}

	c.wg.Add(1)
	go c.readLoop()

	if bbmdUDP != nil {
		ttl := cfg.BBMDTTLSeconds
		if ttl == 0 {
			ttl = 120
		}
		if err := c.RegisterForeignDevice(bbmdUDP, ttl); err != nil {
			logger.Warn("initial BBMD foreign device registration failed", "bbmd", bbmdUDP.String(), "err", err)
		}
		c.wg.Add(1)
		go c.bbmdRenewalLoop(bbmdUDP, ttl)
	}

	return c, nil
}

// LocalAddr returns the bound UDP address of the client.
func (c *Client) LocalAddr() *net.UDPAddr {
	return c.localAddr
}

// Close stops the listener and releases the UDP socket.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.conn.Close()
		c.wg.Wait()
	})
	return err
}

// WriteRaw sends a raw BACnet/IP frame to target.
func (c *Client) WriteRaw(frame []byte, target *net.UDPAddr) error {
	_, err := c.conn.WriteTo(frame, target)
	return err
}

// RegisterForeignDevice sends a BVLC Register-Foreign-Device packet to a BBMD.
func (c *Client) RegisterForeignDevice(bbmd *net.UDPAddr, ttlSeconds uint16) error {
	pkt := EncodeRegisterForeignDevice(ttlSeconds)
	_, err := c.conn.WriteTo(pkt, bbmd)
	return err
}

func (c *Client) bbmdRenewalLoop(bbmd *net.UDPAddr, ttl uint16) {
	defer c.wg.Done()
	interval := time.Duration(ttl/2) * time.Second
	if interval < 15*time.Second {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case <-ticker.C:
			if err := c.RegisterForeignDevice(bbmd, ttl); err != nil {
				c.logger.Warn("BBMD foreign device renewal failed", "bbmd", bbmd.String(), "err", err)
			}
		}
	}
}

// SendWhoIs broadcasts a Who-Is discovery packet to all provided broadcast targets (and BBMD if configured).
func (c *Client) SendWhoIs(broadcastTargets []*net.UDPAddr, low, high *uint32) error {
	var firstErr error
	if c.bbmdAddr != nil {
		pkt := EncodeWhoIs(low, high, true)
		if _, err := c.conn.WriteTo(pkt, c.bbmdAddr); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	pkt := EncodeWhoIs(low, high, false)
	sent := make(map[string]bool)
	for _, target := range broadcastTargets {
		if target == nil {
			continue
		}
		key := target.String()
		if sent[key] {
			continue
		}
		sent[key] = true
		if _, err := c.conn.WriteTo(pkt, target); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *Client) readLoop() {
	defer c.wg.Done()
	buf := make([]byte, 2048)

	for {
		n, addr, err := c.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-c.closed:
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				c.logger.Debug("UDP read error", "err", err)
				continue
			}
		}

		udpSrc, _ := addr.(*net.UDPAddr)
		raw := make([]byte, n)
		copy(raw, buf[:n])

		frame, err := DecodeFrame(raw, udpSrc)
		if err != nil {
			c.logger.Debug("ignored malformed or non-BACnet packet", "from", addr.String(), "bytes", n, "err", err)
			continue
		}

		if c.cfg.OnPacket != nil {
			var rawHex string
			if c.cfg.IncludeRawHex {
				rawHex = hex.EncodeToString(raw)
			}
			c.cfg.OnPacket(DescribeFrame(frame, n, rawHex))
		}

		if frame.IsNetworkMsg {
			continue
		}

		switch frame.PDUType {
		case PDUTypeUnconfirmedRequest:
			c.handleUnconfirmed(frame, udpSrc)
		case PDUTypeSimpleACK, PDUTypeComplexACK, PDUTypeError, PDUTypeReject, PDUTypeAbort:
			c.handleClientResponse(frame, udpSrc)
		case PDUTypeConfirmedRequest:
			if c.cfg.OnConfirmedServer != nil {
				c.cfg.OnConfirmedServer(frame, c.conn, udpSrc)
			}
		}
	}
}

func (c *Client) handleUnconfirmed(frame *DecodedFrame, udpSrc *net.UDPAddr) {
	switch frame.ServiceChoice {
	case ServiceUnconfirmedIAm:
		iam, err := DecodeIAm(frame.ServicePayload, frame.OriginAddress)
		if err != nil {
			c.logger.Warn("failed to decode I-Am", "from", frame.OriginAddress.String(), "err", err)
			return
		}
		if c.cfg.OnIAm != nil {
			c.cfg.OnIAm(iam)
		}

	case ServiceUnconfirmedCOVNotification:
		cov, err := DecodeUnconfirmedCOV(frame.ServicePayload, frame.OriginAddress)
		if err != nil {
			c.logger.Warn("failed to decode Unconfirmed-COV", "from", frame.OriginAddress.String(), "err", err)
			return
		}
		if c.cfg.OnCOV != nil {
			c.cfg.OnCOV(cov)
		}

	case ServiceUnconfirmedWhoIs:
		if c.cfg.OnWhoIsServer != nil {
			low, high, err := DecodeWhoIs(frame.ServicePayload)
			if err == nil {
				c.cfg.OnWhoIsServer(low, high, udpSrc)
			}
		}
	}
}

func ipPortKey(udpAddr *net.UDPAddr) string {
	if udpAddr == nil {
		return ""
	}
	if ip4 := udpAddr.IP.To4(); ip4 != nil {
		return fmt.Sprintf("%s:%d", ip4.String(), udpAddr.Port)
	}
	return udpAddr.String()
}

func (c *Client) handleClientResponse(frame *DecodedFrame, udpSrc *net.UDPAddr) {
	key := pendingKey{
		ipPort: ipPortKey(udpSrc),
		invoke: frame.InvokeID,
	}

	c.mu.Lock()
	tx, ok := c.pending[key]
	c.mu.Unlock()
	if !ok {
		return
	}

	switch frame.PDUType {
	case PDUTypeSimpleACK:
		tx.finish(txResult{
			serviceChoice: frame.ServiceChoice,
			simpleACK:     true,
		})

	case PDUTypeComplexACK:
		segmented := (frame.PDUFlags & 0x08) != 0
		moreFollows := (frame.PDUFlags & 0x04) != 0

		if !segmented {
			payload := make([]byte, len(frame.ServicePayload))
			copy(payload, frame.ServicePayload)
			tx.finish(txResult{
				serviceChoice: frame.ServiceChoice,
				payload:       payload,
			})
			return
		}

		// Reassemble segmented ComplexACK
		c.mu.Lock()
		if !tx.inSegmented {
			tx.inSegmented = true
			tx.expectedSeq = 0
			tx.serviceChoice = frame.ServiceChoice
		}
		if frame.SeqNumber == tx.expectedSeq {
			tx.segmentedBuf = append(tx.segmentedBuf, frame.ServicePayload...)
			tx.expectedSeq++
			ackPkt := EncodeSegmentACK(tx.dest, frame.InvokeID, frame.SeqNumber, 1, false)
			_, _ = c.conn.WriteTo(ackPkt, udpSrc)
			if !moreFollows {
				full := make([]byte, len(tx.segmentedBuf))
				copy(full, tx.segmentedBuf)
				svc := tx.serviceChoice
				c.mu.Unlock()
				tx.finish(txResult{
					serviceChoice: svc,
					payload:       full,
				})
				return
			}
		} else {
			// Out-of-order segment: send Negative SegmentACK requesting last good seq
			lastSeq := tx.expectedSeq - 1
			nackPkt := EncodeSegmentACK(tx.dest, frame.InvokeID, lastSeq, 1, true)
			_, _ = c.conn.WriteTo(nackPkt, udpSrc)
		}
		c.mu.Unlock()

	case PDUTypeError:
		tx.finish(txResult{err: DecodeErrorPayload(frame.ServicePayload)})

	case PDUTypeReject:
		tx.finish(txResult{err: &BACnetError{Kind: "Reject", Reason: frame.ServiceChoice}})

	case PDUTypeAbort:
		tx.finish(txResult{err: &BACnetError{Kind: "Abort", Reason: frame.ServiceChoice}})
	}
}

func (c *Client) nextInvokeID() byte {
	v := c.invokeSeq.Add(1)
	id := byte(v % 255)
	if id == 0 {
		id = 1
	}
	return id
}

func (c *Client) doConfirmedRequest(ctx context.Context, dest Address, buildPacket func(invokeID byte) []byte) (txResult, error) {
	udpTarget := dest.UDPAddr()
	invokeID := c.nextInvokeID()
	key := pendingKey{
		ipPort: ipPortKey(udpTarget),
		invoke: invokeID,
	}

	tx := &pendingTx{
		dest: dest,
		done: make(chan txResult, 1),
	}

	c.mu.Lock()
	c.pending[key] = tx
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
	}()

	pkt := buildPacket(invokeID)
	attempts := c.cfg.MaxRetries + 1

	for attempt := 0; attempt < attempts; attempt++ {
		if _, err := c.conn.WriteTo(pkt, udpTarget); err != nil {
			return txResult{}, fmt.Errorf("write UDP to %s: %w", udpTarget.String(), err)
		}

		timer := time.NewTimer(c.cfg.RequestTimeout)
		select {
		case <-ctx.Done():
			timer.Stop()
			return txResult{}, ctx.Err()
		case <-c.closed:
			timer.Stop()
			return txResult{}, errors.New("BACnet client closed")
		case res := <-tx.done:
			timer.Stop()
			return res, res.err
		case <-timer.C:
			// Timeout on this attempt; retry if attempts remain
		}
	}

	return txResult{}, fmt.Errorf("BACnet request timeout after %d attempt(s) to %s", attempts, dest.String())
}

// ReadProperty executes a Confirmed ReadProperty request and returns the decoded values.
func (c *Client) ReadProperty(ctx context.Context, dest Address, obj ObjectID, prop PropertyID, arrayIndex *uint32) (*ReadPropertyResult, error) {
	res, err := c.doConfirmedRequest(ctx, dest, func(invokeID byte) []byte {
		return EncodeReadProperty(dest, invokeID, obj, prop, arrayIndex, true)
	})
	if err != nil {
		return nil, err
	}
	return DecodeReadPropertyACK(res.payload)
}

// ReadPropertyMultiple executes a Confirmed ReadPropertyMultiple request.
func (c *Client) ReadPropertyMultiple(ctx context.Context, dest Address, specs []ReadAccessSpec) ([]ReadAccessResult, error) {
	res, err := c.doConfirmedRequest(ctx, dest, func(invokeID byte) []byte {
		return EncodeReadPropertyMultiple(dest, invokeID, specs, true)
	})
	if err != nil {
		return nil, err
	}
	return DecodeReadPropertyMultipleACK(res.payload)
}

// SubscribeCOV subscribes to unconfirmed COV notifications for a specific object.
func (c *Client) SubscribeCOV(ctx context.Context, dest Address, processID uint32, obj ObjectID, lifetimeSec uint32) error {
	_, err := c.doConfirmedRequest(ctx, dest, func(invokeID byte) []byte {
		return EncodeSubscribeCOV(dest, invokeID, processID, obj, false, lifetimeSec)
	})
	return err
}

// ReadObjectList reads all ObjectIDs exposed in a Device's Object_List, automatically falling back
// to indexed array reads (index 0 for count, then 1..N) if the device cannot fit Object_List in one APDU.
func (c *Client) ReadObjectList(ctx context.Context, dest Address, deviceID uint32) ([]ObjectID, error) {
	devObj := ObjectID{Type: ObjDevice, Instance: deviceID}
	res, err := c.ReadProperty(ctx, dest, devObj, PropObjectList, nil)
	if err == nil {
		return extractObjectIDs(res.Values), nil
	}

	// Fallback: read Object_List[0] for array length, then read elements 1..N
	idxZero := uint32(0)
	cntRes, cntErr := c.ReadProperty(ctx, dest, devObj, PropObjectList, &idxZero)
	if cntErr != nil {
		return nil, fmt.Errorf("read Object_List failed (%v) and indexed fallback failed (%w)", err, cntErr)
	}
	if len(cntRes.Values) == 0 {
		return nil, nil
	}
	count, ok := cntRes.Values[0].(uint64)
	if !ok {
		return nil, fmt.Errorf("unexpected Object_List[0] count type %T", cntRes.Values[0])
	}

	var objects []ObjectID
	const batchSize = 12
	for start := uint32(1); start <= uint32(count); start += batchSize {
		end := start + batchSize - 1
		if end > uint32(count) {
			end = uint32(count)
		}

		// Try batching indexed reads via ReadPropertyMultiple first
		var propRefs []PropertyRef
		for idx := start; idx <= end; idx++ {
			idxCopy := idx
			propRefs = append(propRefs, PropertyRef{
				Property:   PropObjectList,
				ArrayIndex: &idxCopy,
			})
		}
		rpmRes, rpmErr := c.ReadPropertyMultiple(ctx, dest, []ReadAccessSpec{{
			Object:     devObj,
			Properties: propRefs,
		}})
		if rpmErr == nil && len(rpmRes) > 0 {
			for _, pr := range rpmRes[0].Results {
				if !pr.HasError {
					objects = append(objects, extractObjectIDs(pr.Values)...)
				}
			}
			continue
		}

		// Individual ReadProperty fallback per index
		for idx := start; idx <= end; idx++ {
			idxCopy := idx
			itemRes, itemErr := c.ReadProperty(ctx, dest, devObj, PropObjectList, &idxCopy)
			if itemErr != nil {
				return objects, itemErr
			}
			objects = append(objects, extractObjectIDs(itemRes.Values)...)
		}
	}

	return objects, nil
}

func extractObjectIDs(vals []any) []ObjectID {
	var ids []ObjectID
	for _, v := range vals {
		if oid, ok := v.(ObjectID); ok {
			ids = append(ids, oid)
		}
	}
	return ids
}

