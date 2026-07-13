package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type AuthConfig struct {
	Enabled       bool   `json:"enabled"`
	UserID        string `json:"user_id"`
	Password      string `json:"password"`
	STBID         string `json:"stbid"`
	AuthIP        string `json:"auth_ip"`
	MAC           string `json:"mac"`
	EPGUserIP     string `json:"epg_user_ip"`
	DynamicAuthIP string `json:"dynamic_auth_ip"`
	PlatformBase  string `json:"platform_base"`
	AuthBase      string `json:"auth_base"`
	EASIPBase     string `json:"easip_base"`
	EPGBase       string `json:"epg_base"`
	EASIP         string `json:"easip"`
	NetworkID     string `json:"networkid"`
	UserGroupNMB  string `json:"user_group_nmb"`
	EPGGroupNMB   string `json:"epg_group_nmb"`
	STBType       string `json:"stbtype"`
	MainWinSrc    string `json:"main_win_src"`
}

type Config struct {
	ListenHost     string `json:"listen_host"`
	ListenPort     int    `json:"listen_port"`
	PublicBaseURL  string `json:"public_base_url"`
	XMLTVURL       string `json:"xmltv_url"`
	DataDir        string `json:"data_dir"`
	HTTPTimeout    int    `json:"http_timeout"`
	AuthTimeout    int    `json:"auth_timeout"`
	ResolveTimeout int    `json:"resolve_timeout"`

	DaysBack          int  `json:"days_back"`
	DaysForward       int  `json:"days_forward"`
	IncludeToday      bool `json:"include_today"`
	CatchupDays       int  `json:"catchup_days"`
	RefreshHours      int  `json:"refresh_interval_hours"`
	BackgroundRefresh bool `json:"background_refresh_enabled"`
	EPGConcurrency    int  `json:"epg_fetch_concurrency"`

	CatchupTimeMode         string `json:"catchup_time_mode"`
	TimeshiftFallbackHours  int    `json:"current_timeshift_window_hours"`
	TimeshiftSafetySeconds  int    `json:"timeshift_safety_margin_seconds"`
	CatchupToleranceSeconds int    `json:"catchup_match_tolerance_seconds"`
	NearbyProgramsSpanHours int    `json:"nearby_programs_span_hours"`
	CatchupPlaceholderMode  string `json:"catchup_placeholder_mode"`
	CatchupLogSize          int    `json:"catchup_log_size"`

	RTSPRedirectEnabled bool   `json:"rtsp_redirect_enabled"`
	RTSPListenHost      string `json:"rtsp_listen_host"`
	RTSPListenPort      int    `json:"rtsp_listen_port"`
	RTSPPublicBaseURL   string `json:"rtsp_public_base_url"`
	RTSPClientTimeout   int    `json:"rtsp_client_timeout_seconds"`
	RTSPRedirectTimeout int    `json:"rtsp_redirect_timeout_seconds"`
	RTSPRedirectMaxHops int    `json:"rtsp_redirect_max_hops"`

	ProactiveLoginEnabled   bool   `json:"proactive_login_enabled"`
	ProactiveBeforePlayURL  bool   `json:"proactive_login_before_playurl"`
	PlayURLAuthCheckSeconds int    `json:"playurl_auth_check_interval_seconds"`
	ProtectOnEmptyRefresh   bool   `json:"protect_cache_on_empty_refresh"`
	ResolvePlayURL          bool   `json:"resolve_play_url"`
	AutoRebuildSession      bool   `json:"auto_rebuild_session"`
	EPGAutoTryAltAPI        bool   `json:"epg_auto_try_alt_api"`
	LiveURLFormat           string `json:"live_url_format"`

	HTTPUserAgent   string            `json:"http_user_agent"`
	Headers         map[string]string `json:"headers"`
	Cookie          string            `json:"cookie"`
	EPGBase         string            `json:"epg_base"`
	RTSPUserAgent   string            `json:"rtsp_user_agent"`
	TSRTSPTransport string            `json:"ts_rtsp_transport"`
	TSMaxConcurrent int               `json:"ts_max_concurrent"`
	TSStartTimeout  int               `json:"ts_start_timeout_seconds"`
	TSIdleTimeout   int               `json:"ts_idle_timeout_seconds"`

	Auth AuthConfig `json:"auth"`
}

func defaultConfig() Config {
	return Config{
		ListenHost: "0.0.0.0", ListenPort: 8899, DataDir: "data",
		HTTPTimeout: 12, AuthTimeout: 20, ResolveTimeout: 12,
		DaysBack: 7, DaysForward: 7, IncludeToday: true, CatchupDays: 7,
		RefreshHours: 24, BackgroundRefresh: true,
		EPGConcurrency:  8,
		CatchupTimeMode: "auto", TimeshiftFallbackHours: 4,
		TimeshiftSafetySeconds: 120, CatchupToleranceSeconds: 120,
		NearbyProgramsSpanHours: 8, CatchupPlaceholderMode: "utc", CatchupLogSize: 100,
		RTSPRedirectEnabled: true, RTSPListenHost: "0.0.0.0", RTSPListenPort: 8555,
		RTSPClientTimeout: 15, RTSPRedirectTimeout: 8, RTSPRedirectMaxHops: 4,
		ProactiveLoginEnabled: true, ProactiveBeforePlayURL: true,
		PlayURLAuthCheckSeconds: 3600, ProtectOnEmptyRefresh: true,
		ResolvePlayURL: true, AutoRebuildSession: true,
		EPGAutoTryAltAPI: true, LiveURLFormat: "rtp",
		HTTPUserAgent: "Mozilla/5.0 (Linux; U; Android 4.4.2; zh-cn) AppleWebKit/534.30 IPTV",
		Headers:       map[string]string{}, EPGBase: "http://121.60.129.244:8080",
		RTSPUserAgent:   "HMTL RTSP 1.0; CTC/2.0",
		TSRTSPTransport: "udp",
		TSMaxConcurrent: 3, TSStartTimeout: 15, TSIdleTimeout: 20,
		Auth: AuthConfig{Enabled: true, PlatformBase: "http://121.60.255.6:8080",
			AuthBase: "http://121.60.255.36:4338", EASIPBase: "http://121.60.255.4:8080",
			EPGBase: "http://121.60.129.244:8080", EASIP: "121.60.255.4", NetworkID: "1",
			UserGroupNMB: "73", EPGGroupNMB: "0", STBType: "TY1613",
			MainWinSrc: "/iptvepg/frame234/portal.jsp"},
	}
}

