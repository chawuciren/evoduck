[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$Owner = 'chawuciren'
$Repo = 'evoduck'
$BinName = 'evoduck.exe'
$InstallDir = if ($env:EVODUCK_INSTALL_DIR) { $env:EVODUCK_INSTALL_DIR } else { Join-Path $HOME '.local\bin' }

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

AI Agent Gateway | installer

'@
}

function Get-PlatformAssetName {
    $arch = ''
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    }
    catch {
        $arch = ''
    }

    if ([string]::IsNullOrWhiteSpace($arch)) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    if ([string]::IsNullOrWhiteSpace($arch)) {
        $arch = $env:PROCESSOR_ARCHITECTURE
    }

    switch ($arch.ToLowerInvariant()) {
        'x64' { return 'evoduck-windows-amd64.zip' }
        'amd64' { return 'evoduck-windows-amd64.zip' }
        'arm64' { return 'evoduck-windows-arm64.zip' }
        'aarch64' { return 'evoduck-windows-arm64.zip' }
        default { throw "Unsupported architecture: $arch (PROCESSOR_ARCHITECTURE=$env:PROCESSOR_ARCHITECTURE, PROCESSOR_ARCHITEW6432=$env:PROCESSOR_ARCHITEW6432)" }
    }
}

function Get-DownloadUrl {
    param([string]$AssetName)

    $version = if ($env:EVODUCK_VERSION) { $env:EVODUCK_VERSION } else { 'latest' }
    if ($version -eq 'latest') {
        return "https://github.com/$Owner/$Repo/releases/latest/download/$AssetName"
    }

    return "https://github.com/$Owner/$Repo/releases/download/$version/$AssetName"
}

function Get-ProxyUrl {
    if ($env:EVODUCK_PROXY) { return $env:EVODUCK_PROXY }
    if ($env:HTTPS_PROXY) { return $env:HTTPS_PROXY }
    if ($env:https_proxy) { return $env:https_proxy }
    if ($env:HTTP_PROXY) { return $env:HTTP_PROXY }
    if ($env:http_proxy) { return $env:http_proxy }
    return ''
}

function Invoke-DownloadFile {
    param(
        [string]$Uri,
        [string]$OutFile
    )

    $proxy = Get-ProxyUrl
    if ([string]::IsNullOrWhiteSpace($proxy)) {
        Invoke-WebRequest -Uri $Uri -OutFile $OutFile
        return
    }

    Write-Log "Using proxy $proxy"
    Invoke-WebRequest -Uri $Uri -OutFile $OutFile -Proxy $proxy
}

function Ensure-InstallDir {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}

function Install-Binary {
    param([string]$AssetName)

    $url = Get-DownloadUrl -AssetName $AssetName
    $tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("evoduck-install-" + [System.Guid]::NewGuid().ToString('N'))
    $archivePath = Join-Path $tempRoot $AssetName

    New-Item -ItemType Directory -Force -Path $tempRoot | Out-Null
    try {
        Write-Log "Downloading $url"
        Invoke-DownloadFile -Uri $url -OutFile $archivePath
        Expand-Archive -Path $archivePath -DestinationPath $tempRoot -Force

        $binaryPath = Join-Path $tempRoot $BinName
        if (-not (Test-Path $binaryPath)) {
            throw "Archive does not contain $BinName at its root"
        }

        Replace-Binary -Source $binaryPath -Destination (Join-Path $InstallDir $BinName)
    }
    finally {
        if (Test-Path $tempRoot) {
            Remove-Item -Path $tempRoot -Recurse -Force
        }
    }
}

function Replace-Binary {
    param(
        [string]$Source,
        [string]$Destination
    )

    $newPath = "$Destination.new"
    $backupPath = "$Destination.bak"
    Copy-Item -Path $Source -Destination $newPath -Force

    if (Test-Path $backupPath) {
        Remove-Item -Path $backupPath -Force
    }
    if (Test-Path $Destination) {
        Rename-Item -Path $Destination -NewName ([System.IO.Path]::GetFileName($backupPath)) -Force
    }
    try {
        Move-Item -Path $newPath -Destination $Destination -Force
        if (Test-Path $backupPath) {
            Remove-Item -Path $backupPath -Force
        }
    }
    catch {
        if ((-not (Test-Path $Destination)) -and (Test-Path $backupPath)) {
            Rename-Item -Path $backupPath -NewName ([System.IO.Path]::GetFileName($Destination)) -Force
        }
        throw
    }
}

function Get-ServiceState {
    $svc = Get-Service -Name 'EvoDuck' -ErrorAction SilentlyContinue
    if ($null -eq $svc) {
        return ''
    }
    return $svc.Status.ToString()
}

function Register-Autostart {
    param([string]$TargetPath)

    try {
        & $TargetPath install
        if ($LASTEXITCODE -eq 0) {
            Write-Log 'Registered autostart configuration'
            return
        }
    }
    catch {
    }

    Write-Log "Warning: failed to register autostart configuration via $TargetPath install"
}

function Update-ExistingBinary {
    param([string]$AssetName)

    $target = Join-Path $InstallDir $BinName
    Write-Log 'Existing EvoDuck installation detected, updating...'

    try {
        & $target update
        if ($LASTEXITCODE -eq 0) {
            Register-Autostart -TargetPath $target
            return
        }
    }
    catch {
        Write-Log "Built-in update failed or is unavailable, falling back to script update: $($_.Exception.Message)"
    }

    $state = Get-ServiceState
    $wasRunning = $state -eq 'Running'
    if ($wasRunning) {
        Write-Log 'Stopping EvoDuck service before fallback update...'
        & $target service stop
    }

    Install-Binary -AssetName $AssetName
    Register-Autostart -TargetPath $target
    if ($wasRunning) {
        & $target service start
    }
}

function Ensure-UserPath {
    $currentUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = @()
    if ($currentUserPath) {
        $pathEntries = $currentUserPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
    }

    if ($pathEntries -contains $InstallDir) {
        return
    }

    $newUserPath = if ([string]::IsNullOrWhiteSpace($currentUserPath)) {
        $InstallDir
    }
    else {
        "$currentUserPath;$InstallDir"
    }

    [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')

    if (-not (($env:Path -split ';') -contains $InstallDir)) {
        $env:Path = "$env:Path;$InstallDir"
    }

    Write-Log "Added $InstallDir to the user PATH"
    Write-Log 'Open a new PowerShell session to pick up the persisted PATH change.'
}

function Main {
    Write-BrandHeader
    Ensure-InstallDir
    $assetName = Get-PlatformAssetName
    $target = Join-Path $InstallDir $BinName
    if (Test-Path $target) {
        Update-ExistingBinary -AssetName $assetName
        Write-Log "Updated $BinName at $target"
    }
    else {
        Write-Log 'Installing EvoDuck...'
        Install-Binary -AssetName $assetName
        Register-Autostart -TargetPath $target
        Write-Log "Installed $BinName to $target"
    }
    Ensure-UserPath
    Write-Log "Runtime data remains under $(Join-Path $HOME '.evoduck')"
    Write-Log 'Run: evoduck.exe --help'
}

Main
