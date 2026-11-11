# BACnet/IP Subnet Listener & IMOPS Gateway (`bacnet-gw`)

A zero-dependency Go BACnet/IP listener and telemetry gateway built to run on the same Ethernet subnet/VLAN as the **Optergy Exhibition System** (`Optergy AXON`, `Optergy P864`, `Optergy P442`, and their wired/RS485/wireless field sensors).

It automatically detects local IPv4 subnets, discovers BACnet controllers (`Who-Is` / `I-Am`), reads `Object_List` and live point telemetry (`Present_Value`, `Units`, `Status_Flags`), captures real-time `COV` notifications and BACnet wire packets, serves a **Live Web Dashboard** (`http://127.0.0.1:8090/`), and forwards batched JSON telemetry to an external **IMOPS API**.

---

## How to Start

Choose **Option 1** to test everything locally right now with simulated Optergy controllers, or **Option 2** when running on the site/exhibition PC connected to the real BACnet subnet.

---

### Option 1: Start Locally with Simulated Hardware (No Controllers Required)

This starts 3 simulated BACnet controllers (**Optergy AXON `1001`**, **Optergy P864 `8640`**, **Optergy P442 `4420`** with all 24 Exhibition sensors) plus a mock IMOPS API server on `127.0.0.1:8085`.

#### 1. Open **Terminal 1** (in `c:\Projects\ZAARR-DTE\bacnet gw`) and start the simulator:
```powershell
.\mock-exhibition.exe
```
*(Or from source: `go run ./cmd/mock-exhibition`)*

#### 2. Open **Terminal 2** (in `c:\Projects\ZAARR-DTE\bacnet gw`) and start the gateway:
```powershell
$env:IMOPS_BASE_URL="http://127.0.0.1:8085"
$env:BACNET_PORT="47809"
$env:BACNET_BROADCAST_OVERRIDES="127.0.0.1:47810,127.0.0.1:47811,127.0.0.1:47812"
.\bacnet-gw.exe
```
*(Or from source: `go run ./cmd/bacnet-gw`)*

#### 3. Open the Live Web Dashboard in your browser:
- **Live Visual Dashboard:** `http://127.0.0.1:8090/`
- **Raw Status JSON:** `http://127.0.0.1:8090/status`
- **Health Check:** `http://127.0.0.1:8090/healthz`

Stop either process anytime with `Ctrl + C`.

---

### Option 2: Start on the Real BACnet Subnet (Site / Exhibition PC)

When deploying onto a machine plugged into the same Ethernet switch/VLAN as the **Optergy AXON, P864, and P442** controllers:

#### Step 1 — Check Detected Subnets
Verify that the gateway sees your site Ethernet adapter and its directed broadcast address:
```powershell
.\bacnet-gw.exe -list-subnets
```
Example output:
```text
Detected IPv4 Subnets for BACnet/IP Discovery (UDP 47808):
  - Interface: Ethernet 2   IP: 193.168.100.1   CIDR: 193.168.100.1/24   Broadcast: 193.168.100.255:47808
```

#### Step 2 — Allow BACnet UDP Port `47808` in Windows Firewall (Run PowerShell as Administrator once)
BACnet/IP uses UDP port `47808` (`0xBAC0`) for `Who-Is` / `I-Am` discovery and polling:
```powershell
New-NetFirewallRule -DisplayName "BACnet-IP Gateway (UDP 47808)" -Direction Inbound -Protocol UDP -LocalPort 47808 -Action Allow
```

#### Step 3 — Create `config.yaml`
Copy the template configuration file:
```powershell
Copy-Item config.example.yaml config.yaml
```

Edit `config.yaml` to match your environment:
- **Network interface (optional):** Leave `network.interface: ""` to scan all IPv4 subnets automatically, or set it to your specific NIC name (e.g. `interface: "Ethernet 2"`) if the PC has multiple networks (Wi-Fi + Site LAN).
- **Dry-Run Mode (test BACnet before IMOPS is ready):** Leave `imops.base_url: ""` and `dry_run_log_payloads: true`. The gateway will discover all BACnet devices, populate the dashboard at `http://127.0.0.1:8090/`, and print sample outbound JSON batches to the console without failing.
- **Live IMOPS API Mode:** Fill in `imops.base_url` and your authentication credentials:
  ```yaml
  imops:
    enabled: true
    base_url: "https://api.imops.your-domain.com"
    telemetry_path: "/api/v1/bacnet/telemetry"
    devices_path: "/api/v1/bacnet/devices"
    auth_type: "api_key"     # "api_key" | "bearer" | "basic" | "none"
    api_key_header: "X-API-Key"
    api_key: "YOUR_IMOPS_API_KEY"
  ```
  *(Alternatively, copy `.env.example` to `.env` and set `IMOPS_BASE_URL`, `IMOPS_AUTH_TYPE`, `IMOPS_API_KEY`, or `IMOPS_BEARER_TOKEN`.)*

#### Step 4 — Start the Gateway
```powershell
.\bacnet-gw.exe -config config.yaml
```
*(If `config.yaml` is in the current folder, running `.\bacnet-gw.exe` with no flags loads it automatically.)*

