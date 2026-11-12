@echo off
setlocal EnableExtensions EnableDelayedExpansion

:: ============================================================================
:: Multi-Architecture Static Cross-Compiler for bacnet-gw & mock-exhibition
::
:: Usage:
::   .\build.bat               -> Build ALL targets (Linux amd64/arm64/arm, Windows amd64/arm64, macOS arm64)
::   .\build.bat linux-amd64   -> Build Ubuntu/Debian 64-bit x86_64 binary + systemd tar.gz bundle
::   .\build.bat linux-arm64   -> Build Ubuntu/Raspberry Pi/Edge 64-bit ARM64 binary + systemd tar.gz bundle
::   .\build.bat linux-arm     -> Build Linux 32-bit ARMv7 (industrial IoT gateway) binary + systemd tar.gz bundle
::   .\build.bat windows-amd64 -> Build Windows 64-bit x86_64 .exe
::   .\build.bat windows-arm64 -> Build Windows 64-bit ARM64 .exe
::   .\build.bat darwin-arm64  -> Build macOS Apple Silicon binary
::   .\build.bat menu          -> Interactive numbered target selector
:: ============================================================================

pushd "%~dp0"

where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Go toolchain ^("go"^) was not found in PATH.
    popd
    exit /b 1
)

set "DIST_DIR=%CD%\dist"
if not exist "%DIST_DIR%" mkdir "%DIST_DIR%"

set "TARGET=%~1"
if /I "%TARGET%"=="" set "TARGET=all"

if /I "%TARGET%"=="menu" (
    echo === Select Target Platform to Compile ===
    echo   [1] All platforms ^(Linux amd64/arm64/arm + Windows amd64/arm64 + macOS arm64^)
    echo   [2] Linux amd64   ^(Ubuntu / Debian 64-bit x86_64 + systemd bundle^)
    echo   [3] Linux arm64   ^(Ubuntu ARM64 / Raspberry Pi 4-5 / Edge Gateway + systemd bundle^)
    echo   [4] Linux armv7   ^(32-bit ARM industrial gateway + systemd bundle^)
    echo   [5] Windows amd64 ^(Windows 10/11/Server x86_64 .exe^)
    echo   [6] Windows arm64 ^(Windows ARM64 .exe^)
    echo   [7] macOS arm64   ^(Apple Silicon^)
    set /p "CHOICE=Enter choice [1-7] (default 1): "
    if "!CHOICE!"=="" set "CHOICE=1"
    if "!CHOICE!"=="1" set "TARGET=all"
    if "!CHOICE!"=="2" set "TARGET=linux-amd64"
    if "!CHOICE!"=="3" set "TARGET=linux-arm64"
    if "!CHOICE!"=="4" set "TARGET=linux-arm"
    if "!CHOICE!"=="5" set "TARGET=windows-amd64"
    if "!CHOICE!"=="6" set "TARGET=windows-arm64"
    if "!CHOICE!"=="7" set "TARGET=darwin-arm64"
)

if /I "%TARGET%"=="help" goto :usage
if /I "%TARGET%"=="-h" goto :usage
if /I "%TARGET%"=="--help" goto :usage

set "BUILT_ANY=0"

