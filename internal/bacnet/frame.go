package bacnet

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// DecodedFrame represents a parsed BVLC + NPDU + APDU frame from UDP.
type DecodedFrame struct {
	BVLCFunction  byte
	OriginAddress Address // Actual BACnet source (handles Forwarded-NPDU + routed SNET/SADR)
	DestNet       uint16
	DestMac       []byte
	ExpectReply   bool
	IsNetworkMsg  bool
	NetworkMsgTyp byte

	PDUType       byte
	PDUFlags      byte
	InvokeID      byte
	SeqNumber     byte
	WindowSize    byte
	ServiceChoice byte
	ServicePayload []byte
}

// WrapBVLC wraps an NPDU payload inside a 4-byte BVLC header (0x81, fn, uint16 length).
func WrapBVLC(fn byte, npdu []byte) []byte {
	totalLen := 4 + len(npdu)
	out := make([]byte, totalLen)
	out[0] = BVLCTypeBACnetIP
	out[1] = fn
	binary.BigEndian.PutUint16(out[2:4], uint16(totalLen))
	copy(out[4:], npdu)
	return out
}

// EncodeRegisterForeignDevice builds a BVLC Register-Foreign-Device packet (0x81, 0x05, 0x0006, ttl).
func EncodeRegisterForeignDevice(ttlSeconds uint16) []byte {
	out := make([]byte, 6)
	out[0] = BVLCTypeBACnetIP
	out[1] = BVLCRegisterForeignDevice
	binary.BigEndian.PutUint16(out[2:4], 6)
	binary.BigEndian.PutUint16(out[4:6], ttlSeconds)
	return out
}

// EncodeNPDU wraps an APDU with an ASHRAE-135 NPDU header, optionally targeting a routed sub-network (DNET/DADR)
// or advertising a routed source (SNET/SADR).
func EncodeNPDU(dest *Address, src *Address, expectingReply bool, apdu []byte) []byte {
	var control byte
	hasDest := dest != nil && dest.Net > 0
	hasSrc := src != nil && src.Net > 0
	if hasDest {
		control |= 0x20
	}
	if hasSrc {
		control |= 0x08
	}
	if expectingReply {
		control |= 0x04
	}

	out := make([]byte, 0, 12+len(apdu))
	out = append(out, NPDUVersion, control)
	if hasDest {
		out = append(out, byte(dest.Net>>8), byte(dest.Net))
		out = append(out, byte(len(dest.Mac)))
		out = append(out, dest.Mac...)
	}
	if hasSrc {
		out = append(out, byte(src.Net>>8), byte(src.Net))
		out = append(out, byte(len(src.Mac)))
		out = append(out, src.Mac...)
	}
	if hasDest {
		out = append(out, 0xFF) // HopCount = 255
	}
	out = append(out, apdu...)
	return out
}

