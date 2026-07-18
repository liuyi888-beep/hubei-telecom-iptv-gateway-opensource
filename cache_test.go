package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestChannelCachePersistsTimeshiftURLAndFCC(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	g, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()

	channels := []Channel{{
		ID:               "ch1",
		Name:             "CCTV1HD",
		Index:            "1",
		LiveURL:          "rtp://239.1.1.1:1234",
		TimeshiftURL:     "rtsp://121.60.254.112/live/ch1?AuthInfo=secret",
		TimeshiftEnabled: true,
		TimeshiftLength:  14400,
		Catchup:          true,
		FCC:              "121.60.255.120:15970",
	}}
	programs := []Program{{ChannelID: "ch1", ChannelName: "CCTV1HD", Name: "News", PrevueCode: "p1", Start: nowLocal(), End: nowLocal().Add(time.Hour)}}
	if err := g.saveSnapshot(channels, programs); err != nil {
		t.Fatal(err)
	}
	got, err := g.loadChannels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TimeshiftURL == "" || got[0].FCC == "" {
		t.Fatalf("loaded channels=%+v", got)
	}
}

func TestGatewayRestoresAuthStatusAndLastLogin(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	g, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := AuthStatus{OK: true, Mode: "full_login", Message: "login ok", UserTokenLength: 32, LastLogin: "2026-07-19T01:26:46+08:00"}
	if err := g.stateSet("auth_status", status); err != nil {
		t.Fatal(err)
	}
	if err := g.close(); err != nil {
		t.Fatal(err)
	}

	g2, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g2.close()
	if !g2.authStatus.OK || g2.authStatus.LastLogin != status.LastLogin || g2.lastLogin.IsZero() {
		t.Fatalf("authStatus=%+v lastLogin=%v", g2.authStatus, g2.lastLogin)
	}
}

func TestShouldProtectProgramCacheRejectsLargeDrop(t *testing.T) {
	if err := shouldProtectProgramCache(true, 1000, 400); err == nil || !strings.Contains(err.Error(), "below protected threshold") {
		t.Fatalf("expected protected cache error, got %v", err)
	}
	if err := shouldProtectProgramCache(true, 1000, 600); err != nil {
		t.Fatalf("unexpected protection error: %v", err)
	}
	if err := shouldProtectProgramCache(false, 1000, 0); err != nil {
		t.Fatalf("protection disabled should allow update: %v", err)
	}
}
