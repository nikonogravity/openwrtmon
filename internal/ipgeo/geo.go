package ipgeo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"openwrtmon/internal/icon"
)

type Info struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code"`
	CountryName string `json:"country_name"`
	City        string `json:"city"`
	Org         string `json:"org"`
	EmojiFlag   string `json:"emoji_flag"`
	FlagURL     string `json:"flag_url"`
}

type Service struct {
	client    *http.Client
	flagCache map[string][]byte
	cacheMu   sync.RWMutex
}

func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		flagCache: make(map[string][]byte),
	}
}

// CountryCodeToEmoji converts a 2-letter ISO country code into Unicode Regional Indicator Emoji (e.g. NL -> 🇳🇱)
func CountryCodeToEmoji(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 2 {
		return "🌐"
	}
	r1 := rune(code[0]) - 'A' + 0x1F1E6
	r2 := rune(code[1]) - 'A' + 0x1F1E6
	return string([]rune{r1, r2})
}

// FetchInfo queries public IP geo services with multiple fallbacks
func (s *Service) FetchInfo() (*Info, error) {
	// 1. Try ip-api.com first (ultra fast, ~50-80ms from Russia and Europe)
	info, err := s.tryIpApiCom()
	if err == nil && info.IP != "" {
		return info, nil
	}

	// 2. Try ipwho.is (reliable worldwide, ~100ms)
	info, err = s.tryIpWhoIs()
	if err == nil && info.IP != "" {
		return info, nil
	}

	// 3. Fallback to ipapi.co
	info, err = s.tryIpApiCo()
	if err == nil && info.IP != "" {
		return info, nil
	}

	// 4. Fallback to ipify.org (only IP)
	ip, err := s.tryIpify()
	if err == nil && ip != "" {
		return &Info{
			IP:          ip,
			CountryCode: "??",
			CountryName: "Неизвестно",
			EmojiFlag:   "🌐",
			FlagURL:     "",
		}, nil
	}

	return nil, fmt.Errorf("all IP geo services failed")
}

func (s *Service) tryIpWhoIs() (*Info, error) {
	resp, err := s.client.Get("https://ipwho.is/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipwho.is returned %d", resp.StatusCode)
	}

	var data struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
		Connection  struct {
			ISP string `json:"isp"`
		} `json:"connection"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if !data.Success || data.IP == "" {
		return nil, fmt.Errorf("ipwho.is query failed")
	}

	code := strings.ToUpper(data.CountryCode)
	return &Info{
		IP:          data.IP,
		CountryCode: code,
		CountryName: data.Country,
		City:        data.City,
		Org:         data.Connection.ISP,
		EmojiFlag:   CountryCodeToEmoji(code),
	}, nil
}

func (s *Service) tryIpApiCo() (*Info, error) {
	resp, err := s.client.Get("https://ipapi.co/json/")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipapi.co returned %d", resp.StatusCode)
	}

	var data struct {
		IP          string `json:"ip"`
		CountryCode string `json:"country_code"`
		CountryName string `json:"country_name"`
		City        string `json:"city"`
		Org         string `json:"org"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	code := strings.ToUpper(data.CountryCode)
	return &Info{
		IP:          data.IP,
		CountryCode: code,
		CountryName: data.CountryName,
		City:        data.City,
		Org:         data.Org,
		EmojiFlag:   CountryCodeToEmoji(code),
		FlagURL:     fmt.Sprintf("https://flagcdn.com/w160/%s.png", strings.ToLower(code)),
	}, nil
}

func (s *Service) tryIpApiCom() (*Info, error) {
	resp, err := s.client.Get("http://ip-api.com/json/?fields=status,country,countryCode,city,isp,query")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ip-api.com returned %d", resp.StatusCode)
	}

	var data struct {
		Status      string `json:"status"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		City        string `json:"city"`
		ISP         string `json:"isp"`
		Query       string `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if data.Status != "success" {
		return nil, fmt.Errorf("ip-api returned non-success")
	}

	code := strings.ToUpper(data.CountryCode)
	return &Info{
		IP:          data.Query,
		CountryCode: code,
		CountryName: data.Country,
		City:        data.City,
		Org:         data.ISP,
		EmojiFlag:   CountryCodeToEmoji(code),
		FlagURL:     fmt.Sprintf("https://flagcdn.com/w160/%s.png", strings.ToLower(code)),
	}, nil
}

func (s *Service) tryIpify() (string, error) {
	resp, err := s.client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// FetchFlagPNG fetches PNG flag bytes for the country code (cached)
func (s *Service) FetchFlagPNG(code string) ([]byte, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || code == "??" {
		return nil, fmt.Errorf("invalid code")
	}

	// 1. Try built-in instant flag first (0ms, 100% offline)
	if builtin := icon.GetBuiltinFlagPNG(code); len(builtin) > 0 {
		return builtin, nil
	}

	// 2. Check memory cache
	s.cacheMu.RLock()
	cached, ok := s.flagCache[strings.ToLower(code)]
	s.cacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	url := fmt.Sprintf("https://flagcdn.com/w80/%s.png", strings.ToLower(code))
	resp, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flagcdn returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	s.cacheMu.Lock()
	s.flagCache[strings.ToLower(code)] = data
	s.cacheMu.Unlock()

	return data, nil
}

