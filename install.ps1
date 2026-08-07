#requires -Version 5.1
<#
.SYNOPSIS
    jvm one-line installer (PowerShell).
.DESCRIPTION
    Downloads the latest Release's portable zip, verifies SHA256, extracts to a
    user directory, and runs jvm.exe once to trigger its built-in self-bootstrap
    (registers user PATH + injects PowerShell/bash shell integration).
    Behavior mirrors the NSIS installer (installer/jvm.nsi). Windows x64 only.

    NOTE: Script output is intentionally English. Under `iwr | iex` the response
    body is decoded by the host's default code page; a UTF-8 BOM would be treated
    as content bytes and break parsing on PowerShell 5.1. Keeping the file BOM-less
    with ASCII-only string literals makes `iwr -useb <url> | iex` work everywhere.

    Usage:
      One-line install (all defaults):
        iwr -useb "https://raw.githubusercontent.com/BaixuanZhu/jvm/main/install.ps1" | iex
      Local with params:
        .\install.ps1 -InstallDir "D:\tools\jvm"

    Environment variables:
      JVM_INSTALLER_MIRROR  Download URL prefix override (trailing slash optional).
                            Useful for a mirror / self-hosted source.
                            Defaults to the GitHub Release feed.
      HTTPS_PROXY / HTTP_PROXY  Honored natively by Invoke-WebRequest.
#>
[CmdletBinding()]
param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\jvm')
)

# Force TLS 1.2 (old systems may default to TLS 1.0 only).
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocol]::Tls12
} catch {
    # PS 7+ uses HttpClient and ignores this setting.
}

$ErrorActionPreference = 'Stop'

# -----------------------------------------------------------------------------
# Colored output helpers.
# -----------------------------------------------------------------------------
function Write-Info { param([string]$Msg) Write-Host $Msg -ForegroundColor Cyan }
function Write-Ok   { param([string]$Msg) Write-Host $Msg -ForegroundColor Green }
function Write-Warn { param([string]$Msg) Write-Host $Msg -ForegroundColor Yellow }
function Write-Err  { param([string]$Msg) Write-Host $Msg -ForegroundColor Red }

# -----------------------------------------------------------------------------
# Download a URL to a local file (PS 5.1 compatible; -UseBasicParsing avoids
# the IE engine dependency).
# -----------------------------------------------------------------------------
function Save-Url {
    param([string]$Url, [string]$Destination)
    Invoke-WebRequest -Uri $Url -OutFile $Destination -UseBasicParsing -ErrorAction Stop
}

# -----------------------------------------------------------------------------
# Parse GNU coreutils sha256sum text, supporting both formats:
#   "<hash>  <filename>"  text mode   (two spaces, no asterisk)
#   "<hash> *<filename>"  binary mode (one space + leading asterisk)
# Mirrors internal/upgrade/upgrade.go parseChecksum exactly.
# Returns the lowercase hex hash, or $null if not found.
# -----------------------------------------------------------------------------
function Get-ExpectedChecksum {
    param([string]$ChecksumText, [string]$Filename)
    foreach ($line in ($ChecksumText -split "`n")) {
        $line = $line.Trim()
        if (-not $line) { continue }
        $fields = $line -split '\s+'
        if ($fields.Count -ne 2) { continue }
        $hash = $fields[0]
        $name = $fields[1] -replace '^\*', ''
        if ($name -eq $Filename) { return $hash.ToLower() }
    }
    return $null
}

