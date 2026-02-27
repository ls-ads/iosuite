# ioSuite Uninstallation Script for Windows (PowerShell)

$ST_BIN = Join-Path $HOME ".local\bin"

Write-Host "Uninstalling ioSuite tools from $ST_BIN..."

if (Test-Path (Join-Path $ST_BIN "ioimg.exe")) { Remove-Item (Join-Path $ST_BIN "ioimg.exe") }
if (Test-Path (Join-Path $ST_BIN "iovid.exe")) { Remove-Item (Join-Path $ST_BIN "iovid.exe") }

# Remove shell completions
if ($PROFILE -and (Test-Path $PROFILE)) {
    $Marker = "# ioSuite shell completion"
    $Content = Get-Content $PROFILE
    if ($Content -contains $Marker) {
        Write-Host "Removing shell completion from PowerShell profile..."
        $NewContent = $Content | Where-Object { 
            $_ -notmatch "# ioSuite shell completion" -and 
            $_ -notmatch "ioimg completion" -and 
            $_ -notmatch "iovid completion" 
        }
        $NewContent | Set-Content $PROFILE
    }
}

Write-Host "Uninstallation complete!"
