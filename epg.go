package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

func (g *Gateway) epgBase() string {
	if g.cfg.Auth.EPGBase != "" {
		return strings.TrimRight(g.cfg.Auth.EPGBase, "/")
	}
	return strings.TrimRight(g.cfg.EPGBase, "/")
}

func (g *Gateway) epgURL(ch Channel, date, api string) string {
	q := url.Values{"channelID": {ch.ID}, "curdate": {date}, "pageSize": {"999"}, "isJson": {"-1"}, "isAjax": {"1"}}
	path := "/iptvepg/frame226/publicPage/datajsp/prevueList.jsp"
	if api == "prevueListToLive" {
		path = "/iptvepg/frame226/publicPage/datajsp/prevueListToLive.jsp"
		q.Set("pageIndex", "1")
	} else {
		q.Set("isFristDate", "-1")
		q.Set("fileds", "-1")
	}
	return makeURL(g.epgBase(), path, q)
}

func (g *Gateway) tvodURL(code, channelID string) string {
	return makeURL(g.epgBase(), "/iptvepg/frame226/publicPage/datajsp/getTVODPlayURL.jsp", url.Values{"programCode": {code}, "channelID": {channelID}, "isJson": {"-1"}, "isAjax": {"1"}})
}

func normalizeProgram(m map[string]any, ch Channel, date string) (*Program, error) {
	name := stringValue(m, "prevueName", "prevuename", "name", "programName", "programname", "title", "programTitle")
	code := stringValue(m, "prevuecode", "prevueCode", "programCode", "programcode", "code")
	ss := stringValue(m, "startTime", "starttime", "beginTime", "begintime", "start", "stime")
	es := stringValue(m, "endTime", "endtime", "finishTime", "etime", "end")
	if name == "" || code == "" || ss == "" || es == "" {
		return nil, nil
	}
	st, err := parseLocalTime(ss, date)
	if err != nil {
		return nil, err
	}
	en, err := parseLocalTime(es, date)
	if err != nil {
		return nil, err
	}
	if len(es) <= 8 && !en.After(st) {
		en = en.Add(24 * time.Hour)
	}
	return &Program{ChannelID: ch.ID, ChannelName: ch.Name, Name: name, PrevueCode: code, Start: st, End: en}, nil
}

func parsePrograms(text string, ch Channel, date string) []Program {
	var root any
	if decodeLooseJSON(text, &root) != nil {
		return nil
	}
	out := []Program{}
	seen := map[string]bool{}
	walkMaps(root, func(m map[string]any) {
		p, err := normalizeProgram(m, ch, date)
		if err != nil || p == nil {
			return
		}
		key := p.ChannelID + "|" + p.PrevueCode + "|" + p.Start.Format(time.RFC3339)
		if !seen[key] {
			seen[key] = true
			out = append(out, *p)
		}
	})
	return out
}

func (g *Gateway) fetchPrograms(ch Channel, date string) []Program {
	apis := []string{ch.APIType}
	if g.cfg.EPGAutoTryAltAPI {
		for _, x := range []string{"prevueList", "prevueListToLive"} {
			if x != ch.APIType {
				apis = append(apis, x)
			}
		}
	}
	for _, api := range apis {
		text, _, err := g.request(http.MethodGet, g.epgURL(ch, date, api), nil, nil, time.Duration(g.cfg.HTTPTimeout)*time.Second)
		if err == nil {
			if p := parsePrograms(text, ch, date); len(p) > 0 {
				return p
			}
		}
	}
	return nil
}

var reRTSP = regexp.MustCompile(`rtsp://[^\s"'<>\\]+`)

func extractPlayURL(text string) string {
	var root any
	if decodeLooseJSON(text, &root) == nil {
		found := ""
		walkMaps(root, func(m map[string]any) {
			if found == "" {
				for _, k := range []string{"playUrl", "playurl", "PlayUrl", "playURL", "rtspUrl", "rtspurl", "url", "URL"} {
					v := stringValue(m, k)
					if strings.HasPrefix(v, "rtsp://") {
						found = htmlUnescape(v)
						break
					}
				}
			}
		})
		if found != "" {
			return found
		}
	}
	return strings.TrimSuffix(htmlUnescape(reRTSP.FindString(text)), ";")
}