func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString, escaped := false, false
	for i := 0; i < len(in); {
		ch := in[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			i++
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			i++
			continue
		}
		if ch == '/' && i+1 < len(in) && in[i+1] == '/' {
			i += 2
			for i < len(in) && in[i] != '\n' && in[i] != '\r' {
				i++
			}
			continue
		}
		if ch == '/' && i+1 < len(in) && in[i+1] == '*' {
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		out = append(out, ch)
		i++
	}
	return out
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(stripJSONComments(b), &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	applyLegacyConfig(stripJSONComments(b), &cfg)
	applyEnv(&cfg)
	cfg.PublicBaseURL = strings.TrimRight(cfg.PublicBaseURL, "/")
	cfg.RTSPPublicBaseURL = strings.TrimRight(cfg.RTSPPublicBaseURL, "/")
	if cfg.Auth.EPGBase == "" {
		cfg.Auth.EPGBase = cfg.EPGBase
	}
	return cfg, nil
}

func applyLegacyConfig(raw []byte, c *Config) {
	var legacy struct {
		ProtectOnEmptyRefresh *bool  `json:"protect_sqlite_on_empty_refresh"`
		TSRTSPTransport       string `json:"ffmpeg_rtsp_transport"`
		TSMaxConcurrent       *int   `json:"ffmpeg_max_concurrent"`
		TSStartTimeout        *int   `json:"ffmpeg_start_timeout_seconds"`
		TSIdleTimeout         *int   `json:"ffmpeg_idle_timeout_seconds"`
	}
	if json.Unmarshal(raw, &legacy) != nil {
		return
	}
	if legacy.ProtectOnEmptyRefresh != nil {
		c.ProtectOnEmptyRefresh = *legacy.ProtectOnEmptyRefresh
	}
	if legacy.TSRTSPTransport != "" {
		c.TSRTSPTransport = legacy.TSRTSPTransport
	}
	if legacy.TSMaxConcurrent != nil {
		c.TSMaxConcurrent = *legacy.TSMaxConcurrent
	}
	if legacy.TSStartTimeout != nil {
		c.TSStartTimeout = *legacy.TSStartTimeout
	}
	if legacy.TSIdleTimeout != nil {
		c.TSIdleTimeout = *legacy.TSIdleTimeout
	}
}

func applyEnv(c *Config) {
	setString := func(name string, dst *string) {
		if v := os.Getenv(name); v != "" {
			*dst = v
		}
	}
	setInt := func(name string, dst *int) {
		if v := os.Getenv(name); v != "" {
			if n, e := strconv.Atoi(v); e == nil {
				*dst = n
			}
		}
	}
	setBool := func(name string, dst *bool) {
		if v := os.Getenv(name); v != "" {
			*dst = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
		}
	}
	setString("LISTEN_HOST", &c.ListenHost)
	setInt("LISTEN_PORT", &c.ListenPort)
	setString("GATEWAY_BASE_URL", &c.PublicBaseURL)
	setString("RTSP_LISTEN_HOST", &c.RTSPListenHost)
	setInt("RTSP_LISTEN_PORT", &c.RTSPListenPort)
	setString("RTSP_PUBLIC_BASE_URL", &c.RTSPPublicBaseURL)
	setString("TS_RTSP_TRANSPORT", &c.TSRTSPTransport)
	setBool("RTSP_REDIRECT_ENABLED", &c.RTSPRedirectEnabled)
	setString("USER_ID", &c.Auth.UserID)
	setString("IPTV_PASSWORD", &c.Auth.Password)
	setString("STBID", &c.Auth.STBID)
	setString("MAC", &c.Auth.MAC)
	setString("AUTH_IP", &c.Auth.AuthIP)
	setString("EPG_USER_IP", &c.Auth.EPGUserIP)
	setString("DYNAMIC_AUTH_IP", &c.Auth.DynamicAuthIP)
}

func (c Config) publicBaseURL() string {
	if c.PublicBaseURL != "" {
		return c.PublicBaseURL
	}
	return fmt.Sprintf("http://127.0.0.1:%d", c.ListenPort)
}

func (c Config) rtspBaseURL() string {
	if c.RTSPPublicBaseURL != "" {
		return c.RTSPPublicBaseURL
	}
	host := "127.0.0.1"
	if u, err := parseURL(c.publicBaseURL()); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return fmt.Sprintf("rtsp://%s:%d", host, c.RTSPListenPort)
}
