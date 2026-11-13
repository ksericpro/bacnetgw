<#
.SYNOPSIS
  Toggle the 7 simulated P864 dry-contact points (BI-1 .. BI-7) ON (flashing) or OFF (normal)
  against mock-exhibition.exe (http://127.0.0.1:8085) and watch http://127.0.0.1:8090 live.

.EXAMPLE
  .\sim-contacts.ps1                 # Interactive menu (press 1-7 to toggle, A=All ON, O=All OFF, D=Demo)
  .\sim-contacts.ps1 BI-1 on         # Turn WD3 Water Leak ON (flashing red)
  .\sim-contacts.ps1 BI-1 off        # Turn WD3 Water Leak OFF (solid green)
  .\sim-contacts.ps1 all on          # Turn all 7 dry contacts ON
  .\sim-contacts.ps1 all off         # Reset all 7 dry contacts OFF
  .\sim-contacts.ps1 demo            # Cycle each dry contact ON then OFF one by one
#>

param(
    [Parameter(Position = 0)]
    [string]$Target = "",

    [Parameter(Position = 1)]
    [string]$State = "",

    [string]$SimUrl = "http://127.0.0.1:8085"
)

$ErrorActionPreference = "Stop"

$Contacts = @(
    [PSCustomObject]@{ Key = "1"; Point = "BI-1"; Name = "WD3 Water Leak";                OnLabel = "Water Detected (Flashing Red)";    OffLabel = "Normal";   Val = 0 }
    [PSCustomObject]@{ Key = "2"; Point = "BI-2"; Name = "MSA237D 99 Motion Sensor";      OnLabel = "Motion Detected (Flashing Amber)"; OffLabel = "Clear";    Val = 0 }
    [PSCustomObject]@{ Key = "3"; Point = "BI-3"; Name = "Fire Detection Sensor";         OnLabel = "Fire Alarm (Flashing Red)";        OffLabel = "Normal";   Val = 0 }
    [PSCustomObject]@{ Key = "4"; Point = "BI-4"; Name = "Perimeter Intrusion (MAXIBUS)"; OnLabel = "Intrusion (Flashing Red)";         OffLabel = "Secure";   Val = 0 }
    [PSCustomObject]@{ Key = "5"; Point = "BI-5"; Name = "UPS Monitoring";                OnLabel = "UPS Failure (Flashing Red)";       OffLabel = "Mains OK"; Val = 0 }
    [PSCustomObject]@{ Key = "6"; Point = "BI-6"; Name = "Access Control System";         OnLabel = "Door Forced (Flashing Red)";       OffLabel = "Secure";   Val = 0 }
    [PSCustomObject]@{ Key = "7"; Point = "BI-7"; Name = "Server Rack Door Contact";      OnLabel = "Rack Door Open (Flashing Red)";    OffLabel = "Closed";   Val = 0 }
)

function Set-SimPoint {
    param([string]$Point, [int]$Val)
    $uri = "$SimUrl/sim/set?point=$([uri]::EscapeDataString($Point))&val=$Val"
    try {
        Invoke-RestMethod -Method Post -Uri $uri -TimeoutSec 3 | Out-Null
        $item = $Contacts | Where-Object { $_.Point -ieq $Point }
        if ($item) { $item.Val = $Val }
        $color = if ($Val -eq 1) { "Red" } else { "Green" }
        $stateTxt = if ($Val -eq 1) { "ON  (1 -> Flashing)" } else { "OFF (0 -> Normal)" }
        Write-Host ("  -> Set {0,-5} ({1}) = {2}" -f $Point.ToUpper(), $item.Name, $stateTxt) -ForegroundColor $color
    } catch {
        Write-Host "[ERROR] Could not reach mock-exhibition at $SimUrl. Make sure mock-exhibition.exe is running." -ForegroundColor Yellow
        throw
    }
}

function Parse-OnOff {
    param([string]$Raw)
    switch ($Raw.Trim().ToLowerInvariant()) {
        { $_ -in @("1", "on", "true", "active", "alarm") }   { return 1 }
        { $_ -in @("0", "off", "false", "normal", "clear") } { return 0 }
        default { throw "Invalid state '$Raw'. Use 'on' (1) or 'off' (0)." }
    }
}

function Invoke-DemoSweep {
    Write-Host ""
    Write-Host "Running UI Flashing Demo Sweep (watch http://127.0.0.1:8090)..." -ForegroundColor Cyan
    foreach ($c in $Contacts) {
        Set-SimPoint -Point $c.Point -Val 0
    }
    Start-Sleep -Seconds 1
    foreach ($c in $Contacts) {
        Set-SimPoint -Point $c.Point -Val 1
        Start-Sleep -Seconds 2
        Set-SimPoint -Point $c.Point -Val 0
        Start-Sleep -Milliseconds 500
    }
    Write-Host "Demo sweep complete (all 7 contacts reset to OFF)." -ForegroundColor Green
}

# --- CLI One-Shot Mode ---
if ($Target -ne "") {
    $t = $Target.Trim().ToLowerInvariant()
    if ($t -eq "demo") {
        Invoke-DemoSweep
        exit 0
    }
    if ($t -eq "all") {
        $v = Parse-OnOff $State
        foreach ($c in $Contacts) {
            Set-SimPoint -Point $c.Point -Val $v
        }
        exit 0
    }
    $normPt = $Target.Trim().ToUpperInvariant()
    if ($normPt -match '^[1-7]$') {
        $normPt = "BI-$normPt"
    }
    $v = Parse-OnOff $State
    Set-SimPoint -Point $normPt -Val $v
    exit 0
}

# --- Interactive Menu Mode ---
while ($true) {
    Write-Host ""
    Write-Host "======================================================================" -ForegroundColor Cyan
    Write-Host "  Dry-Contact UI Simulator (Dashboard: http://127.0.0.1:8090)" -ForegroundColor Cyan
    Write-Host "======================================================================" -ForegroundColor Cyan
    foreach ($c in $Contacts) {
        $statusStr = if ($c.Val -eq 1) { "[ON  - FLASHING] $($c.OnLabel)" } else { "[OFF - NORMAL]   $($c.OffLabel)" }
        $color     = if ($c.Val -eq 1) { "Red" } else { "Green" }
        Write-Host ("  [{0}] {1,-5}  {2,-30} " -f $c.Key, $c.Point, $c.Name) -NoNewline
        Write-Host $statusStr -ForegroundColor $color
    }
    Write-Host "----------------------------------------------------------------------"
    Write-Host "  [A] Set ALL 7 Contacts ON (Flashing)    [O] Set ALL 7 Contacts OFF (Normal)"
    Write-Host "  [D] Auto-Demo Sweep (1..7 one by one)   [Q] Quit"
    Write-Host "----------------------------------------------------------------------"
    $choice = (Read-Host "Press 1-7 to toggle ON/OFF (or A / O / D / Q)").Trim().ToUpperInvariant()

    if ($choice -eq "Q") {
        break
    }
    elseif ($choice -eq "A") {
        foreach ($c in $Contacts) { try { Set-SimPoint -Point $c.Point -Val 1 } catch { break } }
    }
    elseif ($choice -eq "O") {
        foreach ($c in $Contacts) { try { Set-SimPoint -Point $c.Point -Val 0 } catch { break } }
    }
    elseif ($choice -eq "D") {
        try { Invoke-DemoSweep } catch {}
    }
    elseif ($choice -match '^[1-7]$') {
        $sel = $Contacts | Where-Object { $_.Key -eq $choice }
        $nextVal = if ($sel.Val -eq 1) { 0 } else { 1 }
        try { Set-SimPoint -Point $sel.Point -Val $nextVal } catch {}
    }
    else {
        Write-Host "  Invalid key '$choice' -- enter 1..7, A, O, D, or Q." -ForegroundColor Yellow
    }
}
