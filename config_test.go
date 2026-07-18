package main

import (
	"os"
	"path/filepath"
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
