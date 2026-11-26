<#
.SYNOPSIS
    Stages and packages WhatsMySpeed into an MSIX package for the Microsoft Store.

.DESCRIPTION
    Builds WhatsMySpeed.exe, stages Assets and AppxManifest.xml with the requested Partner Center
    identity fields into build\msix-staging, automatically downloads the lightweight x64 tools from
    Microsoft.Windows.SDK.BuildTools (NuGet, ~50 MB, skipping multi-GB full SDK install) into
    tools\winsdk-x64 if Windows SDK tools are not already installed, and creates the .msix under dist\.

.EXAMPLE
    .\build-msix.ps1 -IdentityName "YourPublisher.WhatsMySpeed" -Publisher "CN=XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX" -PublisherDisplayName "Your Publisher Name" -Version "1.0.0.0"
#>
param(
    [string]$IdentityName = "WhatsMySpeed.WhatsMySpeed",
    [string]$Publisher = "CN=WhatsMySpeed Contributors",
    [string]$PublisherDisplayName = "WhatsMySpeed Contributors",
    [string]$Version = "1.0.0.0",
    [string]$Out = "dist\WhatsMySpeed_x64.msix",
    [string]$BuildToolsVersion = "10.0.28000.2957",
    [switch]$SignSelf
)

$ErrorActionPreference = "Stop"

$repoRoot = $PSScriptRoot
$toolsDir = Join-Path $repoRoot "tools\winsdk-x64"

function Find-WindowsSdkTool([string]$toolName) {
    $localTool = Join-Path $toolsDir $toolName
    if (Test-Path $localTool) { return $localTool }

    $inPath = Get-Command $toolName -ErrorAction SilentlyContinue
    if ($inPath) { return $inPath.Source }

    $kitsBin = "${env:ProgramFiles(x86)}\Windows Kits\10\bin"
    if (Test-Path $kitsBin) {
        $match = Get-ChildItem $kitsBin -Directory -ErrorAction SilentlyContinue |
            Sort-Object Name -Descending |
            ForEach-Object { Join-Path $_.FullName "x64\$toolName" } |
            Where-Object { Test-Path $_ } |
            Select-Object -First 1
        if ($match) { return $match }
    }

    $nugetRoot = Join-Path $env:USERPROFILE ".nuget\packages\microsoft.windows.sdk.buildtools"
    if (Test-Path $nugetRoot) {
        $match = Get-ChildItem $nugetRoot -Recurse -Filter $toolName -ErrorAction SilentlyContinue |
            Where-Object { $_.FullName -match '\\x64\\' } |
            Sort-Object FullName -Descending |
            Select-Object -First 1
        if ($match) { return $match.FullName }
    }
    return $null
}

