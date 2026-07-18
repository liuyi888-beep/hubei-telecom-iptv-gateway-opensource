package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseCTCSetConfigAndDocumentLocation(t *testing.T) {
	src := `
		Authentication.CTCSetConfig('EPGDomain','http://121.60.255.4:8080/iptvepg/function/index.jsp');
		Authentication.CTCSetConfig("UserGroupNMB","73");
		Authentication.CTCSetConfig('EPGGroupNMB','0');
		document.location='http://121.60.255.46:4338/GetChannelList?UserToken=token';
	`
	cfg := parseCTCSetConfig(src)
	if cfg["EPGDomain"] != "http://121.60.255.4:8080/iptvepg/function/index.jsp" || cfg["UserGroupNMB"] != "73" || cfg["EPGGroupNMB"] != "0" {
		t.Fatalf("ctc config=%+v", cfg)
	}
	next := parseDocumentLocation(src)
	if next != "http://121.60.255.46:4338/GetChannelList?UserToken=token" {
		t.Fatalf("next=%q", next)
	}
}

func TestParseHiddenInputsFromNamedForm(t *testing.T) {
	src := `<form name="authform" method="post">
		<input type="hidden" name="UserToken" value="token">
		<input type="hidden" name="UserID" value="user">
		<input type="hidden" name="STBID" value="stb">
		<input type="hidden" name="easip" value="121.60.255.4">
		<input type="hidden" name="networkid" value="1">
	</form>`
	got := parseHiddenInputs(src, "authform")
	want := url.Values{"UserToken": {"token"}, "UserID": {"user"}, "STBID": {"stb"}, "easip": {"121.60.255.4"}, "networkid": {"1"}}
	for k, v := range want {
		if got.Get(k) != v[0] {
			t.Fatalf("%s=%q all=%v", k, got.Get(k), got)
		}
	}
}

func TestBuildEPGDiscoveryFromAuthPages(t *testing.T) {
	authText := `Authentication.CTCSetConfig('UserGroupNMB','73');
		Authentication.CTCSetConfig('EPGGroupNMB','0');
		document.location='/GetChannelList?UserToken=token';`
	channelListText := `document.location='http://121.60.255.46:4338/GetServiceEntry?UserToken=token';`
	serviceText := `document.location='http://121.60.255.4:8080/iptvepg/function/index.jsp?UserGroupNMB=73&EPGGroupNMB=0&UserToken=token&UserID=user&STBID=stb&DynamicAuthIP=172.168.10.20';`
	easipText := `top.document.location = 'http://121.60.129.244:8080/iptvepg/function/index.jsp?loadbalanced=1&UserIP=10.233.82.236&UserID=user&UserToken=token&STBID=stb&LastTermno=&easip=121.60.255.4&networkid=1';`

	d := newEPGDiscovery("http://121.60.255.46:4338/GetUserToken", authText)
	next, ok := d.nextAuthURL()
	if !ok || next != "http://121.60.255.46:4338/GetChannelList?UserToken=token" {
		t.Fatalf("first next=%q ok=%v", next, ok)
	}
	d.updateFromAuthHop(channelListText, next)
	next, ok = d.nextAuthURL()
	if !ok || next != "http://121.60.255.46:4338/GetServiceEntry?UserToken=token" {
		t.Fatalf("second next=%q ok=%v", next, ok)
	}
	d.updateFromAuthHop(serviceText, next)
	if d.easipEntryURL != "http://121.60.255.4:8080/iptvepg/function/index.jsp?UserGroupNMB=73&EPGGroupNMB=0&UserToken=token&UserID=user&STBID=stb&DynamicAuthIP=172.168.10.20" {
		t.Fatalf("easip=%q", d.easipEntryURL)
	}
	d.updateFromEASIP(easipText)
	if d.epgBase != "http://121.60.129.244:8080" || d.epgEntryURL == "" {
		t.Fatalf("epgBase=%q epgEntry=%q", d.epgBase, d.epgEntryURL)
	}
	if d.epgParams.Get("UserIP") != "10.233.82.236" || d.epgParams.Get("networkid") != "1" {
		t.Fatalf("epg params=%v", d.epgParams)
	}
}

func TestRequestRawRejectsHTTPErrorStatus(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backend failed with secret-token", http.StatusForbidden)
	})}
	defer server.Close()
	go func() { _ = server.Serve(ln) }()

	g := &Gateway{cfg: defaultConfig(), http: &http.Client{}}
	_, _, err = g.requestRaw(http.MethodGet, "http://"+ln.Addr().String(), nil, nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("expected HTTP status error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaked raw body: %v", err)
	}
}
