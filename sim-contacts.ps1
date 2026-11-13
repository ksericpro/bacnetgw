<#
.SYNOPSIS
  Control all 12 simulated P864 points (7 dry contacts BI-1..BI-7 + 5 continuous analog sensors)
  against mock-exhibition.exe (http://127.0.0.1:8085) and watch http://127.0.0.1:8090 & CDE live.

.EXAMPLE
  .\sim-contacts.ps1                  # Interactive menu (1-7 dry contacts, 8-12 analog, R=Randomize, C=Continuous stream)
  .\sim-contacts.ps1 AI-101 26.8      # Set BW-INT-M Temperature to 26.8 °C
  .\sim-contacts.ps1 AI-102 61.5      # Set BW-INT-M Humidity to 61.5 %RH
  .\sim-contacts.ps1 AV-27126 21.0    # Set BW-INT-M Setpoint to 21.0 °C
  .\sim-contacts.ps1 stream           # Continuously fluctuate all 5 analog sensors every 2s (press any key to stop)
  .\sim-contacts.ps1 BI-1 on          # Turn WD3 Water Leak ON (flashing red)
  .\sim-contacts.ps1 all off          # Reset all 7 dry contacts OFF
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

$Analogs = @(
    [PSCustomObject]@{ Key = "8";  Point = "AI-101";   Name = "BW-INT-M Temperature";   Unit = "°C";  Min = 19.0; Max = 29.0; Val = 22.6 }
    [PSCustomObject]@{ Key = "9";  Point = "AI-102";   Name = "BW-INT-M Humidity";      Unit = "%RH"; Min = 40.0; Max = 75.0; Val = 49.5 }
    [PSCustomObject]@{ Key = "10"; Point = "AV-27126"; Name = "BW-INT-M Temp Setpoint"; Unit = "°C";  Min = 18.0; Max = 26.0; Val = 22.0 }
    [PSCustomObject]@{ Key = "11"; Point = "AI-201";   Name = "BW-TH-M Temperature";    Unit = "°C";  Min = 20.0; Max = 30.0; Val = 24.2 }
    [PSCustomObject]@{ Key = "12"; Point = "AI-202";   Name = "BW-TH-M Humidity";       Unit = "%RH"; Min = 42.0; Max = 78.0; Val = 52.8 }
)

function Set-SimPoint {
    param([string]$Point, [double]$Val)
    $valStr = $Val.ToString("0.##", [System.Globalization.CultureInfo]::InvariantCulture)
    $uri = "$SimUrl/sim/set?point=$([uri]::EscapeDataString($Point))&val=$valStr"
    try {
        Invoke-RestMethod -Method Post -Uri $uri -TimeoutSec 3 | Out-Null
        $binItem = $Contacts | Where-Object { $_.Point -ieq $Point }
        if ($binItem) {
            $binItem.Val = [int]$Val
            $color = if ($binItem.Val -eq 1) { "Red" } else { "Green" }
            $stateTxt = if ($binItem.Val -eq 1) { "ON  (1 -> Flashing)" } else { "OFF (0 -> Normal)" }
            Write-Host ("  -> Set {0,-8} ({1}) = {2}" -f $Point.ToUpper(), $binItem.Name, $stateTxt) -ForegroundColor $color
            return
        }
        $anaItem = $Analogs | Where-Object { $_.Point -ieq $Point }
        if ($anaItem) {
            $anaItem.Val = [math]::Round($Val, 2)
            Write-Host ("  -> Set {0,-8} ({1}) = {2} {3}" -f $Point.ToUpper(), $anaItem.Name, $valStr, $anaItem.Unit) -ForegroundColor Cyan
            return
        }
        Write-Host ("  -> Set {0,-8} = {1}" -f $Point.ToUpper(), $valStr) -ForegroundColor Cyan
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

function Invoke-RandomizeAnalogs {
    $rng = [System.Random]::new()
    foreach ($a in $Analogs) {
        if ($a.Point -eq "AV-27126") {
            # Step setpoints by 0.5 °C increments
            $steps = [int](($a.Max - $a.Min) * 2)
            $newVal = $a.Min + ($rng.Next(0, $steps + 1) * 0.5)
        } else {
            $newVal = [math]::Round($a.Min + ($rng.NextDouble() * ($a.Max - $a.Min)), 2)
        }
        Set-SimPoint -Point $a.Point -Val $newVal
    }
}

function Invoke-ContinuousStream {
    Write-Host ""
    Write-Host "Streaming live continuous analog values every 2s (Press ANY KEY to return to menu)..." -ForegroundColor Cyan
    $rng = [System.Random]::new()
    while ($true) {
        if ([Console]::KeyAvailable) {
            [void][Console]::ReadKey($true)
            Write-Host "Stopped continuous stream." -ForegroundColor Yellow
            break
        }
        foreach ($a in $Analogs) {
            if ($a.Point -eq "AV-27126") { continue } # Keep setpoint steady unless manually changed
            $delta = ($rng.NextDouble() - 0.48) * 0.6
            $next = [math]::Round([math]::Max($a.Min, [math]::Min($a.Max, $a.Val + $delta)), 2)
            Set-SimPoint -Point $a.Point -Val $next
        }
        Write-Host "  (Streaming... press any key to stop)" -ForegroundColor DarkGray
        for ($i = 0; $i -lt 10; $i++) {
            if ([Console]::KeyAvailable) { break }
            Start-Sleep -Milliseconds 200
        }
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
    if ($t -in @("stream", "continuous", "drift")) {
        Invoke-ContinuousStream
        exit 0
    }
    if ($t -in @("random", "rand")) {
        Invoke-RandomizeAnalogs
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
    if ($normPt.StartsWith("BI-") -or $normPt.StartsWith("BV-") -or $normPt.StartsWith("BO-")) {
        $v = Parse-OnOff $State
        Set-SimPoint -Point $normPt -Val $v
    } else {
        $fv = 0.0
        if (-not [double]::TryParse($State, [System.Globalization.NumberStyles]::Float, [System.Globalization.CultureInfo]::InvariantCulture, [ref]$fv)) {
            throw "Invalid numeric value '$State' for analog point '$normPt'. Example: .\sim-contacts.ps1 AI-101 25.4"
        }
        Set-SimPoint -Point $normPt -Val $fv
    }
    exit 0
}

# --- Interactive Menu Mode ---
while ($true) {
    Write-Host ""
    Write-Host "======================================================================" -ForegroundColor Cyan
    Write-Host "  12-Sensor Simulator (Local UI: :8090 | CDE Kong: :8088)" -ForegroundColor Cyan
    Write-Host "======================================================================" -ForegroundColor Cyan
    Write-Host "  --- Discrete Dry Contacts (Press 1-7 to Toggle) ---" -ForegroundColor Gray
    foreach ($c in $Contacts) {
        $statusStr = if ($c.Val -eq 1) { "[ON  - FLASHING] $($c.OnLabel)" } else { "[OFF - NORMAL]   $($c.OffLabel)" }
        $color     = if ($c.Val -eq 1) { "Red" } else { "Green" }
        Write-Host ("  [{0,2}] {1,-8} {2,-30} " -f $c.Key, $c.Point, $c.Name) -NoNewline
        Write-Host $statusStr -ForegroundColor $color
    }
    Write-Host ""
    Write-Host "  --- Continuous Analog Sensors (Enter 8-12 to Set Value) ---" -ForegroundColor Gray
    foreach ($a in $Analogs) {
        Write-Host ("  [{0,2}] {1,-8} {2,-30} " -f $a.Key, $a.Point, $a.Name) -NoNewline
        Write-Host ("{0:0.##} {1}" -f $a.Val, $a.Unit) -ForegroundColor Cyan
    }
    Write-Host "----------------------------------------------------------------------"
    Write-Host "  [C] Continuous Live Drift (auto-fluctuate temp & humidity every 2s)"
    Write-Host "  [R] Randomize All 5 Analog Values Once"
    Write-Host "  [A] Set ALL 7 Contacts ON    [O] Set ALL 7 Contacts OFF"
    Write-Host "  [D] Alarm Demo Sweep (1..7)  [Q] Quit"
    Write-Host "----------------------------------------------------------------------"
    $choice = (Read-Host "Enter 1-7 (toggle), 8-12 (set value), C (continuous), R, A, O, D, or Q").Trim().ToUpperInvariant()

    if ($choice -eq "Q") {
        break
    }
    elseif ($choice -eq "C") {
        try { Invoke-ContinuousStream } catch {}
    }
    elseif ($choice -eq "R") {
        try { Invoke-RandomizeAnalogs } catch {}
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
    elseif ($choice -match '^(8|9|10|11|12)$') {
        $selA = $Analogs | Where-Object { $_.Key -eq $choice }
        $rawVal = (Read-Host "  Enter new value for $($selA.Point) ($($selA.Name) in $($selA.Unit), current $($selA.Val))").Trim()
        $fv = 0.0
        if ([double]::TryParse($rawVal, [System.Globalization.NumberStyles]::Float, [System.Globalization.CultureInfo]::InvariantCulture, [ref]$fv)) {
            try { Set-SimPoint -Point $selA.Point -Val $fv } catch {}
        } else {
            Write-Host "  Invalid number '$rawVal'." -ForegroundColor Yellow
        }
    }
    else {
        Write-Host "  Invalid key '$choice' -- enter 1..12, C, R, A, O, D, or Q." -ForegroundColor Yellow
    }
}
