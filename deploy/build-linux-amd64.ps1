# Cross-compiles bacnet-gw (and mock-exhibition) on Windows as static Ubuntu 64-bit (linux/amd64) ELF binaries
# and packages .\dist\ubuntu-amd64\ + .\dist\bacnet-gw-ubuntu-amd64.tar.gz

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
Push-Location $repoRoot
try {
    $distDir = Join-Path $repoRoot "dist"
    $outDir  = Join-Path $distDir "ubuntu-amd64"
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null

    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"

    Write-Host "==> Building static linux/amd64 binary: dist\ubuntu-amd64\bacnet-gw ..."
    go build -trimpath -ldflags="-s -w" -o (Join-Path $outDir "bacnet-gw") ./cmd/bacnet-gw

    Write-Host "==> Building static linux/amd64 simulator: dist\ubuntu-amd64\mock-exhibition ..."
    go build -trimpath -ldflags="-s -w" -o (Join-Path $outDir "mock-exhibition") ./cmd/mock-exhibition

    # Copy Linux files with guaranteed Unix LF (\n) line endings
    foreach ($lfFile in @("deploy\bacnet-gw.service", "deploy\install-ubuntu.sh", "config.example.yaml", ".env.example")) {
        $leaf = Split-Path -Leaf $lfFile
        $raw  = [System.IO.File]::ReadAllText((Join-Path $repoRoot $lfFile)) -replace "`r`n", "`n"
        [System.IO.File]::WriteAllBytes((Join-Path $outDir $leaf), [System.Text.UTF8Encoding]::new($false).GetBytes($raw))
    }

    if (Test-Path "dashboard.jpeg") {
        Copy-Item "dashboard.jpeg" $outDir -Force
    }
    if (Test-Path "bacnet devices.jpeg") {
        Copy-Item "bacnet devices.jpeg" $outDir -Force
    }

    $tarPath = Join-Path $distDir "bacnet-gw-ubuntu-amd64.tar.gz"
    if (Test-Path $tarPath) {
        Remove-Item $tarPath -Force
    }
    tar -czf $tarPath -C $distDir ubuntu-amd64

    Write-Host "==> Ready! Ubuntu 64-bit bundle & tarball created:"
    Write-Host "    Folder : $outDir"
    Write-Host "    Archive: $tarPath"
    Get-ChildItem $outDir | Select-Object Name, Length
} finally {
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Pop-Location
}
