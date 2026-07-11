package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtpmpegts"
	"github.com/pion/rtp"
)

func findMPEGTS(desc *description.Session) (*description.Media, *format.MPEGTS) {
	for _, m := range desc.Medias {
		for _, f := range m.Formats {
			if ts, ok := f.(*format.MPEGTS); ok {
				return m, ts
			}
		}
	}
	return nil, nil
}

func (g *Gateway) streamTS(w http.ResponseWriter, r *http.Request, channel, start, end, mode string) {
	select {
	case g.tsSem <- struct{}{}:
		defer func() { <-g.tsSem }()
	case <-r.Context().Done():
		return
	}
	final, info := g.resolveFinalCatchup(channel, start, end, mode)
	info.RouteMode = "http_ts_" + info.RouteMode
	g.addCatchupLog(info)
	if final == "" {
		writeJSON(w, http.StatusBadGateway, info)
		return
	}
	u, err := base.ParseURL(final)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	proto := gortsplib.ProtocolUDP
	if g.cfg.TSRTSPTransport == "tcp" {
		proto = gortsplib.ProtocolTCP
	}
	c := gortsplib.Client{Scheme: u.Scheme, Host: u.Host, Protocol: &proto, AnyPortEnable: true, UserAgent: g.cfg.RTSPUserAgent, ReadTimeout: time.Duration(g.cfg.TSIdleTimeout) * time.Second, WriteTimeout: time.Duration(g.cfg.RTSPRedirectTimeout) * time.Second, InitialUDPReadTimeout: time.Duration(g.cfg.TSStartTimeout) * time.Second, UDPReadBufferSize: 2 << 20}
	if err = c.Start(); err != nil {
		http.Error(w, "RTSP start: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer c.Close()
	desc, _, err := c.Describe(u)
	if err != nil {
		http.Error(w, "RTSP describe: "+err.Error(), http.StatusBadGateway)
		return
	}
	media, forma := findMPEGTS(desc)
	if media == nil {
		http.Error(w, "upstream has no MPEG-TS RTP track", http.StatusBadGateway)
		return
	}
	if _, err = c.Setup(desc.BaseURL, media, 0, 0); err != nil {
		http.Error(w, "RTSP setup: "+err.Error(), http.StatusBadGateway)
		return
	}
	decoder := &rtpmpegts.Decoder{}
	_ = decoder.Init()
	packets := make(chan []byte, 256)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	c.OnPacketRTP(media, forma, func(pkt *rtp.Packet) {
		ts, err := decoder.Decode(pkt)
		if err != nil {
			return
		}
		n := 0
		for _, p := range ts {
			n += len(p)
		}
		buf := make([]byte, 0, n)
		for _, p := range ts {
			buf = append(buf, p...)
		}
		select {
		case packets <- buf:
		case <-ctx.Done():
		}
	})
	if _, err = c.Play(nil); err != nil {
		http.Error(w, "RTSP play: "+err.Error(), http.StatusBadGateway)
		return
	}
	waitErr := make(chan error, 1)
	go func() { waitErr <- c.Wait() }()
	startTimer := time.NewTimer(time.Duration(g.cfg.TSStartTimeout) * time.Second)
	defer startTimer.Stop()
	var first []byte
	select {
	case first = <-packets:
	case err = <-waitErr:
		http.Error(w, "RTSP ended before first packet: "+fmt.Sprint(err), http.StatusBadGateway)
		return
	case <-startTimer.C:
		http.Error(w, "RTSP first packet timeout", http.StatusGatewayTimeout)
		return
	case <-ctx.Done():
		return
	}
	w.Header().Set("Content-Type", "video/MP2T")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	_, _ = w.Write(first)
	if flusher != nil {
		flusher.Flush()
	}
	idle := time.NewTimer(time.Duration(g.cfg.TSIdleTimeout) * time.Second)
	defer idle.Stop()
	total := len(first)
	for {
		select {
		case b := <-packets:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(time.Duration(g.cfg.TSIdleTimeout) * time.Second)
			if _, err = w.Write(b); err != nil {
				return
			}
			total += len(b)
			if flusher != nil {
				flusher.Flush()
			}
		case err := <-waitErr:
			log.Printf("TS stream ended channel=%s bytes=%d error=%v", channel, total, err)
			return
		case <-idle.C:
			log.Printf("TS stream idle timeout channel=%s bytes=%d", channel, total)
			return
		case <-ctx.Done():
			return
		}
	}
}
