# Windows Production Installer for bacnet-gw (Run PowerShell as Administrator)
# Usage:
#   powershell -ExecutionPolicy Bypass -File .\install-windows.ps1

$ErrorActionPreference = "Stop"

$workDir = $PSScriptRoot
Push-Location $workDir
try {
    if (-not (Test-Path "$workDir\bacnet-gw.exe")) {
        throw "bacnet-gw.exe not found in $workDir"
    }

    if (-not (Test-Path "$workDir\config.yaml")) {
        if (Test-Path "$workDir\config.production.yaml") {
            Copy-Item "$workDir\config.production.yaml" "$workDir\config.yaml"
            Write-Host "==> Created initial config.yaml from config.production.yaml"
        } elseif (Test-Path "$workDir\config.example.yaml") {
            Copy-Item "$workDir\config.example.yaml" "$workDir\config.yaml"
            Write-Host "==> Created initial config.yaml from config.example.yaml"
        }
    } else {
        Write-Host "==> Keeping existing config.yaml"
    }

    Write-Host "==> Ensuring Windows Firewall allows inbound BACnet/IP UDP 47808..."
    $ruleName = "BACnet-IP Gateway (UDP 47808)"
    if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Protocol UDP -LocalPort 47808 -Action Allow | Out-Null
        Write-Host "    Added firewall rule: $ruleName"
    } else {
        Write-Host "    Firewall rule already exists: $ruleName"
    }

    Write-Host "==> Registering Windows Startup Scheduled Task 'ZAARR-BACnet-GW'..."
    $action  = New-ScheduledTaskAction -Execute "$workDir\bacnet-gw.exe" -Argument "-config `"$workDir\config.yaml`"" -WorkingDirectory $workDir
    $trigger = New-ScheduledTaskTrigger -AtStartup
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1)
    Register-ScheduledTask -TaskName "ZAARR-BACnet-GW" -Action $action -Trigger $trigger -Settings $settings -User "SYSTEM" -RunLevel Highest -Force | Out-Null
    Start-ScheduledTask -TaskName "ZAARR-BACnet-GW"

    Write-Host ""
    Write-Host "==> Done! 'ZAARR-BACnet-GW' is running in the background and set to auto-start on boot."
    Write-Host "    Dashboard : http://127.0.0.1:8090/"
    Write-Host "    Config    : $workDir\config.yaml"
    Write-Host "    Scan test : .\bacnet-gw.exe -select-subnet -scan"
} finally {
    Pop-Location
}

