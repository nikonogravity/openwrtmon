#!/usr/bin/env python3
"""
OpenWRT Monitor — System Tray Utility
Monitors AWG (AmneziaWG) interface status on OpenWRT,
displays external IP, and provides quick actions via tray icon.
"""

import sys
import os
import threading
import time
import configparser
import logging
import subprocess
import webbrowser
from pathlib import Path

import requests
import pystray
from PIL import Image, ImageDraw, ImageFont

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
LOG_PATH = Path(__file__).parent / "openwrtmon.log"
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[
        logging.FileHandler(LOG_PATH, encoding="utf-8"),
        logging.StreamHandler(sys.stdout),
    ],
)
log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
CONFIG_PATH = Path(__file__).parent / "config.ini"

def load_config():
    cfg = configparser.ConfigParser()
    cfg.read(CONFIG_PATH, encoding="utf-8")
    return cfg

# ---------------------------------------------------------------------------
# LuCI RPC client
# ---------------------------------------------------------------------------
class LuciClient:
    """Thin wrapper around LuCI JSON-RPC API."""

    def __init__(self, base_url: str, username: str, password: str):
        self.base_url = base_url.rstrip("/")
        self.username = username
        self.password = password
        self.auth_token: str | None = None
        self.session = requests.Session()
        self.session.timeout = 8

    # ---- authentication ------------------------------------------------
    def _rpc(self, service: str, method: str, params: list):
        url = f"{self.base_url}/cgi-bin/luci/rpc/{service}"
        if self.auth_token:
            url += f"?auth={self.auth_token}"
        payload = {"method": method, "id": 1, "params": params}
        resp = self.session.post(url, json=payload, timeout=8)
        resp.raise_for_status()
        return resp.json()

    def login(self) -> bool:
        try:
            result = self._rpc("auth", "login", [self.username, self.password])
            token = result.get("result")
            if token and token != "0" * 32:
                self.auth_token = token
                log.info("LuCI login OK, token=%s…", token[:8])
                return True
            log.warning("LuCI login failed — bad token: %s", token)
            return False
        except Exception as exc:
            log.error("LuCI login error: %s", exc)
            return False

    def ensure_auth(self) -> bool:
        if self.auth_token:
            return True
        return self.login()

    # ---- network interface control ------------------------------------
    def _call_net(self, action: str, iface: str) -> bool:
        """Call network.interface.<action> via ubus RPC."""
        try:
            if not self.ensure_auth():
                return False
            result = self._rpc(
                "sys",
                "exec",
                [f"ifup {iface}" if action == "ifup" else f"ifdown {iface}"],
            )
            log.info("exec %s %s → %s", action, iface, result)
            return True
        except Exception as exc:
            log.error("_call_net(%s, %s) error: %s", action, iface, exc)
            self.auth_token = None  # force re-login next time
            return False

    def ifup(self, iface: str) -> bool:
        return self._call_net("ifup", iface)

    def ifdown(self, iface: str) -> bool:
        return self._call_net("ifdown", iface)

    def ifrestart(self, iface: str) -> bool:
        ok = self.ifdown(iface)
        time.sleep(2)
        ok = self.ifup(iface) and ok
        return ok

    # ---- interface status ---------------------------------------------
    def get_iface_status(self, iface: str) -> dict | None:
        """Return network.interface status dict or None on error."""
        try:
            if not self.ensure_auth():
                return None
            result = self._rpc(
                "uci",
                "get_all",
                ["network", iface],
            )
            return result.get("result")
        except Exception as exc:
            log.error("get_iface_status(%s) error: %s", iface, exc)
            self.auth_token = None
            return None

    def get_iface_up(self, iface: str) -> bool | None:
        """Check if interface is UP using sys exec."""
        try:
            if not self.ensure_auth():
                return None
            result = self._rpc(
                "sys",
                "exec",
                [f"ip link show {iface} 2>/dev/null | grep -c 'state UP'"],
            )
            output = (result.get("result") or "").strip()
            return output == "1"
        except Exception as exc:
            log.error("get_iface_up(%s) error: %s", iface, exc)
            self.auth_token = None
            return None


