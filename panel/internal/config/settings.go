package config

import "webclash/internal/util"

var (
	logLevels    = []string{"debug", "info", "warning", "error", "silent"}
	modes        = []string{"rule", "global", "direct"}
	tunStacks    = []string{"gvisor", "system", "mixed"}
	dnsModes     = []string{"fake-ip", "redir-host"}
	geoipSources = []string{"ip2region", "mmdb"}
)

// DefaultSettings returns the built-in defaults (port of defaultSettings).
func DefaultSettings() Settings {
	return Settings{
		Server: ServerSettings{Port: 7788, Host: "0.0.0.0"},
		Auth:   AuthSettings{PasswordHash: nil, Secret: util.RandomToken(32)},
		General: GeneralSettings{
			Mode: "rule", LogLevel: "info", AllowLan: false, BindAddress: "*", Ipv6: false,
		},
		Ports:          PortsSettings{Mixed: 7890},
		Authentication: []InboundUser{},
		Controller:     ControllerSettings{Port: 9090, Secret: util.RandomToken(24)},
		Tun: TunSettings{
			Enable: false, Stack: "mixed", AutoRoute: true, AutoDetectInterface: true,
			DnsHijack: []string{"any:53"},
		},
		Dns: DnsSettings{
			Enable: true, Listen: "0.0.0.0:1053", EnhancedMode: "fake-ip", FakeIpRange: "198.18.0.1/16",
			Nameservers:        []string{"https://223.5.5.5/dns-query", "https://doh.pub/dns-query"},
			Fallback:           []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"},
			DefaultNameservers: []string{"223.5.5.5", "119.29.29.29"},
		},
		Kernel: KernelSettings{
			AutoStart: true, AutoDownload: true, BinaryPath: "", Version: "latest", Mirror: "",
		},
		GeoIP:       GeoIPSettings{Enable: false, Source: "ip2region"},
		SystemProxy: SystemProxySettings{Enable: false, Shell: "", ConfigPath: ""},
		ConnHistory: ConnHistorySettings{Limit: 3000},
		CustomRules: []string{},
	}
}

// DefaultProfileSettings returns per-subscription defaults.
func DefaultProfileSettings() ProfileSettings {
	return ProfileSettings{
		IntervalHours: 0,
		TestUrl:       "https://cp.cloudflare.com/generate_204",
		TestTimeout:   5000,
		CustomRules:   []string{},
	}
}

// NormalizeProfileSettings coerces arbitrary input into valid ProfileSettings.
func NormalizeProfileSettings(input any) ProfileSettings {
	d := DefaultProfileSettings()
	src := obj(input)
	return ProfileSettings{
		IntervalHours: num(get(src, "intervalHours"), d.IntervalHours, 0, 24*30),
		TestUrl:       strOr(get(src, "testUrl"), d.TestUrl),
		TestTimeout:   num(get(src, "testTimeout"), d.TestTimeout, 1000, 60000),
		CustomRules:   strList(get(src, "customRules"), d.CustomRules),
	}
}

