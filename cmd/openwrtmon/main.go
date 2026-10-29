package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"openwrtmon/internal/autostart"
	"openwrtmon/internal/config"
	"openwrtmon/internal/flyout"
	"openwrtmon/internal/icon"
	"openwrtmon/internal/ipgeo"
	"openwrtmon/internal/luci"
	"openwrtmon/internal/systray"
)

type App struct {
	cfg        *config.Config
	luciClient *luci.Client
	geoService *ipgeo.Service
	flyoutMgr  flyout.Manager

	// State
	mu             sync.RWMutex
	routerOnline   bool
	authError      bool
	authErrorMsg   string
	internetOnline bool
	pingMs         int64
	awgName        string
	awgUp        bool
	geoInfo      *ipgeo.Info
	lastFlagPNG  []byte
	isBusy       bool

	// Tray menu items
	mStatusItem *systray.MenuItem
	mIPItem     *systray.MenuItem
	mAWGItem    *systray.MenuItem
	mAWGToggle  *systray.MenuItem
	mAWGRestart *systray.MenuItem
	mWANRestart *systray.MenuItem
	mLuCI       *systray.MenuItem
	mRefresh    *systray.MenuItem
	mAutostart  *systray.MenuItem
	mQuit       *systray.MenuItem

	stopCh chan struct{}

	// Animated spinner state
	spinMu     sync.Mutex
	spinning   bool
	spinStopCh chan struct{}
}

func NewApp(cfg *config.Config) *App {
	return &App{
		cfg:        cfg,
		luciClient: luci.NewClient(cfg.RouterURL, cfg.Username, cfg.Password),
		geoService: ipgeo.NewService(),
		awgName:        cfg.AWGIface,
		internetOnline: true,
		geoInfo: &ipgeo.Info{
			IP:          "—",
			CountryCode: "--",
			CountryName: "Опрос сети...",
			EmojiFlag:   "🌐",
		},
		stopCh: make(chan struct{}),
	}
}

func (a *App) startSpinner() {
	a.spinMu.Lock()
	if a.spinning {
		a.spinMu.Unlock()
		return
	}
	a.spinning = true
	stopCh := make(chan struct{})
	a.spinStopCh = stopCh
	a.spinMu.Unlock()

	go func() {
		frame := 0
		firstData := icon.GetSpinnerFrame(frame)
		if len(firstData) > 0 {
			systray.SetIcon(firstData)
		}
		frame = (frame + 1) % icon.NumSpinnerFrames

		ticker := time.NewTicker(60 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				data := icon.GetSpinnerFrame(frame)
				if len(data) > 0 {
					systray.SetIcon(data)
				}
				frame = (frame + 1) % icon.NumSpinnerFrames
			}
		}
	}()
}

func (a *App) stopSpinner() {
	a.spinMu.Lock()
	defer a.spinMu.Unlock()
	if !a.spinning {
		return
	}
	a.spinning = false
	if a.spinStopCh != nil {
		close(a.spinStopCh)
		a.spinStopCh = nil
	}
}

