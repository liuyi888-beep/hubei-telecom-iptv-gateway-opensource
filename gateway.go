package main

import (
	"context"
	"log"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

type Gateway struct {
	cfg  Config
	db   *bolt.DB
	http *http.Client

	mu               sync.RWMutex
	loginMu          sync.Mutex
	refreshMu        sync.Mutex
	refreshStateMu   sync.Mutex
	channels         []Channel
	authStatus       AuthStatus
	lastLogin        time.Time
	epgBaseURL       string
	lastRefreshError string
	refreshState     RefreshState
	tsSem            chan struct{}
}

type RefreshState struct {
	Running    bool   `json:"running"`
	Force      bool   `json:"force"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

func newGateway(cfg Config) (*Gateway, error) {
	jar, _ := cookiejar.New(nil)
	g := &Gateway{
		cfg:        cfg,
		http:       &http.Client{Jar: jar, Timeout: time.Duration(cfg.HTTPTimeout) * time.Second},
		authStatus: AuthStatus{Message: "not logged in"},
		tsSem:      make(chan struct{}, max(1, cfg.TSMaxConcurrent)),
	}
	if err := g.openCache(); err != nil {
		return nil, err
	}
	for _, key := range []string{"auth_status", "user_token"} {
		if err := g.stateDelete(key); err != nil {
			_ = g.close()
			return nil, err
		}
	}
	if ch, err := g.loadChannels(context.Background()); err == nil {
		g.channels = ch
	}
	if epgBase, _ := g.stateGet("epg_base"); epgBase != "" {
		g.epgBaseURL = epgBase
	}
	return g, nil
}

func (g *Gateway) authSnapshot() AuthStatus {
	g.mu.RLock()
	status := g.authStatus
	lastLogin := g.lastLogin
	ttlSeconds := g.cfg.AuthSessionTTLSeconds
	g.mu.RUnlock()
	if !lastLogin.IsZero() {
		status.LastLogin = lastLogin.Format(time.RFC3339)
	}
	if !status.OK {
		return status
	}
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	if lastLogin.IsZero() {
		status.OK = false
		status.Message = "not logged in"
		return status
	}
	if time.Since(lastLogin) >= time.Duration(ttlSeconds)*time.Second {
		status.OK = false
		status.Message = "session expired"
	}
	return status
}

func (g *Gateway) close() error {
	if g.db != nil {
		return g.db.Close()
	}
	return nil
}

func (g *Gateway) getChannels() []Channel {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Channel, len(g.channels))
	copy(out, g.channels)
	return out
}

func (g *Gateway) setChannels(ch []Channel) {
	sortChannels(ch)
	g.mu.Lock()
	g.channels = ch
	g.mu.Unlock()
}

func (g *Gateway) setEPGBase(epgBase string) {
	g.mu.Lock()
	g.epgBaseURL = strings.TrimRight(epgBase, "/")
	g.mu.Unlock()
}

func (g *Gateway) logStatus() {
	log.Printf("loaded cached channels=%d", len(g.getChannels()))
}
