package bacnet

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultPort is the standard BACnet/IP UDP port (0xBAC0).
	DefaultPort = 47808

	// BVLCTypeBACnetIP is the BVLC header signature for BACnet/IPv4.
	BVLCTypeBACnetIP byte = 0x81

	// BVLC Function codes
	BVLCResult                       byte = 0x00
	BVLCWriteBroadcastDistTable      byte = 0x01
	BVLCReadBroadcastDistTable       byte = 0x02
	BVLCReadBroadcastDistTableAck    byte = 0x03
	BVLCForwardedNPDU                byte = 0x04
	BVLCRegisterForeignDevice        byte = 0x05
	BVLCReadForeignDeviceTable       byte = 0x06
	BVLCReadForeignDeviceTableAck    byte = 0x07
	BVLCDeleteForeignDeviceTable     byte = 0x08
	BVLCDistributeBroadcastToNetwork byte = 0x09
	BVLCOriginalUnicastNPDU          byte = 0x0A
	BVLCOriginalBroadcastNPDU        byte = 0x0B

	// NPDU Version
	NPDUVersion byte = 0x01

	// APDU PDU Types (upper nibble of byte 0)
	PDUTypeConfirmedRequest   byte = 0x00
	PDUTypeUnconfirmedRequest byte = 0x01
	PDUTypeSimpleACK          byte = 0x02
	PDUTypeComplexACK         byte = 0x03
	PDUTypeSegmentACK         byte = 0x04
	PDUTypeError              byte = 0x05
	PDUTypeReject             byte = 0x06
	PDUTypeAbort              byte = 0x07

	// Confirmed Service Choices
	ServiceConfirmedSubscribeCOV         byte = 0x05
	ServiceConfirmedReadProperty         byte = 0x0C
	ServiceConfirmedReadPropertyMultiple byte = 0x0E

	// Unconfirmed Service Choices
	ServiceUnconfirmedIAm               byte = 0x00
	ServiceUnconfirmedIHave             byte = 0x01
	ServiceUnconfirmedCOVNotification   byte = 0x02
	ServiceUnconfirmedTimeSync          byte = 0x06
	ServiceUnconfirmedWhoHas            byte = 0x07
	ServiceUnconfirmedWhoIs             byte = 0x08

	// Segmentation Support Enumerated Values
	SegmentationBoth     uint32 = 0
	SegmentationTransmit uint32 = 1
	SegmentationReceive  uint32 = 2
	SegmentationNone     uint32 = 3
)

// ObjectType represents a BACnet Object Type (10-bit identifier).
type ObjectType uint16

const (
	ObjAnalogInput          ObjectType = 0
	ObjAnalogOutput         ObjectType = 1
	ObjAnalogValue          ObjectType = 2
	ObjBinaryInput          ObjectType = 3
	ObjBinaryOutput         ObjectType = 4
	ObjBinaryValue          ObjectType = 5
	ObjCalendar             ObjectType = 6
	ObjCommand              ObjectType = 7
	ObjDevice               ObjectType = 8
	ObjEventEnrollment      ObjectType = 9
	ObjFile                 ObjectType = 10
	ObjGroup                ObjectType = 11
	ObjLoop                 ObjectType = 12
	ObjMultiStateInput      ObjectType = 13
	ObjMultiStateOutput     ObjectType = 14
	ObjNotificationClass    ObjectType = 15
	ObjProgram              ObjectType = 16
	ObjSchedule             ObjectType = 17
	ObjAveraging            ObjectType = 18
	ObjMultiStateValue      ObjectType = 19
	ObjTrendLog             ObjectType = 20
	ObjAccumulator          ObjectType = 23
	ObjPulseConverter       ObjectType = 24
	ObjIntegerValue         ObjectType = 45
	ObjLargeAnalogValue     ObjectType = 46
	ObjPositiveIntegerValue ObjectType = 48
)