function Ensure-WindowsSdkBuildTools([string]$pkgVersion) {
    if (Test-Path (Join-Path $toolsDir "makeappx.exe")) {
        return
    }
    Write-Host "Fetching lightweight x64 packaging tools (Microsoft.Windows.SDK.BuildTools $pkgVersion)..."
    New-Item -ItemType Directory -Path $toolsDir -Force | Out-Null
    $nupkgPath = Join-Path $repoRoot "build\buildtools.nupkg"
    New-Item -ItemType Directory -Path (Split-Path $nupkgPath -Parent) -Force | Out-Null

    $prevProgress = $ProgressPreference
    $ProgressPreference = "SilentlyContinue"
    try {
        $url = "https://api.nuget.org/v3-flatcontainer/microsoft.windows.sdk.buildtools/$pkgVersion/microsoft.windows.sdk.buildtools.$pkgVersion.nupkg"
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $nupkgPath
    } finally {
        $ProgressPreference = $prevProgress
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($nupkgPath)
    try {
        $extracted = 0
        foreach ($entry in $zip.Entries) {
            if ([string]::IsNullOrEmpty($entry.Name)) { continue }
            $norm = $entry.FullName -replace '\\', '/'
            if ($norm -match '^bin/(?:[^/]+/)?x64/(.+)$') {
                $relPath = $Matches[1] -replace '/', '\'
                $destPath = Join-Path $toolsDir $relPath
                $destParent = Split-Path $destPath -Parent
                if (-not (Test-Path $destParent)) {
                    New-Item -ItemType Directory -Path $destParent -Force | Out-Null
                }
                [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $destPath, $true)
                $extracted++
            }
        }
        if ($extracted -eq 0) {
            throw "No x64 tools found inside Microsoft.Windows.SDK.BuildTools $pkgVersion"
        }
        Write-Host ("Extracted {0} x64 SDK tool files into {1}" -f $extracted, $toolsDir)
    } finally {
        $zip.Dispose()
        Remove-Item $nupkgPath -ErrorAction SilentlyContinue
    }
}

$stageDir = Join-Path $repoRoot "build\msix-staging"
$assetsSrc = Join-Path $repoRoot "installer\msix\Assets"
$manifestSrc = Join-Path $repoRoot "installer\msix\AppxManifest.xml"
$outPath = if ([System.IO.Path]::IsPathRooted($Out)) { $Out } else { Join-Path $repoRoot $Out }

if (Test-Path $stageDir) {
    Remove-Item $stageDir -Recurse -Force
}
New-Item -ItemType Directory -Path (Join-Path $stageDir "Assets") -Force | Out-Null
New-Item -ItemType Directory -Path (Split-Path $outPath -Parent) -Force | Out-Null

# 1. Build standalone Windows GUI executable directly into staging folder
& (Join-Path $repoRoot "build.ps1") -Out (Join-Path $stageDir "WhatsMySpeed.exe")
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

# 2. Copy Store visual assets
Copy-Item -Path (Join-Path $assetsSrc "*.png") -Destination (Join-Path $stageDir "Assets") -Force

# 3. Render AppxManifest.xml with exact Partner Center identity fields
[xml]$xml = Get-Content -Path $manifestSrc -Raw -Encoding UTF8
$xml.Package.Identity.Name = $IdentityName
$xml.Package.Identity.Publisher = $Publisher
$xml.Package.Identity.Version = $Version
$xml.Package.Properties.PublisherDisplayName = $PublisherDisplayName

$manifestOut = Join-Path $stageDir "AppxManifest.xml"
$settings = New-Object System.Xml.XmlWriterSettings
$settings.Indent = $true
$settings.Encoding = New-Object System.Text.UTF8Encoding($false)
$writer = [System.Xml.XmlWriter]::Create($manifestOut, $settings)
try {
    $xml.Save($writer)
} finally {
    $writer.Dispose()
}

Write-Host "Staged MSIX layout in $stageDir"

# 4. Ensure makeappx.exe (and companion SDK tools) are present; auto-fetch lightweight NuGet package if needed
if (-not (Find-WindowsSdkTool "makeappx.exe")) {
    Ensure-WindowsSdkBuildTools $BuildToolsVersion
}

# 5. Generate resources.pri via makepri.exe so scale/unplated tiles resolve cleanly
$makePri = Find-WindowsSdkTool "makepri.exe"
if ($makePri) {
    $priConfig = Join-Path $repoRoot "build\priconfig.xml"
    & $makePri createconfig /cf $priConfig /dq en-US /o | Out-Null
    if ($LASTEXITCODE -eq 0) {
        & $makePri new /pr $stageDir /cf $priConfig /mn $manifestOut /of (Join-Path $stageDir "resources.pri") /o | Out-Null
        Remove-Item $priConfig -ErrorAction SilentlyContinue
        if ($LASTEXITCODE -ne 0) { throw "makepri.exe failed with exit code $LASTEXITCODE" }
        Write-Host "Generated resources.pri via $makePri"
    }
}

# 6. Pack into .msix via makeappx.exe
$makeAppx = Find-WindowsSdkTool "makeappx.exe"
if (-not $makeAppx) {
    throw "makeappx.exe not found after downloading BuildTools."
}

Write-Host "Packing $outPath with $makeAppx..."
& $makeAppx pack /d $stageDir /p $outPath /o
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

if ($SignSelf) {
    $signTool = Find-WindowsSdkTool "signtool.exe"
    if (-not $signTool) {
        throw "signtool.exe not found in BuildTools; cannot self-sign $outPath"
    }
    $cert = Get-ChildItem Cert:\CurrentUser\My |
        Where-Object { $_.Subject -eq $Publisher -and $_.NotAfter -gt (Get-Date).AddDays(7) } |
        Select-Object -First 1
    if (-not $cert) {
        $cert = New-SelfSignedCertificate -Type Custom -Subject $Publisher `
            -KeyUsage DigitalSignature -FriendlyName "WhatsMySpeed Dev MSIX" `
            -CertStoreLocation "Cert:\CurrentUser\My" `
            -TextExtension @("2.5.29.37={text}1.3.6.1.5.5.7.3.3", "2.5.29.19={text}")
        Write-Host "Created self-signed dev certificate $($cert.Thumbprint) ($Publisher)."
        Write-Host "To sideload locally, trust it once in LocalMachine\TrustedPeople (Admin PowerShell)."
    }
    & $signTool sign /fd SHA256 /sha1 $cert.Thumbprint $outPath
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

$msixItem = Get-Item $outPath
Write-Host ("Built {0} ({1:N2} MB)" -f $msixItem.FullName, ($msixItem.Length / 1MB))
