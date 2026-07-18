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
	src := `jsSetConfig('Channel','ChannelID="ch1",ChannelName="CCTV1HD",UserChannelID="1",ChannelURL="igmp://239.1.1.1:1234",TimeShift="1",TimeShiftURL="rtsp://1.2.3.4/a?AuthInfo=x",TimeShiftLength="14400",FCCEnable="1",FCCFunction="1",ChannelFCCIP="121.60.255.120",ChannelFCCPort="15970"');`
	ch := parseChannelConfigs(src, "rtp")
	if len(ch) != 1 || ch[0].ID != "ch1" || ch[0].TimeshiftLength != 14400 || !ch[0].TimeshiftEnabled || !ch[0].Catchup {
		t.Fatalf("channels=%+v", ch)
	}
	if ch[0].FCC != "121.60.255.120:15970" {
		t.Fatalf("fcc=%q", ch[0].FCC)
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

func TestEPGBaseUsesRuntimeDiscoveredValue(t *testing.T) {
	g := &Gateway{}
	g.setEPGBase("http://121.60.129.244:8080/")
	if got := g.epgBase(); got != "http://121.60.129.244:8080" {
		t.Fatalf("epg base=%q", got)
	}
}

func TestM3UUsesUniqueDisplayNamesForDuplicateChannels(t *testing.T) {
	g := &Gateway{cfg: defaultConfig()}
	g.cfg.PublicBaseURL = "http://gw:8899"
	g.channels = []Channel{
		{ID: "ch5", Name: "CCTV5HD", Index: "5", LiveURL: "rtp://239.1.1.5:5000", Group: "央视"},
		{ID: "ch18", Name: "CCTV5HD", Index: "18", LiveURL: "rtp://239.1.1.18:5018", Group: "央视"},
	}
	got := g.rtp2M3U()
	if !strings.Contains(got, `tvg-id="ch5" tvg-name="CCTV5HD"`) || !strings.Contains(got, ",CCTV5HD\n") {
		t.Fatalf("first duplicate should keep original name:\n%s", got)
	}
	if !strings.Contains(got, `tvg-id="ch18" tvg-name="CCTV5HD [18]"`) || !strings.Contains(got, ",CCTV5HD [18]\n") {
		t.Fatalf("later duplicate should include channel index:\n%s", got)
	}
}

func TestRTP2HTTPDM3UAppendsFCC(t *testing.T) {
	g := &Gateway{cfg: defaultConfig()}
	g.channels = []Channel{
		{ID: "ch1", Name: "CCTV1HD", Index: "1", LiveURL: "rtp://239.253.64.120:5140", Group: "央视", FCC: "121.60.255.120:15970"},
	}
	got := g.rtp2M3U()
	if !strings.Contains(got, "rtp://239.253.64.120:5140/?fcc=121.60.255.120:15970") {
		t.Fatalf("rtp2httpd m3u missing fcc:\n%s", got)
	}
}