func (ot ObjectType) String() string {
	switch ot {
	case ObjAnalogInput:
		return "analog-input"
	case ObjAnalogOutput:
		return "analog-output"
	case ObjAnalogValue:
		return "analog-value"
	case ObjBinaryInput:
		return "binary-input"
	case ObjBinaryOutput:
		return "binary-output"
	case ObjBinaryValue:
		return "binary-value"
	case ObjDevice:
		return "device"
	case ObjMultiStateInput:
		return "multi-state-input"
	case ObjMultiStateOutput:
		return "multi-state-output"
	case ObjMultiStateValue:
		return "multi-state-value"
	case ObjAccumulator:
		return "accumulator"
	case ObjPulseConverter:
		return "pulse-converter"
	case ObjIntegerValue:
		return "integer-value"
	case ObjLargeAnalogValue:
		return "large-analog-value"
	case ObjPositiveIntegerValue:
		return "positive-integer-value"
	case ObjTrendLog:
		return "trend-log"
	case ObjSchedule:
		return "schedule"
	default:
		return fmt.Sprintf("object-type-%d", uint16(ot))
	}
}

// IsTelemetryObject returns true if the object holds readable sensor/actuator telemetry.
func (ot ObjectType) IsTelemetryObject() bool {
	switch ot {
	case ObjAnalogInput, ObjAnalogOutput, ObjAnalogValue,
		ObjBinaryInput, ObjBinaryOutput, ObjBinaryValue,
		ObjMultiStateInput, ObjMultiStateOutput, ObjMultiStateValue,
		ObjAccumulator, ObjPulseConverter,
		ObjIntegerValue, ObjLargeAnalogValue, ObjPositiveIntegerValue:
		return true
	default:
		return false
	}
}

// IsBinary returns true for binary input/output/value objects.
func (ot ObjectType) IsBinary() bool {
	return ot == ObjBinaryInput || ot == ObjBinaryOutput || ot == ObjBinaryValue
}

// IsAnalogOrNumeric returns true for analog, integer, or accumulator objects that expose Units.
func (ot ObjectType) IsAnalogOrNumeric() bool {
	switch ot {
	case ObjAnalogInput, ObjAnalogOutput, ObjAnalogValue,
		ObjAccumulator, ObjPulseConverter,
		ObjIntegerValue, ObjLargeAnalogValue, ObjPositiveIntegerValue:
		return true
	default:
		return false
	}
}

// ObjectID uniquely identifies a BACnet object within a device.
type ObjectID struct {
	Type     ObjectType `json:"type"`
	Instance uint32     `json:"instance"`
}

func (id ObjectID) String() string {
	return fmt.Sprintf("%s:%d", id.Type.String(), id.Instance)
}

// ShortCode returns standard BMS shorthand like "AI-101", "AV-27126", "BI-1" matching Optergy BACnet Points.xlsx.
func (id ObjectID) ShortCode() string {
	var prefix string
	switch id.Type {
	case ObjAnalogInput:
		prefix = "AI"
	case ObjAnalogOutput:
		prefix = "AO"
	case ObjAnalogValue:
		prefix = "AV"
	case ObjBinaryInput:
		prefix = "BI"
	case ObjBinaryOutput:
		prefix = "BO"
	case ObjBinaryValue:
		prefix = "BV"
	case ObjMultiStateInput:
		prefix = "MSI"
	case ObjMultiStateOutput:
		prefix = "MSO"
	case ObjMultiStateValue:
		prefix = "MSV"
	case ObjAccumulator:
		prefix = "ACC"
	case ObjPulseConverter:
		prefix = "PC"
	case ObjIntegerValue:
		prefix = "IV"
	case ObjLargeAnalogValue:
		prefix = "LAV"
	case ObjPositiveIntegerValue:
		prefix = "PIV"
	case ObjDevice:
		prefix = "DEV"
	default:
		return id.String()
	}
	return fmt.Sprintf("%s-%d", prefix, id.Instance)
}

