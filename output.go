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

func (g *Gateway) ku9M3U() string {
	lines := []string{fmt.Sprintf(`#EXTM3U x-tvg-url="%s"`, g.epgURLPublic())}
	for _, c := range g.getChannels() {
		if c.Name == "" || c.LiveURL == "" {
			continue
		}
		if c.CatchupAvailable() {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s" catchup="default" catchup-source="%s",%s`, c.ID, c.Name, c.Group, g.ku9Catchup(c), c.Name))
		} else {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s",%s`, c.ID, c.Name, c.Group, c.Name))
		}
		lines = append(lines, c.LiveURL, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

func (g *Gateway) rtp2M3U() string {
	lines := []string{fmt.Sprintf(`#EXTM3U x-tvg-url="%s"`, g.epgURLPublic())}
	for _, c := range g.getChannels() {
		if c.Name == "" || c.LiveURL == "" {
			continue
		}
		if c.CatchupAvailable() {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s" catchup="default" catchup-days="%d" catchup-source="%s",%s`, c.ID, c.Name, c.Group, g.cfg.CatchupDays, g.httpTSCatchup(c), c.Name))
		} else {
			lines = append(lines, fmt.Sprintf(`#EXTINF:-1 tvg-id="%s" tvg-name="%s" group-title="%s",%s`, c.ID, c.Name, c.Group, c.Name))
		}
		lines = append(lines, c.LiveURL, "")
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
