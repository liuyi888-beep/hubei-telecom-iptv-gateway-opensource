package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(marshalJSON(v))
}

func textResponse(w http.ResponseWriter, contentType, body string) {
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

func htmlResponse(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(body))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func (g *Gateway) refreshPayload() map[string]any {
	state := g.getRefreshState()
	out := map[string]any{
		"interval_hours":     g.cfg.RefreshHours,
		"background_enabled": g.cfg.BackgroundRefresh,
		"cache_fresh":        false,
		"running":            state.Running,
	}
	if state.Running {
		out["running_force"] = state.Force
	}
	if state.StartedAt != "" {
		out["started_at"] = state.StartedAt
	}
	if state.FinishedAt != "" {
		out["finished_at"] = state.FinishedAt
	}
	if state.LastError != "" {
		out["last_error"] = state.LastError
	}
	if last, ok := g.lastRefreshAt(); ok {
		next := last.Add(time.Duration(max(1, g.cfg.RefreshHours)) * time.Hour)
		out["last_refresh"] = last.Format(time.RFC3339)
		out["last_refresh_age_seconds"] = int(time.Since(last).Seconds())
		out["last_refresh_age"] = humanDuration(time.Since(last))
		out["next_refresh"] = next.Format(time.RFC3339)
		out["next_refresh_in_seconds"] = int(time.Until(next).Seconds())
		out["cache_fresh"] = time.Now().Before(next)
	}
	return out
}

func (g *Gateway) statusPayload() map[string]any {
	cc, pc, _ := g.counts()
	return map[string]any{
		"name":    "湖北电信IPTV网关 Go",
		"version": "1.0.1",
		"server": map[string]any{
			"listen_host":          g.cfg.ListenHost,
			"listen_port":          g.cfg.ListenPort,
			"public_base_url":      g.cfg.publicBaseURL(),
			"rtsp_listen_host":     g.cfg.RTSPListenHost,
			"rtsp_listen_port":     g.cfg.RTSPListenPort,
			"rtsp_public_base_url": g.cfg.rtspBaseURL(),
		},
		"auth_status": g.authStatus,
		"cache": map[string]any{
			"channel_count":         cc,
			"program_count":         pc,
			"catchup_channel_count": countCatchup(g.getChannels()),
		},
		"refresh": g.refreshPayload(),
		"routes": map[string]any{
			"status":        "/status.json",
			"auth":          "/api/auth/status",
			"channels":      "/api/channels",
			"diyp":          "/diyp/live.txt",
			"rtp2httpd":     "/rtp2httpd_catchup.m3u",
			"ku9":           "/ku9.m3u",
			"epg":           "/epg.xml",
			"catchup":       g.cfg.rtspBaseURL() + "/catchup",
			"catchup_ts":    "/catchup.ts",
			"catchup_debug": "/catchup_debug",
			"catchup_log":   "/catchup_log",
			"refresh":       "/refresh?force=1",
		},
		"last_refresh_error": g.lastRefreshError,
	}
}

func routeLink(path, label string) string {
	return `<a href="` + html.EscapeString(path) + `">` + html.EscapeString(label) + `</a>`
}

func (g *Gateway) homeHTML() string {
	cc, pc, _ := g.counts()
	catchupCount := countCatchup(g.getChannels())
	authText, authClass := "未登录", "bad"
	if g.authStatus.OK {
		authText, authClass = "已登录", "ok"
	}
	lastText, nextText, cacheText, cacheClass := "无", "等待首次刷新", "待刷新", "bad"
	if last, ok := g.lastRefreshAt(); ok {
		next := last.Add(time.Duration(max(1, g.cfg.RefreshHours)) * time.Hour)
		lastText = last.Format("2006-01-02 15:04:05") + "（" + humanDuration(time.Since(last)) + " 前）"
		nextText = next.Format("2006-01-02 15:04:05")
		if time.Now().Before(next) {
			cacheText, cacheClass = "有效", "ok"
		}
	}
	bgText := "关闭"
	if g.cfg.BackgroundRefresh {
		bgText = "开启"
	}
	errText := "无"
	if g.lastRefreshError != "" {
		errText = g.lastRefreshError
	}
	routes := []string{
		routeLink("/ku9.m3u", "酷9 M3U"),
		routeLink("/rtp2httpd_catchup.m3u", "rtp2httpd M3U"),
		routeLink("/diyp/live.txt", "DIYP 直播源"),
		routeLink("/epg.xml", "XMLTV EPG"),
		routeLink("/status.json", "状态 JSON"),
		routeLink("/api/channels", "频道列表"),
		routeLink("/catchup_log", "回看日志"),
	}
	return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>湖北电信IPTV网关 Go</title>
<style>
body{margin:0;background:#f6f7f9;color:#1f2933;font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
main{max-width:960px;margin:0 auto;padding:28px 18px}
h1{margin:0 0 4px;font-size:26px}h2{margin:0 0 12px;font-size:17px}
.sub{color:#64748b;margin-bottom:20px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px;margin:16px 0}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:8px;padding:14px}
.label{color:#64748b;font-size:12px}.value{font-size:22px;font-weight:650;margin-top:2px}
.ok{color:#067647}.bad{color:#b42318}.muted{color:#64748b}
.panel{background:#fff;border:1px solid #e5e7eb;border-radius:8px;padding:16px;margin-top:14px}
.row{display:flex;gap:8px;flex-wrap:wrap}.row a,.btn{display:inline-block;border:1px solid #cbd5e1;border-radius:6px;background:#fff;color:#0f172a;text-decoration:none;padding:7px 10px}
.btn{cursor:pointer;font:inherit}.btn:disabled{opacity:.55;cursor:not-allowed}.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;word-break:break-all}
table{width:100%;border-collapse:collapse}td{padding:6px 0;border-bottom:1px solid #eef2f7}td:first-child{color:#64748b;width:130px}
</style>
</head>
<body><main>
<h1>湖北电信IPTV网关 Go</h1>
<div class="sub">HTTP :` + strconv.Itoa(g.cfg.ListenPort) + ` · RTSP :` + strconv.Itoa(g.cfg.RTSPListenPort) + `/catchup</div>
<div class="grid">
<div class="card"><div class="label">认证</div><div class="value ` + authClass + `">` + html.EscapeString(authText) + `</div></div>
<div class="card"><div class="label">频道</div><div class="value">` + strconv.Itoa(cc) + `</div></div>
<div class="card"><div class="label">节目</div><div class="value">` + strconv.Itoa(pc) + `</div></div>
<div class="card"><div class="label">可回看</div><div class="value">` + strconv.Itoa(catchupCount) + `</div></div>
</div>
<div class="panel">
<h2>缓存与刷新</h2>
<table>
<tr><td>缓存状态</td><td class="` + cacheClass + `">` + html.EscapeString(cacheText) + `</td></tr>
<tr><td>上次刷新</td><td>` + html.EscapeString(lastText) + `</td></tr>
<tr><td>下次刷新</td><td>` + html.EscapeString(nextText) + `</td></tr>
<tr><td>刷新间隔</td><td>` + strconv.Itoa(g.cfg.RefreshHours) + ` 小时</td></tr>
<tr><td>后台刷新</td><td>` + html.EscapeString(bgText) + `</td></tr>
<tr><td>最后错误</td><td>` + html.EscapeString(errText) + `</td></tr>
</table>
<div style="margin-top:12px"><button id="refreshBtn" class="btn" type="button">强制刷新</button> <span id="refreshMsg" class="muted"></span></div>
</div>
<div class="panel">
<h2>订阅与调试</h2>
<div class="row">` + strings.Join(routes, "") + `</div>
<p class="muted">RTSP 回看入口：</p>
<p class="mono">` + html.EscapeString(g.cfg.rtspBaseURL()+"/catchup") + `</p>
</div>
<script>
const refreshBtn=document.getElementById('refreshBtn');
const refreshMsg=document.getElementById('refreshMsg');
async function updateRefreshState(){
  try{
    const r=await fetch('/status.json',{cache:'no-store'});
    const j=await r.json();
    const running=!!(j.refresh&&j.refresh.running);
    refreshBtn.disabled=running;
    refreshMsg.textContent=running?'正在刷新...':'';
  }catch(e){}
}
refreshBtn.addEventListener('click',async()=>{
  refreshBtn.disabled=true;
  refreshMsg.textContent='已提交，正在刷新...';
  try{
    const r=await fetch('/refresh?force=1',{cache:'no-store'});
    const j=await r.json();
    if(!j.ok&&j.running){
      refreshMsg.textContent='已经在刷新中...';
    }else if(!j.ok){
      refreshMsg.textContent=j.error||j.message||'刷新提交失败';
      refreshBtn.disabled=false;
    }else{
      refreshMsg.textContent='已提交，正在刷新...';
    }
  }catch(e){
    refreshMsg.textContent='刷新提交失败';
    refreshBtn.disabled=false;
  }
});
setInterval(updateRefreshState,5000);
updateRefreshState();
</script>
</main></body></html>`
}

func (g *Gateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		htmlResponse(w, g.homeHTML())
	})
	mux.HandleFunc("/status.json", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, g.statusPayload()) })
	mux.HandleFunc("/api/auth/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, g.authStatus) })
	mux.HandleFunc("/api/channels", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, g.getChannels())
	})
	mux.HandleFunc("/diyp/live.txt", func(w http.ResponseWriter, r *http.Request) { textResponse(w, "text/plain", g.diyp()) })
	mux.HandleFunc("/ku9.m3u", func(w http.ResponseWriter, r *http.Request) { textResponse(w, "audio/x-mpegurl", g.ku9M3U()) })
	mux.HandleFunc("/rtp2httpd_catchup.m3u", func(w http.ResponseWriter, r *http.Request) { textResponse(w, "audio/x-mpegurl", g.rtp2M3U()) })
	mux.HandleFunc("/epg.xml", func(w http.ResponseWriter, r *http.Request) { textResponse(w, "application/xml", g.xmltv()) })
	mux.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
		if !g.refreshAsync(context.Background(), force) {
			writeJSON(w, 409, map[string]any{"ok": false, "running": true, "message": "refresh already running", "refresh": g.refreshPayload()})
			return
		}
		writeJSON(w, 202, map[string]any{"ok": true, "accepted": true, "refresh": g.refreshPayload()})
	})
	mux.HandleFunc("/catchup.ts", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		if start == "" {
			start = q.Get("playseek")
		}
		if q.Get("channel_id") == "" || start == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "missing channel_id/start"})
			return
		}
		g.streamTS(w, r, q.Get("channel_id"), start, q.Get("end"), q.Get("time_mode"))
	})
	mux.HandleFunc("/catchup_debug", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start := q.Get("start")
		if start == "" {
			start = q.Get("playseek")
		}
		final, info := g.resolveFinalCatchup(q.Get("channel_id"), start, q.Get("end"), q.Get("time_mode"))
		if final == "" {
			writeJSON(w, 502, info)
		} else {
			writeJSON(w, 200, info)
		}
	})
	mux.HandleFunc("/catchup_log", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, g.loadCatchupLogs()) })
	return cors(mux)
}

func (g *Gateway) runHTTP(ctx context.Context, onStarted func()) error {
	addr := g.cfg.ListenHost + ":" + strconv.Itoa(g.cfg.ListenPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: g.handler(), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	log.Printf("HTTP listening on http://%s", server.Addr)
	go func() {
		errCh <- server.Serve(ln)
	}()
	if onStarted != nil {
		onStarted()
	}
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	case err := <-errCh:
		return err
	}
}
