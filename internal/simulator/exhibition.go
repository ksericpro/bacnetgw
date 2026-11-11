package simulator

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"bacnet-gw/internal/bacnet"
)

// SimPoint defines a simulated BACnet object on a mock Optergy controller.
type SimPoint struct {
	ObjectID     bacnet.ObjectID
	Name         string
	Description  string
	UnitID       uint32
	PresentValue any // float32 or uint32 (enumerated binary/multistate)
	StatusFlags  bacnet.StatusFlags
	InactiveText string
	ActiveText   string
}

// SimController emulates a BACnet/IP controller (e.g. Optergy AXON, P864, or P442) over UDP.
type SimController struct {
	DeviceID         uint32
	ObjectName       string
	ModelName        string
	VendorName       string
	VendorID         uint32
	FirmwareRevision string
	Description      string
	Location         string
	DisableRPM       bool // Optional test toggle to verify gateway ReadProperty fallback
	ForceIndexedList bool // Optional test toggle to verify gateway Object_List[0..N] fallback

	mu     sync.RWMutex
	points []SimPoint
	byID   map[bacnet.ObjectID]*SimPoint

	cli *bacnet.Client
}

// Start binds a UDP port for this simulated controller and starts answering BACnet requests.
func (sc *SimController) Start(ctx context.Context, bindIP string, port int, logger *slog.Logger) error {
	sc.mu.Lock()
	sc.byID = make(map[bacnet.ObjectID]*SimPoint, len(sc.points))
	for i := range sc.points {
		p := &sc.points[i]
		sc.byID[p.ObjectID] = p
	}
	sc.mu.Unlock()

	cli, err := bacnet.NewClient(ctx, bacnet.ClientConfig{
		BindIP:            bindIP,
		Port:              port,
		ReusePort:         true,
		Logger:            logger,
		OnWhoIsServer:     sc.handleWhoIs,
		OnConfirmedServer: sc.handleConfirmed,
	})
	if err != nil {
		return err
	}
	sc.cli = cli
	return nil
}

// AddPoints registers simulated points prior to Start.
func (sc *SimController) AddPoints(pts ...SimPoint) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.points = append(sc.points, pts...)
}

// LocalAddr returns the bound UDP address of the simulated controller.
func (sc *SimController) LocalAddr() *net.UDPAddr {
	return sc.cli.LocalAddr()
}

// Close shuts down the simulated controller socket.
func (sc *SimController) Close() error {
	if sc.cli != nil {
		return sc.cli.Close()
	}
	return nil
}

// BroadcastIAm sends a spontaneous I-Am packet to target.
func (sc *SimController) BroadcastIAm(target *net.UDPAddr) error {
	pkt := bacnet.EncodeIAm(sc.DeviceID, 1476, bacnet.SegmentationBoth, sc.VendorID, nil)
	return sc.cli.WriteRaw(pkt, target)
}

// EmitCOV updates a point's value/flags and sends an Unconfirmed-COV-Notification to target.
func (sc *SimController) EmitCOV(target *net.UDPAddr, objID bacnet.ObjectID, newVal float32, flags bacnet.StatusFlags) error {
	sc.mu.Lock()
	if pt, ok := sc.byID[objID]; ok {
		if objID.Type.IsBinary() {
			if newVal > 0 {
				pt.PresentValue = uint32(1)
			} else {
				pt.PresentValue = uint32(0)
			}
		} else {
			pt.PresentValue = newVal
		}
		pt.StatusFlags = flags
	}
	sc.mu.Unlock()

	pkt := bacnet.EncodeUnconfirmedCOV(1, sc.DeviceID, objID, 300, newVal, flags)
	return sc.cli.WriteRaw(pkt, target)
}

func (sc *SimController) handleWhoIs(low, high *uint32, udpSrc *net.UDPAddr) {
	if low != nil && high != nil {
		if sc.DeviceID < *low || sc.DeviceID > *high {
			return
		}
	}
	pkt := bacnet.EncodeIAm(sc.DeviceID, 1476, bacnet.SegmentationBoth, sc.VendorID, nil)
	_ = sc.cli.WriteRaw(pkt, udpSrc)
}

