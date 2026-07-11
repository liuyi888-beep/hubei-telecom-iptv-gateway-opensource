package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

func splitCatchupTimes(start, end string) (string, string) {
	start = strings.TrimSpace(strings.TrimPrefix(start, "Playseek="))
	end = strings.TrimSpace(end)
	if strings.Contains(start, "-") && end == "" {
		p := strings.SplitN(start, "-", 2)
		start, end = strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
	}
	return start, end
}

func parseCatchupCandidates(start, end, mode string) []struct {
	mode       string
	start, end time.Time
} {
	start, end = splitCatchupTimes(start, end)
	modes := []string{mode}
	if mode != "local" && mode != "utc" {
		modes = []string{"local", "utc"}
	}
	out := []struct {
		mode       string
		start, end time.Time
	}{}
	for _, m := range modes {
		var st, en time.Time
		var err error
		if m == "utc" {
			st, err = parseUTC14(start)
			st = st.In(shanghai)
			if end != "" {
				en, _ = parseUTC14(end)
				en = en.In(shanghai)
			}
		} else {
			st, err = parseLocal14(start)
			if end != "" {
				en, _ = parseLocal14(end)
			}
		}
		if err == nil {
			out = append(out, struct {
				mode       string
				start, end time.Time
			}{m, st, en})
		}
	}
	return out
}

func (g *Gateway) channelByID(id string) (Channel, bool) {
	for _, c := range g.getChannels() {
		if c.ID == id {
			return c, true
		}
	}
	return Channel{}, false
}

func (g *Gateway) resolveCatchupPlayURL(channelID, startS, endS, mode string, forceTVOD bool) (string, CatchupInfo) {
	startS, endS = splitCatchupTimes(startS, endS)
	if mode == "" {
		mode = g.cfg.CatchupTimeMode
	}
	info := CatchupInfo{ChannelID: channelID, InputStart: startS, InputEnd: endS}
	ch, ok := g.channelByID(channelID)
	if !ok {
		info.Error = "channel not found"
		return "", info
	}
	info.DetectedCatchup = ch.Catchup
	var p *Program
	var selectedMode string
	var reqStart, reqEnd time.Time
	for _, c := range parseCatchupCandidates(startS, endS, mode) {
		found, err := g.findProgram(channelID, c.start, g.cfg.CatchupToleranceSeconds)
		if err == nil && found != nil {
			p = found
			selectedMode = c.mode
			reqStart = c.start
			reqEnd = c.end
			break
		}
	}
	if p == nil {
		info.Error = "no program contains requested start"
		return "", info
	}
	if reqEnd.IsZero() || !reqEnd.After(reqStart) {
		reqEnd = p.End
	}
	now := nowLocal()
	length := ch.TimeshiftLength
	if length <= 0 {
		length = g.cfg.TimeshiftFallbackHours * 3600
	}
	window := max(0, length-g.cfg.TimeshiftSafetySeconds)
	within := reqStart.After(now.Add(-time.Duration(window)*time.Second)) && reqStart.Before(now.Add(2*time.Minute))
	quick := !forceTVOD && within && ch.TimeshiftEnabled && strings.HasPrefix(ch.TimeshiftURL, "rtsp://")
	if quick && reqEnd.After(now) {
		reqEnd = now
	}
	info.Matched = true
	info.MatchedMode = selectedMode
	info.Program = p
	info.TimeshiftLength = length
	info.QuickPathEligible = quick
	info.RTSPStartUTC = utcYMDHMS(reqStart)
	info.RTSPEndUTC = utcYMDHMS(reqEnd)
	baseURL := ""
	if quick {
		info.RouteMode = "current_timeshift"
		info.PlayURLSource = "channel_timeshift_url"
		baseURL = ch.TimeshiftURL
	} else {
		info.RouteMode = "history_playseek"
		info.PlayURLSource = "getTVODPlayURL"
		if err := g.resolvePlayURL(p, forceTVOD); err != nil {
			info.Error = "play_url not resolved: " + err.Error()
			return "", info
		}
		_ = g.updatePlayURL(*p)
		baseURL = p.PlayURL
	}
	info.Playseek = info.RTSPStartUTC + "-" + info.RTSPEndUTC
	return setPlayseek(baseURL, info.RTSPStartUTC, info.RTSPEndUTC), info
}

func (g *Gateway) resolveFinalCatchup(channelID, start, end, mode string) (string, CatchupInfo) {
	entry, info := g.resolveCatchupPlayURL(channelID, start, end, mode, false)
	if entry == "" {
		return "", info
	}
	final, hops, err := g.resolveRTSPChain(entry)
	if err != nil {
		info.FirstError = err.Error()
		log.Printf("RTSP preferred path failed source=%s error=%v; fallback TVOD", info.PlayURLSource, err)
		entry, info2 := g.resolveCatchupPlayURL(channelID, start, end, mode, true)
		info2.FirstError = info.FirstError
		info = info2
		if entry == "" {
			return "", info
		}
		final, hops, err = g.resolveRTSPChain(entry)
		if err != nil {
			info.Error = "RTSP redirect resolution failed: " + err.Error()
			return "", info
		}
	}
	u, _ := url.Parse(final)
	info.RedirectHops = hops
	info.FinalRTSPHost = u.Hostname()
	info.FinalRTSPPort = 554
	if u.Port() != "" {
		fmt.Sscanf(u.Port(), "%d", &info.FinalRTSPPort)
	}
	return final, info
}
