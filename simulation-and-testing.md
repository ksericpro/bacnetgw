# Simulation & Testing Guide (`bacnet-gw`)

This guide covers how to run the full BACnet/IP + Digital Twin UI stack **locally without hardware**, toggle the **7 discrete dry-contact alarms**, inject **5 continuous analog sensor readings**, customize point mappings in `config.yaml`, and run **on-site hardware scans**.

---

## 1. Point Types Overview (12 Monitored Points)

All 12 production points are configured in the `points:` block of `config.yaml` (or `config.sim.yaml` for local testing) — no Go recompilation is needed when changing or adding points.

### A. 7 Discrete Dry-Contact Points (`BI-1` – `BI-7`)
Binary values (`0` = OFF / Inactive, `1` = ON / Active):

| Key | Object | Sensor | `0` (OFF — Solid Green) | `1` (ON — Flashing in UI) | UI Flash Color |
| :-- | :--- | :--- | :--- | :--- | :--- |
| `1` | `BI-1` | WD3 Water Leak | `Normal` | `Water Detected` | Flashing **Red** (`ALARM`) |
| `2` | `BI-2` | MSA237D 99 Motion Sensor | `Clear` | `Motion Detected` | Flashing **Amber** (`ACTIVE`) |
| `3` | `BI-3` | Fire Detection Sensor | `Normal` | `Fire Alarm` | Flashing **Red** (`ALARM`) |
| `4` | `BI-4` | Perimeter Intrusion (MAXIBUS) | `Secure` | `Intrusion Detected` | Flashing **Red** (`ALARM`) |
| `5` | `BI-5` | UPS Monitoring | `Mains OK` | `UPS On Battery / Failure` | Flashing **Red** (`ALARM`) |
| `6` | `BI-6` | Access Control System | `Secure` | `Door Forced Open` | Flashing **Red** (`ALARM`) |
| `7` | `BI-7` | Server Rack Door Contact | `Closed` | `Rack Door Opened` | Flashing **Red** (`ALARM`) |

> **Tip:** Whether a binary point flashes **Red (`ALARM`)** or **Amber (`ACTIVE`)** is controlled by `alarm_on_active: true | false` in `config.yaml`.

### B. 5 Continuous Analog Sensor Points (`AI` / `AV`)
Floating-point numeric telemetry (`°C` and `%RH`) from the two wireless sensors:

| Object | Sensor | Property | Unit | Example Value |
| :--- | :--- | :--- | :--- | :--- |
| `AI-101` | BW-INT-M (Wireless Air Touch Display) | Temperature | `°C` | `23.5` |
| `AI-102` | BW-INT-M (Wireless Air Touch Display) | Humidity | `%RH` | `54.2` |
| `AV-27126` | BW-INT-M (Wireless Air Touch Display) | Temperature Setpoint | `°C` | `22.0` |
| `AI-201` | BW-TH-M (Wireless Temp & Humidity) | Temperature | `°C` | `24.3` |
| `AI-202` | BW-TH-M (Wireless Temp & Humidity) | Humidity | `%RH` | `58.0` |

---

## 2. Local Simulation Quick Start (No Hardware Required)

### One-Command Launch
From the project root (`c:\Projects\ZAARR-DTE\bacnet gw`), run:

```powershell
.\start-sim.bat
```

What `start-sim.bat` starts automatically:
1. **`mock-exhibition.exe`** — Emulates Optergy P864 (`Device ID: 864077`) over BACnet/IP UDP on `127.0.0.1:47810` plus a local Mock IMOPS receiver & control API on `http://127.0.0.1:8085`.
2. **`bacnet-gw.exe -config config.sim.yaml`** — Binds local BACnet listener on `127.0.0.1:47809`, discovers `#864077`, subscribes to real-time `COV`, and serves the Digital Twin Web UI.
3. **Browser Dashboard** — Opens **http://127.0.0.1:8090**.
4. **Interactive Dry-Contact Controller** — Launches `.\sim-contacts.ps1`.

*(If you prefer starting the processes manually in separate terminals instead of `start-sim.bat`:)*
```powershell
# Terminal 1 — Simulated P864 + Mock IMOPS
.\mock-exhibition.exe

# Terminal 2 — Gateway with local simulation config
.\bacnet-gw.exe -config config.sim.yaml

# Terminal 3 — Interactive dry-contact controller
.\sim-contacts.ps1
```

---

## 3. Simulating the 7 Dry Contacts (`sim-contacts.ps1`)

### Mode A: Interactive Keyboard Menu
Run without arguments:
```powershell
.\sim-contacts.ps1
```
- Press **`1` – `7`** to toggle `BI-1` through `BI-7` between **ON (`1` -> Flashing)** and **OFF (`0` -> Solid Green)**.
- Press **`A`** to turn **ALL 7 contacts ON** simultaneously.
- Press **`O`** to reset **ALL 7 contacts OFF** (Normal).
- Press **`D`** to run an **Auto-Demo Sweep** (cycles `BI-1`..`BI-7` ON for 2 seconds each, then resets OFF).
- Press **`Q`** to quit the menu.