func (sc *SimController) handleConfirmed(frame *bacnet.DecodedFrame, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	switch frame.ServiceChoice {
	case bacnet.ServiceConfirmedSubscribeCOV:
		// Reply SimpleACK
		apdu := []byte{bacnet.PDUTypeSimpleACK << 4, frame.InvokeID, bacnet.ServiceConfirmedSubscribeCOV}
		npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
		_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)

	case bacnet.ServiceConfirmedReadProperty:
		sc.replyReadProperty(frame, rawConn, udpSrc)

	case bacnet.ServiceConfirmedReadPropertyMultiple:
		if sc.DisableRPM {
			sc.replyReject(frame.InvokeID, 9, rawConn, udpSrc) // unrecognized-service
			return
		}
		sc.replyReadPropertyMultiple(frame, rawConn, udpSrc)

	default:
		sc.replyReject(frame.InvokeID, 9, rawConn, udpSrc)
	}
}

func (sc *SimController) replyReject(invokeID byte, reason byte, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	apdu := []byte{bacnet.PDUTypeReject << 4, invokeID, reason}
	npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
	_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)
}

func (sc *SimController) replyAbort(invokeID byte, reason byte, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	apdu := []byte{(bacnet.PDUTypeAbort << 4) | 0x01, invokeID, reason}
	npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
	_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)
}

func (sc *SimController) replyError(invokeID byte, service byte, errClass, errCode uint32, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	apdu := []byte{bacnet.PDUTypeError << 4, invokeID, service}
	apdu = bacnet.AppendAppEnumerated(apdu, errClass)
	apdu = bacnet.AppendAppEnumerated(apdu, errCode)
	npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
	_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)
}

func (sc *SimController) replyReadProperty(frame *bacnet.DecodedFrame, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	p := frame.ServicePayload
	hdr0, off, err := bacnet.ReadTag(p, 0)
	if err != nil || !hdr0.IsContext || hdr0.TagNumber != 0 || off+int(hdr0.Length) > len(p) {
		sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
		return
	}
	obj, err := bacnet.DecodeObjectIDBytes(p[off : off+int(hdr0.Length)])
	if err != nil {
		sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
		return
	}
	off += int(hdr0.Length)

	hdr1, off, err := bacnet.ReadTag(p, off)
	if err != nil || !hdr1.IsContext || hdr1.TagNumber != 1 || off+int(hdr1.Length) > len(p) {
		sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
		return
	}
	prop := bacnet.PropertyID(bacnet.DecodeUnsigned(p[off : off+int(hdr1.Length)]))
	off += int(hdr1.Length)

	var arrIdx *uint32
	if off < len(p) {
		hdr2, off2, err := bacnet.ReadTag(p, off)
		if err == nil && hdr2.IsContext && hdr2.TagNumber == 2 && off2+int(hdr2.Length) <= len(p) {
			idx := uint32(bacnet.DecodeUnsigned(p[off2 : off2+int(hdr2.Length)]))
			arrIdx = &idx
		}
	}

	if sc.ForceIndexedList && obj.Type == bacnet.ObjDevice && prop == bacnet.PropObjectList && arrIdx == nil {
		sc.replyAbort(frame.InvokeID, 4, rawConn, udpSrc) // segmentation-not-supported -> triggers indexed fallback!
		return
	}

	encodedVal, propErr := sc.encodePropertyValue(obj, prop, arrIdx)
	if propErr != nil {
		sc.replyError(frame.InvokeID, bacnet.ServiceConfirmedReadProperty, 2, 32, rawConn, udpSrc) // property: unknown-property
		return
	}

	apdu := []byte{bacnet.PDUTypeComplexACK << 4, frame.InvokeID, bacnet.ServiceConfirmedReadProperty}
	apdu = bacnet.AppendContextObjectID(apdu, 0, obj)
	apdu = bacnet.AppendContextUnsigned(apdu, 1, uint64(prop))
	if arrIdx != nil {
		apdu = bacnet.AppendContextUnsigned(apdu, 2, uint64(*arrIdx))
	}
	apdu = bacnet.WriteOpeningTag(apdu, 3)
	apdu = append(apdu, encodedVal...)
	apdu = bacnet.WriteClosingTag(apdu, 3)

	npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
	_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)
}

