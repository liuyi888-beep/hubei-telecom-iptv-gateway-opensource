package main

import (
	"crypto/des"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"log"
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

func (g *Gateway) login() (string, error) {
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	return g.loginLocked()
}

func (g *Gateway) ensureLogin() error {
	if g.authSnapshot().OK {
		return nil
	}
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	if g.authSnapshot().OK {
		return nil
	}
	_, err := g.loginLocked()
	return err
}

func (g *Gateway) lastLoginSnapshot() time.Time {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.lastLogin
}

func (g *Gateway) loginAfterRejectedSession(observed time.Time) error {
	g.loginMu.Lock()
	defer g.loginMu.Unlock()
	if g.authSnapshot().OK && g.lastLoginSnapshot().After(observed) {
		return nil
	}
	_, err := g.loginLocked()
	return err
}

func (g *Gateway) loginLocked() (string, error) {
	g.mu.Lock()
	g.authStatus = AuthStatus{Message: "not logged in"}
	g.mu.Unlock()
	fail := func(err error) (string, error) {
		g.mu.Lock()
		g.authStatus = AuthStatus{Message: "login failed", LastError: err.Error()}
		g.mu.Unlock()
		return "", err
	}
	a := g.cfg.Auth
	missing := []string{}
	for k, v := range map[string]string{"user_id": a.UserID, "password": a.Password, "stbid": a.STBID, "auth_ip": a.AuthIP, "mac": a.MAC, "platform_base": a.PlatformBase} {
		if missingAuthConfigValue(v) {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fail(fmt.Errorf("missing auth config: %s", strings.Join(missing, ",")))
	}
	loginURL := makeURL(a.PlatformBase, "/iptvepg/platform/index.jsp", url.Values{"UserID": {a.UserID}, "Action": {"Login"}})
	text, final, err := g.authRequest(http.MethodGet, loginURL, nil, time.Duration(g.cfg.AuthTimeout)*time.Second)
	if err != nil {
		return fail(err)
	}
	encToken, action := extractEncryptToken(text)
	if encToken == "" {
		return fail(fmt.Errorf("EncryptToken not found"))
	}
	if action == "" {
		return fail(fmt.Errorf("GetUserToken action not found"))
	} else if u, e := url.Parse(final); e == nil {
		if ref, re := url.Parse(action); re == nil {
			action = u.ResolveReference(ref).String()
		}
	}
	auth, err := makeAuthenticator(a, encToken)
	if err != nil {
		return fail(err)
	}
	form := url.Values{"UserID": {a.UserID}, "Authenticator": {auth}}
	text, _, err = g.authRequest(http.MethodPost, action, form, time.Duration(g.cfg.AuthTimeout)*time.Second)
	if err != nil {
		return fail(err)
	}
	token := parseUserToken(text)
	if token == "" {
		preview := regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(text), " ")
		preview = regexp.MustCompile(`[A-Za-z0-9_-]{24,}`).ReplaceAllString(preview, "<redacted>")
		if len(preview) > 240 {
			preview = preview[:240]
		}
		return fail(fmt.Errorf("UserToken not found: %s", preview))
	}
	portalText, epgBase, err := g.initEPGSession(token, text, action)
	if err != nil {
		return fail(err)
	}
	now := nowLocal()
	g.mu.Lock()
	g.lastLogin = now
	g.epgBaseURL = strings.TrimRight(epgBase, "/")
	g.authStatus = AuthStatus{OK: true, Message: "login ok"}
	g.mu.Unlock()
	if err := g.stateSet("epg_base", epgBase); err != nil {
		log.Printf("save EPG base failed: %v", err)
	}
	return portalText, nil
}

func missingAuthConfigValue(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.HasPrefix(value, "YOUR_")
}

func drainAndClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}