### Mode B: One-Shot CLI Commands
Trigger or clear specific contacts from scripts or another PowerShell window:
```powershell
.\sim-contacts.ps1 BI-1 on      # WD3 Water Leak -> Flashing Red
.\sim-contacts.ps1 BI-1 off     # WD3 Water Leak -> Normal (Green)

.\sim-contacts.ps1 BI-2 on      # Motion Sensor -> Flashing Amber
.\sim-contacts.ps1 BI-2 off     # Motion Sensor -> Clear (Green)

.\sim-contacts.ps1 3 on         # Shorthand '3' also maps to BI-3 (Fire Alarm)
.\sim-contacts.ps1 all on       # Flash all 7 dry contacts at once
.\sim-contacts.ps1 all off      # Reset all 7 dry contacts to normal
.\sim-contacts.ps1 demo         # Run sequential 1..7 demo sweep
```

Each toggle updates the simulated P864 object's `Present_Value` **and** emits a real BACnet `Unconfirmed-COV-Notification` UDP packet to `bacnet-gw.exe`, so the Web UI at `http://127.0.0.1:8090` updates within ~1–1.5 seconds.

---

## 4. Simulating the 5 Continuous Analog Sensors

While `mock-exhibition.exe` is running (note: `AI-201` also drifts slightly every 15 seconds automatically), you can push any floating-point value to the 5 analog objects using PowerShell:

```powershell
# BW-INT-M (Wireless Air Touch Display: Temp, Humidity, Setpoint)
Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=AI-101&val=26.8"
Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=AI-102&val=63.5"
Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=AV-27126&val=22.5"

# BW-TH-M (Wireless Temp & Humidity Sensor)
Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=AI-201&val=29.4"
Invoke-RestMethod -Method Post "http://127.0.0.1:8085/sim/set?point=AI-202&val=71.2"
```

---

## 5. Diagnostic Endpoints During Testing

While `bacnet-gw.exe` is running, inspect live state directly via HTTP:

| Endpoint | Description |
| :--- | :--- |
| `http://127.0.0.1:8090/` | Live Digital Twin Web Dashboard + Wiring Diagram toggle |
| `http://127.0.0.1:8090/status` | Full JSON snapshot (devices, 12 point values/flags, recent BACnet UDP frames, IMOPS stats) |
| `http://127.0.0.1:8090/healthz` | Liveness/health check (`200 OK`) |
| `POST http://127.0.0.1:8090/discover` | Force an immediate `Who-Is` broadcast |

---

## 6. Testing Custom Point Mappings in YAML (No Recompile)

Edit `points:` in `config.sim.yaml` (for local testing) or `config.yaml` (for production) and restart `bacnet-gw.exe`:

```yaml
points:
  - device_id: 864077
    controller_ip: "192.168.0.160"
    object: "BI-1"
    sensor_name: "WD3 (WATER LEAK)"
    dashboard_label: "WD3"
    property_label: "Water detected"
    physical_input: "IN 1 • Dry Contact (LED 1)"
    inactive_text: "Normal"
    active_text: "Water Detected"
    alarm_on_active: true      # true = Flashing Red (ALARM), false = Flashing Amber (ACTIVE)
    trigger_on_active: true
```

Supported `object` prefixes: `AI`, `AO`, `AV`, `BI`, `BO`, `BV`, `MSI`, `MSO`, `MSV`, `ACC`, `PC`, `IV`, `LAV`, `PIV` (e.g. `"AI-101"`, `"BI-1"`, `"AV-27126"`).

---

## 7. On-Site Hardware Discovery & Verification Commands

When testing against real Optergy controllers on the `192.168.0.0/24` LAN:

1. **List active IPv4 network adapters & broadcast addresses:**
   ```powershell
   .\bacnet-gw.exe -list-subnets
   ```
   *(If both `Ethernet` and `Wi-Fi` share `192.168.0.0/24`, lock `network.subnet: "Ethernet"` and `network.bind_ip: "<Ethernet-IP>"` in `config.yaml` so Windows routes BACnet UDP strictly over Ethernet.)*

2. **Quick controller discovery (`Who-Is` / `I-Am` summary table):**
   ```powershell
   .\bacnet-gw.exe -scan
   ```

3. **Full object & live value dump across all discovered controllers:**
   ```powershell
   .\bacnet-gw.exe -scan-points
   ```

4. **Run automated Go unit & integration tests (on dev machine):**
   ```powershell
   go test -v ./...
   ```
   - `internal/bacnet`: BVLC/NPDU/APDU encoding, `Who-Is`/`I-Am`, `Unconfirmed-COV`, and subnet filtering.
   - `internal/config`: Single-file YAML parsing (`config.production.yaml`, `config.example.yaml`, `config.sim.yaml`) and `points:` validation.
   - `internal/gateway`: End-to-end simulated P864 discovery, 12-point polling, custom YAML points, and `COV` alarm delivery.
   - `internal/imops`: HTTP batching, auth modes, and retry backoff.