Open `http://127.0.0.1:8090/` on the machine to watch discovered Optergy controllers, live sensor overlays (`dashboard.jpeg` & wiring diagram toggle), BACnet packets, and IMOPS delivery status.

#### Step 5 (Optional) — Auto-Start on Windows Boot
To register `bacnet-gw.exe` so it runs automatically in the background whenever the Windows PC restarts (run PowerShell as Administrator):
```powershell
$workDir = (Get-Location).Path
$action  = New-ScheduledTaskAction -Execute "$workDir\bacnet-gw.exe" -Argument "-config `"$workDir\config.yaml`"" -WorkingDirectory $workDir
$trigger = New-ScheduledTaskTrigger -AtStartup
Register-ScheduledTask -TaskName "ZAARR-BACnet-GW" -Action $action -Trigger $trigger -User "SYSTEM" -RunLevel Highest -Force
Start-ScheduledTask -TaskName "ZAARR-BACnet-GW"
```

---

## Building from Source & Running Tests

Requires **Go 1.24+** (zero external libraries required; builds completely offline).

```powershell
# Build Windows binaries
go build -o bacnet-gw.exe ./cmd/bacnet-gw
go build -o mock-exhibition.exe ./cmd/mock-exhibition

# Cross-compile static Linux binary (e.g., for Linux IPC / Raspberry Pi)
$env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -o bacnet-gw ./cmd/bacnet-gw

# Run unit and end-to-end integration tests
go test -v ./...
```

---

## CLI Flags & Local Endpoints

### CLI Flags (`bacnet-gw`)
| Flag | Description |
| --- | --- |
| `-config <path>` | Path to `config.yaml` or `config.json` (auto-detects `config.yaml` / `config.json` in working directory if omitted) |
| `-env <path>` | Path to `.env` file (auto-loads `.env` in working directory if present) |
| `-list-subnets` | Prints all detected IPv4 interfaces and their subnet-directed BACnet broadcast addresses, then exits |

### Local HTTP Endpoints (`127.0.0.1:8090` by default)
| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/` | `GET` | Live interactive web dashboard (Facility overlay, Wiring view, Points table, Wire packet inspector) |
| `/status` | `GET` | Full JSON snapshot of subnets, controllers, live point values, packet stream, and IMOPS stats |
| `/healthz` | `GET` | Lightweight health probe (`{"status":"ok", ...}`) |
| `/discover` | `POST` | Triggers an immediate `Who-Is` broadcast scan on the subnet |

---

## Outbound IMOPS API Payloads

1. **Device Inventory (`POST {base_url}/api/v1/bacnet/devices`)** — Sent when each controller (`AXON`, `P864`, `P442`, etc.) is discovered and enriched.
2. **Batched Telemetry (`POST {base_url}/api/v1/bacnet/telemetry`)** — Sent every poll cycle (`poll_interval_sec`, default `15s`) and immediately on real-time `COV` events (`source_type: "cov"`):
   ```json
   {
     "gateway_id": "zaarr-bacnet-gw-01",
     "site_name": "Optergy Exhibition System",
     "batch_id": "20261111T073015Z-1",
     "sent_at": "2026-11-11T07:30:15Z",
     "count": 1,
     "readings": [
       {
         "timestamp": "2026-11-11T07:30:15Z",
         "source_type": "poll",
         "device_id": 8640,
         "device_name": "Optergy-P864-IO",
         "controller_model": "P864",
         "device_address": "192.168.1.11:47808",
         "object_type": "analog-input",
         "object_instance": 1,
         "object_id": "analog-input:1",
         "point_name": "UI1_RHPLC_Room_Temp",
         "equipment_profile": "Dwyer RHPLC",
         "connection_bus": "P864 UI1 (0-10 V)",
         "value": 23.4,
         "value_text": "23.4 °C",
         "unit": "°C",
         "unit_id": 62,
         "quality": "good",
         "status_flags": {
           "in_alarm": false,
           "fault": false,
           "overridden": false,
           "out_of_service": false
         }
       }
     ]
   }
   ```
3. **Wire Packet Events (`POST {base_url}/api/v1/bacnet/packets`)** — Optional stream of decoded BVLC/NPDU/APDU packets when `polling.forward_packet_events: true`.

---

## Troubleshooting

- **Running on the same PC as Optergy Studio / another BACnet client:** `network.reuse_port: true` is enabled by default (`SO_REUSEADDR`). If the existing BMS software holds an exclusive lock on UDP `47808`, either start `bacnet-gw` on a dedicated machine/VM on the same VLAN or set `network.port: 47809` with `broadcast_overrides: ["<subnet-broadcast-ip>:47808"]`.
- **No devices discovered on `Who-Is`:**
  1. Confirm the PC IP is in the same subnet/VLAN as the Optergy controllers (`.\bacnet-gw.exe -list-subnets`).
  2. Check Windows Defender Firewall allows inbound/outbound UDP `47808`.
  3. If controllers are across a routed VLAN with a BBMD, set `network.bbmd_address: "<bbmd-ip>:47808"` in `config.yaml` to register as a BACnet Foreign Device.
