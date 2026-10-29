package flyout

// State represents the live router and tunnel status displayed in the flyout.
type State struct {
	CountryName  string `json:"countryName"`
	CountryCode  string `json:"countryCode"`
	IP           string `json:"ip"`
	AWGUp        bool   `json:"awgUp"`
	AWGName      string `json:"awgName"`
	PingMs       int64  `json:"pingMs"`
	FlagBase64   string `json:"flagBase64"` // data:image/png;base64,...
	RouterOnline   bool   `json:"routerOnline"`
	AuthError      bool   `json:"authError"`
	InternetOnline bool   `json:"internetOnline"`
	Autostart      bool   `json:"autostart"`
	IsBusy         bool   `json:"isBusy"`
}

// Callbacks holds event handlers triggered by UI clicks.
type Callbacks struct {
	OnToggleAWG       func()
	OnRestartAWG      func()
	OnRestartWAN      func()
	OnOpenLuCI        func()
	OnRefresh         func()
	OnToggleAutostart func()
	OnQuit            func()
}

// Manager defines the interface for the tray flyout popover window.
type Manager interface {
	ShowNear(x, y int32)
	Hide()
	Toggle(x, y int32)
	Update(state State)
	Destroy()
}

