# radstorm one-liner installer for Windows (PowerShell).
#
# Purpose:
#   Detects the current CPU architecture, fetches the latest release
#   tag from the GitHub API, downloads the matching radstorm.exe and
#   radstorm-api.exe binaries, verifies SHA256 checksums, and installs
#   them into %LOCALAPPDATA%\radstorm\bin. Adds the directory to the
#   user PATH if not already present. Prints next-step instructions on
#   success.
#
# Usage:
#   iwr -useb https://raw.githubusercontent.com/Apextech-sys/reflex-radstorm/main/scripts/install.ps1 | iex
#   Or with a specific version:
#   $env:RADSTORM_VERSION = "v0.1.0"; .\scripts\install.ps1
#
# Related:
#   - scripts/install.sh   (Linux/macOS equivalent)
#   - docs/INSTALLATION.md (full installation documentation)
#   - .github/workflows/release.yml (builds and publishes the binaries)
#
# Briefing: .orchestration/briefings/wave-6-release-pipeline.md
#
# Contract:
#   Exit 0 on success. Throws on any error (set $ErrorActionPreference = "Stop").
#   Env vars:
#     RADSTORM_VERSION    — pin a specific tag (e.g. v0.1.0); default: latest
#     RADSTORM_INSTALL_DIR — override install directory (default: %LOCALAPPDATA%\radstorm\bin)

#Requires -Version 5.1
[CmdletBinding(SupportsShouldProcess)]
param (
    # Pin a specific release tag. Defaults to latest.
    [string]$Version = $env:RADSTORM_VERSION,

    # Override the installation directory.
    [string]$InstallDir = $env:RADSTORM_INSTALL_DIR
)

$ErrorActionPreference = "Stop"

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$Repo            = "Apextech-sys/reflex-radstorm"
$GithubApi       = "https://api.github.com/repos/$Repo/releases/latest"
$GithubReleases  = "https://github.com/$Repo/releases/download"
$QuickstartUrl   = "https://github.com/$Repo/blob/main/docs/QUICKSTART.md"
$Binaries        = @("radstorm", "radstorm-api")

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
function Write-Info  { param([string]$Msg) Write-Host "[radstorm-install] $Msg" -ForegroundColor Green }
function Write-Warn  { param([string]$Msg) Write-Warning "[radstorm-install] $Msg" }
function Fail        { param([string]$Msg) throw "[radstorm-install] ERROR: $Msg" }

# ---------------------------------------------------------------------------
# Detect architecture (only amd64 supported on Windows for now)
# ---------------------------------------------------------------------------
$Arch = $env:PROCESSOR_ARCHITECTURE
switch ($Arch) {
    "AMD64"  { $GoArch = "amd64" }
    "ARM64"  { $GoArch = "arm64" }
    default  { Fail "Unsupported architecture: $Arch" }
}

$Goos = "windows"
Write-Info "Detected platform: $Goos/$GoArch"

# ---------------------------------------------------------------------------
# Resolve version
# ---------------------------------------------------------------------------
if (-not $Version) {
    Write-Info "Fetching latest release from GitHub..."
    try {
        $ReleaseData = Invoke-RestMethod -Uri $GithubApi -Headers @{ "User-Agent" = "radstorm-installer" }
        $Version = $ReleaseData.tag_name
    } catch {
        Fail "Could not fetch latest release from GitHub: $_`nCheck your internet connection or set `$env:RADSTORM_VERSION to pin a version."
    }
    Write-Info "Latest release: $Version"
} else {
    Write-Info "Using pinned version: $Version"
}

# ---------------------------------------------------------------------------
# Determine install directory
# ---------------------------------------------------------------------------
if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "radstorm\bin"
}

Write-Info "Install directory: $InstallDir"

if ($PSCmdlet.ShouldProcess($InstallDir, "Create install directory")) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

# ---------------------------------------------------------------------------
# Download helpers
# ---------------------------------------------------------------------------
function Get-FileFromUrl {
    param([string]$Url, [string]$Dest)
    Write-Info "Downloading: $([System.IO.Path]::GetFileName($Dest))..."
    if ($PSCmdlet.ShouldProcess($Url, "Download")) {
        try {
            $ProgressPreference = "SilentlyContinue"
            Invoke-WebRequest -Uri $Url -OutFile $Dest -UseBasicParsing
        } catch {
            Fail "Download failed: $Url`n$_"
        }
    }
}

