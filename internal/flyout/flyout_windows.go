//go:build windows

package flyout

import (
	"encoding/json"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	u32                    = windows.NewLazySystemDLL("user32.dll")
	k32                    = windows.NewLazySystemDLL("kernel32.dll")
	g32                    = windows.NewLazySystemDLL("gdi32.dll")
	dwm                    = windows.NewLazySystemDLL("dwmapi.dll")
	pSetWindowLong         = u32.NewProc("SetWindowLongW")
	pSetWindowPos          = u32.NewProc("SetWindowPos")
	pShowWindow            = u32.NewProc("ShowWindow")
	pSetForegroundWindow   = u32.NewProc("SetForegroundWindow")
	pBringWindowToTop      = u32.NewProc("BringWindowToTop")
	pSystemParametersInfo  = u32.NewProc("SystemParametersInfoW")
	pDwmSetWindowAttribute = dwm.NewProc("DwmSetWindowAttribute")
	pSetWindowRgn          = u32.NewProc("SetWindowRgn")
	pCreateRoundRectRgn    = g32.NewProc("CreateRoundRectRgn")
	pGetAsyncKeyState        = u32.NewProc("GetAsyncKeyState")
	pGetCursorPos          = u32.NewProc("GetCursorPos")
	pGetWindowRect         = u32.NewProc("GetWindowRect")
	pOpenClipboard         = u32.NewProc("OpenClipboard")
	pCloseClipboard        = u32.NewProc("CloseClipboard")
	pEmptyClipboard        = u32.NewProc("EmptyClipboard")
	pSetClipboardData      = u32.NewProc("SetClipboardData")
	pGlobalAlloc           = k32.NewProc("GlobalAlloc")
	pGlobalLock            = k32.NewProc("GlobalLock")
	pGlobalUnlock          = k32.NewProc("GlobalUnlock")
)

const (
	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002
)

var (
	gwlStyle   int32 = -16
	gwlExStyle int32 = -20
)

