[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$BinName = 'evoduck.exe'
$InstallDir = if ($env:EVODUCK_INSTALL_DIR) { $env:EVODUCK_INSTALL_DIR } else { Join-Path $HOME '.local\bin' }
$Target = Join-Path $InstallDir $BinName
$DataDir = Join-Path $HOME '.evoduck'

function Write-Log {
    param([string]$Message)
    Write-Host $Message
}

function Write-BrandHeader {
    Write-Host @'
                    ░░░░
                ██████████░░
              ██████████████░
            ████  ██  ████████░
            ████  ██  ██████████  ██░░
        ░░██████████████████████ ██▓▓
      ░░██████████████▓▓██████▓▓
      ███████████████▓▓▓▓██████░
      ████████████▓▓▓▓▓▓████░░
        ████████▓▓▓▓▓▓██░░
          ░░██      ██
            ██      ██

████████╗██╗   ██╗ ██████╗ ██████╗ ██╗   ██╗ ██████╗██╗  ██╗
██╔════╝██║   ██║██╔═══██╗██╔══██╗██║   ██║██╔════╝██║ ██╔╝
█████╗  ██║   ██║██║   ██║██║  ██║██║   ██║██║     █████╔╝
██╔══╝  ╚██╗ ██╔╝██║   ██║██║  ██║██║   ██║██║     ██╔═██╗
███████╗ ╚████╔╝ ╚██████╔╝██████╔╝╚██████╔╝╚██████╗██║  ██╗
╚══════╝  ╚═══╝   ╚═════╝ ╚═════╝  ╚═════╝  ╚═════╝╚═╝  ╚═╝
 ░░░░░░    ░░░     ░░░░░   ░░░░░    ░░░░░    ░░░░░  ░░   ░░

AI Agent Gateway | uninstaller

'@
}

function Remove-AutostartDirect {
    $startupDir = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Startup'
    $autostartPath = Join-Path $startupDir 'evoduck.bat'
    if (Test-Path $autostartPath) {
        Remove-Item -Path $autostartPath -Force
        Write-Log "Removed autostart entry at $autostartPath"
    }
}

function Stop-ServiceProcess {
    if (-not (Test-Path $Target)) {
        return
    }

    try {
        & $Target service stop | Out-Null
        if ($LASTEXITCODE -eq 0) {
            Write-Log 'Stopped EvoDuck service'
            return
        }
    }
    catch {
    }

    Write-Log "Warning: failed to stop EvoDuck service via $Target service stop"
}

function Remove-Autostart {
    if (Test-Path $Target) {
        try {
            & $Target uninstall | Out-Null
            if ($LASTEXITCODE -eq 0) {
                Write-Log 'Removed autostart configuration'
                return
            }
        }
        catch {
            Write-Log "Built-in autostart removal failed, falling back to direct cleanup: $($_.Exception.Message)"
        }
    }

    Remove-AutostartDirect
}

function Remove-BinaryArtifacts {
    $removed = $false
    foreach ($path in @($Target, "$Target.new", "$Target.bak")) {
        if (-not (Test-Path $path)) {
            continue
        }

        try {
            Remove-Item -Path $path -Force
            $removed = $true
            Write-Log "Removed $path"
        }
        catch {
            Write-Log "Could not remove ${path}: $($_.Exception.Message)"
        }
    }

    if (-not $removed) {
        Write-Log "No installed EvoDuck binary found in $InstallDir"
    }
}

function Remove-UserPathEntry {
    $currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ([string]::IsNullOrWhiteSpace($currentUserPath)) {
        return
    }

    $entries = @($currentUserPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries))
    $filtered = @()
    foreach ($entry in $entries) {
        if ([string]::Equals($entry.Trim(), $InstallDir, [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        $filtered += $entry
    }

    if ($filtered.Count -eq $entries.Count) {
        return
    }

    $newUserPath = [string]::Join(';', $filtered)
    [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')

    $sessionEntries = @($env:Path.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries))
    $sessionFiltered = @()
    foreach ($entry in $sessionEntries) {
        if ([string]::Equals($entry.Trim(), $InstallDir, [System.StringComparison]::OrdinalIgnoreCase)) {
            continue
        }
        $sessionFiltered += $entry
    }
    $env:Path = [string]::Join(';', $sessionFiltered)

    Write-Log "Removed $InstallDir from the user PATH"
}

function Prompt-RemoveData {
    if (-not (Test-Path $DataDir)) {
        return
    }

    $answer = Read-Host "Remove runtime data at $DataDir? [y/N]"
    if ($answer -match '^(?i:y|yes)$') {
        Remove-Item -Path $DataDir -Recurse -Force
        Write-Log "Removed runtime data at $DataDir"
        return
    }

    Write-Log "Keeping runtime data at $DataDir"
}

function Main {
    Write-BrandHeader
    Stop-ServiceProcess
    Remove-Autostart
    Remove-BinaryArtifacts
    Remove-UserPathEntry
    Prompt-RemoveData
    Write-Log 'EvoDuck uninstall finished.'
}

Main
