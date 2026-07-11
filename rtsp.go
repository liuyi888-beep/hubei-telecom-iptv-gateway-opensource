package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

func resolveLocation(base, loc string) string {
	if u, e := url.Parse(loc); e == nil && u.IsAbs() {
		return u.String()
	}
	b, e := url.Parse(base)
	if e != nil {
		return loc
	}
	r, _ := url.Parse(loc)
	return b.ResolveReference(r).String()
}

func (g *Gateway) rtspDescribe(raw string, cseq int) (int, textproto.MIMEHeader, []byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return 0, nil, nil, err
	}
	port := u.Port()
	if port == "" {
		port = "554"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), time.Duration(g.cfg.RTSPRedirectTimeout)*time.Second)
	if err != nil {
		return 0, nil, nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(g.cfg.RTSPRedirectTimeout) * time.Second))
	fmt.Fprintf(conn, "DESCRIBE %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: %s\r\nAccept: application/sdp\r\nConnection: close\r\n\r\n", raw, cseq, g.cfg.RTSPUserAgent)
	r := textproto.NewReader(bufio.NewReader(conn))
	line, err := r.ReadLine()
	if err != nil {
		return 0, nil, nil, err
	}
	var proto, msg string
	var status int
	if _, err = fmt.Sscanf(line, "%s %d", &proto, &status); err != nil {
		return 0, nil, nil, err
	}
	_ = msg
	h, err := r.ReadMIMEHeader()
	if err != nil {
		return 0, nil, nil, err
	}
	n, _ := strconv.Atoi(h.Get("Content-Length"))
	body := make([]byte, n)
	if n > 0 {
		_, err = io.ReadFull(r.R, body)
	}
	return status, h, body, err
}

func (g *Gateway) resolveRTSPChain(entry string) (string, []RedirectHop, error) {
	cur := entry
	seen := map[string]bool{}
	hops := []RedirectHop{}
	for i := 0; i <= g.cfg.RTSPRedirectMaxHops; i++ {
		if seen[cur] {
			return "", hops, fmt.Errorf("RTSP redirect loop")
		}
		seen[cur] = true
		status, h, b, err := g.rtspDescribe(cur, i+1)
		if err != nil {
			return "", hops, err
		}
		u, _ := url.Parse(cur)
		port := 554
		if u.Port() != "" {
			port, _ = strconv.Atoi(u.Port())
		}
		hops = append(hops, RedirectHop{Status: status, Host: u.Hostname(), Port: port, Server: h.Get("Server"), ContentType: h.Get("Content-Type"), SDPBytes: len(b)})
		if status >= 300 && status < 400 {
			loc := h.Get("Location")
			if loc == "" {
				return "", hops, fmt.Errorf("RTSP redirect missing Location")
			}
			cur = resolveLocation(cur, loc)
			continue
		}
		if status >= 200 && status < 300 {
			return cur, hops, nil
		}
		return "", hops, fmt.Errorf("RTSP DESCRIBE status=%d", status)
	}
	return "", hops, fmt.Errorf("too many RTSP redirects")
}

type RTSPRedirectServer struct {
	g  *Gateway
	ln net.Listener
	wg sync.WaitGroup
}

func (s *RTSPRedirectServer) Start() error {
	if !s.g.cfg.RTSPRedirectEnabled {
		return nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.g.cfg.RTSPListenHost, s.g.cfg.RTSPListenPort))
	if err != nil {
		return err
	}
	s.ln = ln
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			s.wg.Add(1)
			go func() { defer s.wg.Done(); s.handle(c) }()
		}
	}()
	log.Printf("RTSP redirect listening on rtsp://%s:%d/catchup", s.g.cfg.RTSPListenHost, s.g.cfg.RTSPListenPort)
	return nil
}
func (s *RTSPRedirectServer) Close() {
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.wg.Wait()
}

func rtspResponse(w *bufio.Writer, code int, reason, cseq string, headers map[string]string) {
	fmt.Fprintf(w, "RTSP/1.0 %d %s\r\nCSeq: %s\r\nServer: Hubei-IPTV-Gateway-Go/1.0\r\n", code, reason, cseq)
	for k, v := range headers {
		fmt.Fprintf(w, "%s: %s\r\n", k, v)
	}
	fmt.Fprint(w, "Content-Length: 0\r\n\r\n")
	_ = w.Flush()
}

func (s *RTSPRedirectServer) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(s.g.cfg.RTSPClientTimeout) * time.Second))
	r := textproto.NewReader(bufio.NewReader(conn))
	w := bufio.NewWriter(conn)
	for {
		line, err := r.ReadLine()
		if err != nil {
			return
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 {
			rtspResponse(w, 400, "Bad Request", "0", nil)
			return
		}
		h, err := r.ReadMIMEHeader()
		if err != nil {
			return
		}
		cseq := h.Get("CSeq")
		method, target := strings.ToUpper(parts[0]), parts[1]
		if method == "OPTIONS" {
			rtspResponse(w, 200, "OK", cseq, map[string]string{"Public": "OPTIONS, DESCRIBE"})
			continue
		}
		if method != "DESCRIBE" {
			rtspResponse(w, 405, "Method Not Allowed", cseq, map[string]string{"Allow": "OPTIONS, DESCRIBE", "Connection": "close"})
			return
		}
		u, err := url.Parse(target)
		if err != nil || strings.TrimRight(u.Path, "/") != "/catchup" {
			rtspResponse(w, 404, "Not Found", cseq, nil)
			return
		}
		q := u.Query()
		channel, start, end, mode := q.Get("channel_id"), q.Get("start"), q.Get("end"), q.Get("time_mode")
		if start == "" {
			start = q.Get("playseek")
		}
		if channel == "" || start == "" || strings.ContainsAny(start, "{}$") {
			rtspResponse(w, 400, "Bad Catchup Parameters", cseq, nil)
			return
		}
		final, info := s.g.resolveFinalCatchup(channel, start, end, mode)
		s.g.addCatchupLog(info)
		if final == "" {
			log.Printf("WARNING RTSP catchup resolve failed: channel=%s error=%s", channel, info.Error)
			rtspResponse(w, 502, "Bad Gateway", cseq, nil)
			return
		}
		log.Printf("RTSP catchup 302: channel=%s source=%s final=%s:%d peer=%v", channel, info.PlayURLSource, info.FinalRTSPHost, info.FinalRTSPPort, conn.RemoteAddr())
		rtspResponse(w, 302, "Moved Temporarily", cseq, map[string]string{"Location": final, "Connection": "close"})
		return
	}
}

func (s *RTSPRedirectServer) Run(ctx context.Context) error {
	if err := s.Start(); err != nil {
		return err
	}
	<-ctx.Done()
	s.Close()
	return nil
}
