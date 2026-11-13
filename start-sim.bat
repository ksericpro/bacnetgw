@echo off
pushd "%~dp0"

if exist "dist\windows-amd64\bacnet-gw.exe" copy /Y "dist\windows-amd64\bacnet-gw.exe" "bacnet-gw.exe" >nul 2>&1

if not exist "mock-exhibition.exe" (
    echo [ERROR] mock-exhibition.exe not found. Run .\build.bat windows-amd64 first.
    popd
    exit /b 1
)
if not exist "bacnet-gw.exe" (
    echo [ERROR] bacnet-gw.exe not found. Run .\build.bat windows-amd64 first.
    popd
    exit /b 1
)

echo ==> 1. Starting Simulated Optergy P864 Controller (#864077 on 127.0.0.1:47810)...
start "Mock Optergy P864 + IMOPS" cmd /k ".\mock-exhibition.exe"

timeout /t 1 /nobreak >nul

echo ==> 2. Starting Local BACnet Gateway (Dashboard on http://127.0.0.1:8090)...
start "BACnet Gateway (Local Sim)" cmd /k ".\bacnet-gw.exe -config config.sim.yaml"

timeout /t 2 /nobreak >nul

echo ==> 3. Opening Web Dashboard at http://127.0.0.1:8090 ...
start "" "http://127.0.0.1:8090"

echo ==> 4. Launching Interactive Dry-Contact Toggle Controller...
powershell -NoProfile -ExecutionPolicy Bypass -File ".\sim-contacts.ps1"

popd