// DecodeFrame parses a raw BACnet/IP UDP datagram into a DecodedFrame.
func DecodeFrame(packet []byte, udpSource *net.UDPAddr) (*DecodedFrame, error) {
	if len(packet) < 4 {
		return nil, fmt.Errorf("packet too short for BVLC (%d bytes)", len(packet))
	}
	if packet[0] != BVLCTypeBACnetIP {
		return nil, fmt.Errorf("invalid BVLC type 0x%02X (want 0x81)", packet[0])
	}

	fn := packet[1]
	bvlcLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if bvlcLen > len(packet) {
		return nil, fmt.Errorf("BVLC length %d exceeds UDP datagram %d", bvlcLen, len(packet))
	}
	packet = packet[:bvlcLen]

	origin := Address{}
	if udpSource != nil {
		origin.IP = append(net.IP(nil), udpSource.IP...)
		origin.Port = udpSource.Port
	}

	var npduOffset int
	switch fn {
	case BVLCOriginalUnicastNPDU, BVLCOriginalBroadcastNPDU, BVLCDistributeBroadcastToNetwork:
		npduOffset = 4
	case BVLCForwardedNPDU:
		if len(packet) < 10 {
			return nil, fmt.Errorf("Forwarded-NPDU too short (%d bytes)", len(packet))
		}
		origin.IP = net.IPv4(packet[4], packet[5], packet[6], packet[7])
		origin.Port = int(binary.BigEndian.Uint16(packet[8:10]))
		npduOffset = 10
	case BVLCResult, BVLCRegisterForeignDevice:
		return &DecodedFrame{
			BVLCFunction:  fn,
			OriginAddress: origin,
			IsNetworkMsg:  true,
		}, nil
	default:
		return &DecodedFrame{
			BVLCFunction:  fn,
			OriginAddress: origin,
			IsNetworkMsg:  true,
		}, nil
	}

	npdu := packet[npduOffset:]
	if len(npdu) < 2 {
		return nil, fmt.Errorf("NPDU too short (%d bytes)", len(npdu))
	}
	if npdu[0] != NPDUVersion {
		return nil, fmt.Errorf("unsupported NPDU version 0x%02X", npdu[0])
	}

	control := npdu[1]
	idx := 2

	frame := &DecodedFrame{
		BVLCFunction:  fn,
		OriginAddress: origin,
		ExpectReply:   (control & 0x04) != 0,
		IsNetworkMsg:  (control & 0x80) != 0,
	}

	// Destination specifier (DNET, DLEN, DADR)
	if (control & 0x20) != 0 {
		if idx+3 > len(npdu) {
			return nil, fmt.Errorf("truncated NPDU destination")
		}
		frame.DestNet = binary.BigEndian.Uint16(npdu[idx : idx+2])
		dlen := int(npdu[idx+2])
		idx += 3
		if idx+dlen > len(npdu) {
			return nil, fmt.Errorf("truncated NPDU DADR")
		}
		if dlen > 0 {
			frame.DestMac = append([]byte(nil), npdu[idx:idx+dlen]...)
		}
		idx += dlen
	}

	// Source specifier (SNET, SLEN, SADR)
	if (control & 0x08) != 0 {
		if idx+3 > len(npdu) {
			return nil, fmt.Errorf("truncated NPDU source")
		}
		frame.OriginAddress.Net = binary.BigEndian.Uint16(npdu[idx : idx+2])
		slen := int(npdu[idx+2])
		idx += 3
		if idx+slen > len(npdu) {
			return nil, fmt.Errorf("truncated NPDU SADR")
		}
		if slen > 0 {
			frame.OriginAddress.Mac = append([]byte(nil), npdu[idx:idx+slen]...)
		}
		idx += slen
	}

	// HopCount follows SADR if destination specifier is set
	if (control & 0x20) != 0 {
		if idx >= len(npdu) {
			return nil, fmt.Errorf("truncated NPDU hop count")
		}
		idx++ // skip hop count
	}

	// Network-layer NSDU message (e.g. I-Am-Router-To-Network)
	if frame.IsNetworkMsg {
		if idx < len(npdu) {
			frame.NetworkMsgTyp = npdu[idx]
		}
		return frame, nil
	}

	apdu := npdu[idx:]
	if len(apdu) < 2 {
		return nil, fmt.Errorf("APDU too short (%d bytes)", len(apdu))
	}

	frame.PDUType = apdu[0] >> 4
	frame.PDUFlags = apdu[0] & 0x0F

	switch frame.PDUType {
	case PDUTypeConfirmedRequest:
		segmented := (frame.PDUFlags & 0x08) != 0
		minLen := 4
		if segmented {
			minLen = 6
		}
		if len(apdu) < minLen {
			return nil, fmt.Errorf("Confirmed-Request APDU too short (%d)", len(apdu))
		}
		frame.InvokeID = apdu[2]
		if segmented {
			frame.SeqNumber = apdu[3]
			frame.WindowSize = apdu[4]
			frame.ServiceChoice = apdu[5]
			frame.ServicePayload = apdu[6:]
		} else {
			frame.ServiceChoice = apdu[3]
			frame.ServicePayload = apdu[4:]
		}

	case PDUTypeUnconfirmedRequest:
		frame.ServiceChoice = apdu[1]
		frame.ServicePayload = apdu[2:]

	case PDUTypeSimpleACK:
		if len(apdu) < 3 {
			return nil, fmt.Errorf("SimpleACK APDU too short")
		}
		frame.InvokeID = apdu[1]
		frame.ServiceChoice = apdu[2]

	case PDUTypeComplexACK:
		segmented := (frame.PDUFlags & 0x08) != 0
		minLen := 3
		if segmented {
			minLen = 5
		}
		if len(apdu) < minLen {
			return nil, fmt.Errorf("ComplexACK APDU too short (%d)", len(apdu))
		}
		frame.InvokeID = apdu[1]
		if segmented {
			frame.SeqNumber = apdu[2]
			frame.WindowSize = apdu[3]
			frame.ServiceChoice = apdu[4]
			frame.ServicePayload = apdu[5:]
		} else {
			frame.ServiceChoice = apdu[2]
			frame.ServicePayload = apdu[3:]
		}

	case PDUTypeSegmentACK:
		if len(apdu) < 4 {
			return nil, fmt.Errorf("SegmentACK APDU too short")
		}
		frame.InvokeID = apdu[1]
		frame.SeqNumber = apdu[2]
		frame.WindowSize = apdu[3]

	case PDUTypeError:
		if len(apdu) < 3 {
			return nil, fmt.Errorf("Error APDU too short")
		}
		frame.InvokeID = apdu[1]
		frame.ServiceChoice = apdu[2]
		frame.ServicePayload = apdu[3:]

	case PDUTypeReject, PDUTypeAbort:
		if len(apdu) < 3 {
			return nil, fmt.Errorf("Reject/Abort APDU too short")
		}
		frame.InvokeID = apdu[1]
		frame.ServiceChoice = apdu[2] // Holds RejectReason or AbortReason
	}

	return frame, nil
}