const (
	WS_POPUP        = 0x80000000
	WS_CLIPCHILDREN = 0x02000000

	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_TOPMOST    = 0x00000008

	SWP_NOSIZE       = 0x0001
	SWP_NOMOVE       = 0x0002
	SWP_NOZORDER     = 0x0004
	SWP_FRAMECHANGED = 0x0020
	SWP_SHOWWINDOW   = 0x0040

	SW_HIDE = 0
	SW_SHOW = 5

	HWND_TOPMOST = ^uintptr(0)

	SPI_GETWORKAREA = 0x0030

	DWMWA_WINDOW_CORNER_PREFERENCE = 33
	DWMWCP_DONOTROUND              = 1
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type point struct {
	X, Y int32
}

type windowsManager struct {
	cb         Callbacks
	w          webview2.WebView
	hwnd       uintptr
	visible    bool
	monitoring bool
	width      int32
	height     int32
	mu         sync.Mutex
	lastState  State
}

const flyoutHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; user-select: none; }
  html, body {
    width: 100%;
    height: 100%;
    margin: 0;
    padding: 0;
    background: #161822;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    color: #f8fafc;
    overflow: hidden;
  }
  .card {
    width: 100%;
    height: 100%;
    margin: 0;
    padding: 13px 15px 12px 15px;
    background: #161822;
    border: 1px solid rgba(255, 255, 255, 0.16);
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.04);
    border-radius: 0;
    display: flex;
    flex-direction: column;
    justify-content: space-between;
  }
  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding-bottom: 11px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }
  .country-box {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .flag-frame {
    width: 38px;
    height: 26px;
    border-radius: 5px;
    overflow: hidden;
    border: 1px solid rgba(255, 255, 255, 0.25);
    box-shadow: 0 2px 6px rgba(0,0,0,0.3);
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #1e202e;
  }
  .flag-frame img, .flag-frame svg {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .country-title {
    font-size: 13px;
    font-weight: 700;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .badge-code {
    font-size: 10px;
    font-weight: 600;
    color: #94a3b8;
    background: rgba(255, 255, 255, 0.08);
    padding: 1px 4px;
    border-radius: 4px;
  }
  .ip-row {
    font-size: 11px;
    color: #94a3b8;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-top: 2px;
    cursor: pointer;
    user-select: none;
    transition: color 0.15s ease;
  }
  .ip-row:hover {
    color: #f1f5f9;
  }
  .ip-row:hover .copy-icon-btn {
    color: #e2e8f0;
  }
  .copy-icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: #64748b;
    transition: color 0.15s ease, transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .copy-icon-btn.copied {
    transform: scale(1.15);
  }
  #ipText {
    transition: color 0.15s ease;
  }
  #ipText.copied-active {
    color: #34d399 !important;
    font-weight: 600;
  }
  .ping-badge {
    font-size: 10px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 12px;
    border: 1px solid rgba(16, 185, 129, 0.3);
    background: rgba(16, 185, 129, 0.15);
    color: #34d399;
    display: flex;
    align-items: center;
    gap: 5px;
    white-space: nowrap;
    flex-shrink: 0;
  }
  .ping-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #34d399;
    animation: pulse 2s infinite ease-in-out;
  }
  @keyframes pulse {
    0%, 100% { opacity: 1; transform: scale(1); }
    50% { opacity: 0.6; transform: scale(1.1); }
  }
  .spin-icon {
    display: inline-block;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
  .btn-loader {
    width: 14px;
    height: 14px;
    border: 2.2px solid rgba(255, 255, 255, 0.32);
    border-top-color: #ffffff;
    border-radius: 50%;
    display: inline-block;
    animation: spin 0.65s linear infinite;
    flex-shrink: 0;
  }
  .btn-icon-svg {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    flex-shrink: 0;
  }
  .btn-icon-svg svg {
    width: 100%;
    height: 100%;
  }
  .main-btn {
    margin: 8px 0 6px 0;
    width: 100%;
    padding: 10px;
    border-radius: 11px;
    font-size: 13px;
    font-weight: 700;
    border: none;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: white;
    box-shadow: 0 4px 14px rgba(0,0,0,0.3);
    transition: all 0.2s;
  }
  .main-btn:active { transform: scale(0.98); }
  .btn-active {
    background: linear-gradient(135deg, #ef4444 0%, #e11d48 100%);
    box-shadow: 0 4px 14px rgba(239, 68, 68, 0.35);
  }
  .btn-inactive {
    background: linear-gradient(135deg, #10b981 0%, #059669 100%);
    box-shadow: 0 4px 14px rgba(16, 185, 129, 0.35);
  }
  .actions-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    margin-bottom: 6px;
  }
  .action-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 9px;
    color: #e2e8f0;
    font-size: 11px;
    font-weight: 500;
    padding: 8px 10px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    transition: background 0.2s;
  }
  .action-btn:hover { background: rgba(255, 255, 255, 0.12); }
  .act-icon-svg {
    width: 14px;
    height: 14px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }
  .act-icon-svg svg {
    width: 100%;
    height: 100%;
  }
  .text-awg { color: #818cf8; }
  .text-wan { color: #38bdf8; }
  .footer {
    padding-top: 8px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: 11px;
    color: #94a3b8;
  }
  .footer-btn {
    background: none;
    border: none;
    color: #94a3b8;
    cursor: pointer;
    font-size: 11px;
    display: flex;
    align-items: center;
    gap: 4px;
    transition: color 0.2s;
  }
  .footer-btn:hover { color: #f1f5f9; }
  .btn-exit:hover { color: #f87171; }
  .autostart-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #64748b;
    display: inline-block;
    flex-shrink: 0;
    transition: background 0.2s, box-shadow 0.2s;
  }
  .autostart-on .autostart-dot {
    background: #34d399;
    box-shadow: 0 0 6px rgba(52, 211, 153, 0.7);
  }
  .autostart-on span {
    color: #e2e8f0;
  }
</style>
</head>
<body>
  <div class="card">
    <div class="header">
      <div class="country-box">
        <div id="flagFrame" class="flag-frame">
          <div style="width:100%;height:100%;background:#1e202e;display:flex;align-items:center;justify-content:center;color:#64748b;font-size:12px;">🌐</div>
        </div>
        <div>
          <div class="country-title">
            <span id="countryName">Определение...</span>
            <span id="countryCode" class="badge-code">—</span>
          </div>
          <div class="ip-row" onclick="copyIP()" title="Нажмите, чтобы скопировать IP">
            <span id="ipText">—</span>
            <span id="copyIconBox" class="copy-icon-btn">
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>
            </span>
          </div>
        </div>
      </div>
      <div id="pingBadge" class="ping-badge">
        <span class="ping-dot"></span>
        <span id="pingText">Проверка...</span>
      </div>
    </div>

    <button id="mainToggleBtn" class="main-btn btn-active" onclick="onToggleClick()">
      <span id="btnIcon"><span class="btn-loader"></span></span>
      <span id="btnText">Проверка...</span>
    </button>

    <div class="actions-grid">
      <button class="action-btn" onclick="onAction('restart_awg', this)" title="Переподключить VPN-туннель AmneziaWG">
        <span class="act-icon-svg text-awg">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="23 4 23 10 17 10"></polyline>
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
          </svg>
        </span>
        <span>Сброс AWG</span>
      </button>
      <button class="action-btn" onclick="onAction('restart_wan', this)" title="Перезапустить интернет провайдера (PPPoE)">
        <span class="act-icon-svg text-wan">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="10"></circle>
            <line x1="2" y1="12" x2="22" y2="12"></line>
            <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path>
          </svg>
        </span>
        <span>Сброс WAN</span>
      </button>
    </div>

    <div class="footer">
      <button class="footer-btn" onclick="onLuCIClick()" title="Открыть панель LuCI в браузере">
        <span>🖥</span> <span>LuCI</span>
      </button>
      <button id="autostartBtn" class="footer-btn" onclick="onToggleAutostart()" title="Автозапуск при старте Windows">
        <span id="autostartDot" class="autostart-dot"></span>
        <span id="autostartText">Автостарт: Выкл</span>
      </button>
      <div style="display:flex; align-items:center; gap:8px;">
        <button class="footer-btn" onclick="onRefreshClick()" title="Обновить">
          <span>↻</span>
        </button>
        <span style="opacity:0.2;">|</span>
        <button class="footer-btn btn-exit" onclick="onQuitClick()" title="Выход">Выход</button>
      </div>
    </div>
  </div>

  <script>
    let curState = { awgUp: true, ip: "" };
    let copyTimer = null;
    const copySvgNormal = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
    const copySvgSuccess = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#34d399" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';

    const svgPowerOff = '<span class="btn-icon-svg"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"><path d="M18.36 6.64a9 9 0 1 1-12.73 0"></path><line x1="12" y1="2" x2="12" y2="12"></line></svg></span>';
    const svgShieldCheck = '<span class="btn-icon-svg"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path><polyline points="9 12 11 14 15 10"></polyline></svg></span>';
    const svgLoader = '<span class="btn-loader"></span>';
    const svgOffline = '<span class="btn-icon-svg"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="4.93" y1="4.93" x2="19.07" y2="19.07"></line></svg></span>';
    const svgAlert = '<span class="btn-icon-svg"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.3" stroke-linecap="round" stroke-linejoin="round"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg></span>';

    function callGo(fnName, ...args) {
      if (typeof window[fnName] === 'function') {
        window[fnName](...args);
      } else if (window.chrome && window.chrome.webview) {
        window.chrome.webview.postMessage(JSON.stringify({
          id: Date.now(),
          method: fnName,
          params: args
        }));
      }
    }

    function onLuCIClick() {
      callGo('goOpenLuCI');
    }

    function onToggleAutostart() {
      callGo('goToggleAutostart');
    }

    function onRefreshClick() {
      const pBadge = document.getElementById('pingBadge');
      if (pBadge) pBadge.innerHTML = '<span class="spin-icon">↻</span> <span>Обновление...</span>';
      callGo('goRefresh');
    }

    function onQuitClick() {
      callGo('goQuit');
    }

    function copyIP() {
      if (!curState.ip || curState.ip === '—') return;

      // 1. Native Windows Clipboard via Go
      callGo('goCopyText', curState.ip);

      // 2. Browser fallback
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(curState.ip).catch(() => {});
      }

      // 3. Crisp inline feedback: IP text becomes "Скопировано!", icon becomes checkmark
      const ipEl = document.getElementById('ipText');
      const box = document.getElementById('copyIconBox');

      if (ipEl) {
        ipEl.textContent = "Скопировано!";
        ipEl.classList.add('copied-active');
      }
      if (box) {
        box.innerHTML = copySvgSuccess;
        box.classList.add('copied');
      }

      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => {
        if (ipEl) {
          ipEl.textContent = curState.ip || '—';
          ipEl.classList.remove('copied-active');
        }
        if (box) {
          box.innerHTML = copySvgNormal;
          box.classList.remove('copied');
        }
        copyTimer = null;
      }, 1200);
    }

    function renderState(s) {
      curState = s;
      document.getElementById('countryName').textContent = s.countryName || 'Неизвестно';
      document.getElementById('countryCode').textContent = s.countryCode || '—';
      if (!copyTimer) {
        document.getElementById('ipText').textContent = s.ip || '—';
      }

      const btn = document.getElementById('mainToggleBtn');
      const bIcon = document.getElementById('btnIcon');
      const bText = document.getElementById('btnText');
      const pBadge = document.getElementById('pingBadge');

      if (s.isBusy) {
        btn.disabled = true;
        btn.style.opacity = "0.85";
        bIcon.innerHTML = svgLoader;
        bText.textContent = "Применение...";
      } else {
        btn.disabled = false;
        btn.style.opacity = "1";
        if (!s.routerOnline) {
          btn.className = "main-btn";
          btn.style.background = "#334155";
          bIcon.innerHTML = svgOffline;
          bText.textContent = "Роутер офлайн";
          pBadge.innerHTML = '<span style="width:6px;height:6px;border-radius:50%;background:#64748b;"></span><span>Офлайн</span>';
          pBadge.style.borderColor = "rgba(100,116,139,0.3)";
          pBadge.style.color = "#94a3b8";
        } else if (s.authError) {
          btn.className = "main-btn";
          btn.style.background = "#ea580c";
          bIcon.innerHTML = svgAlert;
          bText.textContent = "Ошибка LuCI";
          pBadge.innerHTML = '<span style="width:6px;height:6px;border-radius:50%;background:#fb923c;"></span><span>Пароль?</span>';
        } else if (!s.internetOnline) {
          if (s.awgUp) {
            btn.className = "main-btn btn-active";
            btn.style.background = "";
            bIcon.innerHTML = svgPowerOff;
            bText.textContent = "Отключить AWG";
          } else {
            btn.className = "main-btn btn-inactive";
            btn.style.background = "";
            bIcon.innerHTML = svgShieldCheck;
            bText.textContent = "Включить AWG";
          }
          pBadge.style.borderColor = "rgba(245, 158, 11, 0.4)";
          pBadge.style.background = "rgba(245, 158, 11, 0.16)";
          pBadge.style.color = "#fbbf24";
          pBadge.innerHTML = '<span style="width:6px;height:6px;border-radius:50%;background:#fbbf24;box-shadow:0 0 6px rgba(245,158,11,0.6);"></span><span>Только локалка</span>';
        } else if (s.awgUp) {
          btn.className = "main-btn btn-active";
          btn.style.background = "";
          bIcon.innerHTML = svgPowerOff;
          bText.textContent = "Отключить AWG";
          pBadge.style.borderColor = "rgba(16, 185, 129, 0.3)";
          pBadge.style.background = "rgba(16, 185, 129, 0.15)";
          pBadge.style.color = "#34d399";
          pBadge.innerHTML = '<span class="ping-dot"></span><span>' + (s.pingMs > 0 ? s.pingMs + ' ms' : 'Активен') + '</span>';
        } else {
          btn.className = "main-btn btn-inactive";
          btn.style.background = "";
          bIcon.innerHTML = svgShieldCheck;
          bText.textContent = "Включить AWG";
          pBadge.style.borderColor = "rgba(239, 68, 68, 0.3)";
          pBadge.style.background = "rgba(239, 68, 68, 0.15)";
          pBadge.style.color = "#f87171";
          pBadge.innerHTML = '<span style="width:6px;height:6px;border-radius:50%;background:#f87171;"></span><span>Прямой трафик</span>';
        }
      }

      const fFrame = document.getElementById('flagFrame');
      const cc = (s.countryCode || '').toUpperCase();
      if (!s.internetOnline && s.routerOnline) {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" rx="30" fill="#1e2235"/><g transform="translate(230, 80) scale(18)" fill="none" stroke="#f59e0b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect><rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect><line x1="6" y1="6" x2="6.01" y2="6"></line><line x1="6" y1="18" x2="6.01" y2="18"></line></g></svg>';
      } else if (cc === 'RU') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#D52B1E"/><rect width="900" height="400" fill="#0039A6"/><rect width="900" height="200" fill="#FFFFFF"/></svg>';
      } else if (cc === 'AT') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#ED2939"/><rect y="200" width="900" height="200" fill="#FFFFFF"/></svg>';
      } else if (cc === 'NL') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#21468B"/><rect width="900" height="400" fill="#FFFFFF"/><rect width="900" height="200" fill="#AE1C28"/></svg>';
      } else if (cc === 'DE') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#FFCE00"/><rect width="900" height="400" fill="#DD0000"/><rect width="900" height="200" fill="#000000"/></svg>';
      } else if (cc === 'FR') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="300" height="600" fill="#002654"/><rect x="300" width="300" height="600" fill="#FFFFFF"/><rect x="600" width="300" height="600" fill="#ED2939"/></svg>';
      } else if (cc === 'IT') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="300" height="600" fill="#009246"/><rect x="300" width="300" height="600" fill="#FFFFFF"/><rect x="600" width="300" height="600" fill="#CE2B37"/></svg>';
      } else if (cc === 'PL') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="300" fill="#FFFFFF"/><rect y="300" width="900" height="300" fill="#DC143C"/></svg>';
      } else if (cc === 'UA') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="300" fill="#005BBB"/><rect y="300" width="900" height="300" fill="#FFD500"/></svg>';
      } else if (cc === 'FI') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#FFFFFF"/><rect x="250" width="150" height="600" fill="#002F6C"/><rect y="225" width="900" height="150" fill="#002F6C"/></svg>';
      } else if (cc === 'SE') {
        fFrame.innerHTML = '<svg viewBox="0 0 900 600"><rect width="900" height="600" fill="#006AA7"/><rect x="250" width="120" height="600" fill="#FECC00"/><rect y="240" width="900" height="120" fill="#FECC00"/></svg>';
      } else if (s.flagBase64) {
        fFrame.innerHTML = '<img src="' + s.flagBase64 + '" alt="Flag">';
      } else {
        fFrame.innerHTML = '<div style="width:100%;height:100%;background:#1e202e;display:flex;align-items:center;justify-content:center;color:#64748b;font-size:12px;">🌐</div>';
      }

      const aBtn = document.getElementById('autostartBtn');
      const aText = document.getElementById('autostartText');
      if (aBtn && aText) {
        if (s.autostart) {
          aBtn.classList.add('autostart-on');
          aText.textContent = "Автостарт: Вкл";
          aBtn.title = "Автозапуск включен (нажмите, чтобы выключить)";
        } else {
          aBtn.classList.remove('autostart-on');
          aText.textContent = "Автостарт: Выкл";
          aBtn.title = "Автозапуск выключен (нажмите, чтобы включить)";
        }
      }
    }

    function onToggleClick() {
      const btn = document.getElementById('mainToggleBtn');
      btn.disabled = true;
      document.getElementById('btnIcon').innerHTML = svgLoader;
      document.getElementById('btnText').textContent = "Применение...";
      callGo('goToggleAWG');
      setTimeout(() => { btn.disabled = false; }, 2500);
    }

    function onAction(action, btnEl) {
      if (btnEl) {
        const iconSpan = btnEl.querySelector('.act-icon-svg');
        if (iconSpan) iconSpan.classList.add('spin-icon');
        setTimeout(() => {
          if (iconSpan) iconSpan.classList.remove('spin-icon');
        }, 2200);
      }
      if (action === 'restart_awg') callGo('goRestartAWG');
      if (action === 'restart_wan') callGo('goRestartWAN');
    }

    // Auto-hide when focus is lost
    window.addEventListener('blur', () => {
      setTimeout(() => {
        callGo('goHideFlyout');
      }, 100);
    });

    // Close on Escape key
    window.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        callGo('goHideFlyout');
      }
    });
  </script>