// ParseShortObjectID parses strings like "AI-101", "AV-27126", "BI-1", or "analog-input:101" into an ObjectID.
func ParseShortObjectID(s string) (ObjectID, error) {
	clean := strings.TrimSpace(s)
	var typPart, instPart string
	if idx := strings.IndexAny(clean, "-:"); idx > 0 {
		typPart = strings.ToUpper(strings.TrimSpace(clean[:idx]))
		instPart = strings.TrimSpace(clean[idx+1:])
	} else {
		return ObjectID{}, fmt.Errorf("invalid BACnet object reference %q", s)
	}
	inst, err := strconv.ParseUint(instPart, 10, 32)
	if err != nil {
		return ObjectID{}, fmt.Errorf("invalid BACnet instance in %q: %w", s, err)
	}
	var ot ObjectType
	switch typPart {
	case "AI", "ANALOG-INPUT", "ANALOG_INPUT":
		ot = ObjAnalogInput
	case "AO", "ANALOG-OUTPUT", "ANALOG_OUTPUT":
		ot = ObjAnalogOutput
	case "AV", "ANALOG-VALUE", "ANALOG_VALUE":
		ot = ObjAnalogValue
	case "BI", "BINARY-INPUT", "BINARY_INPUT":
		ot = ObjBinaryInput
	case "BO", "BINARY-OUTPUT", "BINARY_OUTPUT":
		ot = ObjBinaryOutput
	case "BV", "BINARY-VALUE", "BINARY_VALUE":
		ot = ObjBinaryValue
	case "MSI", "MULTI-STATE-INPUT", "MULTI_STATE_INPUT":
		ot = ObjMultiStateInput
	case "MSO", "MULTI-STATE-OUTPUT", "MULTI_STATE_OUTPUT":
		ot = ObjMultiStateOutput
	case "MSV", "MULTI-STATE-VALUE", "MULTI_STATE_VALUE":
		ot = ObjMultiStateValue
	case "ACC", "ACCUMULATOR":
		ot = ObjAccumulator
	case "PC", "PULSE-CONVERTER", "PULSE_CONVERTER":
		ot = ObjPulseConverter
	case "IV", "INTEGER-VALUE", "INTEGER_VALUE":
		ot = ObjIntegerValue
	case "LAV", "LARGE-ANALOG-VALUE", "LARGE_ANALOG_VALUE":
		ot = ObjLargeAnalogValue
	case "PIV", "POSITIVE-INTEGER-VALUE", "POSITIVE_INTEGER_VALUE":
		ot = ObjPositiveIntegerValue
	default:
		return ObjectID{}, fmt.Errorf("unsupported BACnet object prefix %q", typPart)
	}
	return ObjectID{Type: ot, Instance: uint32(inst)}, nil
}

// PropertyID represents an ASHRAE-135 BACnetPropertyIdentifier.
type PropertyID uint32

const (
	PropActiveText                 PropertyID = 4
	PropApplicationSoftwareVersion PropertyID = 12
	PropDescription                PropertyID = 28
	PropFirmwareRevision           PropertyID = 44
	PropInactiveText               PropertyID = 46
	PropLocation                   PropertyID = 58
	PropModelName                  PropertyID = 70
	PropObjectIdentifier           PropertyID = 75
	PropObjectList                 PropertyID = 76
	PropObjectName                 PropertyID = 77
	PropObjectType                 PropertyID = 79
	PropOutOfService               PropertyID = 81
	PropPresentValue               PropertyID = 85
	PropStateText                  PropertyID = 110
	PropStatusFlags                PropertyID = 111
	PropUnits                      PropertyID = 117
	PropVendorIdentifier           PropertyID = 120
	PropVendorName                 PropertyID = 121
)

func (p PropertyID) String() string {
	switch p {
	case PropActiveText:
		return "active-text"
	case PropApplicationSoftwareVersion:
		return "application-software-version"
	case PropDescription:
		return "description"
	case PropFirmwareRevision:
		return "firmware-revision"
	case PropInactiveText:
		return "inactive-text"
	case PropLocation:
		return "location"
	case PropModelName:
		return "model-name"
	case PropObjectIdentifier:
		return "object-identifier"
	case PropObjectList:
		return "object-list"
	case PropObjectName:
		return "object-name"
	case PropObjectType:
		return "object-type"
	case PropOutOfService:
		return "out-of-service"
	case PropPresentValue:
		return "present-value"
	case PropStateText:
		return "state-text"
	case PropStatusFlags:
		return "status-flags"
	case PropUnits:
		return "units"
	case PropVendorIdentifier:
		return "vendor-identifier"
	case PropVendorName:
		return "vendor-name"
	default:
		return fmt.Sprintf("property-%d", uint32(p))
	}
}

