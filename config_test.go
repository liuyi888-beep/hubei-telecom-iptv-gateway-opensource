package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigIgnoresLegacyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := []byte(`{
  "protect_sqlite_on_empty_refresh": false,
  "ffmpeg_rtsp_transport": "tcp",
  "ffmpeg_max_concurrent": 99,
  "ffmpeg_start_timeout_seconds": 91,
  "ffmpeg_idle_timeout_seconds": 92
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ProtectOnEmptyRefresh {
		t.Fatalf("legacy protect_sqlite_on_empty_refresh should be ignored")
	}
	if cfg.TSRTSPTransport != "udp" {
		t.Fatalf("legacy ffmpeg_rtsp_transport should be ignored, got %q", cfg.TSRTSPTransport)
	}
	if cfg.TSMaxConcurrent != 3 || cfg.TSStartTimeout != 15 || cfg.TSIdleTimeout != 20 {
		t.Fatalf("legacy ffmpeg settings should be ignored: concurrent=%d start=%d idle=%d", cfg.TSMaxConcurrent, cfg.TSStartTimeout, cfg.TSIdleTimeout)
	}
}

func TestLoadConfigUsesAuthSessionTTL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := []byte(`{
  "auth_session_ttl_seconds": 90,
  "cookie": "legacy-cookie",
  "auth": {"enabled": true},
  "proactive_login_enabled": false,
  "proactive_login_before_playurl": false,
  "playurl_auth_check_interval_seconds": 1,
  "auto_rebuild_session": false
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthSessionTTLSeconds != 90 {
		t.Fatalf("auth session TTL=%d", cfg.AuthSessionTTLSeconds)
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"enabled"`) || strings.Contains(string(encoded), `"cookie"`) {
		t.Fatalf("obsolete fields remain in config model: %s", encoded)
	}
}
