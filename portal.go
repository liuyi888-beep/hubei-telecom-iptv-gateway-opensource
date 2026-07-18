package main

import (
	"bytes"
	"context"
	"fmt"
	"html"
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
	text := decodeBody(b, resp.Header.Get("Content-Type"))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		preview := regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(text), " ")
		preview = regexp.MustCompile(`[A-Za-z0-9@_=-]{16,}`).ReplaceAllString(preview, "<redacted>")
		preview = regexp.MustCompile(`(?i)(token|auth|password|secret)[A-Za-z0-9@_=-]*`).ReplaceAllString(preview, "<redacted>")
		if len(preview) > 180 {
			preview = preview[:180]
		}
		return "", resp.Request.URL.String(), fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, resp.Request.URL.Host, preview)
	}
	return text, resp.Request.URL.String(), nil
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
	if err := g.renewLogin(); err != nil {
		return text, final, err
	}
	return g.requestRaw(method, rawURL, form, extra, timeout)
}

func (g *Gateway) request(method, rawURL string, form url.Values, extra map[string]string, timeout time.Duration) (string, string, error) {
	return g.requestWithRelogin(method, rawURL, form, extra, timeout, true)
}

type epgDiscovery struct {
	baseURL       string
	nextURL       string
	easipEntryURL string
	epgEntryURL   string
	epgBase       string
	epgParams     url.Values
	ctcConfig     map[string]string
}

func newEPGDiscovery(baseURL, authText string) *epgDiscovery {
	d := &epgDiscovery{baseURL: baseURL, ctcConfig: parseCTCSetConfig(authText)}
	if next := parseDocumentLocation(authText); next != "" {
		d.nextURL = resolveReference(baseURL, next)
	}
	return d
}

func (d *epgDiscovery) nextAuthURL() (string, bool) {
	if d.easipEntryURL != "" || d.nextURL == "" {
		return "", false
	}
	if strings.Contains(d.nextURL, "/iptvepg/function/index.jsp") {
		d.easipEntryURL = d.nextURL
		return "", false
	}
	return d.nextURL, true
}

func (d *epgDiscovery) updateFromAuthHop(text, finalURL string) {
	if next := parseDocumentLocation(text); next != "" {
		d.nextURL = resolveReference(finalURL, next)
		if strings.Contains(d.nextURL, "/iptvepg/function/index.jsp") {
			d.easipEntryURL = d.nextURL
		}
	}
	if d.easipEntryURL == "" && strings.Contains(finalURL, "/iptvepg/function/index.jsp") {
		d.easipEntryURL = finalURL
	}
}

func (d *epgDiscovery) updateFromEASIP(text string) {
	if next := parseDocumentLocation(text); next != "" {
		d.epgEntryURL = resolveReference(d.easipEntryURL, next)
		if u, err := url.Parse(d.epgEntryURL); err == nil {
			d.epgBase = urlOrigin(u)
			d.epgParams = u.Query()
		}
	}
}

func parseCTCSetConfig(text string) map[string]string {
	text = html.UnescapeString(text)
	out := map[string]string{}
	re := regexp.MustCompile(`(?is)CTCSetConfig\(\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]*)['"]\s*\)`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if len(m) > 2 {
			out[strings.TrimSpace(m[1])] = strings.TrimSpace(m[2])
		}
	}
	return out
}

