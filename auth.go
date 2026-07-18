package main

import (
	"crypto/des"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	reEncryptToken = []*regexp.Regexp{
		regexp.MustCompile(`(?i)CTCGetAuthInfo\(['"]([^'"]+)['"]\)`),
		regexp.MustCompile(`(?i)EncryptToken\s*=\s*['"]([^'"]+)['"]`),
		regexp.MustCompile(`(?i)encryptToken\s*=\s*['"]([^'"]+)['"]`),
	}
	reAuthAction = regexp.MustCompile(`(?i)<form[^>]+action=['"]([^'"]*GetUserToken[^'"]*)['"]`)
	reUserToken  = []*regexp.Regexp{
		regexp.MustCompile(`(?i)"UserToken"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`(?i)"userToken"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`(?i)UserToken\s*=\s*['"]?([^'"&<>\s;]+)`),
		regexp.MustCompile(`(?i)X-Frame-UserToken\s*[:=]\s*['"]?([^'"&<>\s;]+)`),
		regexp.MustCompile(`(?i)<UserToken>([^<]+)</UserToken>`),
		regexp.MustCompile(`\b([A-Za-z0-9_-]{24,})\b`),
	}
)

func normalizeMAC(mac string) string {
	re := regexp.MustCompile(`[^0-9A-Fa-f]`)
	s := strings.ToLower(re.ReplaceAllString(mac, ""))
	if len(s) != 12 {
		return mac
	}
	parts := make([]string, 6)
	for i := range parts {
		parts[i] = s[i*2 : i*2+2]
	}
	return strings.Join(parts, ":")
}

func desKeyMaterial(password string) []byte {
	b := []byte(password + strings.Repeat("0", 16))
	if len(b) >= 24 {
		return b[:24]
	}
	out := make([]byte, 24)
	copy(out, b)
	for i := len(b); i < 24; i++ {
		out[i] = '0'
	}
	return out
}

func pkcs7(data []byte, size int) []byte {
	n := size - len(data)%size
	out := append([]byte{}, data...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func encryptECB(block interface {
	BlockSize() int
	Encrypt([]byte, []byte)
}, data []byte) []byte {
	out := make([]byte, len(data))
	bs := block.BlockSize()
	for i := 0; i < len(data); i += bs {
		block.Encrypt(out[i:i+bs], data[i:i+bs])
	}
	return out
}

func makeAuthenticator(a AuthConfig, token string) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return "", err
	}
	plain := fmt.Sprintf("%08d$%s$%s$%s$%s$%s$$CTC", n.Int64(), token, a.UserID, a.STBID, a.AuthIP, normalizeMAC(a.MAC))
	key := desKeyMaterial(a.Password)
	var block interface {
		BlockSize() int
		Encrypt([]byte, []byte)
	}
	if string(key[:8]) == string(key[8:16]) || string(key[8:16]) == string(key[16:24]) {
		k := key[:8]
		if string(key[:8]) == string(key[8:16]) {
			k = key[16:24]
		}
		block, err = des.NewCipher(k)
	} else {
		block, err = des.NewTripleDESCipher(key)
	}
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(encryptECB(block, pkcs7([]byte(plain), 8)))), nil
}

func extractEncryptToken(text string) (string, string) {
	text = html.UnescapeString(text)
	token, action := "", ""
	for _, re := range reEncryptToken {
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			token = strings.TrimSpace(m[1])
			break
		}
	}
	if m := reAuthAction.FindStringSubmatch(text); len(m) > 1 {
		action = strings.TrimSpace(m[1])
	}
	return token, action
}

func parseUserToken(text string) string {
	text = html.UnescapeString(text)
	var v any
	if decodeLooseJSON(text, &v) == nil {
		found := ""
		walkMaps(v, func(m map[string]any) {
			if found == "" {
				found = stringValue(m, "UserToken", "userToken", "usertoken", "token", "Token", "X-Frame-UserToken")
			}
		})
		if len(found) >= 8 {
			return found
		}
	}
	for _, re := range reUserToken {
		if m := re.FindStringSubmatch(text); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

func (g *Gateway) fullLogin() error {
	return g.fullLoginWithChannels(true)
}

func (g *Gateway) renewLogin() error {
	return g.fullLoginWithChannels(false)
}

func (g *Gateway) fullLoginWithChannels(updateChannels bool) error {
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	if !updateChannels && !g.lastLogin.IsZero() && time.Since(g.lastLogin) < 10*time.Second && g.authStatus.OK {
		return nil
	}
	a := g.cfg.Auth
	missing := []string{}
	for k, v := range map[string]string{"user_id": a.UserID, "password": a.Password, "stbid": a.STBID, "auth_ip": a.AuthIP, "mac": a.MAC, "platform_base": a.PlatformBase} {
		if v == "" || strings.Contains(v, "请填") {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing auth config: %s", strings.Join(missing, ","))
	}
	loginURL := makeURL(a.PlatformBase, "/iptvepg/platform/index.jsp", url.Values{"UserID": {a.UserID}, "Action": {"Login"}})
	text, final, err := g.requestWithRelogin(http.MethodGet, loginURL, nil, nil, time.Duration(g.cfg.AuthTimeout)*time.Second, false)
	if err != nil {
		return err
	}
	encToken, action := extractEncryptToken(text)
	if encToken == "" {
		return fmt.Errorf("EncryptToken not found")
	}
	if action == "" {
		return fmt.Errorf("GetUserToken action not found")
	} else if u, e := url.Parse(final); e == nil {
		if ref, re := url.Parse(action); re == nil {
			action = u.ResolveReference(ref).String()
		}
	}
	auth, err := makeAuthenticator(a, encToken)
	if err != nil {
		return err
	}
	form := url.Values{"UserID": {a.UserID}, "Authenticator": {auth}}
	text, _, err = g.requestWithRelogin(http.MethodPost, action, form, nil, time.Duration(g.cfg.AuthTimeout)*time.Second, false)
	if err != nil {
		return err
	}
	token := parseUserToken(text)
	if token == "" {
		preview := regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(text), " ")
		preview = regexp.MustCompile(`[A-Za-z0-9_-]{24,}`).ReplaceAllString(preview, "<redacted>")
		if len(preview) > 240 {
			preview = preview[:240]
		}
		return fmt.Errorf("UserToken not found: %s", preview)
	}
	channels, err := g.initEPGSession(token, text, action, updateChannels)
	if err != nil {
		return err
	}
	g.userToken = token
	g.lastLogin = time.Now()
	mode := "renew_login"
	dynamicChannels := len(g.getChannels())
	if updateChannels {
		mode = "full_login"
		dynamicChannels = len(channels)
	}
	g.authStatus = AuthStatus{OK: true, Mode: mode, Message: "login ok", UserTokenLength: len(token), DynamicChannels: dynamicChannels, LastLogin: nowLocal().Format(time.RFC3339)}
	_ = g.stateSet("user_token", token)
	_ = g.stateSet("auth_status", g.authStatus)
	if updateChannels && len(channels) > 0 {
		g.setChannels(channels)
	}
	return nil
}

func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}
