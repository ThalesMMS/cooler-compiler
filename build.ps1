# SPDX-License-Identifier: AGPL-3.0-or-later

param(
    [Parameter(Position=0)]
    [ValidateSet("test", "vet", "fmt", "check", "build", "package", "package-native", "run", "browser", "clean", "help")]
    [string]$Command = "help"
)

$ErrorActionPreference = "Stop"

function Show-Help {
    Write-Host "Cooler Compiler Lab - Build and Run Script for Windows" -ForegroundColor Cyan
    Write-Host ""
    Write-Host "Usage: .\build.ps1 [command]" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Commands:" -ForegroundColor Yellow
    Write-Host "  test          - Run tests"
    Write-Host "  vet           - Run go vet"
    Write-Host "  fmt           - Format Go code"
    Write-Host "  check         - Run tests, vet, and check JavaScript"
    Write-Host "  build         - Build coolergui and coolerc binaries"
    Write-Host "  package       - Create standalone executable package (browser mode)"
    Write-Host "  package-native - Create standalone executable with native window support"
    Write-Host "  run           - Run the GUI application"
    Write-Host "  browser       - Run the GUI in browser mode"
    Write-Host "  clean         - Remove build artifacts"
    Write-Host "  help          - Show this help message"
    Write-Host ""
    Write-Host "Examples:" -ForegroundColor Yellow
    Write-Host "  .\build.ps1 build"
    Write-Host "  .\build.ps1 package"
    Write-Host "  .\build.ps1 package-native"
    Write-Host "  .\build.ps1 run"
    Write-Host "  .\build.ps1 check"
}

function Invoke-Test {
    Write-Host "Running tests..." -ForegroundColor Green
    $env:CGO_ENABLED = "0"
    go test ./...
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Tests passed!" -ForegroundColor Green
    } else {
        Write-Host "Tests failed!" -ForegroundColor Red
        exit 1
    }
}

function Invoke-Vet {
    Write-Host "Running go vet..." -ForegroundColor Green
    $env:CGO_ENABLED = "0"
    go vet ./...
    if ($LASTEXITCODE -eq 0) {
        Write-Host "Vet passed!" -ForegroundColor Green
    } else {
        Write-Host "Vet failed!" -ForegroundColor Red
        exit 1
    }
}

function Invoke-Fmt {
    Write-Host "Formatting Go code..." -ForegroundColor Green
    $goFiles = Get-ChildItem -Recurse -Filter "*.go" | Where-Object { $_.FullName -notlike "*\dist\*" }
    foreach ($file in $goFiles) {
        gofmt -w $file.FullName
    }
    Write-Host "Code formatted!" -ForegroundColor Green
}

function Invoke-Check {
    Write-Host "Running checks..." -ForegroundColor Green
    Invoke-Test
    Invoke-Vet
    
    Write-Host "Checking JavaScript..." -ForegroundColor Green
    if (Test-Path "internal\web\static\app.js") {
        node --check internal/web/static/app.js
        if ($LASTEXITCODE -eq 0) {
            Write-Host "JavaScript check passed!" -ForegroundColor Green
        } else {
            Write-Host "JavaScript check failed!" -ForegroundColor Red
            exit 1
        }
    } else {
        Write-Host "JavaScript file not found, skipping..." -ForegroundColor Yellow
    }
    
    Write-Host "All checks passed!" -ForegroundColor Green
}

function Invoke-Build {
    Write-Host "Building binaries..." -ForegroundColor Green
    
    $binDir = "bin"
    if (-not (Test-Path $binDir)) {
        New-Item -ItemType Directory -Path $binDir | Out-Null
    }
    
    $env:CGO_ENABLED = "0"
    $ldflags = "-s -w -X main.version=0.1.0"
    
    Write-Host "Building coolergui..." -ForegroundColor Cyan
    go build -trimpath -ldflags $ldflags -o "$binDir\coolergui.exe" ./cmd/coolergui
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "Building coolerc..." -ForegroundColor Cyan
    go build -trimpath -ldflags $ldflags -o "$binDir\coolerc.exe" ./cmd/coolerc
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "Build completed! Binaries are in the 'bin' directory." -ForegroundColor Green
}

