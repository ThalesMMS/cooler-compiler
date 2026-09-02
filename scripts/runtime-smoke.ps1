# SPDX-License-Identifier: AGPL-3.0-or-later
param(
    [Parameter(Mandatory=$true)][Alias("PackageDirectory")][string]$PackagePath,
    [Parameter(Mandatory=$true)][string]$ChecksumManifest,
    [int]$Port = 0
)
$ErrorActionPreference = "Stop"
$root = Join-Path ([System.IO.Path]::GetTempPath()) ("cooler-runtime-smoke-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $root | Out-Null
$gui = $null
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
try {
    function Confirm-Checksum([string]$artifact) {
        $name = Split-Path $artifact -Leaf
        $line = Get-Content $ChecksumManifest | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+\*?$([regex]::Escape($name))$" } | Select-Object -First 1
        if (!$line) { throw "$name is absent from $ChecksumManifest" }
        $expected = ($line -split '\s+')[0].ToLowerInvariant()
        $actual = (Get-FileHash -Algorithm SHA256 $artifact).Hash.ToLowerInvariant()
        if ($actual -ne $expected) { throw "checksum mismatch for $name" }
        Write-Host "[PASS] checksum $actual $name"
    }
    function Invoke-ExpectedNativeFailure(
        [string[]]$Arguments,
        [string]$ErrorPath,
        [string]$DiagnosticPattern,
        [string]$Scenario
    ) {
        # Windows PowerShell 5.1 promotes native stderr to a non-terminating
        # NativeCommandError. With the script-wide Stop preference, expected
        # compiler failures would terminate the smoke test before their exit
        # codes and diagnostics could be asserted.
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = "Continue"
            & $coolerc @Arguments 2> $ErrorPath
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }
        if ($exitCode -ne 1) { throw "$Scenario exit code was $exitCode" }
        if ((Get-Content -Raw $ErrorPath) -notmatch $DiagnosticPattern) { throw "$Scenario diagnostic mismatch" }
    }
    if (Test-Path $PackagePath -PathType Container) {
        $packageRoot = (Resolve-Path $PackagePath).Path
        $coolerc = Join-Path $packageRoot "coolerc-windows-amd64.exe"
        $coolergui = Join-Path $packageRoot "coolergui-windows-amd64.exe"
        $coolerLsp = Join-Path $packageRoot "cooler-lsp-windows-amd64.exe"
        foreach ($artifact in @($coolerc, $coolergui, $coolerLsp)) {
            if (!(Test-Path $artifact -PathType Leaf)) { throw "missing legacy development artifact: $artifact" }
            Confirm-Checksum $artifact
        }
        Write-Host "[INFO] qualified legacy raw-binary mode; release evidence must use a package archive"
    } elseif (Test-Path $PackagePath -PathType Leaf) {
        $archive = (Resolve-Path $PackagePath).Path
        $archiveName = Split-Path $archive -Leaf
        if ($archiveName -notlike "cooler-compiler-v*-windows-amd64.zip") { throw "package does not match Windows amd64: $archiveName" }
        Confirm-Checksum $archive
        $extraction = Join-Path $root "package"
        Expand-Archive -LiteralPath $archive -DestinationPath $extraction
        $directories = @(Get-ChildItem -LiteralPath $extraction -Directory)
        if ($directories.Count -ne 1) { throw "package must contain exactly one top-level directory" }
        $packageRoot = $directories[0].FullName
        $coolerc = Join-Path $packageRoot "coolerc.exe"
        $coolergui = Join-Path $packageRoot "coolergui.exe"
        $coolerLsp = Join-Path $packageRoot "cooler-lsp.exe"
        foreach ($artifact in @($coolerc, $coolergui, $coolerLsp)) {
            if (!(Test-Path $artifact -PathType Leaf)) { throw "package omitted expected executable: $artifact" }
        }
    } else {
        throw "package input does not exist: $PackagePath"
    }
    [System.IO.File]::WriteAllText((Join-Path $root "success.cl"), @'
class Main inherits IO { main() : Object { out_string("smoke-ok\n") }; };
'@, $utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $root "semantic-error.cl"), @'
class Main { main() : Object { missing_identifier }; };
'@, $utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $root "runtime-error.cl"), @'
class Main { main() : Object { 1 / 0 }; };
'@, $utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $root "helper.cl"), @'
class Helper inherits IO { echo() : Object { out_string(in_string()) }; };
'@, $utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $root "main.cl"), @'
class Main { main() : Object { (new Helper).echo() }; };
'@, $utf8NoBom)
    [System.IO.File]::WriteAllText((Join-Path $root "input.txt"), "multifile-input`n", $utf8NoBom)

    if ((& $coolerc --version) -notmatch '^coolerc \d+\.\d+\.\d+') { throw "version check failed" }
    if ((& $coolerLsp --version 2>&1) -notmatch '^cooler-lsp \d+\.\d+\.\d+') { throw "language-server version check failed" }
    if ((& $coolerc run (Join-Path $root "success.cl")) -ne "smoke-ok") { throw "success program failed" }
    Invoke-ExpectedNativeFailure @("check", (Join-Path $root "semantic-error.cl")) (Join-Path $root "semantic.err") "COL3100" "semantic-error"
    Invoke-ExpectedNativeFailure @("run", (Join-Path $root "runtime-error.cl")) (Join-Path $root "runtime.err") "division by zero" "runtime-error"
    $multi = & $coolerc run --input (Join-Path $root "input.txt") (Join-Path $root "helper.cl") (Join-Path $root "main.cl")
    if ($multi -ne "multifile-input") { throw "multifile input failed" }
    foreach ($artifact in @("tokens", "ast", "symbols", "cfg", "ir", "assembly", "report")) {
        & $coolerc emit $artifact (Join-Path $root "success.cl") | Out-File -Encoding utf8 (Join-Path $root "$artifact.out")
        if ($LASTEXITCODE -ne 0) { throw "emit $artifact failed" }
    }
    Write-Host "[PASS] CLI scenarios and artifacts"
    Write-Host "[PASS] language-server executable and version output"

    $guiLog = Join-Path $root "gui.log"
    $guiErrorLog = Join-Path $root "gui-error.log"
    $gui = Start-Process -PassThru -WindowStyle Hidden $coolergui -ArgumentList "--browser", "--open=false", "--addr=127.0.0.1:$Port" -RedirectStandardOutput $guiLog -RedirectStandardError $guiErrorLog
    $ready = $false
    for ($i = 0; $i -lt 100; $i++) {
        if ($gui.HasExited) { throw "browser GUI exited before becoming ready: $(Get-Content -Raw $guiErrorLog)" }
        $guiOutput = if (Test-Path $guiLog) { Get-Content -Raw $guiLog } else { "" }
        if ($guiOutput -match 'Cooler Compiler at (http://\S+)') {
            $guiUrl = $Matches[1]
            try { Invoke-RestMethod "$guiUrl/api/health" | Out-Null; $ready = $true; break } catch {}
        }
        Start-Sleep -Milliseconds 100
    }
    if (!$ready) { throw "browser GUI did not become ready" }
    $page = Invoke-WebRequest -UseBasicParsing "$guiUrl/"
    if ($page.Content -notmatch "Cooler Compiler") { throw "GUI HTML mismatch" }
    if (!$page.Headers["Content-Security-Policy"]) { throw "GUI response omitted Content-Security-Policy" }
    Write-Host "[PASS] browser GUI served health and secured application HTML"
    Write-Host "[MANUAL] exercise debugger, exports, and native WebView2 GUI"
} finally {
    if ($gui -and !$gui.HasExited) { Stop-Process -Id $gui.Id }
    if (Test-Path $root) { Remove-Item -Recurse -Force $root }
}