if /I "%TARGET%"=="all" (
    call :build_linux amd64 "" || goto :fail
    call :build_linux arm64 "" || goto :fail
    call :build_linux arm   "7" || goto :fail
    call :build_windows amd64 "1" || goto :fail
    call :build_windows arm64 "0" || goto :fail
    call :build_darwin arm64 || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="linux-amd64" (
    call :build_linux amd64 "" || goto :fail
    set "BUILT_ANY=1"
)
if /I "%TARGET%"=="amd64" (
    call :build_linux amd64 "" || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="linux-arm64" (
    call :build_linux arm64 "" || goto :fail
    set "BUILT_ANY=1"
)
if /I "%TARGET%"=="arm64" (
    call :build_linux arm64 "" || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="linux-arm" (
    call :build_linux arm "7" || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="windows-amd64" (
    call :build_windows amd64 "1" || goto :fail
    set "BUILT_ANY=1"
)
if /I "%TARGET%"=="windows" (
    call :build_windows amd64 "1" || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="windows-arm64" (
    call :build_windows arm64 "0" || goto :fail
    set "BUILT_ANY=1"
)

if /I "%TARGET%"=="darwin-arm64" (
    call :build_darwin arm64 || goto :fail
    set "BUILT_ANY=1"
)
if /I "%TARGET%"=="macos" (
    call :build_darwin arm64 || goto :fail
    set "BUILT_ANY=1"
)

if "%BUILT_ANY%"=="0" (
    echo [ERROR] Unknown build target: "%TARGET%"
    goto :usage
)

echo.
echo ============================================================================
echo [SUCCESS] Build complete. Output artifacts in "%DIST_DIR%":
echo ============================================================================
dir /b "%DIST_DIR%"
popd
exit /b 0

:usage
echo Usage: build.bat [all ^| linux-amd64 ^| linux-arm64 ^| linux-arm ^| windows-amd64 ^| windows-arm64 ^| darwin-arm64 ^| menu]
popd
exit /b 1

:fail
echo.
echo [ERROR] Compilation failed.
popd
exit /b 1

:: ----------------------------------------------------------------------------
:: Subroutine: Build static Linux binary + systemd deployment tar.gz bundle
:: %1 = GOARCH (amd64 | arm64 | arm), %2 = GOARM (optional, e.g. 7)
:: ----------------------------------------------------------------------------
:build_linux
set "B_ARCH=%~1"
set "B_ARM=%~2"
set "STAGE_NAME=linux-%B_ARCH%"
set "STAGE_DIR=%DIST_DIR%\%STAGE_NAME%"
if not exist "%STAGE_DIR%" mkdir "%STAGE_DIR%"

echo ==^> Compiling Linux/%B_ARCH% static binaries...
set "CGO_ENABLED=0"
set "GOOS=linux"
set "GOARCH=%B_ARCH%"
set "GOARM=%B_ARM%"

go build -trimpath -ldflags="-s -w" -o "%STAGE_DIR%\bacnet-gw" ./cmd/bacnet-gw
if errorlevel 1 exit /b 1
copy /Y "%STAGE_DIR%\bacnet-gw" "%DIST_DIR%\bacnet-gw-linux-%B_ARCH%" >nul

go build -trimpath -ldflags="-s -w" -o "%STAGE_DIR%\mock-exhibition" ./cmd/mock-exhibition
if errorlevel 1 exit /b 1

:: Normalize Unix LF (\n) line endings for systemd & bash scripts
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "foreach ($f in @('deploy\bacnet-gw.service','deploy\install-ubuntu.sh','config.example.yaml','.env.example')) { $leaf = Split-Path -Leaf $f; $txt = [IO.File]::ReadAllText($f) -replace \"`r`n\",\"`n\"; [IO.File]::WriteAllBytes((Join-Path '%STAGE_DIR%' $leaf), [Text.UTF8Encoding]::new($false).GetBytes($txt)) }"
if errorlevel 1 exit /b 1

if exist "dashboard.jpeg" copy /Y "dashboard.jpeg" "%STAGE_DIR%\dashboard.jpeg" >nul
if exist "bacnet devices.jpeg" copy /Y "bacnet devices.jpeg" "%STAGE_DIR%\bacnet devices.jpeg" >nul

set "TAR_OUT=%DIST_DIR%\bacnet-gw-linux-%B_ARCH%.tar.gz"
if exist "%TAR_OUT%" del /F /Q "%TAR_OUT%"
tar -czf "%TAR_OUT%" -C "%DIST_DIR%" "%STAGE_NAME%"
if errorlevel 1 exit /b 1

:: Keep backward-compatible ubuntu-amd64 tarball alias for amd64
if /I "%B_ARCH%"=="amd64" (
    copy /Y "%TAR_OUT%" "%DIST_DIR%\bacnet-gw-ubuntu-amd64.tar.gz" >nul
)

set "GOARM="
exit /b 0

:: ----------------------------------------------------------------------------
:: Subroutine: Build static Windows .exe binaries
:: %1 = GOARCH (amd64 | arm64), %2 = CopyToRoot (1 = also update .\bacnet-gw.exe)
:: ----------------------------------------------------------------------------
:build_windows
set "W_ARCH=%~1"
set "W_ROOT=%~2"
set "W_STAGE=%DIST_DIR%\windows-%W_ARCH%"
if not exist "%W_STAGE%" mkdir "%W_STAGE%"

echo ==^> Compiling Windows/%W_ARCH% binaries...
set "CGO_ENABLED=0"
set "GOOS=windows"
set "GOARCH=%W_ARCH%"

go build -trimpath -ldflags="-s -w" -o "%W_STAGE%\bacnet-gw.exe" ./cmd/bacnet-gw
if errorlevel 1 exit /b 1
copy /Y "%W_STAGE%\bacnet-gw.exe" "%DIST_DIR%\bacnet-gw-windows-%W_ARCH%.exe" >nul

go build -trimpath -ldflags="-s -w" -o "%W_STAGE%\mock-exhibition.exe" ./cmd/mock-exhibition
if errorlevel 1 exit /b 1
copy /Y "%W_STAGE%\mock-exhibition.exe" "%DIST_DIR%\mock-exhibition-windows-%W_ARCH%.exe" >nul

copy /Y "config.example.yaml" "%W_STAGE%\config.example.yaml" >nul
copy /Y ".env.example" "%W_STAGE%\.env.example" >nul
if exist "deploy\install-windows.ps1" copy /Y "deploy\install-windows.ps1" "%W_STAGE%\install-windows.ps1" >nul
if exist "dashboard.jpeg" copy /Y "dashboard.jpeg" "%W_STAGE%\dashboard.jpeg" >nul
if exist "bacnet devices.jpeg" copy /Y "bacnet devices.jpeg" "%W_STAGE%\bacnet devices.jpeg" >nul

set "ZIP_OUT=%DIST_DIR%\bacnet-gw-windows-%W_ARCH%.zip"
if exist "%ZIP_OUT%" del /F /Q "%ZIP_OUT%"
tar -a -cf "%ZIP_OUT%" -C "%DIST_DIR%" "windows-%W_ARCH%"

if "%W_ROOT%"=="1" (
    copy /Y "%W_STAGE%\bacnet-gw.exe" "bacnet-gw.exe" >nul
    copy /Y "%W_STAGE%\mock-exhibition.exe" "mock-exhibition.exe" >nul
)
exit /b 0

:: ----------------------------------------------------------------------------
:: Subroutine: Build macOS binary
:: %1 = GOARCH (arm64 | amd64)
:: ----------------------------------------------------------------------------
:build_darwin
set "D_ARCH=%~1"
echo ==^> Compiling macOS/%D_ARCH% binary...
set "CGO_ENABLED=0"
set "GOOS=darwin"
set "GOARCH=%D_ARCH%"

go build -trimpath -ldflags="-s -w" -o "%DIST_DIR%\bacnet-gw-darwin-%D_ARCH%" ./cmd/bacnet-gw
if errorlevel 1 exit /b 1
exit /b 0

