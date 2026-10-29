package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed dist_files/openwrtmon.exe
var binOpenwrtmon []byte

//go:embed dist_files/config.ini
var binConfig []byte

func showMessageBox(title, message string, isError bool) {
	pTitle, _ := windows.UTF16PtrFromString(title)
	pMsg, _ := windows.UTF16PtrFromString(message)
	var flags uint32 = windows.MB_OK | windows.MB_SETFOREGROUND | windows.MB_TOPMOST
	if isError {
		flags |= windows.MB_ICONERROR
	} else {
		flags |= windows.MB_ICONINFORMATION
	}
	windows.MessageBox(0, pMsg, pTitle, flags)
}

func killRunningProcesses() {
	cmd := exec.Command("taskkill", "/F", "/IM", "openwrtmon.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
	time.Sleep(300 * time.Millisecond)
}

func getTargetDir() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(localAppData, "Programs", "OpenWRTMon")
}

func getStartMenuDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
}

func createShortcut(targetExe, shortcutPath, description string) error {
	psScript := fmt.Sprintf(`
$ws = New-Object -ComObject WScript.Shell
$s = $ws.CreateShortcut('%s')
$s.TargetPath = '%s'
$s.WorkingDirectory = '%s'
$s.IconLocation = '%s,0'
$s.Description = '%s'
$s.Save()
`, strings.ReplaceAll(shortcutPath, "'", "''"),
		strings.ReplaceAll(targetExe, "'", "''"),
		strings.ReplaceAll(filepath.Dir(targetExe), "'", "''"),
		strings.ReplaceAll(targetExe, "'", "''"),
		strings.ReplaceAll(description, "'", "''"))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func registerAutostart(targetExe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("OpenWRTMon", fmt.Sprintf(`"%s"`, targetExe))
}

func unregisterAutostart() {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err == nil {
		defer k.Close()
		_ = k.DeleteValue("OpenWRTMon")
	}
}

func registerUninstall(targetDir, targetExe string) error {
	uninstallKeyPath := `Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenWRTMon`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	uninstallExe := filepath.Join(targetDir, "uninstall.exe")
	_ = k.SetStringValue("DisplayName", "OpenWRT Monitor")
	_ = k.SetStringValue("DisplayVersion", "1.0.0")
	_ = k.SetStringValue("Publisher", "OpenWRT Monitor")
	_ = k.SetStringValue("DisplayIcon", fmt.Sprintf(`"%s",0`, targetExe))
	_ = k.SetStringValue("InstallLocation", targetDir)
	_ = k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" --uninstall`, uninstallExe))
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	return nil
}

func unregisterUninstall() {
	_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenWRTMon`)
}

func startInstalledApp(targetExe string) error {
	verbPtr, _ := windows.UTF16PtrFromString("open")
	exePtr, _ := windows.UTF16PtrFromString(targetExe)
	dirPtr, _ := windows.UTF16PtrFromString(filepath.Dir(targetExe))
	s32 := windows.NewLazySystemDLL("shell32.dll")
	pShellExecute := s32.NewProc("ShellExecuteW")
	r, _, err := pShellExecute.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(exePtr)), 0, uintptr(unsafe.Pointer(dirPtr)), 1)
	if r <= 32 {
		return fmt.Errorf("ShellExecute failed: %v", err)
	}
	return nil
}

func runUninstall() {
	killRunningProcesses()
	unregisterAutostart()
	unregisterUninstall()

	startMenuShortcut := filepath.Join(getStartMenuDir(), "OpenWRT Monitor.lnk")
	_ = os.Remove(startMenuShortcut)

	targetDir := getTargetDir()

	// Self-deletion command for the installation folder
	cleanCmd := fmt.Sprintf(`cmd.exe /c "timeout /t 1 /nobreak >nul & rd /s /q \"%s\""`, targetDir)
	cmd := exec.Command("cmd.exe", "/c", "start", "/b", "", "cmd.exe", "/c", fmt.Sprintf("timeout /t 1 >nul & rd /s /q \"%s\"", targetDir))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
	_ = cleanCmd

	showMessageBox("OpenWRT Monitor — Удаление",
		"OpenWRT Monitor успешно удален с вашего компьютера.",
		false)
}

func runInstall() {
	killRunningProcesses()

	targetDir := getTargetDir()
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		showMessageBox("Ошибка установки", fmt.Sprintf("Не удалось создать папку:\n%s\n%v", targetDir, err), true)
		return
	}

	targetExe := filepath.Join(targetDir, "openwrtmon.exe")
	if err := os.WriteFile(targetExe, binOpenwrtmon, 0755); err != nil {
		showMessageBox("Ошибка установки", fmt.Sprintf("Не удалось записать файл openwrtmon.exe:\n%v", err), true)
		return
	}

	targetConfig := filepath.Join(targetDir, "config.ini")
	// Only create config if not existing to preserve user credentials across updates
	if _, err := os.Stat(targetConfig); os.IsNotExist(err) {
		_ = os.WriteFile(targetConfig, binConfig, 0644)
	}

	// Copy installer as uninstaller
	currentExe, err := os.Executable()
	if err == nil {
		selfBytes, errRead := os.ReadFile(currentExe)
		if errRead == nil {
			_ = os.WriteFile(filepath.Join(targetDir, "uninstall.exe"), selfBytes, 0755)
		}
	}

	// Create Start Menu Shortcut
	startMenuDir := getStartMenuDir()
	_ = os.MkdirAll(startMenuDir, 0755)
	shortcutPath := filepath.Join(startMenuDir, "OpenWRT Monitor.lnk")
	_ = createShortcut(targetExe, shortcutPath, "Мониторинг OpenWRT и переключение AmneziaWG")

	// Register Autostart & Add/Remove Programs
	_ = registerAutostart(targetExe)
	_ = registerUninstall(targetDir, targetExe)

	// Launch installed app immediately in tray
	_ = startInstalledApp(targetExe)

	// Success dialog
	msg := fmt.Sprintf(
		"✓ OpenWRT Monitor успешно установлен!\n\n"+
			"• Программа запущена и работает в трее (возле часов).\n"+
			"• Включен автозапуск при старте Windows.\n"+
			"• Ярлык добавлен в меню «Пуск».\n\n"+
			"Папка установки:\n%s\n\n"+
			"Этот установочный файл теперь можно удалить.",
		targetDir,
	)
	showMessageBox("OpenWRT Monitor — Установка завершена", msg, false)
}

func main() {
	if len(os.Args) > 1 {
		arg := strings.ToLower(os.Args[1])
		if arg == "--uninstall" || arg == "-u" || arg == "/u" {
			runUninstall()
			return
		}
	}

	runInstall()
}