func (a *App) ExecuteAction(act string) error {
	a.mu.Lock()
	if a.isBusy {
		a.mu.Unlock()
		return fmt.Errorf("action already in progress")
	}
	a.isBusy = true
	targetAWG := a.awgName
	if targetAWG == "" {
		targetAWG = a.cfg.AWGIface
	}
	a.mu.Unlock()

	// Update UI immediately to show busy loader on button
	a.updateTrayMenu()

	switch act {
	case "awg_up":
		log.Printf("Executing ifup on %s", targetAWG)
		err := a.luciClient.IfUp(targetAWG)
		if err != nil {
			log.Printf("IfUp failed: %v", err)
			a.mu.Lock()
			a.isBusy = false
			a.mu.Unlock()
			a.updateTrayMenu()
			return err
		}

		// WireGuard cryptographic handshake and routing table insertion
		// typically takes 1.5 - 2.5 seconds.
		time.Sleep(2000 * time.Millisecond)

		var confirmedGeo *ipgeo.Info
		var confirmedFlag []byte

		// Verification loop: confirm that traffic has switched to VPN (non-RU IP)
		for attempt := 1; attempt <= 4; attempt++ {
			geo, errGeo := a.geoService.FetchInfo()
			if errGeo == nil && geo != nil && geo.IP != "" && geo.CountryCode != "" && geo.CountryCode != "RU" {
				confirmedGeo = geo
				if fb, errFlag := a.geoService.FetchFlagPNG(geo.CountryCode); errFlag == nil {
					confirmedFlag = fb
				}
				break
			}
			time.Sleep(1000 * time.Millisecond)
		}

		a.mu.Lock()
		a.awgUp = true
		a.internetOnline = true
		if confirmedGeo != nil {
			a.geoInfo = confirmedGeo
		} else {
			a.geoInfo.CountryName = "Austria"
			a.geoInfo.CountryCode = "AT"
		}
		if len(confirmedFlag) > 0 {
			a.lastFlagPNG = confirmedFlag
		} else if atFlag := icon.GetBuiltinFlagPNG("AT"); len(atFlag) > 0 {
			a.lastFlagPNG = atFlag
		}
		curFlag := a.lastFlagPNG
		a.isBusy = false
		a.mu.Unlock()

		a.updateTrayWithFlag(icon.StateVPN, curFlag)
		a.updateTrayMenu()
		return nil

	case "awg_down":
		log.Printf("Executing ifdown on %s", targetAWG)
		err := a.luciClient.IfDown(targetAWG)
		if err != nil {
			log.Printf("IfDown failed: %v", err)
			a.mu.Lock()
			a.isBusy = false
			a.mu.Unlock()
			a.updateTrayMenu()
			return err
		}

		// Allow kernel routing tables to tear down tunnel route
		time.Sleep(1200 * time.Millisecond)
		a.luciClient.EnsureFallbackRoute(a.cfg.WANIface)

		var confirmedGeo *ipgeo.Info
		var confirmedFlag []byte

		// Verification loop: confirm that traffic has returned to primary WAN
		for attempt := 1; attempt <= 3; attempt++ {
			geo, errGeo := a.geoService.FetchInfo()
			if errGeo == nil && geo != nil && geo.IP != "" && geo.CountryCode != "" {
				confirmedGeo = geo
				if fb, errFlag := a.geoService.FetchFlagPNG(geo.CountryCode); errFlag == nil {
					confirmedFlag = fb
				}
				break
			}
			time.Sleep(800 * time.Millisecond)
		}

		a.mu.Lock()
		a.awgUp = false
		a.internetOnline = true
		if confirmedGeo != nil {
			a.geoInfo = confirmedGeo
		} else {
			a.geoInfo.CountryName = "Russia"
			a.geoInfo.CountryCode = "RU"
		}
		if len(confirmedFlag) > 0 {
			a.lastFlagPNG = confirmedFlag
		} else if ruFlag := icon.GetBuiltinFlagPNG("RU"); len(ruFlag) > 0 {
			a.lastFlagPNG = ruFlag
		}
		curFlag := a.lastFlagPNG
		a.isBusy = false
		a.mu.Unlock()

		a.updateTrayWithFlag(icon.StateNoVPN, curFlag)
		a.updateTrayMenu()
		return nil

	case "awg_restart":
		log.Printf("Executing ifrestart on %s", targetAWG)
		_ = a.luciClient.IfRestart(targetAWG)
		time.Sleep(2500 * time.Millisecond)
		a.mu.Lock()
		a.isBusy = false
		a.mu.Unlock()
		go a.PollOnce()
		return nil

	case "wan_restart":
		log.Printf("Executing ifrestart on %s", a.cfg.WANIface)
		_ = a.luciClient.IfRestart(a.cfg.WANIface)
		time.Sleep(3000 * time.Millisecond)
		a.mu.Lock()
		a.isBusy = false
		a.mu.Unlock()
		go a.PollOnce()
		return nil

	case "refresh":
		a.mu.Lock()
		a.isBusy = false
		a.mu.Unlock()
		go a.PollOnce()
		return nil

	default:
		a.mu.Lock()
		a.isBusy = false
		a.mu.Unlock()
		return fmt.Errorf("unknown action: %s", act)
	}
}