// Address represents a BACnet/IP endpoint plus optional routed BACnet sub-network address (DNET/DADR).
type Address struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
	Net  uint16 `json:"net,omitempty"` // 0 = local BACnet/IP network; >0 = routed sub-network (e.g. MS/TP behind AXON)
	Mac  []byte `json:"mac,omitempty"` // Routed DADR/SADR MAC bytes when Net > 0
}

func (a Address) UDPAddr() *net.UDPAddr {
	port := a.Port
	if port == 0 {
		port = DefaultPort
	}
	return &net.UDPAddr{
		IP:   a.IP,
		Port: port,
	}
}

func (a Address) String() string {
	if a.Net > 0 {
		return fmt.Sprintf("%s:%d[net=%d,mac=%x]", a.IP.String(), a.Port, a.Net, a.Mac)
	}
	return fmt.Sprintf("%s:%d", a.IP.String(), a.Port)
}

// StatusFlags decodes the 4-bit BACnet Status_Flags bitstring.
type StatusFlags struct {
	InAlarm      bool `json:"in_alarm"`
	Fault        bool `json:"fault"`
	Overridden   bool `json:"overridden"`
	OutOfService bool `json:"out_of_service"`
}

// IAmNotification holds decoded data from a BACnet I-Am broadcast/unicast packet.
type IAmNotification struct {
	DeviceID       uint32    `json:"device_id"`
	MaxAPDULength  uint32    `json:"max_apdu_length"`
	Segmentation   uint32    `json:"segmentation"`
	VendorID       uint32    `json:"vendor_id"`
	Source         Address   `json:"source"`
	ReceivedAt     time.Time `json:"received_at"`
}

// COVPropertyValue represents a single property value reported in a COV notification.
type COVPropertyValue struct {
	PropertyID PropertyID `json:"property_id"`
	ArrayIndex *uint32    `json:"array_index,omitempty"`
	Value      any        `json:"value"`
}

// COVNotification holds decoded data from an Unconfirmed-COV-Notification packet.
type COVNotification struct {
	SubscriberProcessID uint32             `json:"subscriber_process_id"`
	InitiatingDeviceID  uint32             `json:"initiating_device_id"`
	MonitoredObject     ObjectID           `json:"monitored_object"`
	TimeRemainingSec    uint32             `json:"time_remaining_sec"`
	Values              []COVPropertyValue `json:"values"`
	Source              Address            `json:"source"`
	ReceivedAt          time.Time          `json:"received_at"`
}

// PacketEvent represents any BACnet/IP packet observed on the wire (for passive monitoring/auditing).
type PacketEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        Address   `json:"source"`
	BVLCFunction  byte      `json:"bvlc_function"`
	BVLCName      string    `json:"bvlc_name"`
	IsNetworkMsg  bool      `json:"is_network_msg"`
	PDUType       string    `json:"pdu_type,omitempty"`
	ServiceChoice byte      `json:"service_choice,omitempty"`
	ServiceName   string    `json:"service_name,omitempty"`
	Length        int       `json:"length"`
	RawHex        string    `json:"raw_hex,omitempty"`
}

// BVLCFunctionName returns a readable name for a BVLC function code.
func BVLCFunctionName(fn byte) string {
	switch fn {
	case BVLCResult:
		return "BVLC-Result"
	case BVLCForwardedNPDU:
		return "Forwarded-NPDU"
	case BVLCRegisterForeignDevice:
		return "Register-Foreign-Device"
	case BVLCDistributeBroadcastToNetwork:
		return "Distribute-Broadcast-To-Network"
	case BVLCOriginalUnicastNPDU:
		return "Original-Unicast-NPDU"
	case BVLCOriginalBroadcastNPDU:
		return "Original-Broadcast-NPDU"
	default:
		return fmt.Sprintf("BVLC-0x%02X", fn)
	}
}