func (g *Gateway) resolvePlayURL(p *Program, force bool) error {
	if !g.cfg.ResolvePlayURL {
		return nil
	}
	if p.PlayURL != "" && !force {
		return nil
	}
	if g.cfg.ProactiveBeforePlayURL && (g.lastLogin.IsZero() || time.Since(g.lastLogin) > time.Duration(g.cfg.PlayURLAuthCheckSeconds)*time.Second) {
		if err := g.fullLogin(); err != nil {
			return err
		}
	}
	text, _, err := g.request(http.MethodGet, g.tvodURL(p.PrevueCode, p.ChannelID), nil, nil, time.Duration(g.cfg.ResolveTimeout)*time.Second)
	if err != nil {
		p.PlayURLError = err.Error()
		return err
	}
	p.PlayURL = extractPlayURL(text)
	if p.PlayURL == "" {
		p.PlayURLError = "getTVODPlayURL returned no rtsp URL"
		return errors.New(p.PlayURLError)
	}
	p.PlayURLError = ""
	return nil
}

func datesAround(back, forward int, includeToday bool) []string {
	today := nowLocal()
	out := []string{}
	for i := -back; i <= forward; i++ {
		if i == 0 && !includeToday {
			continue
		}
		out = append(out, today.AddDate(0, 0, i).Format("20060102"))
	}
	return out
}

func (g *Gateway) lastRefreshAt() (time.Time, bool) {
	raw, _ := g.stateGet("last_refresh_unix")
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, strings.Trim(raw, `"`))
	if err != nil {
		return time.Time{}, false
	}
	return t.In(shanghai), true
}

func (g *Gateway) nextRefreshAt() (time.Time, bool) {
	last, ok := g.lastRefreshAt()
	if !ok {
		return time.Time{}, false
	}
	return last.Add(time.Duration(max(1, g.cfg.RefreshHours)) * time.Hour), true
}

func (g *Gateway) refresh(ctx context.Context, force bool) error {
	g.refreshMu.Lock()
	defer g.refreshMu.Unlock()
	if !force {
		if last, ok := g.lastRefreshAt(); ok && time.Since(last) < time.Duration(g.cfg.RefreshHours)*time.Hour {
			return nil
		}
	}
	if err := g.fullLogin(); err != nil {
		g.lastRefreshError = err.Error()
		return err
	}
	channels := g.getChannels()
	dates := datesAround(g.cfg.DaysBack, g.cfg.DaysForward, g.cfg.IncludeToday)
	log.Printf("refresh EPG channels=%d dates=%d", len(channels), len(dates))
	type job struct {
		ch   Channel
		date string
	}
	jobs := make(chan job)
	results := make(chan []Program)
	workers := max(1, g.cfg.EPGConcurrency)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				results <- g.fetchPrograms(j.ch, j.date)
			}
		}()
	}
	go func() {
		for _, ch := range channels {
			for _, d := range dates {
				jobs <- job{ch, d}
			}
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	programs := []Program{}
	for r := range results {
		programs = append(programs, r...)
	}
	if len(programs) == 0 {
		return fmt.Errorf("EPG refresh returned no programs")
	}
	sort.Slice(programs, func(i, j int) bool {
		if programs[i].ChannelName == programs[j].ChannelName {
			return programs[i].Start.Before(programs[j].Start)
		}
		return programs[i].ChannelName < programs[j].ChannelName
	})
	for i := range channels {
		channels[i].Catchup = channels[i].TimeshiftEnabled && strings.HasPrefix(channels[i].TimeshiftURL, "rtsp://")
	}
	if err := g.saveSnapshot(channels, programs); err != nil {
		return err
	}
	if g.cfg.DetectCatchupCapability {
		sem := make(chan struct{}, max(1, g.cfg.CatchupConcurrency))
		var dwg sync.WaitGroup
		for i := range channels {
			if channels[i].Catchup {
				continue
			}
			dwg.Add(1)
			go func(idx int) {
				defer dwg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ended, _ := g.endedPrograms(channels[idx].ID, 2)
				for j := range ended {
					if g.resolvePlayURL(&ended[j], false) == nil {
						channels[idx].Catchup = true
						break
					}
				}
			}(i)
		}
		dwg.Wait()
	}
	if err := g.updateCatchup(channels); err != nil {
		return err
	}
	g.setChannels(channels)
	_ = g.stateSet("last_refresh_unix", time.Now().Format(time.RFC3339))
	g.lastRefreshError = ""
	log.Printf("refresh complete channels=%d programs=%d catchup=%d", len(channels), len(programs), countCatchup(channels))
	return nil
}

func countCatchup(ch []Channel) int {
	n := 0
	for _, c := range ch {
		if c.CatchupAvailable() {
			n++
		}
	}
	return n
}

func (g *Gateway) scheduler(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(max(1, g.cfg.RefreshHours)) * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.refresh(ctx, true); err != nil {
				log.Printf("scheduled refresh failed: %v", err)
			}
		}
	}
}

func marshalJSON(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
