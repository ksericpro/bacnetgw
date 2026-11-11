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
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeFile(w, r, filename)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>BACnet/IP Subnet Listener & IMOPS Gateway</title>
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
    padding: 18px 24px;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 16px;
    border-bottom: 1px solid var(--border);
    margin-bottom: 18px;
  }
  .title-group h1 { font-size: 1.25rem; font-weight: 700; letter-spacing: -0.01em; }
  .title-group p { font-size: 0.85rem; color: var(--muted); }
  .actions { display: flex; align-items: center; gap: 10px; }
  button {
    background: #0284c7;
    color: #fff;
    border: none;
    border-radius: 6px;
    padding: 8px 14px;
    font-size: 0.85rem;
    font-weight: 600;
    cursor: pointer;
    transition: background 0.15s;
  }
  button:hover { background: #0369a1; }
  button.secondary { background: var(--panel-alt); border: 1px solid var(--border); color: var(--text); }
  button.secondary:hover { background: #334155; }

  .kpi-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 12px;
    margin-bottom: 18px;
  }
  .kpi {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 15px;
  }
  .kpi .label { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--muted); }
  .kpi .val { font-size: 1.45rem; font-weight: 700; margin-top: 4px; color: var(--accent); }
  .kpi .sub { font-size: 0.75rem; color: var(--muted); margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .main-grid {
    display: grid;
    grid-template-columns: 1.35fr 1fr;
    gap: 16px;
    margin-bottom: 18px;
  }
  @media (max-width: 1100px) { .main-grid { grid-template-columns: 1fr; } }

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
    padding: 11px 15px;
    background: rgba(30, 41, 59, 0.55);
    border-bottom: 1px solid var(--border);
    font-weight: 600;
    font-size: 0.9rem;
  }
  .twin-wrap {
    position: relative;
    width: 100%;
    background: #0f172a;
    min-height: 320px;
  }
  .twin-wrap img {
    width: 100%;
    display: block;
    height: auto;
    opacity: 0.94;
  }
  .hotspot {
    position: absolute;
    transform: translate(-50%, -50%);
    background: rgba(15, 23, 42, 0.9);
    border: 1.5px solid var(--accent);
    border-radius: 8px;
    padding: 5px 9px;
    font-size: 0.74rem;
    box-shadow: 0 4px 14px rgba(0,0,0,0.55);
    backdrop-filter: blur(4px);
    min-width: 118px;
    pointer-events: auto;
  }
  .hotspot.alarm { border-color: var(--alarm); background: rgba(69, 10, 10, 0.92); }
  .hotspot .hs-title { font-weight: 700; color: #e2e8f0; font-size: 0.7rem; margin-bottom: 2px; }
  .hotspot .hs-val { font-weight: 700; color: var(--accent); font-size: 0.84rem; }
  .hotspot.alarm .hs-val { color: #fca5a5; }

  .table-wrap { max-height: 440px; overflow-y: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.8rem; }
  th, td { padding: 8px 12px; text-align: left; border-bottom: 1px solid rgba(51, 65, 85, 0.5); }
  th { position: sticky; top: 0; background: var(--panel-alt); color: var(--muted); font-weight: 600; }
  .badge {
    display: inline-block;
    padding: 2px 7px;
    border-radius: 999px;
    font-size: 0.7rem;
    font-weight: 600;
  }
  .badge.good { background: rgba(34,197,94,0.16); color: #4ade80; }
  .badge.alarm { background: rgba(239,68,68,0.22); color: #f87171; }
  .badge.other { background: rgba(245,158,11,0.2); color: #fbbf24; }
  .mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; font-size: 0.76rem; }
</style>
</head>
<body>
<header>
  <div class="title-group">
    <h1 id="siteTitle">BACnet/IP Subnet Listener & IMOPS Gateway</h1>
    <p id="listenInfo">Listening on UDP 47808 • Auto-refreshing every 2s</p>
  </div>
  <div class="actions">
    <button onclick="triggerDiscover()">Broadcast Who-Is Now</button>
    <button class="secondary" onclick="toggleVisual()" id="toggleBtn">Show Wiring Diagram</button>
    <a href="/status" target="_blank"><button class="secondary">Raw /status JSON</button></a>
  </div>
</header>

<section class="kpi-grid">
  <div class="kpi">
    <div class="label">Discovered Controllers</div>
    <div class="val" id="kpiDevices">0</div>
    <div class="sub" id="kpiSubnets">Detecting subnets...</div>
  </div>
  <div class="kpi">
    <div class="label">BACnet Packets Captured</div>
    <div class="val" id="kpiPackets">0</div>
    <div class="sub" id="kpiIamCov">I-Am: 0 • COV: 0</div>
  </div>
  <div class="kpi">
    <div class="label">IMOPS Telemetry Sent</div>
    <div class="val" id="kpiReadings">0</div>
    <div class="sub" id="kpiBatches">Batches: 0 • Failed: 0</div>
  </div>
  <div class="kpi">
    <div class="label">IMOPS Delivery Mode</div>
    <div class="val" id="kpiImopsState" style="font-size:1.1rem">Initializing</div>
    <div class="sub" id="kpiLastSync">Waiting for first batch...</div>
  </div>
</section>

<section class="main-grid">
  <div class="card">
    <div class="card-header">
      <span id="visualTitle">Live Facility Telemetry Overlay</span>
      <span class="mono" style="color:var(--muted)" id="pointCounter">0 points active</span>
    </div>
    <div class="twin-wrap" id="twinContainer">
      <img id="facilityImg" src="/assets/dashboard.jpeg" alt="Facility Digital Twin" onerror="this.style.minHeight='320px';this.style.opacity='0.15'" />
      <div id="hotspotsLayer">
        <div class="hotspot" style="top:22%; left:47%;" id="hs-cooling">
          <div class="hs-title">Dwyer RHPLC / RHL (P864)</div>
          <div class="hs-val" id="val-cooling">-- °C / -- %RH</div>
        </div>
        <div class="hotspot" style="top:52%; left:52%;" id="hs-power">
          <div class="hs-title">Schneider PM1200 (AXON)</div>
          <div class="hs-val" id="val-power">-- kW • -- V</div>
        </div>
        <div class="hotspot" style="top:42%; left:68%;" id="hs-iaq">
          <div class="hs-title">T18XAIR IAQ (P442)</div>
          <div class="hs-val" id="val-iaq">CO₂: -- ppm</div>
        </div>
        <div class="hotspot" style="top:64%; left:44%;" id="hs-leak">
          <div class="hs-title">Dwyer WD3 Leak (P442)</div>
          <div class="hs-val" id="val-leak">Waiting...</div>
        </div>
        <div class="hotspot" style="top:67%; left:64%;" id="hs-occ">
          <div class="hs-title">MSA237D Motion (P442)</div>
          <div class="hs-val" id="val-occ">Waiting...</div>
        </div>
        <div class="hotspot" style="top:34%; left:33%;" id="hs-air">
          <div class="hs-title">Optergy Air (BG-1000-M)</div>
          <div class="hs-val" id="val-air">-- °C (Wireless)</div>
        </div>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="card-header">
      <span>Discovered BACnet Points</span>
      <span class="mono" style="color:var(--muted)">Present_Value</span>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Controller / Equipment</th>
            <th>Point</th>
            <th>Value</th>
            <th>State</th>
          </tr>
        </thead>
        <tbody id="pointsTableBody">
          <tr><td colspan="4" style="color:var(--muted);text-align:center;padding:24px">Waiting for BACnet Who-Is / I-Am discovery...</td></tr>
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
  <div class="table-wrap" style="max-height:220px">
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
        <tr><td colspan="6" style="color:var(--muted);text-align:center;padding:18px">Listening for BACnet packets...</td></tr>
      </tbody>
    </table>
  </div>
</section>

<script>
let showingTopology = false;

function toggleVisual() {
  showingTopology = !showingTopology;
  const img = document.getElementById('facilityImg');
  const hs = document.getElementById('hotspotsLayer');
  const btn = document.getElementById('toggleBtn');
  const title = document.getElementById('visualTitle');
  if (showingTopology) {
    img.src = '/assets/bacnet-devices.jpeg';
    hs.style.display = 'none';
    btn.textContent = 'Show Facility Overlay';
    title.textContent = 'Optergy Exhibition Wiring & Integration Topology';
  } else {
    img.src = '/assets/dashboard.jpeg';
    hs.style.display = 'block';
    btn.textContent = 'Show Wiring Diagram';
    title.textContent = 'Live Facility Telemetry Overlay';
  }
}

async function triggerDiscover() {
  try {
    await fetch('/discover', { method: 'POST' });
    await refreshStatus();
  } catch (e) { console.error(e); }
}

function findPoint(allPoints, matchFn) {
  return allPoints.find(matchFn);
}

async function refreshStatus() {
  try {
    const res = await fetch('/status');
    const s = await res.json();

    document.getElementById('siteTitle').textContent = s.site_name + ' — ' + s.gateway_id;
    document.getElementById('listenInfo').textContent =
      'Bound: ' + s.bacnet_listen_addr + ' • Broadcasts: ' + (s.broadcast_targets || []).join(', ');

    document.getElementById('kpiDevices').textContent = s.device_count;
    document.getElementById('kpiSubnets').textContent =
      (s.detected_subnets || []).map(n => n.interface_name + ' (' + n.cidr + ')').join(', ') || 'Custom broadcast override';

    document.getElementById('kpiPackets').textContent = s.packets_seen;
    document.getElementById('kpiIamCov').textContent = 'I-Am: ' + s.iam_received + ' • COV: ' + s.cov_received;

    const im = s.imops || {};
    document.getElementById('kpiReadings').textContent = im.readings_sent || 0;
    document.getElementById('kpiBatches').textContent =
      'Batches: ' + (im.batches_sent || 0) + ' • Dry-run: ' + (im.dry_run_batches || 0) + ' • Failed: ' + (im.failed_requests || 0);

    if ((im.dry_run_batches || 0) > 0 && (im.batches_sent || 0) === 0) {
      document.getElementById('kpiImopsState').textContent = 'Dry-Run Mode';
      document.getElementById('kpiLastSync').textContent = 'Set IMOPS_BASE_URL to enable live POST';
    } else if (im.last_error) {
      document.getElementById('kpiImopsState').textContent = 'Retry / Error';
      document.getElementById('kpiLastSync').textContent = im.last_error;
    } else {
      document.getElementById('kpiImopsState').textContent = 'Live Connected';
      document.getElementById('kpiLastSync').textContent = im.last_success_time ? ('Last OK: ' + im.last_success_time) : 'Ready';
    }

    const flat = [];
    for (const dev of (s.devices || [])) {
      for (const pt of (dev.points || [])) {
        flat.push({ dev, pt });
      }
    }
    document.getElementById('pointCounter').textContent = flat.length + ' points active';

    // Update Digital Twin Hotspots
    const kw = findPoint(flat, x => (x.pt.unit_symbol === 'kW'));
    const volt = findPoint(flat, x => (x.pt.unit_symbol === 'V'));
    if (kw || volt) {
      document.getElementById('val-power').textContent =
        (kw ? kw.pt.last_value_text : '-- kW') + ' • ' + (volt ? volt.pt.last_value_text : '-- V');
    }

    const rTemp = findPoint(flat, x => (x.pt.equipment_profile || '').includes('RHPLC') && x.pt.unit_symbol === '°C');
    const rRH = findPoint(flat, x => (x.pt.equipment_profile || '').includes('RHPLC') && x.pt.unit_symbol === '%RH');
    if (rTemp || rRH) {
      document.getElementById('val-cooling').textContent =
        (rTemp ? rTemp.pt.last_value_text : '--') + ' / ' + (rRH ? rRH.pt.last_value_text : '--');
    }

    const co2 = findPoint(flat, x => x.pt.unit_symbol === 'ppm');
    const pm25 = findPoint(flat, x => x.pt.unit_symbol === 'µg/m³');
    if (co2 || pm25) {
      document.getElementById('val-iaq').textContent =
        (co2 ? ('CO₂ ' + co2.pt.last_value_text) : '') + (pm25 ? (' • PM2.5 ' + pm25.pt.last_value_text) : '');
    }

    const leak = findPoint(flat, x => (x.pt.equipment_profile || '').includes('WD3') || x.pt.name.toUpperCase().includes('LEAK'));
    if (leak) {
      const hsLeak = document.getElementById('hs-leak');
      document.getElementById('val-leak').textContent = leak.pt.last_value_text || String(leak.pt.last_value);
      const isAlarm = leak.pt.last_status_flags?.in_alarm || leak.pt.last_value === 1;
      hsLeak.classList.toggle('alarm', !!isAlarm);
    }

    const occ = findPoint(flat, x => (x.pt.equipment_profile || '').includes('MSA237') || x.pt.name.toUpperCase().includes('MOTION'));
    if (occ) {
      document.getElementById('val-occ').textContent = occ.pt.last_value_text || String(occ.pt.last_value);
    }

    const air = findPoint(flat, x => (x.pt.equipment_profile || '').includes('BW-INT') && x.pt.unit_symbol === '°C');
    if (air) {
      document.getElementById('val-air').textContent = air.pt.last_value_text + ' (Air Touch)';
    }

    // Points table
    const pBody = document.getElementById('pointsTableBody');
    if (flat.length > 0) {
      pBody.innerHTML = flat.map(({ dev, pt }) => {
        const sf = pt.last_status_flags || {};
        let badge = '<span class="badge good">GOOD</span>';
        if (sf.in_alarm) badge = '<span class="badge alarm">ALARM</span>';
        else if (sf.fault || sf.out_of_service) badge = '<span class="badge other">FAULT/OOS</span>';
        const equip = pt.equipment_profile ? (' • ' + pt.equipment_profile) : '';
        return '<tr>' +
          '<td><strong>' + escapeHtml(dev.object_name) + '</strong><br><span style="color:var(--muted);font-size:0.72rem">' + escapeHtml(dev.address + equip) + '</span></td>' +
          '<td class="mono">' + escapeHtml(pt.name) + '<br><span style="color:var(--muted);font-size:0.7rem">' + escapeHtml(pt.object_id.type + ':' + pt.object_id.instance) + '</span></td>' +
          '<td><strong>' + escapeHtml(pt.last_value_text || '—') + '</strong></td>' +
          '<td>' + badge + '</td>' +
          '</tr>';
      }).join('');
    }

    // Packets table
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
setInterval(refreshStatus, 2000);
</script>
</body>
</html>
`

