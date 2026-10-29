@echo off
chcp 65001 >nul
echo ========================================================
echo   Установка OpenWRT Monitor в систему Windows
echo ========================================================
echo.

set "TARGET_DIR=%LOCALAPPDATA%\Programs\OpenWRTMon"

echo 1. Остановка работающих процессов...
taskkill /f /im openwrtmon.exe 2>nul
timeout /t 1 /nobreak >nul

echo 2. Создание постоянной папки: "%TARGET_DIR%"
if not exist "%TARGET_DIR%" mkdir "%TARGET_DIR%"

echo 3. Копирование файлов...
copy /y "%~dp0openwrtmon.exe" "%TARGET_DIR%\openwrtmon.exe" >nul
if exist "%~dp0config.ini" copy /y "%~dp0config.ini" "%TARGET_DIR%\config.ini" >nul
if exist "%~dp0dist\config.ini" copy /y "%~dp0dist\config.ini" "%TARGET_DIR%\config.ini" >nul

echo 4. Создание ярлыка в меню «Пуск»...
powershell -Command "$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut([Environment]::GetFolderPath('Programs') + '\OpenWRT Monitor.lnk'); $s.TargetPath = '%TARGET_DIR%\openwrtmon.exe'; $s.WorkingDirectory = '%TARGET_DIR%'; $s.Save()"

echo 5. Настройка автозапуска при включении компьютера...
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "OpenWRTMon" /t REG_SZ /d "\"%TARGET_DIR%\openwrtmon.exe\"" /f >nul

echo 6. Запуск установленной программы...
start "" "%TARGET_DIR%\openwrtmon.exe"

echo.
echo ========================================================
echo   Установка завершена успешно!
echo.
echo   Программа установлена в постоянную системную папку:
echo   %TARGET_DIR%
echo.
echo   • Ярлык добавлен в меню «Пуск»
echo   • Автозапуск с Windows активирован
echo   • Теперь эту исходную папку можно удалять или перемещать!
echo ========================================================
echo.
pause