// DescribeFrame builds a PacketEvent summary for passive packet inspection/logging.
func DescribeFrame(frame *DecodedFrame, totalBytes int, rawHex string) PacketEvent {
	ev := PacketEvent{
		Timestamp:     time.Now().UTC(),
		Source:        frame.OriginAddress,
		BVLCFunction:  frame.BVLCFunction,
		BVLCName:      BVLCFunctionName(frame.BVLCFunction),
		IsNetworkMsg:  frame.IsNetworkMsg,
		ServiceChoice: frame.ServiceChoice,
		Length:        totalBytes,
		RawHex:        rawHex,
	}
	if frame.IsNetworkMsg {
		ev.PDUType = "Network-Layer-Message"
		return ev
	}

	switch frame.PDUType {
	case PDUTypeConfirmedRequest:
		ev.PDUType = "Confirmed-Request"
		ev.ServiceName = ConfirmedServiceName(frame.ServiceChoice)
	case PDUTypeUnconfirmedRequest:
		ev.PDUType = "Unconfirmed-Request"
		ev.ServiceName = UnconfirmedServiceName(frame.ServiceChoice)
	case PDUTypeSimpleACK:
		ev.PDUType = "Simple-ACK"
		ev.ServiceName = ConfirmedServiceName(frame.ServiceChoice)
	case PDUTypeComplexACK:
		ev.PDUType = "Complex-ACK"
		ev.ServiceName = ConfirmedServiceName(frame.ServiceChoice)
	case PDUTypeSegmentACK:
		ev.PDUType = "Segment-ACK"
	case PDUTypeError:
		ev.PDUType = "Error"
		ev.ServiceName = ConfirmedServiceName(frame.ServiceChoice)
	case PDUTypeReject:
		ev.PDUType = "Reject"
	case PDUTypeAbort:
		ev.PDUType = "Abort"
	}
	return ev
}

func ConfirmedServiceName(choice byte) string {
	switch choice {
	case ServiceConfirmedSubscribeCOV:
		return "SubscribeCOV"
	case ServiceConfirmedReadProperty:
		return "ReadProperty"
	case ServiceConfirmedReadPropertyMultiple:
		return "ReadPropertyMultiple"
	default:
		return fmt.Sprintf("Confirmed-0x%02X", choice)
	}
}

func UnconfirmedServiceName(choice byte) string {
	switch choice {
	case ServiceUnconfirmedIAm:
		return "I-Am"
	case ServiceUnconfirmedIHave:
		return "I-Have"
	case ServiceUnconfirmedCOVNotification:
		return "Unconfirmed-COV-Notification"
	case ServiceUnconfirmedTimeSync:
		return "TimeSynchronization"
	case ServiceUnconfirmedWhoHas:
		return "Who-Has"
	case ServiceUnconfirmedWhoIs:
		return "Who-Is"
	default:
		return fmt.Sprintf("Unconfirmed-0x%02X", choice)
	}
}

// EncodeWhoIs builds a BACnet/IP broadcast Who-Is frame (with optional device ID range).
func EncodeWhoIs(lowLimit, highLimit *uint32, useForeignDeviceBroadcast bool) []byte {
	apdu := []byte{PDUTypeUnconfirmedRequest << 4, ServiceUnconfirmedWhoIs}
	if lowLimit != nil && highLimit != nil {
		apdu = AppendContextUnsigned(apdu, 0, uint64(*lowLimit))
		apdu = AppendContextUnsigned(apdu, 1, uint64(*highLimit))
	}
	// Broadcast DNET=0xFFFF (global BACnet broadcast across local + routed sub-networks behind AXON!)
	globalDest := &Address{Net: 0xFFFF}
	npdu := EncodeNPDU(globalDest, nil, false, apdu)
	if useForeignDeviceBroadcast {
		return WrapBVLC(BVLCDistributeBroadcastToNetwork, npdu)
	}
	return WrapBVLC(BVLCOriginalBroadcastNPDU, npdu)
}

