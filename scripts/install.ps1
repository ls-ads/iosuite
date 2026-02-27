# ioSuite Installation Script for Windows (PowerShell)
# Downloads pre-built binaries from GitHub Releases

$ErrorActionPreference = "Stop"

$ReleaseVersion = "v0.1.0"
$BaseUrl = "https://github.com/ls-ads/iosuite/releases/download/$ReleaseVersion"
$InstallDir = Join-Path $HOME ".local\bin"

if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir
}

# Detect Architecture
$Arch = "amd64" # Default for most Windows systems
if ([Environment]::Is64BitProcess -and [IntPtr]::Size -eq 8) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
        $Arch = "arm64"
    }
}

function Download-Binary {
    param($Name)
    $BinaryName = "${Name}-windows-${Arch}.exe"
    $Url = "$BaseUrl/$BinaryName"
    $Dst = Join-Path $InstallDir "${Name}.exe"

    Write-Host "Downloading $Name (windows/$Arch) from $Url..."
    Invoke-WebRequest -Uri $Url -OutFile $Dst
}

Download-Binary "ioimg"
Download-Binary "iovid"

# Shell Completion Setup
if ($PROFILE) {
    $Marker = "# ioSuite shell completion"
    $ProfilePath = $PROFILE
    
    if (!(Test-Path $ProfilePath)) {
        New-Item -ItemType File -Path $ProfilePath -Force
    }

    $ProfileContent = Get-Content $ProfilePath
    if (!($ProfileContent -contains $Marker)) {
        Write-Host "Adding shell completion to PowerShell profile..."
        Add-Content $ProfilePath "`n$Marker"
        Add-Content $ProfilePath 'if (Get-Command ioimg -ErrorAction SilentlyContinue) { ioimg completion powershell | Out-String | Invoke-Expression }'
        Add-Content $ProfilePath 'if (Get-Command iovid -ErrorAction SilentlyContinue) { iovid completion powershell | Out-String | Invoke-Expression }'
    }
}

Write-Host "Installation complete! Please restart your terminal for changes to take effect."
