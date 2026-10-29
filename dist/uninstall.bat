@echo off
chcp 65001 >nul
echo ========================================================
echo   Удаление OpenWRT Monitor из системы
echo ========================================================
echo.

taskkill /f /im openwrtmon.exe 2>nul
timeout /t 1 /nobreak >nul

reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "OpenWRTMon" /f 2>nul

powershell -Command "Remove-Item ([Environment]::GetFolderPath('Programs') + '\OpenWRT Monitor.lnk') -ErrorAction SilentlyContinue"

set "TARGET_DIR=%LOCALAPPDATA%\Programs\OpenWRTMon"
if exist "%TARGET_DIR%" rmdir /s /q "%TARGET_DIR%"

echo.
echo ========================================================
echo   OpenWRT Monitor полностью удален из системы.
echo ========================================================
echo.
pause

