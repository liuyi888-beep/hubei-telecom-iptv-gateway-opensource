package main

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (g *Gateway) epgURLPublic() string {
	if g.cfg.XMLTVURL != "" {
		return g.cfg.XMLTVURL
	}
	return g.cfg.publicBaseURL() + "/epg.xml"
}
func (g *Gateway) ku9Catchup(c Channel) string {
	return g.cfg.rtspBaseURL() + "/catchup?channel_id=" + url.QueryEscape(c.ID) + "&time_mode=local&playseek=${(b)yyyyMMddHHmmss|Asia/Shanghai}-${(e)yyyyMMddHHmmss|Asia/Shanghai}"
}
func (g *Gateway) httpTSCatchup(c Channel) string {
	a, b := "{utc:YmdHMS}", "{utcend:YmdHMS}"
	mode := "utc"
	if g.cfg.CatchupPlaceholderMode == "local" {
		a, b = "{(b)YmdHMS}", "{(e)YmdHMS}"
		mode = "local"
	}
	return g.cfg.publicBaseURL() + "/catchup.ts?channel_id=" + url.QueryEscape(c.ID) + "&time_mode=" + mode + "&start=" + a + "&end=" + b
}

type channelAPIPayload struct {
	ID               string `json:"channel_id"`
	Name             string `json:"name"`
	Index            string `json:"channel_index"`
	LiveURL          string `json:"live_url"`
	FCC              string `json:"fcc,omitempty"`
	Group            string `json:"group"`
	APIType          string `json:"api_type"`
	Catchup          bool   `json:"catchup"`
	TimeshiftEnabled bool   `json:"timeshift_enabled"`
	TimeshiftLength  int    `json:"timeshift_length"`
	FetchedAt        int64  `json:"fetched_at_ts,omitempty"`
}

func channelAPIPayloads(channels []Channel) []channelAPIPayload {
	out := make([]channelAPIPayload, 0, len(channels))
	for _, c := range channels {
		out = append(out, channelAPIPayload{
			ID:               c.ID,
			Name:             c.Name,
			Index:            c.Index,
			LiveURL:          c.LiveURL,
			FCC:              c.FCC,
			Group:            c.Group,
			APIType:          c.APIType,
			Catchup:          c.Catchup,
			TimeshiftEnabled: c.TimeshiftEnabled,
			TimeshiftLength:  c.TimeshiftLength,
			FetchedAt:        c.FetchedAt,
		})
	}
	return out
}

func uniqueChannelDisplayNames(channels []Channel) map[string]string {
	seen := map[string]int{}
	out := map[string]string{}
	for _, c := range channels {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		seen[name]++
		display := name
		if seen[name] > 1 {
			suffix := strings.TrimSpace(c.Index)
			if suffix == "" {
				suffix = fmt.Sprint(seen[name])
			}
			display = fmt.Sprintf("%s [%s]", name, suffix)
		}
		out[c.ID] = display
	}
	return out
}

func rtp2LiveURL(c Channel) string {
	if c.FCC == "" || !strings.HasPrefix(c.LiveURL, "rtp://") {
		return c.LiveURL
	}
	if strings.Contains(c.LiveURL, "?") {
		return c.LiveURL + "&fcc=" + c.FCC
	}
	return strings.TrimRight(c.LiveURL, "/") + "/?fcc=" + c.FCC
}

func (g *Gateway) ku9M3U() string {
	lines := []string{fmt.Sprintf(`#EXTM3U x-tvg-url="%s"`, g.epgURLPublic())}
	channels := g.getChannels()
	names := uniqueChannelDisplayNames(channels)
	for _, c := range channels {
		if c.Name == "" || c.LiveURL == "" {
			continue
		}
		name := names[c.ID]
		if c.Catchup {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s" catchup="default" catchup-source="%s",%s`, c.ID, name, c.Group, g.ku9Catchup(c), name))
		} else {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s",%s`, c.ID, name, c.Group, name))
		}
		lines = append(lines, c.LiveURL, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

func (g *Gateway) rtp2M3U() string {
	lines := []string{fmt.Sprintf(`#EXTM3U x-tvg-url="%s"`, g.epgURLPublic())}
	channels := g.getChannels()
	names := uniqueChannelDisplayNames(channels)
	for _, c := range channels {
		if c.Name == "" || c.LiveURL == "" {
			continue
		}
		name := names[c.ID]
		if c.Catchup {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s" catchup="default" catchup-days="%d" catchup-source="%s",%s`, c.ID, name, c.Group, g.cfg.CatchupDays, g.httpTSCatchup(c), name))
		} else {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s",%s`, c.ID, name, c.Group, name))
		}
		lines = append(lines, rtp2LiveURL(c), "")
	}
	return strings.Join(lines, "\n") + "\n"
}

func (g *Gateway) diyp() string {
	groups := map[string][]Channel{}
	for _, c := range g.getChannels() {
		groups[c.Group] = append(groups[c.Group], c)
	}
	order := []string{"央视", "卫视", "湖北本地", "少儿动漫", "其他"}
	lines := []string{}
	for _, group := range order {
		ch := groups[group]
		if len(ch) == 0 {
			continue
		}
		lines = append(lines, group+",#genre#")
		for _, c := range ch {
			lines = append(lines, c.Name+","+c.LiveURL)
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func (g *Gateway) xmltv() string {
	programs, _ := g.allPrograms()
	channels := g.getChannels()
	sortChannels(channels)
	lines := []string{`<?xml version="1.0" encoding="UTF-8"?>`, `<tv generator-info-name="iptv-gateway-go">`}
	for _, c := range channels {
		lines = append(lines, fmt.Sprintf(`  <channel id="%s"><display-name lang="zh">%s</display-name></channel>`, xmlEscape(c.ID), xmlEscape(c.Name)))
	}
	sort.Slice(programs, func(i, j int) bool { return programs[i].Start.Before(programs[j].Start) })
	for _, p := range programs {
		lines = append(lines, fmt.Sprintf(`  <programme start="%s +0800" stop="%s +0800" channel="%s"><title lang="zh">%s</title></programme>`, p.Start.In(shanghai).Format("20060102150405"), p.End.In(shanghai).Format("20060102150405"), xmlEscape(p.ChannelID), xmlEscape(p.Name)))
	}
	lines = append(lines, "</tv>")
	return strings.Join(lines, "\n") + "\n"
}
