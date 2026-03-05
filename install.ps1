# install.ps1 — Windows installer for atlaskb
# Usage: irm https://raw.githubusercontent.com/tgeorge06/atlaskb/main/install.ps1 | iex
#        .\install.ps1 [-Prefix <path>] [-Version <tag>] [-DryRun]
param(
    [string]$Prefix = "",
    [string]$Version = "",
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$Repo = "tgeorge06/atlaskb"
$GithubApi = "https://api.github.com"
$GithubDl = "https://github.com"

if ($Prefix) {
    $InstallDir = $Prefix
} elseif ($env:ATLASKB_INSTALL_DIR) {
    $InstallDir = $env:ATLASKB_INSTALL_DIR
} else {
    $InstallDir = Join-Path $env:LOCALAPPDATA "atlaskb\bin"
}

function Write-Info { param($Msg) Write-Host "info " -ForegroundColor Green -NoNewline; Write-Host " $Msg" }
function Write-Warn { param($Msg) Write-Host "warn " -ForegroundColor Yellow -NoNewline; Write-Host " $Msg" }
function Write-Err { param($Msg) Write-Host "error" -ForegroundColor Red -NoNewline; Write-Host " $Msg"; exit 1 }

function Get-ResolvedVersion {
    if ($Version) {
        if (-not $Version.StartsWith("v")) { $Version = "v$Version" }
        $script:Version = $Version
        Write-Info "Using specified version: $script:Version"
        return $script:Version
    }

    Write-Info "Fetching latest release..."
    $release = Invoke-RestMethod -Uri "$GithubApi/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "atlaskb-installer" }
    $script:Version = $release.tag_name
    Write-Info "Latest version: $script:Version"
    return $script:Version
}

function Test-Checksum {
    param($TarballPath, $ChecksumsPath)

    $filename = Split-Path $TarballPath -Leaf
    $line = Get-Content $ChecksumsPath | Where-Object { $_ -match [regex]::Escape($filename) } | Select-Object -First 1
    if (-not $line) {
        Write-Warn "No checksum found for $filename, skipping verification"
        return
    }

    $expected = ($line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash $TarballPath -Algorithm SHA256).Hash.ToLower()

    if ($expected -ne $actual) {
        Write-Err "Checksum mismatch!`n  Expected: $expected`n  Got:      $actual"
    }
    Write-Info "Checksum verified"
}

function Install-AtlasKB {
    $tarball = "atlaskb-windows-x86_64.tar.gz"
    $downloadUrl = "$GithubDl/$Repo/releases/download/$Version/$tarball"
    $checksumsUrl = "$GithubDl/$Repo/releases/download/$Version/checksums.sha256"

    if ($DryRun) {
        Write-Host ""
        Write-Host "Dry run - would perform:"
        Write-Host "  1. Download  $downloadUrl"
        Write-Host "  2. Download  $checksumsUrl"
        Write-Host "  3. Verify    SHA256 checksum"
        Write-Host "  4. Extract   atlaskb.exe to $InstallDir\atlaskb.exe"
        Write-Host "  5. Health    atlaskb version"
        return
    }

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "atlaskb-install-$(Get-Random)"
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        Write-Info "Downloading $tarball..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile (Join-Path $tmpDir $tarball) -UseBasicParsing

        Write-Info "Downloading checksums..."
        try {
            Invoke-WebRequest -Uri $checksumsUrl -OutFile (Join-Path $tmpDir "checksums.sha256") -UseBasicParsing
            Test-Checksum -TarballPath (Join-Path $tmpDir $tarball) -ChecksumsPath (Join-Path $tmpDir "checksums.sha256")
        } catch {
            Write-Warn "checksums.sha256 not found in release, skipping verification"
        }

        Write-Info "Extracting..."
        tar xzf (Join-Path $tmpDir $tarball) -C $tmpDir

        $exe = Get-ChildItem -Path $tmpDir -Recurse -Filter "atlaskb.exe" | Select-Object -First 1
        if (-not $exe) {
            Write-Err "Could not find atlaskb.exe in archive"
        }

        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        Copy-Item $exe.FullName (Join-Path $InstallDir "atlaskb.exe") -Force
        Write-Info "Installed to $InstallDir\atlaskb.exe"
    } finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }
}

function Ensure-Path {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if ($userPath) { $parts = $userPath -split ';' }
    if ($parts -contains $InstallDir) { return }

    Write-Warn "$InstallDir is not in your PATH"
    $answer = Read-Host "Add $InstallDir to your user PATH? [Y/n]"
    if ($answer -eq '' -or $answer -match '^[Yy]') {
        [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
        $env:Path = "$env:Path;$InstallDir"
        Write-Info "Added to user PATH (active in new terminals)"
    } else {
        Write-Host "Add it manually in System Settings > Environment Variables."
    }
}

function Test-Health {
    if ($DryRun) { return }
    $exe = Join-Path $InstallDir "atlaskb.exe"
    try {
        $ver = & $exe version 2>&1 | Select-Object -First 1
        if ($LASTEXITCODE -eq 0 -and $ver) {
            Write-Info "Verified: $ver"
        } else {
            Write-Warn "Binary installed at $exe"
        }
    } catch {
        Write-Warn "Binary installed at $exe"
    }
}

Write-Host ""
Write-Host "  atlaskb installer"
Write-Host ""
Write-Host "Scoop alternative:"
Write-Host "  scoop bucket add atlaskb https://github.com/tgeorge06/scoop-atlaskb"
Write-Host "  scoop install atlaskb"
Write-Host ""

Get-ResolvedVersion | Out-Null
Install-AtlasKB

if (-not $DryRun) {
    Ensure-Path
    Test-Health
    Write-Host ""
    Write-Host "  atlaskb $Version installed successfully!" -ForegroundColor Green
    Write-Host "  Run atlaskb setup then atlaskb to get started."
    Write-Host ""
}