# =============================================================================
# Main flow
# =============================================================================
try {
    Write-Info '==> jvm installer'

    # ---- 1. Environment check ------------------------------------------------
    if (-not [System.Environment]::Is64BitOperatingSystem) {
        throw 'This system is not 64-bit Windows. jvm supports Windows x64 only.'
    }

    # ---- 2. Resolve download source ------------------------------------------
    $base = if ($env:JVM_INSTALLER_MIRROR) { $env:JVM_INSTALLER_MIRROR.TrimEnd('/') + '/' } else { 'https://github.com/BaixuanZhu/jvm/releases/latest/download/' }
    $zipName = 'jvm-windows-amd64.zip'
    $checksumName = 'checksums.txt'
    $zipUrl = $base + $zipName
    $checksumUrl = $base + $checksumName
    Write-Info "Source: $base"

    # ---- 3. Prepare working directory ----------------------------------------
    $workDir = Join-Path $env:TEMP ("jvm-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $workDir -Force | Out-Null
    $zipPath = Join-Path $workDir $zipName
    $checksumPath = Join-Path $workDir $checksumName

    try {
        # ---- 4. Download the zip ---------------------------------------------
        Write-Info "Downloading $zipName ..."
        Save-Url -Url $zipUrl -Destination $zipPath
        Write-Ok "Downloaded: $zipPath"

        # ---- 5. Verify SHA256 (shares checksums.txt with `jvm upgrade`) ------
        $expected = $null
        $hasChecksum = $false
        try {
            Save-Url -Url $checksumUrl -Destination $checksumPath
            $checksumText = Get-Content -Path $checksumPath -Raw
            $expected = Get-ExpectedChecksum -ChecksumText $checksumText -Filename $zipName
            $hasChecksum = $true
        } catch {
            # Release has no checksums.txt (old version / mirror missing): warn but continue,
            # matching upgrade.go's backward-compatible behavior.
            Write-Warn "Could not fetch $checksumName; skipping SHA256 verification."
        }

        if ($hasChecksum) {
            if (-not $expected) {
                throw "No entry for $zipName in $checksumName; aborting verification."
            }
            $actual = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLower()
            Write-Info "Expected SHA256: $expected"
            Write-Info "Actual   SHA256: $actual"
            if ($actual -ne $expected) {
                throw 'SHA256 verification failed; the download may be corrupted or tampered with. Aborting.'
            }
            Write-Ok 'SHA256 verified'
        }

        # ---- 6. Install dir + handle existing jvm.exe (upgrade scenario) -----
        Write-Info "Install dir: $InstallDir"
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $exePath = Join-Path $InstallDir 'jvm.exe'
        if (Test-Path $exePath) {
            # A running exe cannot be overwritten but can be renamed (mirrors
            # upgrade.go replaceSelf's .bak strategy).
            $bak = Join-Path $InstallDir 'jvm.exe.bak'
            if (Test-Path $bak) { Remove-Item $bak -Force }
            try {
                Rename-Item -Path $exePath -NewName 'jvm.exe.bak' -Force
                Write-Warn 'Existing jvm.exe found; backed up as jvm.exe.bak.'
            } catch {
                throw "Cannot replace the existing jvm.exe (it may be running): $($_.Exception.Message). Close all jvm processes and retry."
            }
        }

        # ---- 7. Extract jvm.exe from the zip (single file, ExtractToFile) ----
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        try {
            $archive = [System.IO.Compression.ZipFile]::OpenRead($zipPath)
            try {
                $entry = $archive.Entries | Where-Object { $_.Name -eq 'jvm.exe' } | Select-Object -First 1
                if (-not $entry) {
                    throw 'jvm.exe not found inside the zip; the package is malformed.'
                }
                [System.IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $exePath, $true)
            } finally {
                $archive.Dispose()
            }
        } catch {
            throw "Extraction failed: $($_.Exception.Message)"
        }
        Write-Ok "Extracted: $exePath"

        # ---- 8. Record install location (matches installer/jvm.nsi
        #         HKCU\Software\jvm\InstallDir, so `jvm upgrade` can reuse the
        #         same dir). We do NOT add an "Add/Remove Programs" entry: there
        #         is no uninstaller for a script install, and a leftover entry
        #         would mislead users. -----------------------------------------
        try {
            New-Item -Path 'HKCU:\Software\jvm' -Force | Out-Null
            Set-ItemProperty -Path 'HKCU:\Software\jvm' -Name 'InstallDir' -Value $InstallDir
        } catch {
            # Non-fatal: jvm's bootstrap still derives its dir via os.Executable().
            Write-Warn "Failed to write HKCU\Software\jvm\InstallDir (non-fatal): $($_.Exception.Message)"
        }

        # ---- 9. Run jvm.exe once to trigger self-bootstrap
        # (registers user PATH + injects shell integration). This is the
        # equivalent of NSIS SecConfig (installer/jvm.nsi:80-86). -------------
        Write-Info 'Triggering bootstrap (PATH + shell integration) ...'
        try {
            $versionOutput = & $exePath version 2>&1 | Out-String
            $versionLine = ($versionOutput -split "`n" | Where-Object { $_ -match '\S' } | Select-Object -First 1)
        } catch {
            # Bootstrap failure does not roll back the extracted exe; the user
            # can manually run `jvm version` to diagnose.
            Write-Warn "First run of jvm.exe failed (PATH/shell integration may be incomplete): $($_.Exception.Message)"
        }

        # ---- 10. Done --------------------------------------------------------
        Write-Ok ''
        Write-Ok 'Installation complete.'
        if ($versionLine) { Write-Ok "   version: $($versionLine.Trim())" }
        Write-Ok "   path:    $exePath"
        Write-Info ''
        Write-Info 'Next steps:'
        Write-Info '  1. Open a new terminal (so PATH and shell integration take effect)'
        Write-Info '  2. jvm install 21      # install a JDK'
        Write-Info '  3. jvm use 21          # switch to it'
        Write-Info ''
        Write-Info 'Uninstall: delete the install directory (user data in ~/.jvm is untouched).'
    } finally {
        # Clean up the temp dir (on both success and failure).
        if (Test-Path $workDir) {
            Remove-Item -Path $workDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
} catch {
    # Friendly message + rethrow so `.\install.ps1` exits non-zero.
    # Under `iwr | iex`, throw does not close the session, it just prints the error record.
    Write-Err ''
    Write-Err "Installation failed: $($_.Exception.Message)"
    throw
}