// Polling
func (a *App) PollOnce() {
	a.mu.RLock()
	if a.isBusy {
		a.mu.RUnlock()
		return
	}
	a.mu.RUnlock()
	// Ping and LuCI status
	pingDuration, err := a.luciClient.Ping()
	isOnline := err == nil
	var pingMs int64 = -1
	if isOnline {
		pingMs = pingDuration.Milliseconds()
	}

	// Auto-detect AmneziaWG interface and its up/down state
	awgName, awgUp, errAwg := a.luciClient.GetAWGIface(a.cfg.AWGIface)
	authErr := false
	authErrMsg := ""
	if errAwg != nil && isOnline {
		authErr = true
		authErrMsg = errAwg.Error()
		log.Printf("LuCI status check error: %v", errAwg)
	}

	// Fetch External IP and Country Geo
	geo, errGeo := a.geoService.FetchInfo()
	internetUp := isOnline && (errGeo == nil && geo != nil && geo.IP != "")
	var flagBytes []byte
	if internetUp && geo != nil && geo.CountryCode != "" {
		if fb, errFlag := a.geoService.FetchFlagPNG(geo.CountryCode); errFlag == nil {
			flagBytes = fb
		}
	} else if isOnline {
		// Instant fallback when geo lookup is pending
		fallbackCode := "RU"
		if awgUp {
			fallbackCode = "AT"
		}
		if fb, errFlag := a.geoService.FetchFlagPNG(fallbackCode); errFlag == nil {
			flagBytes = fb
		}
	}

	a.mu.Lock()
	a.routerOnline = isOnline
	a.authError = authErr
	a.authErrorMsg = authErrMsg
	a.pingMs = pingMs
	if awgName != "" {
		a.awgName = awgName
	}
	a.awgUp = awgUp
	a.internetOnline = internetUp
	if internetUp && geo != nil {
		a.geoInfo = geo
	} else if !internetUp && isOnline {
		routerHost := "192.168.1.1"
		if u, err := url.Parse(a.cfg.RouterURL); err == nil && u.Hostname() != "" {
			routerHost = u.Hostname()
		}
		a.geoInfo = &ipgeo.Info{
			IP:          routerHost,
			CountryCode: "LAN",
			CountryName: "Только локальная сеть",
			EmojiFlag:   "🏠",
		}
	}
	if len(flagBytes) > 0 {
		a.lastFlagPNG = flagBytes
	}
	curFlag := a.lastFlagPNG
	a.mu.Unlock()

	// Guarantee fallback route exists in OpenWRT so non-VPN traffic routes via WAN when AWG is down
	if isOnline && !authErr {
		go a.luciClient.EnsureFallbackRoute(a.cfg.WANIface)
	}

	// Always stop spinner once status polling is completed
	a.stopSpinner()

	if !isOnline {
		a.updateTrayIcon(icon.StateError)
	} else if authErr {
		a.updateTrayWithFlag(icon.StateError, curFlag)
	} else if awgUp {
		a.updateTrayWithFlag(icon.StateVPN, curFlag)
	} else {
		a.updateTrayWithFlag(icon.StateNoVPN, curFlag)
	}

	a.updateTrayMenu()
}

func (a *App) updateTrayWithFlag(state icon.State, flagPNG []byte) {
	iconBytes, err := icon.GetPlatformIcon(state, flagPNG)
	if err == nil && len(iconBytes) > 0 {
		systray.SetIcon(iconBytes)
	}
}

func (a *App) updateTrayIcon(state icon.State) {
	a.updateTrayWithFlag(state, nil)
}

