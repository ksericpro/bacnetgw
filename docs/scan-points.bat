@echo off
setlocal EnableExtensions
chcp 65001 >nul

:: ============================================================================
:: Manual BACnet Subnet & Live Point Scanner
::   1. Temporarily pauses running bacnet-gw.exe (if active) to free UDP 47808
::   2. Scans all BACnet devices & point values -> saves to scan-points.txt
::   3. Prints a summary of Wireless Sensors, Gateway Status & Dry Contacts
::   4. Restarts bacnet-gw.exe automatically if it was running before the scan
:: ============================================================================

pushd "%~dp0"

if not exist "bacnet-gw.exe" (
    echo [ERROR] bacnet-gw.exe not found in "%CD%".
    popd
    pause
    exit /b 1
)

set "CFG_ARGS="
if not exist "config.yaml" (
    if exist "config.production.yaml" (
        set "CFG_ARGS=-config config.production.yaml"
    )
)

:: Check if bacnet-gw.exe is currently running so we can resume it afterwards
set "WAS_RUNNING=0"
tasklist /FI "IMAGENAME eq bacnet-gw.exe" 2>nul | find /I "bacnet-gw.exe" >nul
if not errorlevel 1 (
    set "WAS_RUNNING=1"
    echo [1/3] Pausing running bacnet-gw.exe to free UDP port 47808...
    taskkill /F /IM bacnet-gw.exe >nul 2>&1
    timeout /t 1 /nobreak >nul
) else (
    echo [1/3] UDP port 47808 is ready.
)

echo [2/3] Scanning BACnet devices and reading all live points ^(takes ~15s^)...
bacnet-gw.exe %CFG_ARGS% -scan-points > "scan-points.txt" 2>&1
if errorlevel 1 (
    echo [ERROR] Scan failed. Output from scan-points.txt:
    type "scan-points.txt"
    goto :restore
)

echo [3/3] Full results saved to: "%CD%\scan-points.txt"
echo.
echo === Key Monitored Points ^& Wireless Diagnostics ===
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; Select-String -Path 'scan-points.txt' -Encoding UTF8 -Pattern '^\s*(300|864077)\s+|^\s*(AI-101|AI-102|AV-27126|AI-201|AI-202|BI-[1-7]|BI-20008|BI-20021|BI-20022|BI-20031|BI-20032)\s+' | Select-Object -ExpandProperty Line"
echo.

:restore
if "%WAS_RUNNING%"=="1" (
    echo Resuming bacnet-gw.exe in background window...
    start "ZAARR BACnet Gateway" bacnet-gw.exe %CFG_ARGS%
)

popd
pause
