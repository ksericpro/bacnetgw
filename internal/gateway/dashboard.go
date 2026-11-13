package gateway

import (
	"net/http"
	"os"
)

func (g *Gateway) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(dashboardHTML))
}

func (g *Gateway) handleDashboardImage(w http.ResponseWriter, r *http.Request) {
	serveLocalJPEG(w, r, "dashboard.jpeg")
}

func (g *Gateway) handleTopologyImage(w http.ResponseWriter, r *http.Request) {
	serveLocalJPEG(w, r, "bacnet devices.jpeg")
}

func serveLocalJPEG(w http.ResponseWriter, r *http.Request, filename string) {
	if _, err := os.Stat(filename); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filename)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>Smart Nation Exhibition System</title>
<style>
  :root {
    --bg: #0b1120;
    --panel: #111827;
    --panel-alt: #1e293b;
    --border: #334155;
    --text: #f1f5f9;
    --muted: #94a3b8;
    --accent: #38bdf8;
    --good: #22c55e;
    --warn: #f59e0b;
    --alarm: #ef4444;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.45;
    padding: 16px 22px;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 14px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 16px;
  }
  .title-group h1 { font-size: 1.2rem; font-weight: 700; letter-spacing: -0.01em; }
  .title-group p { font-size: 0.82rem; color: var(--muted); }
  .actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  button {
    background: #0284c7;
    color: #fff;
    border: none;
    border-radius: 6px;
    padding: 7px 13px;
    font-size: 0.82rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }
  button:hover { background: #0369a1; }
  button.secondary { background: var(--panel-alt); border: 1px solid var(--border); color: var(--text); }
  button.secondary:hover { background: #334155; }
  select.subnet-picker {
    background: var(--panel-alt);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 6px 10px;
    font-size: 0.8rem;
    font-weight: 500;
    cursor: pointer;
    outline: none;
  }
  select.subnet-picker:focus { border-color: var(--accent); }

  .kpi-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .kpi {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 11px 14px;
  }
  .kpi .label { font-size: 0.72rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--muted); }
  .kpi .val { font-size: 1.4rem; font-weight: 700; margin-top: 3px; color: var(--accent); }
  .kpi .val.alarm-text { color: var(--alarm); }
  .kpi .sub { font-size: 0.74rem; color: var(--muted); margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .main-grid {
    display: grid;
    grid-template-columns: 1.42fr 1fr;
    gap: 16px;
    margin-bottom: 16px;
  }
  @media (max-width: 1200px) { .main-grid { grid-template-columns: 1fr; } }

  .card {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 10px;
    overflow: hidden;
  }
  .card-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 14px;
    background: rgba(30, 41, 59, 0.6);
    border-bottom: 1px solid var(--border);
    font-weight: 600;
    font-size: 0.88rem;
  }
  .twin-wrap {
    position: relative;
    width: 100%;
    background: #ffffff;
    min-height: 320px;
  }
  .twin-wrap img {
    width: 100%;
    display: block;
    height: auto;
  }
  .pill {
    position: absolute;
    transform: translate(-50%, -50%);
    background: rgba(15, 23, 42, 0.92);
    border: 1.5px solid var(--accent);
    border-radius: 7px;
    padding: 3px 8px;
    font-size: 0.7rem;
    box-shadow: 0 4px 12px rgba(0,0,0,0.45);
    backdrop-filter: blur(4px);
    white-space: nowrap;
    z-index: 3;
    transition: all 0.2s ease;
  }
  .pill .p-tag {
    font-size: 0.62rem;
    font-weight: 700;
    color: #cbd5e1;
    letter-spacing: 0.02em;
  }
  .pill .p-val {
    font-weight: 700;
    color: var(--accent);
    font-size: 0.76rem;
  }
  .pill.ok { border-color: #22c55e; }
  .pill.ok .p-val { color: #4ade80; }
  .pill.active-event {
    border-color: var(--warn);
    background: rgba(69, 39, 6, 0.94);
    animation: pulseEvent 1.1s infinite alternate;
    z-index: 5;
  }
  .pill.active-event .p-val { color: #fcd34d; }
  .pill.alarm {
    border-color: var(--alarm);
    background: rgba(127, 29, 29, 0.96);
    animation: pulseAlarm 1.1s infinite alternate;
    z-index: 6;
  }
  .pill.alarm .p-val { color: #fecaca; }

  @keyframes pulseEvent {
    from { box-shadow: 0 0 6px rgba(245,158,11,0.55); transform: translate(-50%, -50%) scale(1.0); }
    to   { box-shadow: 0 0 18px rgba(245,158,11,1.0); transform: translate(-50%, -50%) scale(1.05); }
  }
  @keyframes pulseAlarm {
    from { box-shadow: 0 0 6px rgba(239,68,68,0.6); transform: translate(-50%, -50%) scale(1.0); }
    to   { box-shadow: 0 0 18px rgba(239,68,68,1.0); transform: translate(-50%, -50%) scale(1.05); }
  }

  .table-wrap { max-height: 520px; overflow-y: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.78rem; }
  th, td { padding: 7px 10px; text-align: left; border-bottom: 1px solid rgba(51, 65, 85, 0.5); }
  th { position: sticky; top: 0; background: var(--panel-alt); color: var(--muted); font-weight: 600; z-index: 2; }
  tr.row-alarm { background: rgba(239, 68, 68, 0.12); }
  .badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 999px;
    font-size: 0.68rem;
    font-weight: 700;
  }
  .badge.good { background: rgba(34,197,94,0.16); color: #4ade80; }
  .badge.event { background: rgba(245,158,11,0.22); color: #fbbf24; }
  .badge.alarm { background: rgba(239,68,68,0.28); color: #fca5a5; }
  .mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 0.74rem; }
</style>
</head>
<body>
<header>
  <div class="title-group">
    <h1 id="siteTitle">Smart Nation Exhibition System</h1>
    <p id="listenInfo">Target: P864 (Device 864077 @ 192.168.0.160) • UDP 47808</p>
  </div>
  <div class="actions">
    <select id="subnetSelect" class="subnet-picker" onchange="onSelectSubnet(this.value)" title="Select local IPv4 subnet for BACnet discovery">
      <option value="">Subnet: All Detected (Auto)</option>
    </select>
    <button onclick="triggerDiscover()">Broadcast Who-Is Now</button>
    <button class="secondary" onclick="toggleVisual()" id="toggleBtn">Show Wiring Diagram</button>
    <a href="/status" target="_blank"><button class="secondary">Raw /status JSON</button></a>
  </div>
</header>

<section class="kpi-grid">
  <div class="kpi">
    <div class="label">P864 Controller Status</div>
    <div class="val" id="kpiDevices">Scanning...</div>
    <div class="sub" id="kpiSubnets">Detecting subnet...</div>
  </div>
  <div class="kpi">
    <div class="label">Active Alarms / Triggers</div>
    <div class="val" id="kpiAlarms">0</div>
    <div class="sub" id="kpiPointsCount">0 / 12 production points synced</div>
  </div>
  <div class="kpi">
    <div class="label">BACnet Wire Packets</div>
    <div class="val" id="kpiPackets">0</div>
    <div class="sub" id="kpiIamCov">I-Am: 0 • COV: 0</div>
  </div>
  <div class="kpi">
    <div class="label">IMOPS API Delivery</div>
    <div class="val" id="kpiReadings">0</div>
    <div class="sub" id="kpiBatches">Batches: 0 • Failed: 0</div>
  </div>
</section>

<section class="main-grid">
  <div class="card">
    <div class="card-header">
      <span id="visualTitle">Smart Nation Digital Twin — Live BACnet Telemetry</span>
      <span class="mono" style="color:var(--muted)" id="ctrlBadge">Device 864077</span>
    </div>
    <div class="twin-wrap">
      <img id="facilityImg" src="/assets/dashboard.jpeg" alt="Digital Twin Facility" />
      <div id="hotspotsLayer">
        <!-- Aligned directly with printed callout badges on dashboard.jpeg -->
        <div class="pill" style="left:32.6%; top:68.5%;" id="pill-bw-int">
          <div class="p-tag">AI-101/102 • AV-27126</div>
          <div class="p-val" id="val-bw-int">-- °C | -- %RH (SP --)</div>
        </div>

        <div class="pill" style="left:45.8%; top:44.2%;" id="pill-bw-th">
          <div class="p-tag">AI-201 / AI-202</div>
          <div class="p-val" id="val-bw-th">-- °C | -- %RH</div>
        </div>

        <div class="pill ok" style="left:47.4%; top:70.5%;" id="pill-wd3">
          <div class="p-tag">BI-1 • IN 1</div>
          <div class="p-val" id="val-wd3">Waiting...</div>
        </div>

        <div class="pill ok" style="left:60.5%; top:16.5%;" id="pill-fire">
          <div class="p-tag">BI-3 • IN 3 (Fire)</div>
          <div class="p-val" id="val-fire">Waiting...</div>
        </div>

        <div class="pill ok" style="left:62.0%; top:40.2%;" id="pill-motion">
          <div class="p-tag">BI-2 • IN 2</div>
          <div class="p-val" id="val-motion">Waiting...</div>
        </div>

        <div class="pill ok" style="left:67.2%; top:50.8%;" id="pill-bg1000">
          <div class="p-tag">P864 #864077</div>
          <div class="p-val" id="val-bg1000">Scanning...</div>
        </div>

        <!-- Additional 4 Dry-Contact Production Points from Optergy BACnet Points.xlsx -->
        <div class="pill ok" style="left:19.5%; top:53.5%;" id="pill-intrusion">
          <div class="p-tag">BI-4 • Perimeter (MAXIBUS)</div>
          <div class="p-val" id="val-intrusion">Waiting...</div>
        </div>

        <div class="pill ok" style="left:38.5%; top:50.5%;" id="pill-ups">
          <div class="p-tag">BI-5 • UPS Monitoring</div>
          <div class="p-val" id="val-ups">Waiting...</div>
        </div>

        <div class="pill ok" style="left:59.5%; top:75.0%;" id="pill-access">
          <div class="p-tag">BI-6 • Access Control</div>
          <div class="p-val" id="val-access">Waiting...</div>
        </div>

        <div class="pill ok" style="left:52.8%; top:55.5%;" id="pill-rack">
          <div class="p-tag">BI-7 • Rack Door</div>
          <div class="p-val" id="val-rack">Waiting...</div>
        </div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-header">
      <span>Production Points (Optergy BACnet Points.xlsx)</span>
      <span class="mono" style="color:var(--muted)">Object / Value</span>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Object</th>
            <th>Sensor & Property</th>
            <th>Value</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody id="pointsTableBody">
          <tr><td colspan="4" style="color:var(--muted);text-align:center;padding:24px">Waiting for Optergy P864 (864077) discovery...</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</section>

<section class="card">
  <div class="card-header">
    <span>Recent BACnet/IP Wire Packets (UDP 0xBAC0)</span>
    <span class="mono" style="color:var(--muted)">BVLC + NPDU + APDU</span>
  </div>
  <div class="table-wrap" style="max-height:200px">
    <table>
      <thead>
        <tr>
          <th>UTC Time</th>
          <th>Source Endpoint</th>
          <th>BVLC</th>
          <th>PDU Type</th>
          <th>BACnet Service</th>
          <th>Bytes</th>
        </tr>
      </thead>
      <tbody id="packetsTableBody">
        <tr><td colspan="6" style="color:var(--muted);text-align:center;padding:16px">Listening for BACnet packets...</td></tr>
      </tbody>
    </table>
  </div>
</section>

<script>
let showingTopology = false;
let lastSubnetSig = '';

function toggleVisual() {
  showingTopology = !showingTopology;
  const img = document.getElementById('facilityImg');
  const hs = document.getElementById('hotspotsLayer');
  const btn = document.getElementById('toggleBtn');
  const title = document.getElementById('visualTitle');
  if (showingTopology) {
    img.src = '/assets/bacnet-devices.jpeg';
    hs.style.display = 'none';
    btn.textContent = 'Show Digital Twin';
    title.textContent = 'Optergy Wiring & Integration Reference';
  } else {
    img.src = '/assets/dashboard.jpeg';
    hs.style.display = 'block';
    btn.textContent = 'Show Wiring Diagram';
    title.textContent = 'Smart Nation Digital Twin — Live BACnet Telemetry';
  }
}

async function onSelectSubnet(subnetVal) {
  try {
    const res = await fetch('/subnet', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ subnet: subnetVal || '' })
    });
    if (!res.ok) {
      const errData = await res.json().catch(() => ({}));
      alert('Failed to switch subnet: ' + (errData.error || res.statusText));
    }
    await refreshStatus();
  } catch (e) { console.error(e); }
}

async function triggerDiscover() {
  try {
    await fetch('/discover', { method: 'POST' });
    await refreshStatus();
  } catch (e) { console.error(e); }
}

function setBinaryPill(pillId, valId, pt, isCritical) {
  if (!pt) return;
  const pill = document.getElementById(pillId);
  const valEl = document.getElementById(valId);
  valEl.textContent = pt.last_value_text || String(pt.last_value ?? '--');
  pill.classList.remove('ok', 'alarm', 'active-event');
  if (pt.is_alarm || (isCritical && pt.last_value === 1)) {
    pill.classList.add('alarm');
  } else if (pt.is_triggered || pt.last_value === 1) {
    pill.classList.add('active-event');
  } else {
    pill.classList.add('ok');
  }
}

async function refreshStatus() {
  try {
    const res = await fetch('/status');
    const s = await res.json();

    document.getElementById('siteTitle').textContent = s.site_name || 'Smart Nation Exhibition System';
    document.getElementById('listenInfo').textContent =
      'Bound: ' + s.bacnet_listen_addr + ' • Targets: ' + (s.broadcast_targets || []).join(', ');

    document.getElementById('kpiSubnets').textContent =
      (s.detected_subnets || []).map(n => n.interface_name + ' (' + (n.network_cidr || n.cidr) + ')').join(', ') || 'Direct target override';

    // Sync subnet selector dropdown without interrupting active user focus
    const selEl = document.getElementById('subnetSelect');
    const avail = s.available_subnets || [];
    const sig = avail.map(n => n.interface_name + '|' + (n.network_cidr || n.cidr) + '|' + n.local_ip).join(';');
    if (sig !== lastSubnetSig && document.activeElement !== selEl) {
      lastSubnetSig = sig;
      let optsHtml = '<option value="">Subnet: All Detected (Auto)</option>';
      for (const n of avail) {
        const cidr = n.network_cidr || n.cidr;
        optsHtml += '<option value="' + escapeHtml(cidr) + '">' +
          escapeHtml(n.interface_name + ' — ' + cidr + ' (' + n.local_ip + ')') + '</option>';
      }
      if (s.selected_subnet && !avail.some(n => (n.network_cidr || n.cidr) === s.selected_subnet)) {
        optsHtml += '<option value="' + escapeHtml(s.selected_subnet) + '">Selected: ' + escapeHtml(s.selected_subnet) + '</option>';
      }
      selEl.innerHTML = optsHtml;
    }
    if (document.activeElement !== selEl) {
      const activeMatch = avail.find(n =>
        s.selected_subnet && (
          (n.network_cidr || '').toLowerCase() === s.selected_subnet.toLowerCase() ||
          (n.cidr || '').toLowerCase() === s.selected_subnet.toLowerCase() ||
          (n.interface_name || '').toLowerCase() === s.selected_subnet.toLowerCase() ||
          (n.local_ip || '') === s.selected_subnet
        )
      );
      selEl.value = activeMatch ? (activeMatch.network_cidr || activeMatch.cidr) : (s.selected_subnet || '');
    }

    document.getElementById('kpiPackets').textContent = s.packets_seen;
    document.getElementById('kpiIamCov').textContent = 'I-Am: ' + s.iam_received + ' • COV: ' + s.cov_received;

    const im = s.imops || {};
    if (im.enabled === false) {
      document.getElementById('kpiReadings').textContent = 'DISABLED';
      document.getElementById('kpiBatches').textContent = 'imops.enabled: false in config';
    } else {
      document.getElementById('kpiReadings').textContent = im.readings_sent || 0;
      const drySuffix = (im.dry_run_batches || 0) > 0 ? (' • Dry-run: ' + im.dry_run_batches) : '';
      document.getElementById('kpiBatches').textContent =
        'Batches: ' + (im.batches_sent || 0) + drySuffix + ' • Failed: ' + (im.failed_requests || 0);
    }

    const byShort = {};
    const flat = [];
    let p864Dev = null;
    for (const dev of (s.devices || [])) {
      if (!p864Dev || dev.device_id === 864077 || (dev.model_name || '').includes('P864')) {
        p864Dev = dev;
      }
      for (const pt of (dev.points || [])) {
        flat.push({ dev, pt });
        if (pt.short_id) {
          byShort[pt.short_id] = pt;
        }
      }
    }

    // Controller & BG-1000-M status
    const devKpi = document.getElementById('kpiDevices');
    const bgPill = document.getElementById('pill-bg1000');
    const bgVal = document.getElementById('val-bg1000');
    if (!p864Dev) {
      devKpi.textContent = 'Searching...';
      devKpi.classList.remove('alarm-text');
      bgVal.textContent = 'Scanning UDP...';
    } else if (!p864Dev.online) {
      devKpi.textContent = 'COMM FAULT';
      devKpi.classList.add('alarm-text');
      bgPill.classList.remove('ok');
      bgPill.classList.add('alarm');
      bgVal.textContent = 'COMM FAILURE';
      document.getElementById('ctrlBadge').textContent = 'P864 #' + p864Dev.device_id + ' (OFFLINE)';
    } else {
      devKpi.textContent = 'ONLINE (' + s.device_count + ')';
      devKpi.classList.remove('alarm-text');
      bgPill.classList.remove('alarm');
      bgPill.classList.add('ok');
      bgVal.textContent = 'Online • ' + p864Dev.address;
      document.getElementById('ctrlBadge').textContent = p864Dev.object_name + ' (#' + p864Dev.device_id + ')';
    }

    // BW-INT-M (AI-101, AI-102, AV-27126)
    const ai101 = byShort['AI-101'];
    const ai102 = byShort['AI-102'];
    const avsp  = byShort['AV-27126'];
    if (ai101 || ai102 || avsp) {
      document.getElementById('val-bw-int').textContent =
        (ai101?.last_value_text || '--') + ' | ' +
        (ai102?.last_value_text || '--') + ' (SP ' + (avsp?.last_value_text || '--') + ')';
    }

    // BW-TH-M (AI-201, AI-202)
    const ai201 = byShort['AI-201'];
    const ai202 = byShort['AI-202'];
    if (ai201 || ai202) {
      document.getElementById('val-bw-th').textContent =
        (ai201?.last_value_text || '--') + ' | ' + (ai202?.last_value_text || '--');
    }

    // Binary Inputs BI-1 .. BI-7
    setBinaryPill('pill-wd3',       'val-wd3',       byShort['BI-1'], true);
    setBinaryPill('pill-motion',    'val-motion',    byShort['BI-2'], false);
    setBinaryPill('pill-fire',      'val-fire',      byShort['BI-3'], true);
    setBinaryPill('pill-intrusion', 'val-intrusion', byShort['BI-4'], true);
    setBinaryPill('pill-ups',       'val-ups',       byShort['BI-5'], true);
    setBinaryPill('pill-access',    'val-access',    byShort['BI-6'], true);
    setBinaryPill('pill-rack',      'val-rack',      byShort['BI-7'], true);

    // Count alarms
    let alarmCnt = 0;
    for (const { pt } of flat) {
      if (pt.is_alarm) alarmCnt++;
    }
    const alarmEl = document.getElementById('kpiAlarms');
    alarmEl.textContent = alarmCnt;
    alarmEl.classList.toggle('alarm-text', alarmCnt > 0);
    document.getElementById('kpiPointsCount').textContent = flat.length + ' monitored points active';

    // Points Table
    const pBody = document.getElementById('pointsTableBody');
    if (flat.length > 0) {
      pBody.innerHTML = flat.map(({ dev, pt }) => {
        let badge = '<span class="badge good">NORMAL</span>';
        let rowCls = '';
        if (pt.is_alarm) {
          badge = '<span class="badge alarm">ALARM</span>';
          rowCls = ' class="row-alarm"';
        } else if (pt.is_triggered) {
          badge = '<span class="badge event">ACTIVE</span>';
        }
        const labelTitle = pt.dashboard_label || pt.equipment_profile || dev.object_name;
        const subProp = pt.property_label || pt.name;
        const inputInfo = pt.connection_bus ? (' • ' + pt.connection_bus) : '';
        return '<tr' + rowCls + '>' +
          '<td class="mono"><strong>' + escapeHtml(pt.short_id || (pt.object_id.type + ':' + pt.object_id.instance)) + '</strong></td>' +
          '<td><strong>' + escapeHtml(labelTitle) + '</strong><br><span style="color:var(--muted);font-size:0.7rem">' + escapeHtml(subProp + inputInfo) + '</span></td>' +
          '<td><strong>' + escapeHtml(pt.last_value_text || '—') + '</strong></td>' +
          '<td>' + badge + '</td>' +
          '</tr>';
      }).join('');
    }

    // Packets Table
    const pkts = (s.recent_packets || []).slice().reverse().slice(0, 25);
    const pktBody = document.getElementById('packetsTableBody');
    if (pkts.length > 0) {
      pktBody.innerHTML = pkts.map(p => {
        const src = (p.source?.ip || '') + ':' + (p.source?.port || '');
        return '<tr>' +
          '<td class="mono">' + escapeHtml((p.timestamp || '').replace('T', ' ').slice(0, 19)) + '</td>' +
          '<td class="mono">' + escapeHtml(src) + '</td>' +
          '<td>' + escapeHtml(p.bvlc_name || '') + '</td>' +
          '<td>' + escapeHtml(p.pdu_type || '') + '</td>' +
          '<td><strong>' + escapeHtml(p.service_name || '—') + '</strong></td>' +
          '<td class="mono">' + (p.length || 0) + '</td>' +
          '</tr>';
      }).join('');
    }
  } catch (err) {
    console.error('status fetch failed', err);
  }
}

function escapeHtml(str) {
  return String(str ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

refreshStatus();
setInterval(refreshStatus, 1500);
</script>
</body>
</html>
`