// DecodeWhoIs parses optional low/high limits from a Who-Is service payload.
func DecodeWhoIs(payload []byte) (low *uint32, high *uint32, err error) {
	if len(payload) == 0 {
		return nil, nil, nil
	}
	hdr0, off, err := ReadTag(payload, 0)
	if err != nil {
		return nil, nil, err
	}
	if !hdr0.IsContext || hdr0.TagNumber != 0 || off+int(hdr0.Length) > len(payload) {
		return nil, nil, fmt.Errorf("invalid Who-Is low limit tag")
	}
	l := uint32(DecodeUnsigned(payload[off : off+int(hdr0.Length)]))
	off += int(hdr0.Length)

	hdr1, off, err := ReadTag(payload, off)
	if err != nil {
		return nil, nil, err
	}
	if !hdr1.IsContext || hdr1.TagNumber != 1 || off+int(hdr1.Length) > len(payload) {
		return nil, nil, fmt.Errorf("invalid Who-Is high limit tag")
	}
	h := uint32(DecodeUnsigned(payload[off : off+int(hdr1.Length)]))
	return &l, &h, nil
}

// EncodeIAm encodes a BACnet/IP I-Am frame.
func EncodeIAm(deviceID uint32, maxAPDU uint32, segmentation uint32, vendorID uint32, src *Address) []byte {
	apdu := []byte{PDUTypeUnconfirmedRequest << 4, ServiceUnconfirmedIAm}
	apdu = AppendAppObjectID(apdu, ObjectID{Type: ObjDevice, Instance: deviceID})
	apdu = AppendAppUnsigned(apdu, uint64(maxAPDU))
	apdu = AppendAppEnumerated(apdu, segmentation)
	apdu = AppendAppUnsigned(apdu, uint64(vendorID))
	globalDest := &Address{Net: 0xFFFF}
	npdu := EncodeNPDU(globalDest, src, false, apdu)
	return WrapBVLC(BVLCOriginalBroadcastNPDU, npdu)
}

// DecodeIAm parses an I-Am APDU service payload.
func DecodeIAm(payload []byte, src Address) (*IAmNotification, error) {
	v0, off, err := DecodeApplicationValue(payload, 0)
	if err != nil {
		return nil, fmt.Errorf("I-Am device ObjectID: %w", err)
	}
	devObj, ok := v0.(ObjectID)
	if !ok || devObj.Type != ObjDevice {
		return nil, fmt.Errorf("I-Am expected Device ObjectID, got %v", v0)
	}

	v1, off, err := DecodeApplicationValue(payload, off)
	if err != nil {
		return nil, fmt.Errorf("I-Am maxAPDU: %w", err)
	}
	maxAPDU, ok := v1.(uint64)
	if !ok {
		return nil, fmt.Errorf("I-Am invalid maxAPDU type %T", v1)
	}

	v2, off, err := DecodeApplicationValue(payload, off)
	if err != nil {
		return nil, fmt.Errorf("I-Am segmentation: %w", err)
	}
	seg, ok := v2.(uint32)
	if !ok {
		return nil, fmt.Errorf("I-Am invalid segmentation type %T", v2)
	}

	v3, _, err := DecodeApplicationValue(payload, off)
	if err != nil {
		return nil, fmt.Errorf("I-Am vendorID: %w", err)
	}
	var vendorID uint32
	switch vt := v3.(type) {
	case uint64:
		vendorID = uint32(vt)
	case uint32:
		vendorID = vt
	default:
		return nil, fmt.Errorf("I-Am invalid vendorID type %T", v3)
	}

	return &IAmNotification{
		DeviceID:      devObj.Instance,
		MaxAPDULength: uint32(maxAPDU),
		Segmentation:  seg,
		VendorID:      vendorID,
		Source:        src,
		ReceivedAt:    time.Now().UTC(),
	}, nil
}

// EncodeSegmentACK builds a BACnet/IP SegmentACK unicast frame to acknowledge segmented ComplexACK packets.
func EncodeSegmentACK(dest Address, invokeID byte, seqNum byte, windowSize byte, negativeACK bool) []byte {
	var flags byte
	if negativeACK {
		flags |= 0x02
	}
	if windowSize == 0 {
		windowSize = 1
	}
	apdu := []byte{(PDUTypeSegmentACK << 4) | flags, invokeID, seqNum, windowSize}
	npdu := EncodeNPDU(&dest, nil, false, apdu)
	return WrapBVLC(BVLCOriginalUnicastNPDU, npdu)
}

