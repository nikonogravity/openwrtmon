@echo off
setlocal
cd /d "%~dp0\.."

set PATH=C:\Program Files\Go\bin;%PATH%
set CGO_ENABLED=0

echo ==========================================
echo  Сборка OpenWRT Monitor (Go Edition)
echo ==========================================
echo.

if not exist "dist" mkdir "dist"

echo [1/2] Сборка для Windows (openwrtmon.exe, без черного окна)...
set GOOS=windows
set GOARCH=amd64
go build -ldflags="-s -w -H windowsgui" -o "dist\openwrtmon.exe" ./cmd/openwrtmon
if %ERRORLEVEL% EQU 0 (echo  -> Успешно: dist\openwrtmon.exe) else (echo  -> ОШИБКА Windows)

echo.
echo [2/2] Сборка для Linux x64 (openwrtmon-linux-amd64)...
set GOOS=linux
set GOARCH=amd64
go build -ldflags="-s -w" -o "dist\openwrtmon-linux-amd64" ./cmd/openwrtmon
if %ERRORLEVEL% EQU 0 (echo  -> Успешно: dist\openwrtmon-linux-amd64) else (echo  -> ОШИБКА Linux)

echo.
echo Копирование config.ini в dist\...
copy /Y config.ini dist\config.ini >nul

echo.
echo ==========================================
echo  Сборка завершена! Бинарные файлы в папке dist\
echo  Для запуска на Windows: dist\openwrtmon.exe
echo  Для установки в систему запустите: install.ps1
echo ==========================================
pause