func (a *App) updateTrayMenu() {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Tooltip
	statusStr := "DOWN ✗"
	if a.authError {
		statusStr = "ОШИБКА АВТОРИЗАЦИИ ⚠️"
	} else if !a.routerOnline {
		statusStr = "ОФЛАЙН ?"
	} else if !a.internetOnline {
		if a.awgUp {
			statusStr = "AWG UP [ТОЛЬКО ЛОКАЛКА] ⚠️"
		} else {
			statusStr = "AWG DOWN [ТОЛЬКО ЛОКАЛКА] ⚠️"
		}
	} else if a.awgUp {
		statusStr = "UP ✓"
	}

	tooltip := fmt.Sprintf("OpenWRT Monitor\nAWG (%s): %s\nIP: %s\n%s %s",
		a.awgName, statusStr, a.geoInfo.IP, a.geoInfo.EmojiFlag, a.geoInfo.CountryName)
	systray.SetTooltip(tooltip)

	// Update menu labels
	if a.mStatusItem != nil {
		a.mStatusItem.SetTitle(fmt.Sprintf("%s (%s)", a.geoInfo.CountryName, a.geoInfo.CountryCode))
	}
	if a.mIPItem != nil {
		a.mIPItem.SetTitle(fmt.Sprintf("IP: %s", a.geoInfo.IP))
	}
	if a.mAWGItem != nil {
		if a.authError {
			a.mAWGItem.SetTitle("⚠️ Ошибка LuCI")
		} else if a.awgUp {
			a.mAWGItem.SetTitle(fmt.Sprintf("🟢 AWG: активен [%s]", a.awgName))
		} else if a.routerOnline {
			a.mAWGItem.SetTitle(fmt.Sprintf("🔴 AWG: выключен [%s]", a.awgName))
		} else {
			a.mAWGItem.SetTitle("⚫ Роутер офлайн")
		}
	}

	if a.mAWGToggle != nil {
		if a.awgUp {
			a.mAWGToggle.SetTitle("⛔ Отключить AWG")
			a.mAWGToggle.SetTooltip(fmt.Sprintf("Остановить интерфейс %s", a.awgName))
		} else {
			a.mAWGToggle.SetTitle("🛡 Включить AWG")
			a.mAWGToggle.SetTooltip(fmt.Sprintf("Запустить интерфейс %s", a.awgName))
		}
	}

	// Update Autostart menu checkbox
	autoEnabled := autostart.IsEnabled()
	if a.mAutostart != nil {
		if autoEnabled {
			a.mAutostart.Check()
		} else {
			a.mAutostart.Uncheck()
		}
	}

	// Update Modern Flyout if active
	var flagB64 string
	if len(a.lastFlagPNG) > 0 {
		flagB64 = "data:image/png;base64," + base64.StdEncoding.EncodeToString(a.lastFlagPNG)
	}
	if a.flyoutMgr != nil {
		a.flyoutMgr.Update(flyout.State{
			CountryName:    a.geoInfo.CountryName,
			CountryCode:    a.geoInfo.CountryCode,
			IP:             a.geoInfo.IP,
			AWGUp:          a.awgUp,
			AWGName:        a.awgName,
			PingMs:         a.pingMs,
			FlagBase64:     flagB64,
			RouterOnline:   a.routerOnline,
			AuthError:      a.authError,
			InternetOnline: a.internetOnline,
			Autostart:      autoEnabled,
			IsBusy:         a.isBusy,
		})
	}
}

func (a *App) StartPollLoop() {
	ticker := time.NewTicker(time.Duration(a.cfg.PollIntervalSec) * time.Second)
	defer ticker.Stop()

	// First poll immediately
	go a.PollOnce()

	for {
		select {
		case <-ticker.C:
			a.PollOnce()
		case <-a.stopCh:
			return
		}
	}
}