// EncodeReadProperty builds a Confirmed-Request ReadProperty packet.
func EncodeReadProperty(dest Address, invokeID byte, obj ObjectID, prop PropertyID, arrayIndex *uint32, acceptSegmented bool) []byte {
	var pduFlags byte
	if acceptSegmented {
		pduFlags = 0x02 // SegmentedResponseAccepted
	}
	// MaxSegs = 0x70 (>64 segments), MaxAPDU = 0x05 (1476 octets)
	apdu := []byte{(PDUTypeConfirmedRequest << 4) | pduFlags, 0x75, invokeID, ServiceConfirmedReadProperty}
	apdu = AppendContextObjectID(apdu, 0, obj)
	apdu = AppendContextUnsigned(apdu, 1, uint64(prop))
	if arrayIndex != nil {
		apdu = AppendContextUnsigned(apdu, 2, uint64(*arrayIndex))
	}
	npdu := EncodeNPDU(&dest, nil, true, apdu)
	return WrapBVLC(BVLCOriginalUnicastNPDU, npdu)
}

// ReadPropertyResult holds the parsed response of a ReadProperty ComplexACK.
type ReadPropertyResult struct {
	Object     ObjectID
	Property   PropertyID
	ArrayIndex *uint32
	Values     []any
}

// DecodeReadPropertyACK parses a ReadProperty-ACK service payload.
func DecodeReadPropertyACK(payload []byte) (*ReadPropertyResult, error) {
	res := &ReadPropertyResult{}
	hdr0, off, err := ReadTag(payload, 0)
	if err != nil {
		return nil, err
	}
	if !hdr0.IsContext || hdr0.TagNumber != 0 || off+int(hdr0.Length) > len(payload) {
		return nil, fmt.Errorf("ReadPropertyACK missing context [0] ObjectID")
	}
	obj, err := DecodeObjectIDBytes(payload[off : off+int(hdr0.Length)])
	if err != nil {
		return nil, err
	}
	res.Object = obj
	off += int(hdr0.Length)

	hdr1, off, err := ReadTag(payload, off)
	if err != nil {
		return nil, err
	}
	if !hdr1.IsContext || hdr1.TagNumber != 1 || off+int(hdr1.Length) > len(payload) {
		return nil, fmt.Errorf("ReadPropertyACK missing context [1] PropertyID")
	}
	res.Property = PropertyID(DecodeUnsigned(payload[off : off+int(hdr1.Length)]))
	off += int(hdr1.Length)

	hdrNext, afterHdr, err := ReadTag(payload, off)
	if err != nil {
		return nil, err
	}
	// Optional [2] PropertyArrayIndex
	if hdrNext.IsContext && !hdrNext.IsOpening && hdrNext.TagNumber == 2 {
		if afterHdr+int(hdrNext.Length) > len(payload) {
			return nil, fmt.Errorf("ReadPropertyACK truncated array index")
		}
		idxVal := uint32(DecodeUnsigned(payload[afterHdr : afterHdr+int(hdrNext.Length)]))
		res.ArrayIndex = &idxVal
		off = afterHdr + int(hdrNext.Length)
		hdrNext, afterHdr, err = ReadTag(payload, off)
		if err != nil {
			return nil, err
		}
	}

	if !hdrNext.IsContext || !hdrNext.IsOpening || hdrNext.TagNumber != 3 {
		return nil, fmt.Errorf("ReadPropertyACK expected opening tag [3], got tag %d", hdrNext.TagNumber)
	}
	vals, _, err := DecodeValuesUntilClosing(payload, afterHdr, 3)
	if err != nil {
		return nil, err
	}
	res.Values = vals
	return res, nil
}

// PropertyRef specifies a property (and optional array index) to read in ReadPropertyMultiple.
type PropertyRef struct {
	Property   PropertyID
	ArrayIndex *uint32
}

// ReadAccessSpec specifies an object and list of properties for ReadPropertyMultiple.
type ReadAccessSpec struct {
	Object     ObjectID
	Properties []PropertyRef
}

// EncodeReadPropertyMultiple builds a Confirmed-Request ReadPropertyMultiple packet.
func EncodeReadPropertyMultiple(dest Address, invokeID byte, specs []ReadAccessSpec, acceptSegmented bool) []byte {
	var pduFlags byte
	if acceptSegmented {
		pduFlags = 0x02
	}
	apdu := []byte{(PDUTypeConfirmedRequest << 4) | pduFlags, 0x75, invokeID, ServiceConfirmedReadPropertyMultiple}
	for _, spec := range specs {
		apdu = AppendContextObjectID(apdu, 0, spec.Object)
		apdu = WriteOpeningTag(apdu, 1)
		for _, p := range spec.Properties {
			apdu = AppendContextUnsigned(apdu, 0, uint64(p.Property))
			if p.ArrayIndex != nil {
				apdu = AppendContextUnsigned(apdu, 1, uint64(*p.ArrayIndex))
			}
		}
		apdu = WriteClosingTag(apdu, 1)
	}
	npdu := EncodeNPDU(&dest, nil, true, apdu)
	return WrapBVLC(BVLCOriginalUnicastNPDU, npdu)
}