func (sc *SimController) replyReadPropertyMultiple(frame *bacnet.DecodedFrame, rawConn net.PacketConn, udpSrc *net.UDPAddr) {
	p := frame.ServicePayload
	off := 0
	apdu := []byte{bacnet.PDUTypeComplexACK << 4, frame.InvokeID, bacnet.ServiceConfirmedReadPropertyMultiple}

	for off < len(p) {
		hdrObj, next, err := bacnet.ReadTag(p, off)
		if err != nil || !hdrObj.IsContext || hdrObj.TagNumber != 0 || next+int(hdrObj.Length) > len(p) {
			sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
			return
		}
		obj, err := bacnet.DecodeObjectIDBytes(p[next : next+int(hdrObj.Length)])
		if err != nil {
			sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
			return
		}
		off = next + int(hdrObj.Length)

		hdrOpen, next, err := bacnet.ReadTag(p, off)
		if err != nil || !hdrOpen.IsContext || !hdrOpen.IsOpening || hdrOpen.TagNumber != 1 {
			sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
			return
		}
		off = next

		apdu = bacnet.AppendContextObjectID(apdu, 0, obj)
		apdu = bacnet.WriteOpeningTag(apdu, 1)

		for off < len(p) {
			hdrProp, afterProp, err := bacnet.ReadTag(p, off)
			if err != nil {
				sc.replyReject(frame.InvokeID, 2, rawConn, udpSrc)
				return
			}
			if hdrProp.IsContext && hdrProp.IsClosing && hdrProp.TagNumber == 1 {
				off = afterProp
				break
			}
			prop := bacnet.PropertyID(bacnet.DecodeUnsigned(p[afterProp : afterProp+int(hdrProp.Length)]))
			off = afterProp + int(hdrProp.Length)

			var arrIdx *uint32
			if off < len(p) {
				peek, afterPeek, err := bacnet.ReadTag(p, off)
				if err == nil && peek.IsContext && !peek.IsOpening && !peek.IsClosing && peek.TagNumber == 1 {
					idx := uint32(bacnet.DecodeUnsigned(p[afterPeek : afterPeek+int(peek.Length)]))
					arrIdx = &idx
					off = afterPeek + int(peek.Length)
				}
			}

			apdu = bacnet.AppendContextUnsigned(apdu, 2, uint64(prop))
			if arrIdx != nil {
				apdu = bacnet.AppendContextUnsigned(apdu, 3, uint64(*arrIdx))
			}

			encodedVal, propErr := sc.encodePropertyValue(obj, prop, arrIdx)
			if propErr != nil {
				apdu = bacnet.WriteOpeningTag(apdu, 5)
				apdu = bacnet.AppendAppEnumerated(apdu, 2)  // property
				apdu = bacnet.AppendAppEnumerated(apdu, 32) // unknown-property
				apdu = bacnet.WriteClosingTag(apdu, 5)
			} else {
				apdu = bacnet.WriteOpeningTag(apdu, 4)
				apdu = append(apdu, encodedVal...)
				apdu = bacnet.WriteClosingTag(apdu, 4)
			}
		}

		apdu = bacnet.WriteClosingTag(apdu, 1)
	}

	npdu := bacnet.EncodeNPDU(nil, nil, false, apdu)
	_, _ = rawConn.WriteTo(bacnet.WrapBVLC(bacnet.BVLCOriginalUnicastNPDU, npdu), udpSrc)
}

