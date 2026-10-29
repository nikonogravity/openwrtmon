package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	RouterURL       string `json:"router_url"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	AWGIface        string `json:"awg_iface"`
	WANIface        string `json:"wan_iface"`
	PollIntervalSec int    `json:"poll_interval_sec"`

	path string
	mu   sync.RWMutex
}

func DefaultConfig() *Config {
	return &Config{
		RouterURL:       "http://192.168.1.1",
		Username:        "root",
		Password:        "",
		AWGIface:        "awg",
		WANIface:        "wan",
		PollIntervalSec: 5,
	}
}

// FindConfigFile looks in current working directory, then next to executable, then user config dir.
func FindConfigFile() string {
	candidates := []string{
		"config.ini",
	}

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "config.ini"))
	}

	if userConfig, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(userConfig, "openwrtmon", "config.ini"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "config.ini"
}

// Load loads the configuration from disk or returns defaults.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	cfgPath := FindConfigFile()
	cfg.path = cfgPath

	file, err := os.Open(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Save default config so user can edit it
			_ = cfg.Save()
			return cfg, nil
		}
		return cfg, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.ToLower(line[1 : len(line)-1])
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		// Strip comments in value if any
		if idx := strings.IndexAny(val, ";#"); idx != -1 {
			val = strings.TrimSpace(val[:idx])
		}

		switch currentSection {
		case "router":
			switch key {
			case "url":
				cfg.RouterURL = val
			case "username":
				cfg.Username = val
			case "password":
				cfg.Password = val
			}
		case "interfaces":
			switch key {
			case "awg":
				cfg.AWGIface = val
			case "wan":
				cfg.WANIface = val
			}
		case "settings":
			switch key {
			case "poll_interval":
				if sec, err := strconv.Atoi(val); err == nil && sec > 0 {
					cfg.PollIntervalSec = sec
				}
			}
		}
	}

	return cfg, scanner.Err()
}

// Save writes current config back to disk.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dir := filepath.Dir(c.path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	content := fmt.Sprintf(`[router]
url = %s
username = %s
password = %s

[interfaces]
awg = %s
wan = %s

[settings]
poll_interval = %d
`, c.RouterURL, c.Username, c.Password, c.AWGIface, c.WANIface, c.PollIntervalSec)

	return os.WriteFile(c.path, []byte(content), 0644)
}

func (c *Config) Update(routerURL, username, password, awg, wan string, pollInterval int) error {
	c.mu.Lock()
	c.RouterURL = routerURL
	c.Username = username
	c.Password = password
	c.AWGIface = awg
	c.WANIface = wan
	if pollInterval > 0 {
		c.PollIntervalSec = pollInterval
	}
	c.mu.Unlock()
	return c.Save()
}