// RPMPropertyResult holds a single property result or access error from ReadPropertyMultiple-ACK.
type RPMPropertyResult struct {
	Property   PropertyID
	ArrayIndex *uint32
	Values     []any
	HasError   bool
	ErrorClass uint32
	ErrorCode  uint32
}

// ReadAccessResult holds all property results for one object in ReadPropertyMultiple-ACK.
type ReadAccessResult struct {
	Object  ObjectID
	Results []RPMPropertyResult
}

// DecodeReadPropertyMultipleACK parses a ReadPropertyMultiple-ACK service payload.
func DecodeReadPropertyMultipleACK(payload []byte) ([]ReadAccessResult, error) {
	var out []ReadAccessResult
	off := 0
	for off < len(payload) {
		hdrObj, next, err := ReadTag(payload, off)
		if err != nil {
			return nil, err
		}
		if !hdrObj.IsContext || hdrObj.TagNumber != 0 || next+int(hdrObj.Length) > len(payload) {
			return nil, fmt.Errorf("RPM ACK expected context [0] ObjectID at %d", off)
		}
		obj, err := DecodeObjectIDBytes(payload[next : next+int(hdrObj.Length)])
		if err != nil {
			return nil, err
		}
		off = next + int(hdrObj.Length)

		hdrOpen1, next, err := ReadTag(payload, off)
		if err != nil {
			return nil, err
		}
		if !hdrOpen1.IsContext || !hdrOpen1.IsOpening || hdrOpen1.TagNumber != 1 {
			return nil, fmt.Errorf("RPM ACK expected opening tag [1] at %d", off)
		}
		off = next

		rar := ReadAccessResult{Object: obj}
		for off < len(payload) {
			hdr, afterHdr, err := ReadTag(payload, off)
			if err != nil {
				return nil, err
			}
			if hdr.IsContext && hdr.IsClosing && hdr.TagNumber == 1 {
				off = afterHdr
				break
			}
			// [2] PropertyIdentifier
			if !hdr.IsContext || hdr.TagNumber != 2 || afterHdr+int(hdr.Length) > len(payload) {
				return nil, fmt.Errorf("RPM ACK expected context [2] PropertyID at %d", off)
			}
			propRes := RPMPropertyResult{
				Property: PropertyID(DecodeUnsigned(payload[afterHdr : afterHdr+int(hdr.Length)])),
			}
			off = afterHdr + int(hdr.Length)

			hdrChoice, afterChoice, err := ReadTag(payload, off)
			if err != nil {
				return nil, err
			}
			// Optional [3] PropertyArrayIndex
			if hdrChoice.IsContext && !hdrChoice.IsOpening && hdrChoice.TagNumber == 3 {
				if afterChoice+int(hdrChoice.Length) > len(payload) {
					return nil, fmt.Errorf("RPM ACK truncated array index")
				}
				arrIdx := uint32(DecodeUnsigned(payload[afterChoice : afterChoice+int(hdrChoice.Length)]))
				propRes.ArrayIndex = &arrIdx
				off = afterChoice + int(hdrChoice.Length)
				hdrChoice, afterChoice, err = ReadTag(payload, off)
				if err != nil {
					return nil, err
				}
			}

			if hdrChoice.IsContext && hdrChoice.IsOpening && hdrChoice.TagNumber == 4 {
				vals, afterVals, err := DecodeValuesUntilClosing(payload, afterChoice, 4)
				if err != nil {
					return nil, err
				}
				propRes.Values = vals
				off = afterVals
			} else if hdrChoice.IsContext && hdrChoice.IsOpening && hdrChoice.TagNumber == 5 {
				errVals, afterErr, err := DecodeValuesUntilClosing(payload, afterChoice, 5)
				if err != nil {
					return nil, err
				}
				propRes.HasError = true
				if len(errVals) >= 1 {
					if ec, ok := errVals[0].(uint32); ok {
						propRes.ErrorClass = ec
					}
				}
				if len(errVals) >= 2 {
					if ecode, ok := errVals[1].(uint32); ok {
						propRes.ErrorCode = ecode
					}
				}
				off = afterErr
			} else {
				return nil, fmt.Errorf("RPM ACK expected opening tag [4] or [5], got %d", hdrChoice.TagNumber)
			}

			rar.Results = append(rar.Results, propRes)
		}
		out = append(out, rar)
	}
	return out, nil
}

