package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func decodeBody(body []byte, contentType string) string {
	lower := strings.ToLower(contentType)
	if strings.Contains(lower, "gbk") || strings.Contains(lower, "gb18030") || !utf8.Valid(body) {
		if b, err := io.ReadAll(transform.NewReader(bytes.NewReader(body), simplifiedchinese.GB18030.NewDecoder())); err == nil {
			return string(b)
		}
	}
	return string(body)
}

func (g *Gateway) requestRaw(method, rawURL string, form url.Values, extra map[string]string, timeout time.Duration) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", g.cfg.HTTPUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("X-Requested-With", "com.android.smart.terminal.iptv")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range g.cfg.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	if g.cfg.Cookie != "" {
		req.Header.Set("Cookie", g.cfg.Cookie)
	}
	resp, err := g.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", "", err
	}
	return decodeBody(b, resp.Header.Get("Content-Type")), resp.Request.URL.String(), nil
}

var reRebuildURL = regexp.MustCompile(`(?s)weburl\s*=\s*'([^']*)'\s*\+\s*usertoken\s*\+\s*'([^']*)'`)

func rebuildSessionURL(finalURL, text, token string) string {
	if token == "" {
		return ""
	}
	m := reRebuildURL.FindStringSubmatch(text)
	if len(m) < 3 {
		return ""
	}
	base, err := url.Parse(finalURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(m[1] + url.QueryEscape(token) + m[2])
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

func (g *Gateway) requestWithRelogin(method, rawURL string, form url.Values, extra map[string]string, timeout time.Duration, allowRelogin bool) (string, string, error) {
	text, final, err := g.requestRaw(method, rawURL, form, extra, timeout)
	if err != nil || !g.cfg.AutoRebuildSession || !strings.Contains(text, "rebuildsessionresponse.jsp") {
		return text, final, err
	}
	rebuild := rebuildSessionURL(final, text, g.userToken)
	if rebuild != "" {
		if _, _, err = g.requestRaw(http.MethodGet, rebuild, nil, extra, timeout); err != nil {
			return "", "", err
		}
		text, final, err = g.requestRaw(method, rawURL, form, extra, timeout)
		if err != nil || !strings.Contains(text, "rebuildsessionresponse.jsp") {
			return text, final, err
		}
	}
	if !allowRelogin {
		return text, final, nil
	}
	if err := g.fullLogin(); err != nil {
		return text, final, err
	}
	return g.requestRaw(method, rawURL, form, extra, timeout)
}

func (g *Gateway) request(method, rawURL string, form url.Values, extra map[string]string, timeout time.Duration) (string, string, error) {
	return g.requestWithRelogin(method, rawURL, form, extra, timeout, true)
}

func (g *Gateway) initEPGSession(token string) ([]Channel, error) {
	a := g.cfg.Auth
	timeout := time.Duration(g.cfg.AuthTimeout) * time.Second
	texts := []string{}
	call := func(method, base, path string, q url.Values, form url.Values) {
		u := makeURL(base, path, q)
		text, _, err := g.request(method, u, form, map[string]string{"User-Agent": "webkit;Resolution(PAL,720P,1080P)"}, timeout)
		if err == nil {
			texts = append(texts, text)
		}
	}
	call(http.MethodGet, a.EASIPBase, "/iptvepg/function/index.jsp", url.Values{
		"UserGroupNMB": {a.UserGroupNMB}, "EPGGroupNMB": {a.EPGGroupNMB}, "UserToken": {token}, "UserID": {a.UserID}, "STBID": {a.STBID}, "DynamicAuthIP": {a.DynamicAuthIP}}, nil)
	call(http.MethodGet, a.EPGBase, "/iptvepg/function/index.jsp", url.Values{
		"loadbalanced": {"1"}, "UserIP": {a.EPGUserIP}, "UserID": {a.UserID}, "UserToken": {token}, "STBID": {a.STBID}, "LastTermno": {""}, "easip": {a.EASIP}, "networkid": {a.NetworkID}}, nil)
	call(http.MethodPost, a.EPGBase, "/iptvepg/function/funcportalauth.jsp", nil, url.Values{
		"UserToken": {token}, "UserID": {a.UserID}, "STBID": {a.STBID}, "stbinfo": {""}, "prmid": {""}, "easip": {a.EASIP}, "networkid": {a.NetworkID}, "stbtype": {a.STBType}, "drmsupplier": {""}})
	call(http.MethodGet, a.EPGBase, "/iptvepg/js/setConfig.js", nil, nil)
	call(http.MethodGet, a.EPGBase, "/iptvepg/function/frame.jsp", nil, nil)
	call(http.MethodPost, a.EPGBase, "/iptvepg/function/frameset_judger.jsp", nil, url.Values{"picturetype": {"1,3,5"}})
	call(http.MethodPost, a.EPGBase, "/iptvepg/function/frameset_builder.jsp", nil, url.Values{"MAIN_WIN_SRC": {a.MainWinSrc}, "NEED_UPDATE_STB": {"1"}, "BUILD_ACTION": {"FRAMESET_BUILDER"}, "hdmistatus": {"undefined"}})
	channels := parseChannelConfigs(strings.Join(texts, "\n"), g.cfg.LiveURLFormat)
	if len(channels) == 0 {
		return nil, fmt.Errorf("portal returned no channels")
	}
	return channels, nil
}

var (
	reChannelDouble = regexp.MustCompile(`(?s)jsSetConfig\(\s*['"]Channel['"]\s*,\s*"(.*?)"\s*\)`)
	reChannelSingle = regexp.MustCompile(`(?s)jsSetConfig\(\s*['"]Channel['"]\s*,\s*'(.*?)'\s*\)`)
	reAttr          = regexp.MustCompile(`([A-Za-z0-9_]+)="([^"]*)"`)
)

func parseChannelConfigs(text, format string) []Channel {
	text = htmlUnescape(text)
	blocks := [][]string{}
	for _, re := range []*regexp.Regexp{reChannelDouble, reChannelSingle} {
		blocks = append(blocks, re.FindAllStringSubmatch(text, -1)...)
	}
	seen := map[string]bool{}
	out := []Channel{}
	for _, m := range blocks {
		if len(m) < 2 {
			continue
		}
		block := strings.NewReplacer(`\"`, `"`, `\'`, `'`, `\/`, `/`).Replace(m[1])
		attrs := map[string]string{}
		for _, a := range reAttr.FindAllStringSubmatch(block, -1) {
			attrs[a[1]] = a[2]
		}
		igmp := strings.TrimSpace(attrs["ChannelURL"])
		if !strings.HasPrefix(igmp, "igmp://") {
			continue
		}
		id, name := attrs["ChannelID"], attrs["ChannelName"]
		key := attrs["UserChannelID"] + "|" + name + "|" + igmp
		if id == "" || name == "" || seen[key] {
			continue
		}
		seen[key] = true
		length, _ := strconv.Atoi(attrs["TimeShiftLength"])
		ch := Channel{ID: id, Name: name, Index: attrs["UserChannelID"], LiveURL: normalizeLiveURL(igmp, format), Group: guessGroup(name), APIType: guessEPGAPI(name),
			TimeshiftURL: strings.TrimSpace(attrs["TimeShiftURL"]), TimeshiftEnabled: attrs["TimeShift"] == "1", TimeshiftLength: length}
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool {
		a, ea := strconv.Atoi(out[i].Index)
		b, eb := strconv.Atoi(out[j].Index)
		if ea == nil && eb == nil {
			return a < b
		}
		return out[i].Index < out[j].Index
	})
	return out
}
