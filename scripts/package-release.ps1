<#
.SYNOPSIS
    Assembles the release dist/ directory (docs/10 §3 release assets).

.DESCRIPTION
    Shared by the release workflow and local verification:
      - portable: src-tauri/target/release/floattranslate.exe
          -> dist/FloatTranslate_<version>_x64-portable.exe  (bare exe renamed)
      - installer: src-tauri/target/release/bundle/nsis/*-setup.exe
          -> dist/FloatTranslate_<version>_x64-setup.exe
      - checksums: dist/checksums.txt  (SHA256 over dist/*.exe, sha256sum format)

    Signing is intentionally NOT done here: CI signs conditionally (secrets
    gated) AFTER renaming and BEFORE checksumming, because signing changes the
    file bytes and would invalidate the hashes.

.PARAMETER Version
    App version used in asset names. Defaults to src-tauri/tauri.conf.json.

.PARAMETER DistDir
    Output directory (default: dist at repo root). Created if missing.

.PARAMETER AllowMissingInstaller
    Skip the installer asset when no NSIS bundle exists (local fallback when
    the NSIS toolchain cannot be downloaded). The portable exe is always
    produced.

.PARAMETER ChecksumsOnly
    Do not copy anything; only (re)generate checksums.txt for existing
    dist/*.exe. Used in CI after the conditional signing step.

.PARAMETER Checksums
    Generate checksums.txt after copying (one-shot local mode).

.EXAMPLE
    pwsh scripts/package-release.ps1 -Checksums
    pwsh scripts/package-release.ps1 -ChecksumsOnly
#>
[CmdletBinding()]
param(
    [string]$Version,
    [string]$DistDir,
    [switch]$AllowMissingInstaller,
    [switch]$ChecksumsOnly,
    [switch]$Checksums
)

$ErrorActionPreference = 'Stop'
$repoRoot = Resolve-Path (Join-Path $PSScriptRoot '..')
if (-not $DistDir) { $DistDir = Join-Path $repoRoot 'dist' }

if (-not $Version) {
    $conf = Get-Content (Join-Path $repoRoot 'src-tauri\tauri.conf.json') -Raw | ConvertFrom-Json
    $Version = $conf.version
}
$DistDir = [System.IO.Path]::GetFullPath($DistDir)
Write-Host "package-release: version=$Version dist=$DistDir"

if (-not $ChecksumsOnly) {
    New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

    # --- Go sidecar: required by BOTH distribution shapes. --------------------
    # improvement (portable showed "backend starting" forever): the portable
    # exe resolves its backend via <exe_dir>\floattranslate-backend.exe; the
    # NSIS installer embeds it as a bundle resource.
    $backendSrc = Join-Path $repoRoot 'backend\bin\floattranslate-backend.exe'
    if (-not (Test-Path $backendSrc)) {
        throw "backend sidecar not found: $backendSrc (build with: go build -o backend/bin/floattranslate-backend.exe ./cmd/server)"
    }
    Copy-Item $backendSrc (Join-Path $DistDir 'floattranslate-backend.exe') -Force
    Write-Host '  sidecar  : floattranslate-backend.exe'

    # --- Portable: the bare release exe, renamed, in portable data mode. ------
    # portable.flag next to the exe switches the data root to
    # <exe_dir>\FloatTranslateData (docs/07 §8); without it the portable exe
    # silently ran in installed mode writing to %APPDATA%.
    $portableSrc = Join-Path $repoRoot 'src-tauri\target\release\floattranslate.exe'
    if (-not (Test-Path $portableSrc)) {
        throw "portable exe not found: $portableSrc (build the release binary first)"
    }
    $portableName = "FloatTranslate_${Version}_x64-portable.exe"
    Copy-Item $portableSrc (Join-Path $DistDir $portableName) -Force
    New-Item -ItemType File -Force -Path (Join-Path $DistDir 'portable.flag') | Out-Null
    Write-Host "  portable : $portableName (+ portable.flag)"

    # --- Portable ZIP: self-contained download (exe + sidecar + flag). --------
    $zipName = "FloatTranslate_${Version}_x64-portable.zip"
    $zipMembers = @(
        (Join-Path $DistDir $portableName),
        (Join-Path $DistDir 'floattranslate-backend.exe'),
        (Join-Path $DistDir 'portable.flag')
    )
    Compress-Archive -Path $zipMembers -DestinationPath (Join-Path $DistDir $zipName) -Force
    Write-Host "  portable zip: $zipName"

    # --- Installer: the NSIS setup exe produced by the tauri bundler. --------
    $nsisDir = Join-Path $repoRoot 'src-tauri\target\release\bundle\nsis'
    $installerSrc = $null
    if (Test-Path $nsisDir) {
        $installerSrc = Get-ChildItem $nsisDir -Filter '*-setup.exe' |
            Sort-Object LastWriteTime -Descending |
            Select-Object -First 1
    }
    if ($installerSrc) {
        $installerName = "FloatTranslate_${Version}_x64-setup.exe"
        Copy-Item $installerSrc.FullName (Join-Path $DistDir $installerName) -Force
        Write-Host "  installer: $installerName"
    } elseif ($AllowMissingInstaller) {
        Write-Warning 'no NSIS installer found; producing portable asset only (-AllowMissingInstaller)'
    } else {
        throw "no NSIS installer found under $nsisDir (build with: npx @tauri-apps/cli@2 build --bundles nsis)"
    }
}

if ($Checksums -or $ChecksumsOnly) {
    $assets = Get-ChildItem $DistDir -Include '*.exe', '*.zip' -Recurse -Depth 0 | Sort-Object Name
    if (-not $assets) { throw "no .exe/.zip assets in $DistDir to checksum" }
    $lines = foreach ($asset in $assets) {
        $hash = (Get-FileHash -Path $asset.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        "$hash  $($asset.Name)"
    }
    $checksumPath = Join-Path $DistDir 'checksums.txt'
    $lines | Set-Content -Path $checksumPath -Encoding ascii
    Write-Host "  checksums: $(Split-Path -Leaf $checksumPath) ($($assets.Count) asset(s))"
}

Write-Host 'package-release: done'