// EncodeSubscribeCOV builds a Confirmed-Request SubscribeCOV packet (requesting unconfirmed COV notifications).
func EncodeSubscribeCOV(dest Address, invokeID byte, processID uint32, monitoredObj ObjectID, confirmed bool, lifetimeSec uint32) []byte {
	apdu := []byte{PDUTypeConfirmedRequest << 4, 0x75, invokeID, ServiceConfirmedSubscribeCOV}
	apdu = AppendContextUnsigned(apdu, 0, uint64(processID))
	apdu = AppendContextObjectID(apdu, 1, monitoredObj)
	apdu = AppendContextBool(apdu, 2, confirmed)
	if lifetimeSec > 0 {
		apdu = AppendContextUnsigned(apdu, 3, uint64(lifetimeSec))
	}
	npdu := EncodeNPDU(&dest, nil, true, apdu)
	return WrapBVLC(BVLCOriginalUnicastNPDU, npdu)
}

// EncodeUnconfirmedCOV builds an Unconfirmed-COV-Notification broadcast/unicast packet (used for simulation/tests).
func EncodeUnconfirmedCOV(processID uint32, deviceID uint32, monitoredObj ObjectID, timeRemaining uint32, presentVal float32, statusFlags StatusFlags) []byte {
	apdu := []byte{PDUTypeUnconfirmedRequest << 4, ServiceUnconfirmedCOVNotification}
	apdu = AppendContextUnsigned(apdu, 0, uint64(processID))
	apdu = AppendContextObjectID(apdu, 1, ObjectID{Type: ObjDevice, Instance: deviceID})
	apdu = AppendContextObjectID(apdu, 2, monitoredObj)
	apdu = AppendContextUnsigned(apdu, 3, uint64(timeRemaining))
	apdu = WriteOpeningTag(apdu, 4)

	// Property 1: Present_Value
	apdu = AppendContextUnsigned(apdu, 0, uint64(PropPresentValue))
	apdu = WriteOpeningTag(apdu, 2)
	apdu = AppendAppReal(apdu, presentVal)
	apdu = WriteClosingTag(apdu, 2)

	// Property 2: Status_Flags
	apdu = AppendContextUnsigned(apdu, 0, uint64(PropStatusFlags))
	apdu = WriteOpeningTag(apdu, 2)
	apdu = AppendAppStatusFlags(apdu, statusFlags)
	apdu = WriteClosingTag(apdu, 2)

	apdu = WriteClosingTag(apdu, 4)
	npdu := EncodeNPDU(&Address{Net: 0xFFFF}, nil, false, apdu)
	return WrapBVLC(BVLCOriginalBroadcastNPDU, npdu)
}

