package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	defaultConfigPath := filepath.Join("config", "config.json")
	if _, err := os.Stat(defaultConfigPath); err != nil {
		defaultConfigPath = filepath.Join("..", "config", "config.json")
	}
	configPath := flag.String("config", defaultConfigPath, "config JSON path")
	httpPort := flag.Int("http-port", 0, "override HTTP port")
	rtspPort := flag.Int("rtsp-port", 0, "override RTSP port")
	dataDir := flag.String("data-dir", "", "override data directory")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if *httpPort > 0 {
		cfg.ListenPort = *httpPort
	}
	if *rtspPort > 0 {
		cfg.RTSPListenPort = *rtspPort
		cfg.RTSPPublicBaseURL = fmt.Sprintf("rtsp://127.0.0.1:%d", *rtspPort)
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	g, err := newGateway(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer g.close()
	g.logStatus()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rtsp := &RTSPRedirectServer{g: g}
	if err := rtsp.Start(); err != nil {
		log.Fatal(err)
	}
	defer rtsp.Close()
	if err := g.runHTTP(ctx, func() {
		if cfg.BackgroundRefresh {
			go g.scheduler(ctx)
		} else {
			log.Printf("automatic refresh disabled")
		}
	}); err != nil && err != context.Canceled {
		log.Printf("HTTP stopped: %v", err)
	}
}