func parseDocumentLocation(text string) string {
	text = html.UnescapeString(text)
	var urls []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?is)(?:top\.)?document\.location\s*=\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?is)window\.location\s*=\s*['"]([^'"]+)['"]`),
	} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if len(m) > 1 {
				urls = append(urls, strings.TrimSpace(m[1]))
			}
		}
	}
	for _, u := range urls {
		if strings.Contains(u, "/iptvepg/function/index.jsp") {
			return u
		}
	}
	if len(urls) > 0 {
		return urls[0]
	}
	return ""
}

func parseHiddenInputs(text, formName string) url.Values {
	text = html.UnescapeString(text)
	source := text
	reAttrTag := regexp.MustCompile(`(?is)([A-Za-z_:][\w:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	if formName != "" {
		reForm := regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
		source = ""
		for _, m := range reForm.FindAllStringSubmatch(text, -1) {
			attrs := map[string]string{}
			for _, a := range reAttrTag.FindAllStringSubmatch(m[1], -1) {
				if len(a) > 4 {
					attrs[strings.ToLower(a[1])] = a[2] + a[3] + a[4]
				}
			}
			if attrs["name"] == formName {
				source = m[2]
				break
			}
		}
		if source == "" {
			return url.Values{}
		}
	}
	values := url.Values{}
	reInput := regexp.MustCompile(`(?is)<input\b[^>]*>`)
	for _, tag := range reInput.FindAllString(source, -1) {
		attrs := map[string]string{}
		for _, m := range reAttrTag.FindAllStringSubmatch(tag, -1) {
			if len(m) > 4 {
				attrs[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
			}
		}
		name := attrs["name"]
		if name != "" {
			values.Set(name, attrs["value"])
		}
	}
	return values
}

func resolveReference(baseURL, ref string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(u).String()
}

func urlOrigin(u *url.URL) string {
	if u == nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (g *Gateway) authRequest(method, rawURL string, form url.Values, timeout time.Duration) (string, string, error) {
	return g.requestWithRelogin(method, rawURL, form, map[string]string{"User-Agent": "webkit;Resolution(PAL,720P,1080P)"}, timeout, false)
}

func (g *Gateway) initEPGSession(token, authText, authAction string, parseChannels bool) ([]Channel, error) {
	a := g.cfg.Auth
	timeout := time.Duration(g.cfg.AuthTimeout) * time.Second
	texts := []string{}
	d := newEPGDiscovery(authAction, authText)
	for i := 0; i < 4; i++ {
		next, ok := d.nextAuthURL()
		if !ok {
			break
		}
		text, final, err := g.authRequest(http.MethodGet, next, nil, timeout)
		if err != nil {
			return nil, err
		}
		if parseChannels {
			texts = append(texts, text)
		}
		d.updateFromAuthHop(text, final)
	}
	if d.easipEntryURL == "" {
		epgDomain := strings.TrimSpace(d.ctcConfig["EPGDomain"])
		if epgDomain != "" {
			q := url.Values{"UserToken": {token}, "UserID": {a.UserID}, "STBID": {a.STBID}}
			if v := strings.TrimSpace(d.ctcConfig["UserGroupNMB"]); v != "" {
				q.Set("UserGroupNMB", v)
			}
			if v := strings.TrimSpace(d.ctcConfig["EPGGroupNMB"]); v != "" {
				q.Set("EPGGroupNMB", v)
			}
			u, err := url.Parse(epgDomain)
			if err == nil {
				base := urlOrigin(u)
				d.easipEntryURL = makeURL(base, u.Path, q)
			}
		}
	}
	if d.easipEntryURL == "" {
		return nil, fmt.Errorf("EPG service entry not found")
	}
	text, _, err := g.authRequest(http.MethodGet, d.easipEntryURL, nil, timeout)
	if err != nil {
		return nil, err
	}
	if parseChannels {
		texts = append(texts, text)
	}
	d.updateFromEASIP(text)
	if d.epgEntryURL == "" || d.epgBase == "" {
		return nil, fmt.Errorf("EPG entry not found")
	}
	g.setEPGBase(d.epgBase)
	_ = g.stateSet("epg_base", g.epgBase())
	epgIndexText, _, err := g.authRequest(http.MethodGet, d.epgEntryURL, nil, timeout)
	if err != nil {
		return nil, err
	}
	if parseChannels {
		texts = append(texts, epgIndexText)
	}
	portalForm := parseHiddenInputs(epgIndexText, "authform")
	if len(portalForm) == 0 {
		return nil, fmt.Errorf("authform not found")
	}
	callEPG := func(method, path string, form url.Values) error {
		text, _, err := g.authRequest(method, makeURL(d.epgBase, path, nil), form, timeout)
		if err != nil {
			return err
		}
		if parseChannels {
			texts = append(texts, text)
		}
		return nil
	}
	if err := callEPG(http.MethodPost, "/iptvepg/function/funcportalauth.jsp", portalForm); err != nil {
		return nil, err
	}
	if err := callEPG(http.MethodGet, "/iptvepg/js/setConfig.js", nil); err != nil {
		return nil, err
	}
	if err := callEPG(http.MethodGet, "/iptvepg/function/frame.jsp", nil); err != nil {
		return nil, err
	}
	judgerText, _, err := g.authRequest(http.MethodPost, makeURL(d.epgBase, "/iptvepg/function/frameset_judger.jsp", nil), url.Values{"picturetype": {"1,3,5"}}, timeout)
	if err != nil {
		return nil, err
	}
	if parseChannels {
		texts = append(texts, judgerText)
	}
	builderForm := parseHiddenInputs(judgerText, "mainWinSrcForm")
	if len(builderForm) == 0 {
		return nil, fmt.Errorf("mainWinSrcForm not found")
	}
	if builderForm.Get("hdmistatus") == "" {
		builderForm.Set("hdmistatus", "undefined")
	}
	if err := callEPG(http.MethodPost, "/iptvepg/function/frameset_builder.jsp", builderForm); err != nil {
		return nil, err
	}
	if !parseChannels {
		return nil, nil
	}
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
		timeshiftURL := strings.TrimSpace(attrs["TimeShiftURL"])
		timeshiftEnabled := attrs["TimeShift"] == "1"
		fcc := ""
		if attrs["FCCEnable"] == "1" && attrs["ChannelFCCIP"] != "" && attrs["ChannelFCCPort"] != "" {
			fcc = attrs["ChannelFCCIP"] + ":" + attrs["ChannelFCCPort"]
		}
		ch := Channel{ID: id, Name: name, Index: attrs["UserChannelID"], LiveURL: normalizeLiveURL(igmp, format), Group: guessGroup(name), APIType: guessEPGAPI(name),
			Catchup: timeshiftEnabled && strings.HasPrefix(timeshiftURL, "rtsp://"), TimeshiftURL: timeshiftURL, TimeshiftEnabled: timeshiftEnabled, TimeshiftLength: length, FCC: fcc}
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
