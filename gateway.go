package main

import (
	"context"
	"log"
	"net/http"
	"net/http/cookiejar"
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
	userToken        string
	lastLogin        time.Time
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
		authStatus: AuthStatus{Mode: "init", Message: "not checked"},
		tsSem:      make(chan struct{}, max(1, cfg.TSMaxConcurrent)),
	}
	if err := g.openCache(); err != nil {
		return nil, err
	}
	if ch, err := g.loadChannels(context.Background()); err == nil {
		g.channels = ch
	}
	if token, _ := g.stateGet("user_token"); token != "" {
		g.userToken = token
	}
	return g, nil
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

func (g *Gateway) logStatus() {
	log.Printf("loaded cached channels=%d", len(g.getChannels()))
}