function Test-FileChecksum {
    param([string]$FilePath, [string]$Expected)
    if (-not $Expected) {
        Write-Warn "No checksum entry found. Skipping verification."
        return
    }
    Write-Info "Verifying checksum..."
    $Hash = (Get-FileHash -Algorithm SHA256 -Path $FilePath).Hash.ToLower()
    if ($Hash -ne $Expected.ToLower()) {
        Fail "Checksum mismatch for $([System.IO.Path]::GetFileName($FilePath))!`n  Expected: $Expected`n  Got:      $Hash`nThe download may be corrupted or tampered with."
    }
    Write-Info "Checksum OK."
}

# ---------------------------------------------------------------------------
# Download and verify
# ---------------------------------------------------------------------------
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "radstorm-install-$([System.Guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    # Download SHA256SUMS
    $ChecksumsUrl  = "$GithubReleases/$Version/SHA256SUMS"
    $ChecksumsFile = Join-Path $TmpDir "SHA256SUMS"
    Get-FileFromUrl -Url $ChecksumsUrl -Dest $ChecksumsFile

    $ChecksumLines = @{}
    if (Test-Path $ChecksumsFile) {
        Get-Content $ChecksumsFile | ForEach-Object {
            $Parts = $_ -split '\s+', 2
            if ($Parts.Count -eq 2) {
                $ChecksumLines[$Parts[1].Trim()] = $Parts[0].Trim()
            }
        }
    }

    foreach ($Bin in $Binaries) {
        $Artifact    = "${Bin}-${Goos}-${GoArch}.exe"
        $DownloadUrl = "$GithubReleases/$Version/$Artifact"
        $TmpFile     = Join-Path $TmpDir $Artifact
        $DestFile    = Join-Path $InstallDir "${Bin}.exe"

        Get-FileFromUrl -Url $DownloadUrl -Dest $TmpFile

        $ExpectedSum = $ChecksumLines[$Artifact]
        Test-FileChecksum -FilePath $TmpFile -Expected $ExpectedSum

        if ($PSCmdlet.ShouldProcess($DestFile, "Install binary")) {
            Copy-Item -Path $TmpFile -Destination $DestFile -Force
            Write-Info "Installed: $DestFile"
        }
    }
} finally {
    # Clean up temp directory
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}

# ---------------------------------------------------------------------------
# Add install directory to user PATH (if not already present)
# ---------------------------------------------------------------------------
$UserPath = [System.Environment]::GetEnvironmentVariable("PATH", "User") ?? ""
if ($UserPath -notlike "*$InstallDir*") {
    if ($PSCmdlet.ShouldProcess("User PATH", "Add $InstallDir")) {
        $NewPath = "$InstallDir;$UserPath".TrimEnd(";")
        [System.Environment]::SetEnvironmentVariable("PATH", $NewPath, "User")
        Write-Info "Added $InstallDir to your user PATH."
        Write-Warn "Restart your terminal (or open a new PowerShell window) to use radstorm from PATH."
    }
} else {
    Write-Info "$InstallDir is already in your PATH."
}

# ---------------------------------------------------------------------------
# Verify installation
# ---------------------------------------------------------------------------
Write-Info "Verifying installation..."
$RadstormExe = Join-Path $InstallDir "radstorm.exe"
if (Test-Path $RadstormExe) {
    try {
        $InstalledVersion = & $RadstormExe --version 2>&1
        Write-Info "radstorm version: $InstalledVersion"
    } catch {
        Write-Info "radstorm installed at $RadstormExe"
    }
}

# ---------------------------------------------------------------------------
# Next steps
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "Installation complete. radstorm $Version is ready." -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Run your first test:"
Write-Host "       radstorm run-scenario --config your-scenario.toml --out results\my-first-run"
Write-Host ""
Write-Host "  2. Inspect results:"
Write-Host "       radstorm analyze-results results\my-first-run"
Write-Host ""
Write-Host "  3. Full documentation:"
Write-Host "       $QuickstartUrl"
Write-Host ""
Write-Host "To uninstall:"
Write-Host "  Remove-Item -Path '$InstallDir\radstorm.exe', '$InstallDir\radstorm-api.exe'"
Write-Host ""