function Invoke-Package {
    Write-Host "Creating standalone executable package..." -ForegroundColor Green
    
    $distDir = "dist"
    if (-not (Test-Path $distDir)) {
        New-Item -ItemType Directory -Path $distDir | Out-Null
    }
    Copy-Item -Path "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md" -Destination $distDir -Force
    
    $env:CGO_ENABLED = "0"
    $ldflags = "-s -w -X main.version=0.1.0"
    
    $version = "0.1.0"
    $outputName = "coolergui-windows-amd64-v$version.exe"
    
    Write-Host "Building standalone executable (browser mode)..." -ForegroundColor Cyan
    Write-Host "Note: The executable includes embedded web assets (HTML, CSS, JS)" -ForegroundColor Yellow
    
    go build -trimpath -ldflags $ldflags -o "$distDir\$outputName" ./cmd/coolergui
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Package build failed!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "Building CLI executable..." -ForegroundColor Cyan
    $cliOutputName = "coolerc-windows-amd64-v$version.exe"
    go build -trimpath -ldflags $ldflags -o "$distDir\$cliOutputName" ./cmd/coolerc
    if ($LASTEXITCODE -ne 0) {
        Write-Host "CLI build failed!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "Package completed!" -ForegroundColor Green
    Write-Host "Executables are in the 'dist' directory:" -ForegroundColor Cyan
    Write-Host "  - $outputName (GUI application - browser mode)" -ForegroundColor White
    Write-Host "  - $cliOutputName (CLI compiler)" -ForegroundColor White
    Write-Host ""
    Write-Host "These are standalone executables that can be run on any Windows machine." -ForegroundColor Yellow
}

function Invoke-PackageNative {
    Write-Host "Creating standalone executable package with native window support..." -ForegroundColor Green
    
    $distDir = "dist"
    if (-not (Test-Path $distDir)) {
        New-Item -ItemType Directory -Path $distDir | Out-Null
    }
    Copy-Item -Path "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md" -Destination $distDir -Force
    
    $env:CGO_ENABLED = "1"
    $ldflags = "-s -w -X main.version=0.1.0"
    
    $version = "0.1.0"
    $outputName = "coolergui-windows-amd64-native-v$version.exe"
    
    Write-Host "Building standalone executable with native window support..." -ForegroundColor Cyan
    Write-Host "Note: This uses CGO and the webview library for true desktop window" -ForegroundColor Yellow
    Write-Host "The executable will open in a native desktop window (not browser)" -ForegroundColor Yellow
    
    go build -trimpath -ldflags $ldflags -o "$distDir\$outputName" ./cmd/coolergui
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Native package build failed!" -ForegroundColor Red
        Write-Host "This may require WebView2 runtime or C compiler" -ForegroundColor Yellow
        exit 1
    }
    
    Write-Host "Building CLI executable..." -ForegroundColor Cyan
    $cliOutputName = "coolerc-windows-amd64-v$version.exe"
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags $ldflags -o "$distDir\$cliOutputName" ./cmd/coolerc
    if ($LASTEXITCODE -ne 0) {
        Write-Host "CLI build failed!" -ForegroundColor Red
        exit 1
    }
    
    Write-Host "Native package completed!" -ForegroundColor Green
    Write-Host "Executables are in the 'dist' directory:" -ForegroundColor Cyan
    Write-Host "  - $outputName (GUI application - native desktop window)" -ForegroundColor White
    Write-Host "  - $cliOutputName (CLI compiler)" -ForegroundColor White
    Write-Host ""
    Write-Host "The GUI executable opens in a true desktop window using WebView2." -ForegroundColor Yellow
}

function Invoke-Run {
    Write-Host "Running GUI application..." -ForegroundColor Green
    go run ./cmd/coolergui
}

function Invoke-Browser {
    Write-Host "Running GUI in browser mode..." -ForegroundColor Green
    go run ./cmd/coolergui --browser
}

function Invoke-Clean {
    Write-Host "Cleaning build artifacts..." -ForegroundColor Green
    
    if (Test-Path "bin") {
        Remove-Item -Recurse -Force "bin"
        Write-Host "Removed 'bin' directory" -ForegroundColor Cyan
    }
    
    if (Test-Path "dist") {
        Remove-Item -Recurse -Force "dist"
        Write-Host "Removed 'dist' directory" -ForegroundColor Cyan
    }
    
    if (Test-Path "coverage.out") {
        Remove-Item -Force "coverage.out"
        Write-Host "Removed coverage.out" -ForegroundColor Cyan
    }
    
    Write-Host "Clean completed!" -ForegroundColor Green
}

# Main script logic
switch ($Command) {
    "test"           { Invoke-Test }
    "vet"            { Invoke-Vet }
    "fmt"            { Invoke-Fmt }
    "check"          { Invoke-Check }
    "build"          { Invoke-Build }
    "package"        { Invoke-Package }
    "package-native" { Invoke-PackageNative }
    "run"            { Invoke-Run }
    "browser"        { Invoke-Browser }
    "clean"          { Invoke-Clean }
    "help"           { Show-Help }
    default          { Show-Help }
}