// EngineeringUnitSymbol converts ASHRAE-135 EngineeringUnits enumeration IDs to standard unit symbols.
func EngineeringUnitSymbol(unitID uint32) string {
	switch unitID {
	case 0:
		return "m²"
	case 2:
		return "mA"
	case 3:
		return "A"
	case 5:
		return "V"
	case 6:
		return "kV"
	case 8:
		return "VA"
	case 9:
		return "kVA"
	case 10:
		return "MVA"
	case 11:
		return "var"
	case 12:
		return "kvar"
	case 15:
		return "PF"
	case 18:
		return "Wh"
	case 19:
		return "kWh"
	case 20:
		return "MWh"
	case 27:
		return "Hz"
	case 29:
		return "%RH"
	case 47:
		return "W"
	case 48:
		return "kW"
	case 49:
		return "MW"
	case 53:
		return "Pa"
	case 54:
		return "kPa"
	case 55:
		return "bar"
	case 62:
		return "°C"
	case 63:
		return "K"
	case 64:
		return "°F"
	case 73:
		return "s"
	case 72:
		return "min"
	case 71:
		return "h"
	case 84:
		return "L/s"
	case 85:
		return "m³/s"
	case 95:
		return "" // no-units
	case 96:
		return "ppm"
	case 97:
		return "ppb"
	case 98:
		return "%"
	case 135:
		return "m³/h"
	case 142:
		return "ft³/min"
	case 219:
		return "µg/m³"
	case 220:
		return "mg/m³"
	default:
		return fmt.Sprintf("unit-%d", unitID)
	}
}

// ParseEngineeringUnit converts a unit symbol/name (e.g. "°C", "C", "%RH", "ppm") or numeric ASHRAE unit ID into its uint32 ID.
func ParseEngineeringUnit(s string) (uint32, bool) {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(clean), "unit-") {
		if u, err := strconv.ParseUint(strings.TrimSpace(clean[5:]), 10, 32); err == nil {
			return uint32(u), true
		}
	}
	if u, err := strconv.ParseUint(clean, 10, 32); err == nil {
		return uint32(u), true
	}

	switch strings.ToLower(clean) {
	case "m²", "m2", "sq m":
		return 0, true
	case "ma":
		return 2, true
	case "a", "amp", "amps":
		return 3, true
	case "v", "volt", "volts":
		return 5, true
	case "kv":
		return 6, true
	case "va":
		return 8, true
	case "kva":
		return 9, true
	case "mva":
		return 10, true
	case "var":
		return 11, true
	case "kvar":
		return 12, true
	case "pf":
		return 15, true
	case "wh":
		return 18, true
	case "kwh":
		return 19, true
	case "mwh":
		return 20, true
	case "hz":
		return 27, true
	case "%rh", "rh":
		return 29, true
	case "w", "watt", "watts":
		return 47, true
	case "kw":
		return 48, true
	case "mw":
		return 49, true
	case "pa":
		return 53, true
	case "kpa":
		return 54, true
	case "bar":
		return 55, true
	case "°c", "c", "degc", "celsius":
		return 62, true
	case "k", "kelvin":
		return 63, true
	case "°f", "f", "degf", "fahrenheit":
		return 64, true
	case "h", "hr", "hour", "hours":
		return 71, true
	case "min", "minute", "minutes":
		return 72, true
	case "s", "sec", "second", "seconds":
		return 73, true
	case "l/s":
		return 84, true
	case "m³/s", "m3/s":
		return 85, true
	case "none", "no-units":
		return 95, true
	case "ppm":
		return 96, true
	case "ppb":
		return 97, true
	case "%", "percent":
		return 98, true
	case "m³/h", "m3/h":
		return 135, true
	case "ft³/min", "ft3/min", "cfm":
		return 142, true
	case "µg/m³", "ug/m3", "ug/m³":
		return 219, true
	case "mg/m³", "mg/m3":
		return 220, true
	default:
		return 0, false
	}
}


