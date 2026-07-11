package main

import (
	"strings"
	"testing"
	"time"
)

func TestStripJSONComments(t *testing.T) {
	in := []byte(`{"url":"http://a/b",// line
"n":1/*block*/}`)
	got := string(stripJSONComments(in))
	if strings.Contains(got, "line") || strings.Contains(got, "block") || !strings.Contains(got, "http://a/b") {
		t.Fatalf("bad stripped JSON: %s", got)
	}
}

func TestTimeConversion(t *testing.T) {
	v, err := parseLocal14("20260710223530")
	if err != nil {
		t.Fatal(err)
	}
	if got := utcYMDHMS(v); got != "20260710143530" {
		t.Fatalf("UTC=%s", got)
	}
	if v.Location() != shanghai {
		t.Fatal("wrong timezone")
	}
	_ = time.Second
}

func TestPlayseekPreservesAuthValue(t *testing.T) {
	u := setPlayseek("rtsp://x/a?AuthInfo=abc%2Fdef%3D%3D&Playseek=old", "20260710143530", "20260710151230")
	if !strings.Contains(u, "Playseek=20260710143530-20260710151230") || !strings.Contains(u, "AuthInfo=abc%2Fdef%3D%3D") {
		t.Fatalf("url=%s", u)
	}
}

func TestChannelParser(t *testing.T) {
	src := `jsSetConfig('Channel','ChannelID="ch1",ChannelName="CCTV1HD",UserChannelID="1",ChannelURL="igmp://239.1.1.1:1234",TimeShift="1",TimeShiftURL="rtsp://1.2.3.4/a?AuthInfo=x",TimeShiftLength="14400"');`
	ch := parseChannelConfigs(src, "rtp")
	if len(ch) != 1 || ch[0].ID != "ch1" || ch[0].TimeshiftLength != 14400 || !ch[0].TimeshiftEnabled {
		t.Fatalf("channels=%+v", ch)
	}
}

func TestRTP2HTTPDCatchupTimeMode(t *testing.T) {
	g := &Gateway{cfg: defaultConfig()}
	g.cfg.PublicBaseURL = "http://gw:8899"
	ch := Channel{ID: "ch1"}
	got := g.httpTSCatchup(ch)
	if !strings.Contains(got, "time_mode=utc") || !strings.Contains(got, "start={utc:YmdHMS}") {
		t.Fatalf("default rtp2httpd catchup url=%s", got)
	}
	g.cfg.CatchupPlaceholderMode = "local"
	got = g.httpTSCatchup(ch)
	if !strings.Contains(got, "time_mode=local") || !strings.Contains(got, "start={(b)YmdHMS}") {
		t.Fatalf("local rtp2httpd catchup url=%s", got)
	}
}