// NormalizeSettings coerces any source object into a valid Settings, defending
// against corrupt JSON / frontend input (port of normalizeSettings).
func NormalizeSettings(input any) Settings {
	d := DefaultSettings()
	src := obj(input)

	server := obj(get(src, "server"))
	auth := obj(get(src, "auth"))
	general := obj(get(src, "general"))
	ports := obj(get(src, "ports"))
	controller := obj(get(src, "controller"))
	tun := obj(get(src, "tun"))
	dns := obj(get(src, "dns"))
	kernel := obj(get(src, "kernel"))
	geoip := obj(get(src, "geoip"))
	systemProxy := obj(get(src, "systemProxy"))
	connHistory := obj(get(src, "connHistory"))

	var passwordHash *string
	if s, ok := get(auth, "passwordHash").(string); ok {
		passwordHash = &s
	}
	authSecret := str(get(auth, "secret"), "")
	if authSecret == "" {
		authSecret = util.RandomToken(32)
	}
	controllerSecret := str(get(controller, "secret"), "")
	if controllerSecret == "" {
		controllerSecret = util.RandomToken(24)
	}

	return Settings{
		Server: ServerSettings{
			Port: port(get(server, "port"), d.Server.Port, false),
			Host: strOr(get(server, "host"), d.Server.Host),
		},
		Auth: AuthSettings{PasswordHash: passwordHash, Secret: authSecret},
		General: GeneralSettings{
			Mode:        pick(get(general, "mode"), modes, d.General.Mode),
			LogLevel:    pick(get(general, "logLevel"), logLevels, d.General.LogLevel),
			AllowLan:    boolOr(get(general, "allowLan"), d.General.AllowLan),
			BindAddress: strOr(get(general, "bindAddress"), d.General.BindAddress),
			Ipv6:        boolOr(get(general, "ipv6"), d.General.Ipv6),
		},
		Ports: PortsSettings{
			Mixed: port(get(ports, "mixed"), d.Ports.Mixed, false),
			HTTP:  port(get(ports, "http"), d.Ports.HTTP, true),
			SOCKS: port(get(ports, "socks"), d.Ports.SOCKS, true),
		},
		Authentication: authUsers(get(src, "authentication")),
		Controller: ControllerSettings{
			Port:   port(get(controller, "port"), d.Controller.Port, false),
			Secret: controllerSecret,
		},
		Tun: TunSettings{
			Enable:              boolOr(get(tun, "enable"), d.Tun.Enable),
			Stack:               pick(get(tun, "stack"), tunStacks, d.Tun.Stack),
			AutoRoute:           boolOr(get(tun, "autoRoute"), d.Tun.AutoRoute),
			AutoDetectInterface: boolOr(get(tun, "autoDetectInterface"), d.Tun.AutoDetectInterface),
			DnsHijack:           strList(get(tun, "dnsHijack"), d.Tun.DnsHijack),
		},
		Dns: DnsSettings{
			Enable:             boolOr(get(dns, "enable"), d.Dns.Enable),
			Listen:             strOr(get(dns, "listen"), d.Dns.Listen),
			EnhancedMode:       pick(get(dns, "enhancedMode"), dnsModes, d.Dns.EnhancedMode),
			FakeIpRange:        strOr(get(dns, "fakeIpRange"), d.Dns.FakeIpRange),
			Nameservers:        strList(get(dns, "nameservers"), d.Dns.Nameservers),
			Fallback:           strList(get(dns, "fallback"), d.Dns.Fallback),
			DefaultNameservers: strList(get(dns, "defaultNameservers"), d.Dns.DefaultNameservers),
		},
		Kernel: KernelSettings{
			AutoStart:    boolOr(get(kernel, "autoStart"), d.Kernel.AutoStart),
			AutoDownload: boolOr(get(kernel, "autoDownload"), d.Kernel.AutoDownload),
			BinaryPath:   str(get(kernel, "binaryPath"), ""),
			Version:      strOr(get(kernel, "version"), d.Kernel.Version),
			Mirror:       str(get(kernel, "mirror"), ""),
		},
		GeoIP: GeoIPSettings{
			Enable: boolOr(get(geoip, "enable"), d.GeoIP.Enable),
			Source: pick(get(geoip, "source"), geoipSources, d.GeoIP.Source),
		},
		SystemProxy: SystemProxySettings{
			Enable:     boolOr(get(systemProxy, "enable"), d.SystemProxy.Enable),
			Shell:      str(get(systemProxy, "shell"), ""),
			ConfigPath: str(get(systemProxy, "configPath"), ""),
		},
		ConnHistory: ConnHistorySettings{
			Limit: num(get(connHistory, "limit"), d.ConnHistory.Limit, 100, 20000),
		},
		CustomRules: strList(get(src, "customRules"), d.CustomRules),
	}
}

// PublicAuth is the auth projection exposed to the frontend.
type PublicAuth struct {
	HasPassword bool `json:"hasPassword"`
}

// PublicSettings mirrors Settings but hides passwordHash/secret and exposes
// auth.hasPassword. controller.secret remains visible (port of publicSettings).
type PublicSettings struct {
	Server         ServerSettings      `json:"server"`
	Auth           PublicAuth          `json:"auth"`
	General        GeneralSettings     `json:"general"`
	Ports          PortsSettings       `json:"ports"`
	Authentication []InboundUser       `json:"authentication"`
	Controller     ControllerSettings  `json:"controller"`
	Tun            TunSettings         `json:"tun"`
	Dns            DnsSettings         `json:"dns"`
	Kernel         KernelSettings      `json:"kernel"`
	GeoIP          GeoIPSettings       `json:"geoip"`
	SystemProxy    SystemProxySettings `json:"systemProxy"`
	ConnHistory    ConnHistorySettings `json:"connHistory"`
	CustomRules    []string            `json:"customRules"`
}

// ToPublic projects Settings into its public form.
func ToPublic(s Settings) PublicSettings {
	return PublicSettings{
		Server:         s.Server,
		Auth:           PublicAuth{HasPassword: s.Auth.PasswordHash != nil && *s.Auth.PasswordHash != ""},
		General:        s.General,
		Ports:          s.Ports,
		Authentication: append([]InboundUser{}, s.Authentication...),
		Controller:     s.Controller,
		Tun:            s.Tun,
		Dns:            s.Dns,
		Kernel:         s.Kernel,
		GeoIP:          s.GeoIP,
		SystemProxy:    s.SystemProxy,
		ConnHistory:    s.ConnHistory,
		CustomRules:    append([]string{}, s.CustomRules...),
	}
}