func openBrowser(url string) {
	if runtime.GOOS == "windows" {
		verbPtr, _ := windows.UTF16PtrFromString("open")
		urlPtr, _ := windows.UTF16PtrFromString(url)
		s32 := windows.NewLazySystemDLL("shell32.dll")
		pShellExecute := s32.NewProc("ShellExecuteW")
		pShellExecute.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(urlPtr)), 0, 0, 1)
		return
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", url)
	} else {
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func main() {
	exePath, _ := os.Executable()
	logPath := "openwrtmon.log"
	if exePath != "" {
		logPath = filepath.Join(filepath.Dir(exePath), "openwrtmon.log")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	}

	hMutex, isFirst := acquireSingleInstance()
	if !isFirst {
		log.Println("Another instance is already running. Exiting.")
		return
	}
	defer runtime.KeepAlive(hMutex)

	log.Println("=== OpenWRT Monitor starting ===")

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning loading config: %v", err)
	}

	app := NewApp(cfg)

	// Initialize Modern Flyout Popover
	app.flyoutMgr = flyout.New(flyout.Callbacks{
		OnToggleAWG: func() {
			app.mu.RLock()
			isUp := app.awgUp
			app.mu.RUnlock()
			if isUp {
				go app.ExecuteAction("awg_down")
			} else {
				go app.ExecuteAction("awg_up")
			}
		},
		OnRestartAWG: func() {
			go app.ExecuteAction("awg_restart")
		},
		OnRestartWAN: func() {
			go app.ExecuteAction("wan_restart")
		},
		OnOpenLuCI: func() {
			if app.flyoutMgr != nil {
				app.flyoutMgr.Hide()
			}
			openBrowser(fmt.Sprintf("%s/cgi-bin/luci/", app.cfg.RouterURL))
		},
		OnRefresh: func() {
			go app.PollOnce()
		},
		OnToggleAutostart: func() {
			newState, err := autostart.Toggle()
			if err != nil {
				log.Printf("Error toggling autostart: %v", err)
			} else {
				log.Printf("Autostart toggled to: %v", newState)
			}
			app.updateTrayMenu()
		},
		OnQuit: func() {
			app.stopSpinner()
			close(app.stopCh)
			if app.flyoutMgr != nil {
				app.flyoutMgr.Destroy()
			}
			systray.Quit()
			go func() {
				time.Sleep(100 * time.Millisecond)
				os.Exit(0)
			}()
		},
	})

	// Run Systray on main thread
	systray.Run(func() {
		// Click (left or right) on tray icon toggles the modern flyout
		toggleFlyout := func(x, y int32) {
			if app.flyoutMgr != nil {
				app.flyoutMgr.Toggle(x, y)
			}
		}
		systray.SetOnClick(toggleFlyout)
		systray.SetOnRightClick(toggleFlyout)

		// Initial default icon - start spinning while initial check runs
		app.startSpinner()
		systray.SetTitle("OpenWRT Monitor")

		// Information section
		app.mStatusItem = systray.AddMenuItem("Определение страны...", "")
		app.mStatusItem.Disable()

		app.mIPItem = systray.AddMenuItem("IP: —", "")
		app.mIPItem.Disable()

		app.mAWGItem = systray.AddMenuItem("Проверка AWG...", "")
		app.mAWGItem.Disable()

		systray.AddSeparator()

		// Actions
		app.mAWGToggle = systray.AddMenuItem("⏳ Проверка AWG...", "Переключить состояние AWG")
		app.mAWGRestart = systray.AddMenuItem("🔄 Перезапустить AWG", "Перезапустить AWG")

		systray.AddSeparator()
		app.mWANRestart = systray.AddMenuItem("🌐 Перезапустить WAN", "Перезапустить интерфейс провайдера")

		systray.AddSeparator()
		app.mLuCI = systray.AddMenuItem("🖥 Открыть LuCI", "Открыть LuCI")
		app.mRefresh = systray.AddMenuItem("🔃 Обновить статус", "Обновить статус вручную")

		systray.AddSeparator()
		app.mAutostart = systray.AddMenuItemCheckbox("Автозапуск с Windows", "Запускать при старте системы", autostart.IsEnabled())
		app.mQuit = systray.AddMenuItem("❌ Выход", "Закрыть OpenWRT Monitor")

		// Handle clicks
		go func() {
			for {
				select {
				case <-app.mAWGToggle.ClickedCh:
					app.mu.RLock()
					isUp := app.awgUp
					app.mu.RUnlock()
					if isUp {
						go app.ExecuteAction("awg_down")
					} else {
						go app.ExecuteAction("awg_up")
					}
				case <-app.mAWGRestart.ClickedCh:
					go app.ExecuteAction("awg_restart")
				case <-app.mWANRestart.ClickedCh:
					go app.ExecuteAction("wan_restart")
				case <-app.mLuCI.ClickedCh:
					openBrowser(fmt.Sprintf("%s/cgi-bin/luci/", app.cfg.RouterURL))
				case <-app.mRefresh.ClickedCh:
					go app.PollOnce()
				case <-app.mAutostart.ClickedCh:
					_, _ = autostart.Toggle()
					app.updateTrayMenu()
				case <-app.mQuit.ClickedCh:
					app.stopSpinner()
					close(app.stopCh)
					if app.flyoutMgr != nil {
						app.flyoutMgr.Destroy()
					}
					systray.Quit()
					return
				}
			}
		}()

		// Start background polling after tray icon initializes
		go func() {
			time.Sleep(400 * time.Millisecond)
			app.StartPollLoop()
		}()

	}, func() {
		log.Println("Systray exiting...")
		if app.flyoutMgr != nil {
			app.flyoutMgr.Destroy()
		}
	})
}
