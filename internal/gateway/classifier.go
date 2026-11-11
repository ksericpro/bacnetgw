package gateway

import (
	"strings"

	"bacnet-gw/internal/bacnet"
)

// ClassifyController identifies the Exhibition System controller role from BACnet Device metadata.
func ClassifyController(modelName, objectName, vendorName, description string) string {
	combined := strings.ToUpper(modelName + " " + objectName + " " + description)
	switch {
	case strings.Contains(combined, "AXON"):
		return "Optergy AXON (Main Controller)"
	case strings.Contains(combined, "P864") || strings.Contains(combined, "864"):
		return "Optergy P864 (I/O Controller)"
	case strings.Contains(combined, "P442") || strings.Contains(combined, "442"):
		return "Optergy P442 (I/O Controller)"
	case strings.Contains(combined, "BG-1000") || strings.Contains(combined, "BG1000"):
		return "Optergy BG-1000-M (Multi Transceiver)"
	case strings.Contains(strings.ToUpper(vendorName), "OPTERGY"):
		return "Optergy Controller"
	default:
		return "BACnet/IP Controller"
	}
}

// ClassifyPoint matches a BACnet point's name/description/controller/units to Exhibition System field devices
// defined in 'bacnet devices to be integrated.md' while gracefully falling back for generic points.
func ClassifyPoint(controllerRole, pointName, description string, objID bacnet.ObjectID, unitSymbol string) (equipment string, bus string) {
	text := strings.ToUpper(pointName + " " + description)
	ctrl := strings.ToUpper(controllerRole)

	switch {
	case strings.Contains(text, "PM1200") || strings.Contains(text, "SCHNEIDER"):
		return "Schneider PM1200", "AXON DTA1 (RS485)"

	case strings.Contains(text, "RHPLC") || (strings.Contains(ctrl, "864") && strings.Contains(text, "UI1")):
		return "Dwyer RHPLC", "P864 UI1 (0-10 V)"

	case strings.Contains(text, "RH/RHL") || strings.Contains(text, "RHL") || (strings.Contains(ctrl, "864") && strings.Contains(text, "UI2")):
		return "Dwyer RH/RHL", "P864 UI2 (0-10 V)"

	case strings.Contains(text, "WD3") || strings.Contains(text, "LEAK") || (strings.Contains(ctrl, "442") && (strings.Contains(text, "IN1") || (objID.Type == bacnet.ObjBinaryInput && objID.Instance == 1))):
		return "Dwyer WD3", "P442 IN1 (DI)"

	case strings.Contains(text, "MSA237") || strings.Contains(text, "MOTION") || (strings.Contains(ctrl, "442") && (strings.Contains(text, "IN2") || (objID.Type == bacnet.ObjBinaryInput && objID.Instance == 2))):
		return "MSA237D 99", "P442 IN2 (DI)"

	case strings.Contains(text, "T18X") || strings.Contains(text, "CO2") || strings.Contains(text, "PM2.5") || strings.Contains(text, "PM25") || strings.Contains(text, "TVOC") || unitSymbol == "ppm" || unitSymbol == "µg/m³":
		return "T18XAIR-485-WIFI(TY)", "P442 DATA2 (RS485)"

	case strings.Contains(text, "BW-INT") || strings.Contains(text, "BW_INT") || strings.Contains(text, "AIR TOUCH") || strings.Contains(text, "SETPOINT"):
		return "BW-INT-M (Air Touch)", "BG-1000-M (Wireless)"

	case strings.Contains(text, "BW-TH") || strings.Contains(text, "BW_TH"):
		return "BW-TH-M", "BG-1000-M (Wireless)"

	case strings.Contains(ctrl, "AXON") && (unitSymbol == "kW" || unitSymbol == "kWh" || unitSymbol == "kVA" || unitSymbol == "V" || unitSymbol == "A" || unitSymbol == "PF"):
		return "Schneider PM1200", "AXON DTA1 (RS485)"
	}

	return "", ""
}

