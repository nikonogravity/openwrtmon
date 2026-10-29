# Self-contained PowerShell Installer for OpenWRT Monitor (Windows)
$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Установка OpenWRT Monitor (Go Edition) " -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

$installDir = "$env:LOCALAPPDATA\OpenWRTMonitor"
$exeSource = "$PSScriptRoot\dist\openwrtmon.exe"
$configSource = "$PSScriptRoot\config.ini"

if (-not (Test-Path $exeSource)) {
    # If not in dist, check current folder
    if (Test-Path "$PSScriptRoot\openwrtmon.exe") {
        $exeSource = "$PSScriptRoot\openwrtmon.exe"
    } else {
        Write-Host "Сборка исполняемого файла..." -ForegroundColor Yellow
        $env:PATH = "C:\Program Files\Go\bin;$env:PATH"
        $env:CGO_ENABLED = "0"
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        & "C:\Program Files\Go\bin\go.exe" build -ldflags="-s -w -H windowsgui" -o "$PSScriptRoot\dist\openwrtmon.exe" "$PSScriptRoot\cmd\openwrtmon"
        $exeSource = "$PSScriptRoot\dist\openwrtmon.exe"
    }
}

# 1. Create target directory
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# 2. Stop running instance if any
Get-Process -Name "openwrtmon" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Milliseconds 500

# 3. Copy executable
Copy-Item -Path $exeSource -Destination "$installDir\openwrtmon.exe" -Force
Write-Host "✓ Файл openwrtmon.exe скопирован в $installDir" -ForegroundColor Green

# 4. Copy config.ini only if not already present (so user settings are not overwritten)
if (-not (Test-Path "$installDir\config.ini")) {
    Copy-Item -Path $configSource -Destination "$installDir\config.ini" -Force
    Write-Host "✓ Создан начальный файл config.ini" -ForegroundColor Green
} else {
    Write-Host "ℹ Существующий config.ini сохранён" -ForegroundColor DarkGray
}

# 5. Create Desktop shortcut
$wsh = New-Object -ComObject WScript.Shell
$desktopPath = [Environment]::GetFolderPath("Desktop")
$shortcut = $wsh.CreateShortcut("$desktopPath\OpenWRT Monitor.lnk")
$shortcut.TargetPath = "$installDir\openwrtmon.exe"
$shortcut.WorkingDirectory = $installDir
$shortcut.Description = "Мониторинг OpenWRT и управление VPN"
$shortcut.Save()
Write-Host "✓ Ярлык на Рабочем столе создан" -ForegroundColor Green

# 6. Create Windows Startup shortcut (Auto-launch)
$startupPath = [Environment]::GetFolderPath("Startup")
$startupShortcut = $wsh.CreateShortcut("$startupPath\OpenWRT Monitor.lnk")
$startupShortcut.TargetPath = "$installDir\openwrtmon.exe"
$startupShortcut.WorkingDirectory = $installDir
$startupShortcut.Description = "OpenWRT Monitor Автозагрузка"
$startupShortcut.Save()
Write-Host "✓ Добавлено в автозагрузку Windows" -ForegroundColor Green

# 7. Start application now
Start-Process -FilePath "$installDir\openwrtmon.exe" -WorkingDirectory $installDir
Write-Host ""
Write-Host "🎉 Установка успешно завершена!" -ForegroundColor Green
Write-Host "Иконка приложения уже появилась в системном трее." -ForegroundColor Cyan
Write-Host "Двойной клик по иконке или пункт 'Панель управления' откроет красивый интерфейс." -ForegroundColor Cyan
Write-Host ""
pause

