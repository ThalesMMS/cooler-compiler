# SPDX-License-Identifier: AGPL-3.0-or-later

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidatePattern('^[0-9A-Za-z][0-9A-Za-z._-]*$')]
    [string]$Version = "0.1.0"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw "The native Windows application must be built and run on Windows."
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "Go was not found on PATH. Install Go before running this script."
}

$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$distDir = Join-Path $root "dist"
$executable = Join-Path $distDir "coolergui-windows-amd64-native-v$Version.exe"

New-Item -ItemType Directory -Path $distDir -Force | Out-Null

$hadCgoEnabled = Test-Path Env:CGO_ENABLED
$previousCgoEnabled = $env:CGO_ENABLED
$env:CGO_ENABLED = "1"

try {
    Push-Location $root
    try {
        Write-Host "Building the native Windows application..." -ForegroundColor Cyan
        $ldflags = "-s -w -X main.version=$Version"
        & go build -buildvcs=false -trimpath -ldflags $ldflags -o $executable ./cmd/coolergui

        if ($LASTEXITCODE -ne 0) {
            throw "The native Windows build failed. Make sure a MinGW-w64 C compiler and WebView2 are available."
        }
    }
    finally {
        Pop-Location
    }

    Write-Host "Starting $executable" -ForegroundColor Green
    Start-Process -FilePath $executable -WorkingDirectory $root
}
finally {
    if ($hadCgoEnabled) {
        $env:CGO_ENABLED = $previousCgoEnabled
    }
    else {
        Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    }
}
