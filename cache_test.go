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

func TestGatewayIgnoresCachedAuthenticationState(t *testing.T) {
	cfg := defaultConfig()
	cfg.DataDir = t.TempDir()
	g, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	status := AuthStatus{OK: true, Message: "login ok", LastLogin: "2026-07-19T01:26:46+08:00"}
	if err := g.stateSet("auth_status", status); err != nil {
		t.Fatal(err)
	}
	if err := g.stateSet("user_token", "stale-token"); err != nil {
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
	if snapshot := g2.authSnapshot(); snapshot.OK || snapshot.Message != "not logged in" {
		t.Fatalf("auth snapshot=%+v", snapshot)
	}
	if !g2.lastLogin.IsZero() {
		t.Fatalf("lastLogin restored from cache: %v", g2.lastLogin)
	}
	for _, key := range []string{"auth_status", "user_token"} {
		value, err := g2.stateGet(key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if value != "" {
			t.Fatalf("legacy state %s was not removed: %q", key, value)
		}
	}
}

func TestAuthSnapshotExpiresAfterTTL(t *testing.T) {
	cfg := defaultConfig()
	cfg.AuthSessionTTLSeconds = 60
	g := &Gateway{
		cfg:        cfg,
		authStatus: AuthStatus{OK: true, Message: "login ok", LastLogin: nowLocal().Add(-2 * time.Minute).Format(time.RFC3339)},
		lastLogin:  nowLocal().Add(-2 * time.Minute),
	}
	if snapshot := g.authSnapshot(); snapshot.OK || snapshot.Message != "session expired" {
		t.Fatalf("auth snapshot=%+v", snapshot)
	}
}

func TestAuthSnapshotFormatsLastLoginFromRuntimeTime(t *testing.T) {
	loggedInAt := nowLocal().Add(-time.Minute).Truncate(time.Second)
	g := &Gateway{
		cfg:        defaultConfig(),
		authStatus: AuthStatus{OK: true, Message: "login ok"},
		lastLogin:  loggedInAt,
	}
	if got := g.authSnapshot().LastLogin; got != loggedInAt.Format(time.RFC3339) {
		t.Fatalf("last_login=%q, want %q", got, loggedInAt.Format(time.RFC3339))
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
