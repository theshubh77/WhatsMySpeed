<#
.SYNOPSIS
    Builds WhatsMySpeed as a standalone, console-less Windows executable.
#>
param(
    [string]$Out = "WhatsMySpeed.exe"
)

$ErrorActionPreference = "Stop"
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
$env:GOARCH = "amd64"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    $defaultGoBin = "C:\Program Files\Go\bin"
    if (Test-Path "$defaultGoBin\go.exe") {
        $env:Path = "$defaultGoBin;$env:Path"
    }
}

Write-Host "Building $Out..."
go build -trimpath -ldflags="-s -w -H=windowsgui" -o $Out ./cmd/whatsmyspeed
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

$item = Get-Item $Out
Write-Host ("Built {0} ({1:N2} MB)" -f $item.FullName, ($item.Length / 1MB))
