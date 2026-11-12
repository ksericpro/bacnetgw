package gateway

import (
	"strings"

	"bacnet-gw/internal/bacnet"
)

const (
	// ProductionP864DeviceID is the BACnet Device Instance defined in 'Optergy BACnet Points.xlsx'.
	ProductionP864DeviceID uint32 = 864077

	// ProductionP864IP is the static IP of the Optergy P864 controller in 'Optergy BACnet Points.xlsx'.
	ProductionP864IP = "192.168.0.160"
)

// CatalogPoint defines a monitored production BACnet point from 'Optergy BACnet Points.xlsx'.
type CatalogPoint struct {
	Order           int             `json:"order"`
	DeviceID        uint32          `json:"device_id"`
	ControllerType  string          `json:"controller_type"`
	ControllerIP    string          `json:"controller_ip"`
	ObjectID        bacnet.ObjectID `json:"object_id"`
	ShortID         string          `json:"short_id"`
	SensorName      string          `json:"sensor_name"`
	DashboardLabel  string          `json:"dashboard_label"`
	PropertyLabel   string          `json:"property_label"`
	PhysicalInput   string          `json:"physical_input"`
	DefaultUnitID   uint32          `json:"default_unit_id,omitempty"`
	DefaultInactive string          `json:"default_inactive,omitempty"`
	DefaultActive   string          `json:"default_active,omitempty"`
	AlarmOnActive   bool            `json:"alarm_on_active"`
	TriggerOnActive bool            `json:"trigger_on_active"`
}

