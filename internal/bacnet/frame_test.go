package bacnet

import (
	"math"
	"net"
	"testing"
)

func TestEncodeDecodeWhoIsAndIAm(t *testing.T) {
	low, high := uint32(1000), uint32(9000)
	whoIsPkt := EncodeWhoIs(&low, &high, false)

	srcUDP := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 50), Port: 47808}
	frame, err := DecodeFrame(whoIsPkt, srcUDP)
	if err != nil {
		t.Fatalf("DecodeFrame Who-Is: %v", err)
	}
	if frame.PDUType != PDUTypeUnconfirmedRequest || frame.ServiceChoice != ServiceUnconfirmedWhoIs {
		t.Fatalf("unexpected PDU/service: %d / %d", frame.PDUType, frame.ServiceChoice)
	}
	gotLow, gotHigh, err := DecodeWhoIs(frame.ServicePayload)
	if err != nil || gotLow == nil || gotHigh == nil || *gotLow != 1000 || *gotHigh != 9000 {
		t.Fatalf("DecodeWhoIs mismatch: %v, %v, err=%v", gotLow, gotHigh, err)
	}

	routedSrc := &Address{Net: 20, Mac: []byte{0x05}}
	iamPkt := EncodeIAm(8640, 1476, SegmentationBoth, 681, routedSrc)
	iamFrame, err := DecodeFrame(iamPkt, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 11), Port: 47808})
	if err != nil {
		t.Fatalf("DecodeFrame I-Am: %v", err)
	}
	if iamFrame.OriginAddress.Net != 20 || len(iamFrame.OriginAddress.Mac) != 1 || iamFrame.OriginAddress.Mac[0] != 0x05 {
		t.Fatalf("routed SNET/SADR not preserved: %+v", iamFrame.OriginAddress)
	}
	iam, err := DecodeIAm(iamFrame.ServicePayload, iamFrame.OriginAddress)
	if err != nil {
		t.Fatalf("DecodeIAm: %v", err)
	}
	if iam.DeviceID != 8640 || iam.VendorID != 681 || iam.MaxAPDULength != 1476 {
		t.Fatalf("unexpected I-Am fields: %+v", iam)
	}
}

func TestForwardedNPDUPreservesOriginIP(t *testing.T) {
	// Build an I-Am APDU+NPDU, then wrap in BVLC Forwarded-NPDU (0x04)
	apdu := []byte{PDUTypeUnconfirmedRequest << 4, ServiceUnconfirmedIAm}
	apdu = AppendAppObjectID(apdu, ObjectID{Type: ObjDevice, Instance: 4420})
	apdu = AppendAppUnsigned(apdu, 1476)
	apdu = AppendAppEnumerated(apdu, SegmentationBoth)
	apdu = AppendAppUnsigned(apdu, 681)
	npdu := EncodeNPDU(nil, nil, false, apdu)

	totalLen := 10 + len(npdu)
	pkt := make([]byte, 10, totalLen)
	pkt[0] = BVLCTypeBACnetIP
	pkt[1] = BVLCForwardedNPDU
	pkt[2] = byte(totalLen >> 8)
	pkt[3] = byte(totalLen)
	// Real controller origin: 10.20.30.40:47808
	copy(pkt[4:8], []byte{10, 20, 30, 40})
	pkt[8] = 0xBA
	pkt[9] = 0xC0
	pkt = append(pkt, npdu...)

	// Received from BBMD 192.168.1.1:47808
	frame, err := DecodeFrame(pkt, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 47808})
	if err != nil {
		t.Fatalf("DecodeFrame Forwarded-NPDU: %v", err)
	}
	if frame.OriginAddress.IP.String() != "10.20.30.40" || frame.OriginAddress.Port != 47808 {
		t.Fatalf("expected origin 10.20.30.40:47808, got %s", frame.OriginAddress.String())
	}
}

func TestDecodeUnconfirmedCOV(t *testing.T) {
	pkt := EncodeUnconfirmedCOV(
		1,
		4420,
		ObjectID{Type: ObjAnalogValue, Instance: 10},
		300,
		642.5,
		StatusFlags{InAlarm: true},
	)
	frame, err := DecodeFrame(pkt, &net.UDPAddr{IP: net.IPv4(192, 168, 1, 12), Port: 47808})
	if err != nil {
		t.Fatalf("DecodeFrame COV: %v", err)
	}
	cov, err := DecodeUnconfirmedCOV(frame.ServicePayload, frame.OriginAddress)
	if err != nil {
		t.Fatalf("DecodeUnconfirmedCOV: %v", err)
	}
	if cov.InitiatingDeviceID != 4420 || cov.MonitoredObject.Instance != 10 || len(cov.Values) != 2 {
		t.Fatalf("unexpected COV decoded: %+v", cov)
	}
	pv, ok := cov.Values[0].Value.(float64)
	if !ok || math.Abs(pv-642.5) > 0.01 {
		t.Fatalf("unexpected COV Present_Value: %v", cov.Values[0].Value)
	}
	sf, ok := cov.Values[1].Value.(StatusFlags)
	if !ok || !sf.InAlarm {
		t.Fatalf("expected InAlarm=true in COV StatusFlags, got %+v", cov.Values[1].Value)
	}
}

