package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

func nowLocal() time.Time          { return time.Now().In(shanghai) }
func localUnix(t time.Time) int64  { return t.In(shanghai).Unix() }
func utcYMDHMS(t time.Time) string { return t.UTC().Format("20060102150405") }

func parseLocal14(s string) (time.Time, error) {
	return time.ParseInLocation("20060102150405", strings.TrimSpace(strings.TrimSuffix(s, "GMT")), shanghai)
}

func parseUTC14(s string) (time.Time, error) {
	return time.ParseInLocation("20060102150405", strings.TrimSpace(strings.TrimSuffix(s, "GMT")), time.UTC)
}

func parseLocalTime(s, date string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{"2006.01.02 15:04:05", "2006-01-02 15:04:05", "2006/01/02 15:04:05", "20060102150405", "200601021504", "15:04:05", "15:04"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, shanghai); err == nil {
			if strings.HasPrefix(layout, "15:") {
				base, e := time.ParseInLocation("20060102", date, shanghai)
				if e != nil {
					return time.Time{}, e
				}
				return time.Date(base.Year(), base.Month(), base.Day(), t.Hour(), t.Minute(), t.Second(), 0, shanghai), nil
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported local time %q", s)
}

func parseURL(s string) (*url.URL, error) { return url.Parse(strings.TrimSpace(s)) }

func makeURL(base, path string, q url.Values) string {
	u := strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

func normalizeLiveURL(igmp, format string) string {
	if format == "igmp" {
		return igmp
	}
	tail := strings.TrimPrefix(igmp, "igmp://")
	if format == "udp" {
		return "udp://" + tail
	}
	return "rtp://" + tail
}

func guessGroup(name string) string {
	u := strings.ToUpper(name)
	if strings.Contains(u, "CCTV") || strings.Contains(name, "央视") {
		return "央视"
	}
	if strings.Contains(name, "卫视") {
		return "卫视"
	}
	for _, x := range []string{"少儿", "卡通", "动漫", "优漫", "金鹰"} {
		if strings.Contains(name, x) {
			return "少儿动漫"
		}
	}
	for _, x := range []string{"湖北", "荆州", "石首", "洪湖", "松滋", "监利", "公安", "江陵"} {
		if strings.Contains(name, x) {
			return "湖北本地"
		}
	}
	return "其他"
}

func guessEPGAPI(name string) string {
	if strings.Contains(strings.ToUpper(name), "CCTV") {
		return "prevueListToLive"
	}
	return "prevueList"
}

func stripJSONP(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return s
	}
	start := strings.IndexAny(s, "{[")
	end := strings.LastIndexAny(s, "}]")
	if start >= 0 && end > start {
		return strings.TrimSuffix(strings.TrimSpace(s[start:end+1]), ";")
	}
	return s
}

func decodeLooseJSON(s string, dst any) error {
	raw := stripJSONP(s)
	if err := json.Unmarshal([]byte(raw), dst); err == nil {
		return nil
	}
	reTrailing := regexp.MustCompile(`,\s*([}\]])`)
	raw = reTrailing.ReplaceAllString(strings.ReplaceAll(raw, "'", `"`), "$1")
	return json.Unmarshal([]byte(raw), dst)
}

func stringValue(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func walkMaps(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, v2 := range x {
			walkMaps(v2, fn)
		}
	case []any:
		for _, v2 := range x {
			walkMaps(v2, fn)
		}
	}
}

func setPlayseek(raw, startUTC, endUTC string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("Playseek", startUTC+"-"+endUTC)
	u.RawQuery = q.Encode()
	return u.String()
}

func redactRTSP(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	sensitive := map[string]bool{"authinfo": true, "usercode": true, "userid": true, "stbid": true, "usersessionid": true, "iptvsessionid": true, "crypt": true, "purl": true}
	q := u.Query()
	for k := range q {
		if sensitive[strings.ToLower(k)] {
			q.Set(k, "<redacted>")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func escapeM3U(s string) string { return strings.NewReplacer(`&`, `&amp;`, `"`, `'`).Replace(s) }

func sortChannels(ch []Channel) {
	group := map[string]int{"央视": 0, "卫视": 1, "湖北本地": 2, "少儿动漫": 3, "其他": 99}
	sort.Slice(ch, func(i, j int) bool {
		gi, gj := group[ch[i].Group], group[ch[j].Group]
		if gi != gj {
			return gi < gj
		}
		ai, ei := strconv.Atoi(ch[i].Index)
		aj, ej := strconv.Atoi(ch[j].Index)
		if ei == nil && ej == nil && ai != aj {
			return ai < aj
		}
		return ch[i].Name < ch[j].Name
	})
}

func htmlUnescape(s string) string { return html.UnescapeString(s) }
