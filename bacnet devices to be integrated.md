This diagram illustrates the wiring and integration architecture for an **Exhibition System** combining Optergy controllers (AXON, P864, P442), an Optergy Air transceiver, and third-party multi-vendor IoT devices into **Optergy Studio (BMS / Visualization)**.

---

### 1. System Overview & Communication Types

- **Ethernet (TCP/IP - Blue):** Interconnects the Ethernet Switch (Site Network) to Optergy Studio, the AXON main controller, and the P864 and P442 I/O controllers.

- **RS485 / Modbus RTU (Purple):** Used for serial communication between controllers and field devices.

- **Analog Signals (Orange/Yellow):** Transmits 0–10 VDC / 4–20 mA signals to universal inputs.

- **Digital Inputs (Brown):** Connects dry contact NO/NC devices to digital inputs.

- **Wireless (Green dashed):** 2.4 GHz Optergy Air wireless protocol.

- **Power Distribution:** A central 24 VDC power supply powers the P864, P442, BG-1000-M, and wired sensors using 24 VDC (Red) and 0V/GND (Black) lines.

---

### 2. Controller & Device Connections

#### **Optergy AXON (Main Controller)**

- **RS485 Port (DTA1):** Connects directly to the **Schneider PM1200 Power Meter** (A+ to A+, B- to B-).

- **Auxiliary Power:** The PM1200 uses 44–277 VAC/DC auxiliary power.

#### **Optergy P864 (I/O Controller)**

- **UI1 (0–10 V):** Wired to the **Dwyer RHPLC** (Temperature & Relative Humidity).

- **UI2 (0–10 V):** Wired to the **Dwyer RH/RHL** (Temperature & Relative Humidity).

- **DATA2 (RS485):** Connects to the **Optergy BG-1000-M Multi Transceiver** using a 4-core cable (max 30 m) carrying DATA+, DATA-, GND, and 24 VDC power.

- **Wireless Sub-network (via BG-1000-M):**
- **Optergy Air Touch BW-INT-M:** Battery-powered wireless sensor (Temp, RH, Setpoint).

- **Optergy Air BW-TH-M:** Battery-powered wireless sensor (Temp, RH).

#### **Optergy P442 (I/O Controller)**

- **IN1 (Dry Contact):** Connects to the **Dwyer WD3 Water Leak Detector** (COM & NO/NC).

- **IN2 (Dry Contact):** Connects to the **MSA237D 99 Motion / Presence Sensor** (COM & NO).

- **DATA2 (RS485):** Connects to the **T18XAIR-485-WIFI(TY) Air Quality Sensor** (A+ to A1, B- to B1, with 24 VDC power and GND).

---

### 3. Sensor Terminal References

- **Dwyer RHPLC / RH/RHL (0–10 V):**
- `V+` $\rightarrow$ 24 VDC

- `GND` $\rightarrow$ 0 V

- `RH` $\rightarrow$ P864 UIx (0–10 V)

- `T` $\rightarrow$ P864 UIx (0–10 V)

- **Dwyer WD3 (Water Leak):**
- `COM` $\rightarrow$ P442 IN1

- `NO/NC` $\rightarrow$ P442 IN1 (as configured)

- **MSA237D 99 (Motion):**
- `V+` $\rightarrow$ 24 VDC

- `GND` $\rightarrow$ 0 V

- `NO` $\rightarrow$ P442 IN2

- `COM` $\rightarrow$ P442 IN2

---

### 4. Integration Data Points in Optergy Studio

| Device               | Connection        | Example Data Points            |
| -------------------- | ----------------- | ------------------------------ |
| **Schneider PM1200** | AXON DTA1 (RS485) | kW, kWh, kVA, V, I, PF, Demand |

|
| **Dwyer RHPLC** | P864 UI1 (0–10 V) | Room RH (%), Room Temp (°C)

|
| **Dwyer RH/RHL** | P864 UI2 (0–10 V) | Zone RH (%), Zone Temp (°C)

|
| **Dwyer WD3** | P442 IN1 (DI) | Water Leak Alarm (Normal/Alarm)

|
| **MSA237D 99** | P442 IN2 (DI) | Occupancy (Occupied/Unoccupied)

|
| **T18XAIR-485-WIFI(TY)** | P442 DATA2 (RS485) | $\text{CO}_2$ (ppm), $\text{PM2.5}$ ($\mu\text{g/m}^3$), TVOC, Temp (°C), RH (%)

|
| **BW-INT-M (Air Touch)** | BG-1000-M (Wireless) | Temp, RH, Setpoint, Occupancy

|
| **BW-TH-M** | BG-1000-M (Wireless) | Temp (°C), RH (%)

|


---

### 5. Production Monitored BACnet Points & Alarm Matrix (`Optergy BACnet Points.xlsx`)

In production (`192.168.0.0/24` subnet, Gateway `192.168.0.1`), the monitored sensors are consolidated onto **Optergy P864** (`IP: 192.168.0.160`, `BACnet Device Instance: 864077`):

| Sensor / System | BACnet Property | BACnet Object | Input / Comm | LED Output | Trigger Condition & Expected Response |
| --- | --- | --- | --- | --- | --- |
| **BW-INT-M** (Wireless Air Touch Display) | Temperature | `AI-101` | `BG-1000-M` | N/A | Live temperature reading (`°C`) & threshold check |
| **BW-INT-M** (Wireless Air Touch Display) | Humidity | `AI-102` | `BG-1000-M` | N/A | Live relative humidity reading (`%RH`) |
| **BW-INT-M** (Wireless Air Touch Display) | Temperature Setpoint | `AV-27126` | `BG-1000-M` | N/A | Live room setpoint (`°C`) |
| **BW-TH-M** (Wireless Temp & Humidity) | Temperature | `AI-201` | `BG-1000-M` | N/A | High temperature alert; displays actual reading & threshold |
| **BW-TH-M** (Wireless Temp & Humidity) | Humidity | `AI-202` | `BG-1000-M` | N/A | High humidity alert; displays actual `%RH` reading |
| **WD3** (Water Leak) | Water detected | `BI-1` | `IN 1` (Dry Contact) | `LED 1` | Water leak icon flashes red; critical alarm & location displayed |
| **MSA237D 99** (Motion Sensor) | Motion / presence detected | `BI-2` | `IN 2` (Dry Contact) | `LED 2` | Motion icon flashes; event timestamp recorded |
| **Fire Sensor** | Fire alarm contact activated | `BI-3` | `IN 3` (Dry Contact) | `LED 3` | Fire icon flashes red; critical fire alarm displayed |
| **Perimeter Intrusion (MAXIBUS)** | Intrusion Detector | `BI-4` | `IN 4` (Dry Contact) | `LED 4` | Perimeter intrusion icon flashes red; high-priority zone alarm |
| **UPS Monitoring** | UPS on battery / power failure | `BI-5` | `IN 5` (Dry Contact) | `LED 5` | UPS icon flashes red; power failure alarm & UPS status displayed |
| **Access Control System** | Unauthorized access / door forced | `BI-6` | `IN 6` (Dry Contact) | `LED 6` | Access door icon flashes red; security alarm displayed |
| **Server Rack Door Contact** | Rack door opened unauthorized | `BI-7` | `IN 7` (Dry Contact) | `LED 7` | Affected rack icon flashes red; unauthorized access alarm displayed |
| **Communication Health** | Controller / Switch reachability | Device `864077` | Ethernet / `RS485` | N/A | Communication fault icon flashes on poll timeout / device loss |
