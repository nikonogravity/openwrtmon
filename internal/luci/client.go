package luci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Client struct {
	baseURL    string
	username   string
	password   string
	authToken  string
	isUbus     bool
	httpClient *http.Client
	mu         sync.Mutex
}

func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		httpClient: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

// ---- Modern OpenWRT ubus JSON-RPC 2.0 ------------------------------------

type ubusReq struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type ubusResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) callUbus(object, method string, args interface{}) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/ubus/", c.baseURL)

	token := c.authToken
	if token == "" {
		token = "00000000000000000000000000000000"
	}

	reqBody, err := json.Marshal(ubusReq{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "call",
		Params:  []interface{}{token, object, method, args},
	})
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var uResp ubusResp
	if err := json.Unmarshal(body, &uResp); err != nil {
		return nil, err
	}

	if uResp.Error != nil {
		return nil, fmt.Errorf("ubus error %d: %s", uResp.Error.Code, uResp.Error.Message)
	}

	return uResp.Result, nil
}

func (c *Client) loginUbus() (bool, error) {
	// Call session login
	res, err := c.callUbus("session", "login", map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return false, err
	}

	// Result in ubus is: [ 0, { "ubus_rpc_session": "..." } ]
	var rawArr []json.RawMessage
	if err := json.Unmarshal(res, &rawArr); err != nil || len(rawArr) < 2 {
		return false, fmt.Errorf("bad ubus login format")
	}

	var code int
	if err := json.Unmarshal(rawArr[0], &code); err == nil && code != 0 {
		return false, fmt.Errorf("ubus login code %d", code)
	}

	var sessionData struct {
		Session string `json:"ubus_rpc_session"`
	}
	if err := json.Unmarshal(rawArr[1], &sessionData); err != nil || sessionData.Session == "" {
		return false, fmt.Errorf("no session token in ubus response")
	}

	c.authToken = sessionData.Session
	c.isUbus = true
	return true, nil
}

// ---- Legacy OpenWRT LuCI RPC ---------------------------------------------

type legacyReq struct {
	Method string        `json:"method"`
	ID     int           `json:"id"`
	Params []interface{} `json:"params"`
}

type legacyResp struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  interface{}     `json:"error"`
}

func (c *Client) callLegacy(service, method string, params []interface{}) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/cgi-bin/luci/rpc/%s", c.baseURL, service)
	if c.authToken != "" {
		url += fmt.Sprintf("?auth=%s", c.authToken)
	}

	reqBody, _ := json.Marshal(legacyReq{
		Method: method,
		ID:     1,
		Params: params,
	})

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rResp legacyResp
	if err := json.Unmarshal(body, &rResp); err != nil {
		return nil, err
	}
	if rResp.Error != nil {
		return nil, fmt.Errorf("RPC error: %v", rResp.Error)
	}

	return rResp.Result, nil
}

func (c *Client) loginLegacy() (bool, error) {
	params := []interface{}{c.username, c.password}
	res, err := c.callLegacy("auth", "login", params)
	if err != nil {
		return false, err
	}

	var token string
	if err := json.Unmarshal(res, &token); err != nil || token == "" || token == strings.Repeat("0", 32) {
		return false, fmt.Errorf("invalid legacy token")
	}

	c.authToken = token
	c.isUbus = false
	return true, nil
}

// ---- Unified Public API --------------------------------------------------

func (c *Client) Login() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Try modern OpenWRT /ubus/ endpoint first
	if ok, err := c.loginUbus(); ok && err == nil {
		return true, nil
	}

	// 2. Fallback to legacy /cgi-bin/luci/rpc/auth
	return c.loginLegacy()
}

func (c *Client) EnsureAuth() error {
	if c.authToken != "" {
		return nil
	}
	ok, err := c.Login()
	if !ok || err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	return nil
}

func (c *Client) IfUp(iface string) error {
	if err := c.EnsureAuth(); err != nil {
		return err
	}

	if c.isUbus {
		// Modern OpenWRT: file.exec /sbin/ifup [iface]
		args := map[string]interface{}{
			"command": "/sbin/ifup",
			"params":  []string{iface},
		}
		_, err := c.callUbus("file", "exec", args)
		return err
	}

	// Legacy OpenWRT: sys.exec "ifup iface"
	_, err := c.callLegacy("sys", "exec", []interface{}{fmt.Sprintf("ifup %s", iface)})
	return err
}

func (c *Client) IfDown(iface string) error {
	if err := c.EnsureAuth(); err != nil {
		return err
	}

	if c.isUbus {
		// Modern OpenWRT: file.exec /sbin/ifdown [iface]
		args := map[string]interface{}{
			"command": "/sbin/ifdown",
			"params":  []string{iface},
		}
		_, err := c.callUbus("file", "exec", args)
		return err
	}

	// Legacy OpenWRT: sys.exec "ifdown iface"
	_, err := c.callLegacy("sys", "exec", []interface{}{fmt.Sprintf("ifdown %s", iface)})
	return err
}

func (c *Client) IfRestart(iface string) error {
	if err := c.IfDown(iface); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return c.IfUp(iface)
}

