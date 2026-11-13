package gateway

import (
	"strings"

	"bacnet-gw/internal/bacnet"
)

// ClassifyController identifies the Optergy controller role from BACnet Device metadata.
func ClassifyController(modelName, objectName, vendorName, description string) string {
	combined := strings.ToUpper(modelName + " " + objectName + " " + description)
	switch {
	case strings.Contains(combined, "P864") || strings.Contains(combined, "864"):
		return "Optergy P864 (Production I/O Controller)"
	case strings.Contains(combined, "AXON"):
		return "Optergy AXON (Main Controller)"
	case strings.Contains(combined, "P442") || strings.Contains(combined, "442"):
		return "Optergy P442 (I/O Controller)"
	case strings.Contains(combined, "BG-1000") || strings.Contains(combined, "BG1000") || strings.Contains(combined, "WBG1000"):
		return "Optergy BG-1000-M (Multi Transceiver)"
	case strings.Contains(strings.ToUpper(vendorName), "OPTERGY"):
		return "Optergy Controller"
	default:
		return "BACnet/IP Controller"
	}
}

// ClassifyPoint matches a BACnet point against the active catalog first, then falls back to name heuristics.
func ClassifyPoint(catalog []CatalogPoint, deviceID uint32, modelName, deviceObjName, controllerRole, pointName, description string, objID bacnet.ObjectID, unitSymbol string) (equipment string, bus string) {
	if cp, ok := LookupCatalogPointInList(catalog, deviceID, modelName, deviceObjName, objID); ok {
		return cp.SensorName, cp.PhysicalInput
	}

	text := strings.ToUpper(pointName + " " + description)
	ctrl := strings.ToUpper(controllerRole)

	switch {
	case strings.Contains(text, "BW-INT") || strings.Contains(text, "BW_INT") || strings.Contains(text, "AIR TOUCH"):
		return "BW-INT-M (WIRELESS AIR TOUCH DISPLAY)", "BG-1000-M (Wireless)"
	case strings.Contains(text, "BW-TH") || strings.Contains(text, "BW_TH"):
		return "BW-TH-M (WIRELESS TEMPERATURE & HUMIDITY SENSOR)", "BG-1000-M (Wireless)"
	case strings.Contains(text, "WD3") || strings.Contains(text, "LEAK") || strings.Contains(text, "WATER"):
		return "WD3 (WATER LEAK)", "Dry Contact"
	case strings.Contains(text, "MSA237") || strings.Contains(text, "MOTION"):
		return "MSA237D 99 (MOTION SENSOR)", "Dry Contact"
	case strings.Contains(text, "FIRE"):
		return "FIRE SENSOR", "Dry Contact"
	case strings.Contains(text, "MAXIBUS") || strings.Contains(text, "INTRUSION") || strings.Contains(text, "PERIMETER"):
		return "PERIMETER INTRUSION (MAXIBUS)", "Dry Contact"
	case strings.Contains(text, "UPS"):
		return "UPS MONITORING (DRY Contact)", "Dry Contact"
	case strings.Contains(text, "ACCESS") || strings.Contains(text, "DOOR FORCED"):
		return "Access Control System (DRY Contact)", "Dry Contact"
	case strings.Contains(text, "RACK DOOR") || strings.Contains(text, "SERVER RACK"):
		return "Server Rack Door Contact (DRY Contact)", "Dry Contact"
	case strings.Contains(text, "PM1200") || strings.Contains(text, "SCHNEIDER") ||
		(strings.Contains(ctrl, "AXON") && (unitSymbol == "kW" || unitSymbol == "kWh" || unitSymbol == "kVA" || unitSymbol == "V" || unitSymbol == "A" || unitSymbol == "PF")):
		return "Schneider PM1200", "AXON DTA1 (RS485)"
	case strings.Contains(text, "RHPLC"):
		return "Dwyer RHPLC", "0-10 V"
	case strings.Contains(text, "RH/RHL") || strings.Contains(text, "RHL"):
		return "Dwyer RH/RHL", "0-10 V"
	case strings.Contains(text, "T18X") || strings.Contains(text, "CO2") || strings.Contains(text, "PM2.5") || strings.Contains(text, "TVOC") || unitSymbol == "ppm" || unitSymbol == "µg/m³":
		return "T18XAIR-485-WIFI(TY)", "RS485 Modbus"
	}

	return "", ""
}