</body>
</html>`

// New creates and starts the Windows Flyout manager.
func New(cb Callbacks) Manager {
	fm := &windowsManager{
		cb:     cb,
		width:  320,
		height: 255,
	}
	ready := make(chan struct{})

	go func() {
		runtime.LockOSThread()

		w := webview2.New(false)
		if w == nil {
			log.Printf("[Flyout] Failed to initialize webview2")
			close(ready)
			return
		}
		fm.w = w
		hwnd := uintptr(w.Window())
		fm.hwnd = hwnd

		// 1. Immediately hide the window so it never flashes on startup
		pShowWindow.Call(hwnd, SW_HIDE)

		// 2. Strip window styles completely -> WS_POPUP (NO title bar, NO borders, NO minimize/maximize buttons)
		pSetWindowLong.Call(hwnd, uintptr(uint32(gwlStyle)), uintptr(WS_POPUP|WS_CLIPCHILDREN))

		// 3. Set ToolWindow (NO taskbar button!) and TopMost
		pSetWindowLong.Call(hwnd, uintptr(uint32(gwlExStyle)), uintptr(WS_EX_TOOLWINDOW|WS_EX_TOPMOST))

		// 4. Set size and notify Windows of frame change to strip taskbar presence
		pSetWindowPos.Call(hwnd, HWND_TOPMOST, 0, 0, uintptr(fm.width), uintptr(fm.height), SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOZORDER)

		// 5. Windows 11: enforce clean, crisp square rectangular corners
		cornerPref := uint32(DWMWCP_DONOTROUND)
		pDwmSetWindowAttribute.Call(hwnd, DWMWA_WINDOW_CORNER_PREFERENCE, uintptr(unsafe.Pointer(&cornerPref)), 4)

		// Register Go bindings BEFORE setting HTML so they are injected when the document is created
		w.Bind("goHideFlyout", func() {
			fm.Hide()
		})
		w.Bind("goToggleAWG", func() {
			if fm.cb.OnToggleAWG != nil {
				fm.cb.OnToggleAWG()
			}
		})
		w.Bind("goRestartAWG", func() {
			if fm.cb.OnRestartAWG != nil {
				fm.cb.OnRestartAWG()
			}
		})
		w.Bind("goRestartWAN", func() {
			if fm.cb.OnRestartWAN != nil {
				fm.cb.OnRestartWAN()
			}
		})
		w.Bind("goOpenLuCI", func() {
			if fm.cb.OnOpenLuCI != nil {
				fm.cb.OnOpenLuCI()
			}
		})
		w.Bind("goRefresh", func() {
			if fm.cb.OnRefresh != nil {
				fm.cb.OnRefresh()
			}
		})
		w.Bind("goToggleAutostart", func() {
			if fm.cb.OnToggleAutostart != nil {
				fm.cb.OnToggleAutostart()
			}
		})
		w.Bind("goQuit", func() {
			if fm.cb.OnQuit != nil {
				fm.cb.OnQuit()
			}
		})
		w.Bind("goCopyText", func(text string) {
			setClipboardText(fm.hwnd, text)
		})

		w.SetHtml(flyoutHTML)

		close(ready)
		w.Run()
	}()

	<-ready
	return fm
}

func setClipboardText(hwnd uintptr, text string) bool {
	u16, err := windows.UTF16FromString(text)
	if err != nil {
		return false
	}
	r, _, _ := pOpenClipboard.Call(hwnd)
	if r == 0 {
		return false
	}
	defer pCloseClipboard.Call()

	pEmptyClipboard.Call()

	size := len(u16) * 2
	hMem, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return false
	}
	pMem, _, _ := pGlobalLock.Call(hMem)
	if pMem == 0 {
		return false
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(pMem)), size)
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u16[0])), size)
	copy(dst, src)
	pGlobalUnlock.Call(hMem)
	pSetClipboardData.Call(CF_UNICODETEXT, hMem)
	return true
}

func (fm *windowsManager) startOutsideClickMonitor() {
	fm.mu.Lock()
	if fm.monitoring {
		fm.mu.Unlock()
		return
	}
	fm.monitoring = true
	fm.mu.Unlock()

	go func() {
		// Grace period of 200ms so the initial tray click doesn't dismiss the flyout immediately
		time.Sleep(200 * time.Millisecond)

		for {
			fm.mu.Lock()
			vis := fm.visible
			fm.mu.Unlock()
			if !vis {
				fm.mu.Lock()
				fm.monitoring = false
				fm.mu.Unlock()
				return
			}

			// Check if left or right mouse button is pressed anywhere on screen
			stateL, _, _ := pGetAsyncKeyState.Call(0x01) // VK_LBUTTON
			stateR, _, _ := pGetAsyncKeyState.Call(0x02) // VK_RBUTTON
			if (stateL&0x8000 != 0) || (stateR&0x8000 != 0) {
				var pt point
				pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
				var rc rect
				pGetWindowRect.Call(fm.hwnd, uintptr(unsafe.Pointer(&rc)))

				inside := pt.X >= rc.Left && pt.X <= rc.Right && pt.Y >= rc.Top && pt.Y <= rc.Bottom
				if !inside {
					fm.Hide()
					fm.mu.Lock()
					fm.monitoring = false
					fm.mu.Unlock()
					return
				}
			}

			time.Sleep(40 * time.Millisecond)
		}
	}()
}

func (fm *windowsManager) ShowNear(x, y int32) {
	if fm.w == nil || fm.hwnd == 0 {
		return
	}
	fm.w.Dispatch(func() {
		// Immediately re-render cached state on show
		fm.mu.Lock()
		st := fm.lastState
		fm.mu.Unlock()
		if data, err := json.Marshal(st); err == nil {
			fm.w.Eval(fmt.Sprintf("renderState(%s)", string(data)))
		}

		var wa rect
		pSystemParametersInfo.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&wa)), 0)

		targetX := x - fm.width/2
		if targetX < wa.Left+10 {
			targetX = wa.Left + 10
		}
		if targetX+fm.width > wa.Right-10 {
			targetX = wa.Right - fm.width - 10
		}

		targetY := wa.Bottom - fm.height - 10
		if targetY < wa.Top+10 {
			targetY = y + 15 // Taskbar at top
		}

		pSetWindowPos.Call(fm.hwnd, HWND_TOPMOST, uintptr(targetX), uintptr(targetY), uintptr(fm.width), uintptr(fm.height), SWP_SHOWWINDOW)
		pShowWindow.Call(fm.hwnd, SW_SHOW)
		pSetForegroundWindow.Call(fm.hwnd)
		pBringWindowToTop.Call(fm.hwnd)

		fm.mu.Lock()
		fm.visible = true
		fm.mu.Unlock()

		fm.startOutsideClickMonitor()
	})
}

func (fm *windowsManager) Hide() {
	if fm.w == nil || fm.hwnd == 0 {
		return
	}
	fm.w.Dispatch(func() {
		pShowWindow.Call(fm.hwnd, SW_HIDE)
		fm.mu.Lock()
		fm.visible = false
		fm.monitoring = false
		fm.mu.Unlock()
	})
}

func (fm *windowsManager) Toggle(x, y int32) {
	fm.mu.Lock()
	vis := fm.visible
	fm.mu.Unlock()

	if vis {
		fm.Hide()
	} else {
		fm.ShowNear(x, y)
	}
}

func (fm *windowsManager) Update(state State) {
	if fm.w == nil {
		return
	}
	fm.mu.Lock()
	fm.lastState = state
	fm.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	fm.w.Dispatch(func() {
		fm.w.Eval(fmt.Sprintf("renderState(%s)", string(data)))
	})
}

func (fm *windowsManager) Destroy() {
	if fm.w != nil {
		fm.w.Dispatch(func() {
			fm.w.Terminate()
		})
	}
}
