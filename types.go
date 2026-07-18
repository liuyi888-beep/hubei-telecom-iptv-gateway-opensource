package main

import "time"

type Channel struct {
	ID               string `json:"channel_id"`
	Name             string `json:"name"`
	Index            string `json:"channel_index"`
	LiveURL          string `json:"live_url"`
	FCC              string `json:"fcc,omitempty"`
	Group            string `json:"group"`
	APIType          string `json:"api_type"`
	Catchup          bool   `json:"catchup"`
	TimeshiftURL     string `json:"timeshift_url,omitempty"`
	TimeshiftEnabled bool   `json:"timeshift_enabled"`
	TimeshiftLength  int    `json:"timeshift_length"`
	FetchedAt        int64  `json:"fetched_at_ts,omitempty"`
}

type Program struct {
	ID           int64     `json:"id,omitempty"`
	ChannelID    string    `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	Name         string    `json:"name"`
	PrevueCode   string    `json:"prevuecode"`
	Start        time.Time `json:"start"`
	End          time.Time `json:"end"`
	PlayURL      string    `json:"play_url,omitempty"`
	PlayURLError string    `json:"play_url_error,omitempty"`
}

type AuthStatus struct {
	OK              bool   `json:"ok"`
	Mode            string `json:"mode"`
	Message         string `json:"message"`
	UserTokenLength int    `json:"user_token_len,omitempty"`
	DynamicChannels int    `json:"dynamic_channels,omitempty"`
	LastLogin       string `json:"last_login,omitempty"`
}

type RedirectHop struct {
	Status      int    `json:"status"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Server      string `json:"server,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SDPBytes    int    `json:"sdp_bytes,omitempty"`
}

type CatchupInfo struct {
	ChannelID         string        `json:"channel_id"`
	InputStart        string        `json:"input_start"`
	InputEnd          string        `json:"input_end"`
	Catchup           bool          `json:"catchup"`
	Matched           bool          `json:"matched"`
	MatchedMode       string        `json:"matched_mode,omitempty"`
	RouteMode         string        `json:"route_mode,omitempty"`
	PlayURLSource     string        `json:"play_url_source,omitempty"`
	Playseek          string        `json:"playseek,omitempty"`
	Program           *Program      `json:"program,omitempty"`
	RTSPStartUTC      string        `json:"rtsp_start_utc,omitempty"`
	RTSPEndUTC        string        `json:"rtsp_end_utc,omitempty"`
	TimeshiftLength   int           `json:"timeshift_length_seconds,omitempty"`
	QuickPathEligible bool          `json:"timeshift_quick_path_eligible"`
	RedirectHops      []RedirectHop `json:"rtsp_redirect_hops,omitempty"`
	FinalRTSPHost     string        `json:"final_rtsp_host,omitempty"`
	FinalRTSPPort     int           `json:"final_rtsp_port,omitempty"`
	Error             string        `json:"error,omitempty"`
	FirstError        string        `json:"rtsp_first_error,omitempty"`
	Time              string        `json:"time,omitempty"`
}