// DecodeUnconfirmedCOV parses an Unconfirmed-COV-Notification service payload.
func DecodeUnconfirmedCOV(payload []byte, src Address) (*COVNotification, error) {
	cov := &COVNotification{
		Source:     src,
		ReceivedAt: time.Now().UTC(),
	}
	// [0] SubscriberProcessIdentifier
	hdr0, off, err := ReadTag(payload, 0)
	if err != nil || !hdr0.IsContext || hdr0.TagNumber != 0 || off+int(hdr0.Length) > len(payload) {
		return nil, fmt.Errorf("COV invalid [0] subscriberProcessIdentifier")
	}
	cov.SubscriberProcessID = uint32(DecodeUnsigned(payload[off : off+int(hdr0.Length)]))
	off += int(hdr0.Length)

	// [1] InitiatingDeviceIdentifier
	hdr1, off, err := ReadTag(payload, off)
	if err != nil || !hdr1.IsContext || hdr1.TagNumber != 1 || off+int(hdr1.Length) > len(payload) {
		return nil, fmt.Errorf("COV invalid [1] initiatingDeviceIdentifier")
	}
	devObj, err := DecodeObjectIDBytes(payload[off : off+int(hdr1.Length)])
	if err != nil {
		return nil, err
	}
	cov.InitiatingDeviceID = devObj.Instance
	off += int(hdr1.Length)

	// [2] MonitoredObjectIdentifier
	hdr2, off, err := ReadTag(payload, off)
	if err != nil || !hdr2.IsContext || hdr2.TagNumber != 2 || off+int(hdr2.Length) > len(payload) {
		return nil, fmt.Errorf("COV invalid [2] monitoredObjectIdentifier")
	}
	monObj, err := DecodeObjectIDBytes(payload[off : off+int(hdr2.Length)])
	if err != nil {
		return nil, err
	}
	cov.MonitoredObject = monObj
	off += int(hdr2.Length)

	// [3] TimeRemaining
	hdr3, off, err := ReadTag(payload, off)
	if err != nil || !hdr3.IsContext || hdr3.TagNumber != 3 || off+int(hdr3.Length) > len(payload) {
		return nil, fmt.Errorf("COV invalid [3] timeRemaining")
	}
	cov.TimeRemainingSec = uint32(DecodeUnsigned(payload[off : off+int(hdr3.Length)]))
	off += int(hdr3.Length)

	// [4] ListOfValues
	hdr4, off, err := ReadTag(payload, off)
	if err != nil || !hdr4.IsContext || !hdr4.IsOpening || hdr4.TagNumber != 4 {
		return nil, fmt.Errorf("COV missing opening tag [4] listOfValues")
	}

	for off < len(payload) {
		hdr, afterHdr, err := ReadTag(payload, off)
		if err != nil {
			return nil, err
		}
		if hdr.IsContext && hdr.IsClosing && hdr.TagNumber == 4 {
			break
		}
		if !hdr.IsContext || hdr.TagNumber != 0 || afterHdr+int(hdr.Length) > len(payload) {
			return nil, fmt.Errorf("COV listOfValues expected [0] PropertyIdentifier")
		}
		pv := COVPropertyValue{
			PropertyID: PropertyID(DecodeUnsigned(payload[afterHdr : afterHdr+int(hdr.Length)])),
		}
		off = afterHdr + int(hdr.Length)

		hdrNext, afterNext, err := ReadTag(payload, off)
		if err != nil {
			return nil, err
		}
		// Optional [1] PropertyArrayIndex
		if hdrNext.IsContext && !hdrNext.IsOpening && hdrNext.TagNumber == 1 {
			if afterNext+int(hdrNext.Length) > len(payload) {
				return nil, fmt.Errorf("COV truncated array index")
			}
			idx := uint32(DecodeUnsigned(payload[afterNext : afterNext+int(hdrNext.Length)]))
			pv.ArrayIndex = &idx
			off = afterNext + int(hdrNext.Length)
			hdrNext, afterNext, err = ReadTag(payload, off)
			if err != nil {
				return nil, err
			}
		}

		if !hdrNext.IsContext || !hdrNext.IsOpening || hdrNext.TagNumber != 2 {
			return nil, fmt.Errorf("COV expected opening tag [2] value")
		}
		vals, afterVals, err := DecodeValuesUntilClosing(payload, afterNext, 2)
		if err != nil {
			return nil, err
		}
		if len(vals) == 1 {
			pv.Value = vals[0]
		} else {
			pv.Value = vals
		}
		off = afterVals

		// Optional [3] Priority
		if off < len(payload) {
			peek, afterPeek, err := ReadTag(payload, off)
			if err == nil && peek.IsContext && !peek.IsOpening && !peek.IsClosing && peek.TagNumber == 3 {
				off = afterPeek + int(peek.Length)
			}
		}

		cov.Values = append(cov.Values, pv)
	}

	return cov, nil
}

// BACnetError represents a BACnet Error/Reject/Abort PDU returned by a controller.
type BACnetError struct {
	Kind       string // "Error", "Reject", "Abort"
	ErrorClass uint32
	ErrorCode  uint32
	Reason     byte
}

func (e *BACnetError) Error() string {
	if e.Kind == "Error" {
		return fmt.Sprintf("BACnet Error (class=%d, code=%d)", e.ErrorClass, e.ErrorCode)
	}
	return fmt.Sprintf("BACnet %s (reason=%d)", e.Kind, e.Reason)
}

// IsSegmentationUnsupported returns true if the controller aborted or rejected due to segmentation/buffer limits.
func (e *BACnetError) IsSegmentationUnsupported() bool {
	if e.Kind == "Abort" && (e.Reason == 2 || e.Reason == 4) { // 2=buffer-overflow, 4=segmentation-not-supported
		return true
	}
	if e.Kind == "Error" && e.ErrorCode == 37 { // 37 = segmentation-not-supported
		return true
	}
	return false
}

// DecodeErrorPayload extracts ErrorClass and ErrorCode from an Error-PDU payload.
func DecodeErrorPayload(payload []byte) *BACnetError {
	be := &BACnetError{Kind: "Error"}
	off := 0
	if len(payload) > 0 {
		hdr, next, err := ReadTag(payload, 0)
		if err == nil && hdr.IsContext && hdr.IsOpening && hdr.TagNumber == 0 {
			off = next
		}
	}
	v1, off, err := DecodeApplicationValue(payload, off)
	if err == nil {
		if ec, ok := v1.(uint32); ok {
			be.ErrorClass = ec
		}
	}
	v2, _, err := DecodeApplicationValue(payload, off)
	if err == nil {
		if ecode, ok := v2.(uint32); ok {
			be.ErrorCode = ecode
		}
	}
	return be
}