# ---------------------------------------------------------------------------
# Icon drawing
# ---------------------------------------------------------------------------
ICON_SIZE = 64

def _make_icon(state: str) -> Image.Image:
    """
    state: 'vpn'   → green shield
           'novpn' → red shield
           'error' → grey shield
           'busy'  → orange shield
    """
    img = Image.new("RGBA", (ICON_SIZE, ICON_SIZE), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    colors = {
        "vpn":   {"bg": (34, 197, 94),   "fg": (255, 255, 255)},
        "novpn": {"bg": (239, 68, 68),   "fg": (255, 255, 255)},
        "error": {"bg": (107, 114, 128), "fg": (255, 255, 255)},
        "busy":  {"bg": (251, 146, 60),  "fg": (255, 255, 255)},
    }
    c = colors.get(state, colors["error"])

    # Draw shield shape (approximate polygon)
    s = ICON_SIZE
    shield = [
        (s * 0.5, s * 0.05),  # top centre
        (s * 0.95, s * 0.2),  # top right
        (s * 0.95, s * 0.55), # mid right
        (s * 0.5, s * 0.97),  # bottom centre
        (s * 0.05, s * 0.55), # mid left
        (s * 0.05, s * 0.2),  # top left
    ]
    draw.polygon(shield, fill=c["bg"])

    # Draw letter
    letter = {"vpn": "V", "novpn": "N", "error": "?", "busy": "…"}.get(state, "?")
    try:
        font = ImageFont.truetype("arial.ttf", int(s * 0.45))
    except Exception:
        font = ImageFont.load_default()

    bbox = draw.textbbox((0, 0), letter, font=font)
    tw, th = bbox[2] - bbox[0], bbox[3] - bbox[1]
    tx = (s - tw) / 2 - bbox[0]
    ty = (s - th) / 2 - bbox[1] - s * 0.04
    draw.text((tx, ty), letter, font=font, fill=c["fg"])

    return img


# ---------------------------------------------------------------------------
# Background monitor thread
# ---------------------------------------------------------------------------
class Monitor:
    def __init__(self, cfg: configparser.ConfigParser):
        self.cfg = cfg
        self.awg_iface = cfg.get("interfaces", "awg", fallback="awg")
        self.wan_iface = cfg.get("interfaces", "wan", fallback="wan")
        self.poll_interval = cfg.getint("settings", "poll_interval", fallback=10)
        self.ip_check_url = cfg.get("settings", "ip_check_url", fallback="https://api.ipify.org")
        self.country_check_url = cfg.get("settings", "country_check_url",
                                         fallback="https://ipapi.co/{ip}/country_name/")

        self.luci = LuciClient(
            base_url=cfg.get("router", "url", fallback="http://192.168.1.1"),
            username=cfg.get("router", "username", fallback="root"),
            password=cfg.get("router", "password", fallback=""),
        )

        self.state = "error"       # vpn / novpn / error / busy
        self.ext_ip = "—"
        self.ext_country = "—"
        self.awg_up: bool | None = None
        self._lock = threading.Lock()
        self._stop_event = threading.Event()
        self._tray: pystray.Icon | None = None
        self._busy = False

    # ---- public state getters ----------------------------------------
    @property
    def tooltip(self) -> str:
        lines = ["OpenWRT Monitor"]
        lines.append(f"AWG: {'UP ✓' if self.awg_up else 'DOWN ✗' if self.awg_up is False else '?'}")
        lines.append(f"IP: {self.ext_ip}")
        lines.append(f"Страна: {self.ext_country}")
        return "\n".join(lines)

    # ---- actions (called from tray menu) ----------------------------
    def _run_action(self, fn, *args):
        """Run a blocking action in a daemon thread; set busy state."""
        def _worker():
            with self._lock:
                self._busy = True
                self._update_tray_icon("busy")
            try:
                fn(*args)
            finally:
                with self._lock:
                    self._busy = False
                self._poll_now()
        t = threading.Thread(target=_worker, daemon=True)
        t.start()

    def action_awg_up(self, icon=None, item=None):
        log.info("Action: ifup %s", self.awg_iface)
        self._run_action(self.luci.ifup, self.awg_iface)

    def action_awg_down(self, icon=None, item=None):
        log.info("Action: ifdown %s", self.awg_iface)
        self._run_action(self.luci.ifdown, self.awg_iface)

    def action_awg_restart(self, icon=None, item=None):
        log.info("Action: restart %s", self.awg_iface)
        self._run_action(self.luci.ifrestart, self.awg_iface)

    def action_wan_restart(self, icon=None, item=None):
        log.info("Action: restart %s", self.wan_iface)
        self._run_action(self.luci.ifrestart, self.wan_iface)

    def action_open_luci(self, icon=None, item=None):
        url = self.cfg.get("router", "url", fallback="http://192.168.1.1")
        webbrowser.open(f"{url}/cgi-bin/luci/")

    def action_refresh(self, icon=None, item=None):
        threading.Thread(target=self._poll_now, daemon=True).start()

    def action_quit(self, icon=None, item=None):
        self._stop_event.set()
        if self._tray:
            self._tray.stop()

    # ---- polling -------------------------------------------------------
    def _get_external_ip(self) -> tuple[str, str]:
        try:
            resp = requests.get(self.ip_check_url, timeout=6)
            ip = resp.text.strip()
            country_url = self.country_check_url.replace("{ip}", ip)
            try:
                country = requests.get(country_url, timeout=6).text.strip()
            except Exception:
                country = "—"
            return ip, country
        except Exception as exc:
            log.warning("IP check failed: %s", exc)
            return "—", "—"

    def _poll_now(self):
        """Single poll cycle."""
        awg_up = self.luci.get_iface_up(self.awg_iface)
        ext_ip, ext_country = self._get_external_ip()

        with self._lock:
            self.awg_up = awg_up
            self.ext_ip = ext_ip
            self.ext_country = ext_country
            if not self._busy:
                if awg_up is None:
                    new_state = "error"
                elif awg_up:
                    new_state = "vpn"
                else:
                    new_state = "novpn"
                self.state = new_state

        self._update_tray_icon(self.state)

    def _update_tray_icon(self, state: str):
        if self._tray is None:
            return
        try:
            self._tray.icon = _make_icon(state)
            self._tray.title = self.tooltip
        except Exception as exc:
            log.debug("Tray update error: %s", exc)

    def _poll_loop(self):
        while not self._stop_event.is_set():
            try:
                if not self._busy:
                    self._poll_now()
            except Exception as exc:
                log.error("Poll loop error: %s", exc)
            self._stop_event.wait(self.poll_interval)

    # ---- tray setup ----------------------------------------------------
    def build_menu(self) -> pystray.Menu:
        return pystray.Menu(
            pystray.MenuItem("🛡  Запустить AWG (ifup)",    self.action_awg_up),
            pystray.MenuItem("⛔  Остановить AWG (ifdown)", self.action_awg_down),
            pystray.MenuItem("🔄  Перезапустить AWG",        self.action_awg_restart),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("🌐  Перезапустить WAN",        self.action_wan_restart),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("🔃  Обновить статус",          self.action_refresh),
            pystray.MenuItem("🖥  Открыть LuCI в браузере",  self.action_open_luci),
            pystray.Menu.SEPARATOR,
            pystray.MenuItem("❌  Выход",                    self.action_quit),
        )

    def run(self):
        # Initial login
        self.luci.login()

        # Start poll loop in background
        poll_thread = threading.Thread(target=self._poll_loop, daemon=True)
        poll_thread.start()

        # Create tray icon
        icon = pystray.Icon(
            "openwrtmon",
            _make_icon("error"),
            title="OpenWRT Monitor\nЗагрузка…",
            menu=self.build_menu(),
        )
        self._tray = icon

        # Kick off first poll immediately
        threading.Thread(target=self._poll_now, daemon=True).start()

        log.info("Starting tray icon…")
        icon.run()
        log.info("Tray icon stopped.")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------
def main():
    if not CONFIG_PATH.exists():
        print(f"Config not found: {CONFIG_PATH}")
        sys.exit(1)
    cfg = load_config()
    monitor = Monitor(cfg)
    monitor.run()


if __name__ == "__main__":
    main()
