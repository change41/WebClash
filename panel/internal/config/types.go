// Package config ports server/src/types.ts and settings.ts: the settings /
// profile / kernel data model, defaults, normalization and public projection.
package config

import "webclash/internal/util"

// Settings is the full panel configuration (server/src/types.ts Settings).
type Settings struct {
	Server         ServerSettings      `json:"server"`
	Auth           AuthSettings        `json:"auth"`
	General        GeneralSettings     `json:"general"`
	Ports          PortsSettings       `json:"ports"`
	Authentication []InboundUser       `json:"authentication"`
	Controller     ControllerSettings  `json:"controller"`
	Tun            TunSettings         `json:"tun"`
	Dns            DnsSettings         `json:"dns"`
	Kernel         KernelSettings      `json:"kernel"`
	GeoIP          GeoIPSettings       `json:"geoip"`
	ConnHistory    ConnHistorySettings `json:"connHistory"`
	CustomRules    []string            `json:"customRules"`
}

type ServerSettings struct {
	Port int    `json:"port"`
	Host string `json:"host"`
}

type AuthSettings struct {
	PasswordHash *string `json:"passwordHash"`
	Secret       string  `json:"secret"`
}

type GeneralSettings struct {
	Mode        string `json:"mode"`
	LogLevel    string `json:"logLevel"`
	AllowLan    bool   `json:"allowLan"`
	BindAddress string `json:"bindAddress"`
	Ipv6        bool   `json:"ipv6"`
}

type PortsSettings struct {
	Mixed int `json:"mixed"`
	HTTP  int `json:"http"`
	SOCKS int `json:"socks"`
}

// InboundUser is one username/password pair for proxy port authentication.
// It maps to mihomo's global `authentication` list and applies to the mixed,
// HTTP and SOCKS ports.
type InboundUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ControllerSettings struct {
	Port   int    `json:"port"`
	Secret string `json:"secret"`
}

type TunSettings struct {
	Enable              bool     `json:"enable"`
	Stack               string   `json:"stack"`
	AutoRoute           bool     `json:"autoRoute"`
	AutoDetectInterface bool     `json:"autoDetectInterface"`
	DnsHijack           []string `json:"dnsHijack"`
}

type DnsSettings struct {
	Enable             bool     `json:"enable"`
	Listen             string   `json:"listen"`
	EnhancedMode       string   `json:"enhancedMode"`
	FakeIpRange        string   `json:"fakeIpRange"`
	Nameservers        []string `json:"nameservers"`
	Fallback           []string `json:"fallback"`
	DefaultNameservers []string `json:"defaultNameservers"`
}

type KernelSettings struct {
	AutoStart    bool   `json:"autoStart"`
	AutoDownload bool   `json:"autoDownload"`
	BinaryPath   string `json:"binaryPath"`
	Version      string `json:"version"`
	Mirror       string `json:"mirror"`
}

// GeoIPSettings controls the opt-in connection IP-geolocation feature.
type GeoIPSettings struct {
	Enable bool   `json:"enable"`
	Source string `json:"source"`
}

// ConnHistorySettings controls the in-memory closed-connection retention ring.
type ConnHistorySettings struct {
	Limit int `json:"limit"`
}

// ProfileSettings is the per-subscription auto-update / speed-test config.
type ProfileSettings struct {
	IntervalHours int      `json:"intervalHours"`
	TestUrl       string   `json:"testUrl"`
	TestTimeout   int      `json:"testTimeout"`
	CustomRules   []string `json:"customRules"`
}

// ProfileMeta describes one subscription / local profile.
type ProfileMeta struct {
	ID        string                     `json:"id"`
	Name      string                     `json:"name"`
	Type      string                     `json:"type"`
	URL       *string                    `json:"url,omitempty"`
	CreatedAt int64                      `json:"createdAt"`
	UpdatedAt int64                      `json:"updatedAt"`
	UserInfo  *util.SubscriptionUserInfo `json:"userInfo,omitempty"`
	Settings  ProfileSettings            `json:"settings"`
}

// ProfilesState is the persisted profiles.json shape.
type ProfilesState struct {
	ActiveID *string       `json:"activeId"`
	Profiles []ProfileMeta `json:"profiles"`
}

// DownloadState is the kernel download progress snapshot.
type DownloadState struct {
	Active   bool    `json:"active"`
	Progress int     `json:"progress"`
	Received int64   `json:"received"`
	Total    int64   `json:"total"`
	Stage    *string `json:"stage"`
	Version  *string `json:"version"`
	Error    *string `json:"error"`
}

// BinaryStatus describes the resolved mihomo binary.
type BinaryStatus struct {
	Exists  bool    `json:"exists"`
	Path    *string `json:"path"`
	Version *string `json:"version"`
	Managed bool    `json:"managed"`
}

// KernelStatus is the kernel runtime snapshot returned by the API.
type KernelStatus struct {
	State     string        `json:"state"`
	Running   bool          `json:"running"`
	PID       *int          `json:"pid"`
	StartedAt *int64        `json:"startedAt"`
	LastError *string       `json:"lastError"`
	Binary    BinaryStatus  `json:"binary"`
	Download  DownloadState `json:"download"`
}

// ProxyDelayResult is a single proxy latency measurement.
type ProxyDelayResult struct {
	Delay int `json:"delay"`
}

// RuleProviderMeta is a rule-provider summary from the Clash API.
type RuleProviderMeta struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Behavior    string  `json:"behavior"`
	VehicleType string  `json:"vehicleType"`
	RuleCount   int     `json:"ruleCount"`
	UpdatedAt   string  `json:"updatedAt"`
	Format      *string `json:"format,omitempty"`
}

// RuleProviderContent is a rule-provider plus its resolved rule lines.
type RuleProviderContent struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Behavior    string   `json:"behavior"`
	VehicleType string   `json:"vehicleType"`
	RuleCount   int      `json:"ruleCount"`
	UpdatedAt   string   `json:"updatedAt"`
	Format      string   `json:"format"`
	Source      string   `json:"source"`
	Rules       []string `json:"rules"`
}