func (sc *SimController) encodePropertyValue(obj bacnet.ObjectID, prop bacnet.PropertyID, arrIdx *uint32) ([]byte, error) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	if obj.Type == bacnet.ObjDevice && obj.Instance == sc.DeviceID {
		switch prop {
		case bacnet.PropObjectName:
			return bacnet.AppendAppCharString(nil, sc.ObjectName), nil
		case bacnet.PropModelName:
			return bacnet.AppendAppCharString(nil, sc.ModelName), nil
		case bacnet.PropVendorName:
			return bacnet.AppendAppCharString(nil, sc.VendorName), nil
		case bacnet.PropVendorIdentifier:
			return bacnet.AppendAppUnsigned(nil, uint64(sc.VendorID)), nil
		case bacnet.PropFirmwareRevision:
			return bacnet.AppendAppCharString(nil, sc.FirmwareRevision), nil
		case bacnet.PropDescription:
			return bacnet.AppendAppCharString(nil, sc.Description), nil
		case bacnet.PropLocation:
			return bacnet.AppendAppCharString(nil, sc.Location), nil
		case bacnet.PropObjectList:
			all := make([]bacnet.ObjectID, 0, len(sc.points)+1)
			all = append(all, bacnet.ObjectID{Type: bacnet.ObjDevice, Instance: sc.DeviceID})
			for _, pt := range sc.points {
				all = append(all, pt.ObjectID)
			}
			if arrIdx != nil {
				idx := int(*arrIdx)
				if idx == 0 {
					return bacnet.AppendAppUnsigned(nil, uint64(len(all))), nil
				}
				if idx < 1 || idx > len(all) {
					return nil, fmt.Errorf("array index %d out of range", idx)
				}
				return bacnet.AppendAppObjectID(nil, all[idx-1]), nil
			}
			var out []byte
			for _, oid := range all {
				out = bacnet.AppendAppObjectID(out, oid)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("unknown device property %d", prop)
		}
	}

	pt, ok := sc.byID[obj]
	if !ok {
		return nil, fmt.Errorf("unknown object %s", obj.String())
	}

	switch prop {
	case bacnet.PropObjectName:
		return bacnet.AppendAppCharString(nil, pt.Name), nil
	case bacnet.PropDescription:
		return bacnet.AppendAppCharString(nil, pt.Description), nil
	case bacnet.PropUnits:
		if !obj.Type.IsAnalogOrNumeric() {
			return nil, fmt.Errorf("no units on binary")
		}
		return bacnet.AppendAppEnumerated(nil, pt.UnitID), nil
	case bacnet.PropInactiveText:
		if pt.InactiveText == "" {
			return nil, fmt.Errorf("no inactive text")
		}
		return bacnet.AppendAppCharString(nil, pt.InactiveText), nil
	case bacnet.PropActiveText:
		if pt.ActiveText == "" {
			return nil, fmt.Errorf("no active text")
		}
		return bacnet.AppendAppCharString(nil, pt.ActiveText), nil
	case bacnet.PropStatusFlags:
		return bacnet.AppendAppStatusFlags(nil, pt.StatusFlags), nil
	case bacnet.PropPresentValue:
		switch v := pt.PresentValue.(type) {
		case float32:
			return bacnet.AppendAppReal(nil, v), nil
		case float64:
			return bacnet.AppendAppReal(nil, float32(v)), nil
		case uint32:
			return bacnet.AppendAppEnumerated(nil, v), nil
		default:
			return nil, fmt.Errorf("unsupported simulated value %T", pt.PresentValue)
		}
	default:
		return nil, fmt.Errorf("unsupported property %d", prop)
	}
}