// ProductionP864Points returns the ordered list of 12 monitored BACnet points from 'Optergy BACnet Points.xlsx'.
func ProductionP864Points() []CatalogPoint {
	return []CatalogPoint{
		{
			Order:          1,
			DeviceID:       ProductionP864DeviceID,
			ControllerType: "P864",
			ControllerIP:   ProductionP864IP,
			ObjectID:       bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 101},
			ShortID:        "AI-101",
			SensorName:     "BW-INT-M (WIRELESS AIR TOUCH DISPLAY)",
			DashboardLabel: "BW-INT-M",
			PropertyLabel:  "Temperature",
			PhysicalInput:  "BG-1000-M (Wireless)",
			DefaultUnitID:  62, // °C
		},
		{
			Order:          2,
			DeviceID:       ProductionP864DeviceID,
			ControllerType: "P864",
			ControllerIP:   ProductionP864IP,
			ObjectID:       bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 102},
			ShortID:        "AI-102",
			SensorName:     "BW-INT-M (WIRELESS AIR TOUCH DISPLAY)",
			DashboardLabel: "BW-INT-M",
			PropertyLabel:  "Humidity",
			PhysicalInput:  "BG-1000-M (Wireless)",
			DefaultUnitID:  29, // %RH
		},
		{
			Order:          3,
			DeviceID:       ProductionP864DeviceID,
			ControllerType: "P864",
			ControllerIP:   ProductionP864IP,
			ObjectID:       bacnet.ObjectID{Type: bacnet.ObjAnalogValue, Instance: 27126},
			ShortID:        "AV-27126",
			SensorName:     "BW-INT-M (WIRELESS AIR TOUCH DISPLAY)",
			DashboardLabel: "BW-INT-M",
			PropertyLabel:  "Temperature Setpoint",
			PhysicalInput:  "BG-1000-M (Wireless)",
			DefaultUnitID:  62, // °C
		},
		{
			Order:          4,
			DeviceID:       ProductionP864DeviceID,
			ControllerType: "P864",
			ControllerIP:   ProductionP864IP,
			ObjectID:       bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 201},
			ShortID:        "AI-201",
			SensorName:     "BW-TH-M (WIRELESS TEMPERATURE & HUMIDITY SENSOR)",
			DashboardLabel: "BW-TH-M",
			PropertyLabel:  "Temperature",
			PhysicalInput:  "BG-1000-M (Wireless)",
			DefaultUnitID:  62, // °C
		},
		{
			Order:          5,
			DeviceID:       ProductionP864DeviceID,
			ControllerType: "P864",
			ControllerIP:   ProductionP864IP,
			ObjectID:       bacnet.ObjectID{Type: bacnet.ObjAnalogInput, Instance: 202},
			ShortID:        "AI-202",
			SensorName:     "BW-TH-M (WIRELESS TEMPERATURE & HUMIDITY SENSOR)",
			DashboardLabel: "BW-TH-M",
			PropertyLabel:  "Humidity",
			PhysicalInput:  "BG-1000-M (Wireless)",
			DefaultUnitID:  29, // %RH
		},
		{
			Order:           6,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 1},
			ShortID:         "BI-1",
			SensorName:      "WD3 (WATER LEAK)",
			DashboardLabel:  "WD3",
			PropertyLabel:   "Water detected",
			PhysicalInput:   "IN 1 • Dry Contact (LED 1)",
			DefaultInactive: "Normal",
			DefaultActive:   "Water Detected",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
		{
			Order:           7,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 2},
			ShortID:         "BI-2",
			SensorName:      "MSA237D 99 (MOTION SENSOR)",
			DashboardLabel:  "MSA237D 99",
			PropertyLabel:   "Motion / presence detected",
			PhysicalInput:   "IN 2 • Dry Contact (LED 2)",
			DefaultInactive: "Clear",
			DefaultActive:   "Motion Detected",
			AlarmOnActive:   false,
			TriggerOnActive: true,
		},
		{
			Order:           8,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 3},
			ShortID:         "BI-3",
			SensorName:      "FIRE SENSOR",
			DashboardLabel:  "Fire Detection Sensor",
			PropertyLabel:   "Fire alarm contact activated",
			PhysicalInput:   "IN 3 • Dry Contact (LED 3)",
			DefaultInactive: "Normal",
			DefaultActive:   "Fire Alarm",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
		{
			Order:           9,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 4},
			ShortID:         "BI-4",
			SensorName:      "PERIMETER INTRUSION (MAXIBUS)",
			DashboardLabel:  "PERIMETER INTRUSION (MAXIBUS)",
			PropertyLabel:   "Intrusion Detector",
			PhysicalInput:   "IN 4 • Dry Contact (LED 4)",
			DefaultInactive: "Secure",
			DefaultActive:   "Intrusion Detected",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
		{
			Order:           10,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 5},
			ShortID:         "BI-5",
			SensorName:      "UPS MONITORING (DRY Contact)",
			DashboardLabel:  "UPS MONITORING",
			PropertyLabel:   "UPS on battery / power failure",
			PhysicalInput:   "IN 5 • Dry Contact (LED 5)",
			DefaultInactive: "Mains OK",
			DefaultActive:   "UPS On Battery / Failure",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
		{
			Order:           11,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 6},
			ShortID:         "BI-6",
			SensorName:      "Access Control System (DRY Contact)",
			DashboardLabel:  "Access Control System",
			PropertyLabel:   "Unauthorized access / door forced open",
			PhysicalInput:   "IN 6 • Dry Contact (LED 6)",
			DefaultInactive: "Secure",
			DefaultActive:   "Door Forced Open",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
		{
			Order:           12,
			DeviceID:        ProductionP864DeviceID,
			ControllerType:  "P864",
			ControllerIP:    ProductionP864IP,
			ObjectID:        bacnet.ObjectID{Type: bacnet.ObjBinaryInput, Instance: 7},
			ShortID:         "BI-7",
			SensorName:      "Server Rack Door Contact (DRY Contact)",
			DashboardLabel:  "Server Rack Door Contact",
			PropertyLabel:   "Rack door opened without authorization",
			PhysicalInput:   "IN 7 • Dry Contact (LED 7)",
			DefaultInactive: "Closed",
			DefaultActive:   "Rack Door Opened",
			AlarmOnActive:   true,
			TriggerOnActive: true,
		},
	}
}

// IsProductionP864Controller returns true if the discovered BACnet device matches the production P864 controller
// (by Device Instance 864077 or ModelName/ObjectName containing P864).
func IsProductionP864Controller(deviceID uint32, modelName, objectName string) bool {
	if deviceID == ProductionP864DeviceID {
		return true
	}
	combined := strings.ToUpper(modelName + " " + objectName)
	return strings.Contains(combined, "P864") || strings.Contains(combined, "864077")
}

// LookupCatalogPoint returns the CatalogPoint definition if (deviceID/model, objID) matches a production point.
func LookupCatalogPoint(deviceID uint32, modelName, objectName string, objID bacnet.ObjectID) (CatalogPoint, bool) {
	if !IsProductionP864Controller(deviceID, modelName, objectName) {
		return CatalogPoint{}, false
	}
	for _, cp := range ProductionP864Points() {
		if cp.ObjectID == objID {
			return cp, true
		}
	}
	return CatalogPoint{}, false
}