// EnsureFallbackRoute guarantees that when AWG is turned off,
// the router's main routing table has a fallback default route to WAN (metric 100),
// and PBR does not drop non-VPN traffic.
func (c *Client) EnsureFallbackRoute(wanIface string) {
	if err := c.EnsureAuth(); err != nil || !c.isUbus {
		return
	}
	if wanIface == "" {
		wanIface = "wan"
	}

	// 1. Ensure pbr strict_enforcement is 0 so it doesn't kill non-VPN traffic
	seRes, errSe := c.callUbus("uci", "get", map[string]interface{}{
		"config":  "pbr",
		"section": "config",
		"option":  "strict_enforcement",
	})
	if errSe == nil {
		var seArr []json.RawMessage
		if err := json.Unmarshal(seRes, &seArr); err == nil && len(seArr) >= 2 {
			var seData struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(seArr[1], &seData); err == nil && seData.Value == "1" {
				_, _ = c.callUbus("uci", "set", map[string]interface{}{
					"config":  "pbr",
					"section": "config",
					"values": map[string]string{
						"strict_enforcement": "0",
					},
				})
				_, _ = c.callUbus("uci", "apply", map[string]interface{}{"rollback": false})
				_, _ = c.callUbus("rc", "init", map[string]interface{}{"name": "pbr", "action": "reload"})
			}
		}
	}

	// 2. Check if wan_fallback route already exists in network
	res, err := c.callUbus("uci", "get", map[string]interface{}{
		"config":  "network",
		"section": "wan_fallback",
	})
	if err == nil && len(res) > 0 {
		var rawArr []json.RawMessage
		if err := json.Unmarshal(res, &rawArr); err == nil && len(rawArr) >= 2 {
			var code int
			if err := json.Unmarshal(rawArr[0], &code); err == nil && code == 0 {
				return // already configured and active
			}
		}
	}

	// 3. Add wan_fallback route to /etc/config/network
	_, _ = c.callUbus("uci", "add", map[string]interface{}{
		"config": "network",
		"type":   "route",
		"name":   "wan_fallback",
	})
	_, _ = c.callUbus("uci", "set", map[string]interface{}{
		"config":  "network",
		"section": "wan_fallback",
		"values": map[string]string{
			"interface": wanIface,
			"target":    "0.0.0.0",
			"netmask":   "0.0.0.0",
			"metric":    "100",
		},
	})
	_, _ = c.callUbus("uci", "apply", map[string]interface{}{
		"rollback": false,
	})
	_, _ = c.callUbus("rc", "init", map[string]interface{}{
		"name":   "network",
		"action": "reload",
	})
}

// GetAWGIface dynamically discovers the AmneziaWG interface.
// If configuredName matches an interface, it is used.
// If not found or if renamed, it auto-detects by protocol "amneziawg".
func (c *Client) GetAWGIface(configuredName string) (string, bool, error) {
	if err := c.EnsureAuth(); err != nil {
		return configuredName, false, err
	}

	if c.isUbus {
		res, err := c.callUbus("network.interface", "dump", map[string]interface{}{})
		if err == nil {
			var rawArr []json.RawMessage
			if err := json.Unmarshal(res, &rawArr); err == nil && len(rawArr) >= 2 {
				var dump struct {
					Interfaces []struct {
						Interface string `json:"interface"`
						Proto     string `json:"proto"`
						Up        bool   `json:"up"`
					} `json:"interface"`
				}
				if err := json.Unmarshal(rawArr[1], &dump); err == nil {
					// 1. Try exact name match first if configured
					if configuredName != "" {
						for _, item := range dump.Interfaces {
							if strings.EqualFold(item.Interface, configuredName) {
								return item.Interface, item.Up, nil
							}
						}
					}

					// 2. Auto-detect by protocol "amneziawg"
					for _, item := range dump.Interfaces {
						p := strings.ToLower(item.Proto)
						if p == "amneziawg" || strings.Contains(p, "amnezia") {
							return item.Interface, item.Up, nil
						}
					}

					// 3. Fallback: check for wireguard or name containing "awg"
					for _, item := range dump.Interfaces {
						p := strings.ToLower(item.Proto)
						if strings.Contains(p, "wireguard") || strings.Contains(strings.ToLower(item.Interface), "awg") {
							return item.Interface, item.Up, nil
						}
					}
				}
			}
		}
	}

	up, err := c.IsIfaceUp(configuredName)
	return configuredName, up, err
}

func (c *Client) IsIfaceUp(iface string) (bool, error) {
	if err := c.EnsureAuth(); err != nil {
		return false, err
	}

	if c.isUbus {
		// 1. Query network.interface dump for 100% reliable real-time interface status
		res, err := c.callUbus("network.interface", "dump", map[string]interface{}{})
		if err == nil {
			var rawArr []json.RawMessage
			if err := json.Unmarshal(res, &rawArr); err == nil && len(rawArr) >= 2 {
				var dump struct {
					Interfaces []struct {
						Interface string `json:"interface"`
						Up        bool   `json:"up"`
					} `json:"interface"`
				}
				if err := json.Unmarshal(rawArr[1], &dump); err == nil {
					for _, item := range dump.Interfaces {
						if strings.EqualFold(item.Interface, iface) {
							return item.Up, nil
						}
					}
				}
			}
		}
	}

	// 2. Legacy fallback
	cmd := fmt.Sprintf(`(ip link show %s 2>/dev/null || ip link show %s0 2>/dev/null) | grep -q -E '<[^>]*UP[^>]*>|state UP' && echo 1 || echo 0`, iface, iface)
	res, err := c.callLegacy("sys", "exec", []interface{}{cmd})
	if err != nil {
		return false, err
	}
	var out string
	_ = json.Unmarshal(res, &out)
	return strings.TrimSpace(out) == "1", nil
}

func (c *Client) Ping() (time.Duration, error) {
	start := time.Now()
	resp, err := c.httpClient.Get(c.baseURL + "/")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return time.Since(start), nil
}