// NewExhibitionControllers constructs simulated Optergy AXON, P864, and P442 controllers
// matching 'bacnet devices to be integrated.md'.
func NewExhibitionControllers() []*SimController {
	axon := &SimController{
		DeviceID:         1001,
		ObjectName:       "Optergy-AXON-Main",
		ModelName:        "AXON",
		VendorName:       "Optergy",
		VendorID:         681,
		FirmwareRevision: "5.1.0",
		Description:      "Main Exhibition Controller (DTA1 RS485 -> Schneider PM1200)",
		Location:         "Exhibition Rack A",
	}
	axon.AddPoints(
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 1}, Name: "PM1200_Active_Power_kW", Description: "Schneider PM1200 Total Active Power", UnitID: 48, PresentValue: float32(14.85)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 2}, Name: "PM1200_Total_Energy_kWh", Description: "Schneider PM1200 Cumulative Energy", UnitID: 19, PresentValue: float32(12840.5)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 3}, Name: "PM1200_Apparent_Power_kVA", Description: "Schneider PM1200 Apparent Power", UnitID: 9, PresentValue: float32(15.42)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 4}, Name: "PM1200_Voltage_L_N", Description: "Schneider PM1200 Average Voltage", UnitID: 5, PresentValue: float32(239.8)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 5}, Name: "PM1200_Current_Avg", Description: "Schneider PM1200 Average Current", UnitID: 3, PresentValue: float32(21.4)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 6}, Name: "PM1200_Power_Factor", Description: "Schneider PM1200 Power Factor", UnitID: 15, PresentValue: float32(0.96)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 7}, Name: "PM1200_Peak_Demand_kW", Description: "Schneider PM1200 Demand", UnitID: 48, PresentValue: float32(18.2)},
	)

	p864 := &SimController{
		DeviceID:         8640,
		ObjectName:       "Optergy-P864-IO",
		ModelName:        "P864",
		VendorName:       "Optergy",
		VendorID:         681,
		FirmwareRevision: "4.8.2",
		Description:      "P864 IO Controller (Dwyer RHPLC/RHL + BG-1000-M Wireless)",
		Location:         "Exhibition Panel B",
		ForceIndexedList: true, // Exercises Object_List[0..N] indexed fallback!
	}
	p864.AddPoints(
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 1}, Name: "UI1_RHPLC_Room_Temp", Description: "Dwyer RHPLC UI1 Room Temp", UnitID: 62, PresentValue: float32(23.4)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 2}, Name: "UI1_RHPLC_Room_RH", Description: "Dwyer RHPLC UI1 Room Relative Humidity", UnitID: 29, PresentValue: float32(51.2)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 3}, Name: "UI2_RHL_Zone_Temp", Description: "Dwyer RH/RHL UI2 Zone Temp", UnitID: 62, PresentValue: float32(22.8)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 4}, Name: "UI2_RHL_Zone_RH", Description: "Dwyer RH/RHL UI2 Zone RH", UnitID: 29, PresentValue: float32(49.6)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 101}, Name: "BW_INT_M_Temp", Description: "Optergy Air Touch BW-INT-M Temperature", UnitID: 62, PresentValue: float32(22.2)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 102}, Name: "BW_INT_M_RH", Description: "Optergy Air Touch BW-INT-M Humidity", UnitID: 29, PresentValue: float32(48.0)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 103}, Name: "BW_INT_M_Setpoint", Description: "Optergy Air Touch BW-INT-M Setpoint", UnitID: 62, PresentValue: float32(22.0)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjBinaryValue, Instance: 104}, Name: "BW_INT_M_Occupancy", Description: "Optergy Air Touch BW-INT-M Occupancy", PresentValue: uint32(1), InactiveText: "Unoccupied", ActiveText: "Occupied"},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 201}, Name: "BW_TH_M_Temp", Description: "Optergy Air BW-TH-M Wireless Temp", UnitID: 62, PresentValue: float32(24.1)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 202}, Name: "BW_TH_M_RH", Description: "Optergy Air BW-TH-M Wireless RH", UnitID: 29, PresentValue: float32(53.7)},
	)

	p442 := &SimController{
		DeviceID:         4420,
		ObjectName:       "Optergy-P442-IO",
		ModelName:        "P442",
		VendorName:       "Optergy",
		VendorID:         681,
		FirmwareRevision: "4.8.2",
		Description:      "P442 IO Controller (WD3 Leak, MSA237D Motion, T18XAIR IAQ)",
		Location:         "Exhibition Panel C",
	}
	p442.AddPoints(
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 1}, Name: "IN1_WD3_Water_Leak", Description: "Dwyer WD3 Water Leak Alarm", PresentValue: uint32(0), InactiveText: "Normal", ActiveText: "Alarm"},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 2}, Name: "IN2_MSA237D_Occupancy", Description: "MSA237D 99 Motion Presence Sensor", PresentValue: uint32(1), InactiveText: "Unoccupied", ActiveText: "Occupied"},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 10}, Name: "T18XAIR_CO2", Description: "T18XAIR-485-WIFI CO2 Concentration", UnitID: 96, PresentValue: float32(615.0)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 11}, Name: "T18XAIR_PM25", Description: "T18XAIR-485-WIFI PM2.5", UnitID: 219, PresentValue: float32(12.4)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 12}, Name: "T18XAIR_TVOC", Description: "T18XAIR-485-WIFI TVOC", UnitID: 97, PresentValue: float32(180.0)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 13}, Name: "T18XAIR_Temp", Description: "T18XAIR-485-WIFI Temperature", UnitID: 62, PresentValue: float32(23.1)},
		SimPoint{ObjectID: bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 14}, Name: "T18XAIR_RH", Description: "T18XAIR-485-WIFI Relative Humidity", UnitID: 29, PresentValue: float32(50.3)},
	)

	return []*SimController{axon, p864, p442}
}

