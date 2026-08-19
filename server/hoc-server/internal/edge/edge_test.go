package edge

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

func getJSON(t *testing.T, method, target string, form url.Values) (int, http.Header, map[string]any) {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("%s %s: decode JSON %q: %v", method, target, rr.Body.String(), err)
	}
	return rr.Code, rr.Header(), got
}

func TestAuthParity(t *testing.T) {
	loadEdgeAccounts(t)

	code, _, fail := getJSON(t, http.MethodPost, "/authorize", url.Values{
		"username": {"gllive:enterpries1"},
		"password": {"wrong"},
		"scope":    {"auth"},
	})
	if code != http.StatusOK || fail["status"] != float64(0) || fail["error_code"] != float64(1) {
		t.Fatalf("auth fail shape: code=%d body=%v", code, fail)
	}
	if fail["error_message"] != "invalid_credentials" || fail["message"] != "invalid_credentials" {
		t.Fatalf("auth fail messages: %v", fail)
	}

	_, _, first := getJSON(t, http.MethodGet, "/BlueFox/authenticate?password=trash&scope=auth", nil)
	if first["status"] != float64(1) || first["username"] != "bluefox" || first["nickname"] != "BlueFox" {
		t.Fatalf("open registration shape: %v", first)
	}
	firstToken, _ := first["access_token"].(string)
	if firstToken == "" || firstToken == accounts.StableAccessToken {
		t.Fatalf("temporary access token=%q", firstToken)
	}

	_, _, second := getJSON(t, http.MethodGet, "/RedWolf/authenticate?password=trash2&scope=auth", nil)
	secondToken, _ := second["access_token"].(string)
	if second["nickname"] != "RedWolf" || secondToken == "" || secondToken == firstToken {
		t.Fatalf("second identity collapsed first=%v second=%v", first, second)
	}

	_, _, profile := getJSON(t, http.MethodGet, "/profiles/me/myprofile?access_token="+url.QueryEscape(firstToken), nil)
	if profile["username"] != "bluefox" || profile["nickname"] != "BlueFox" || profile["display_name"] != "BlueFox" {
		t.Fatalf("profile did not resolve token: %v", profile)
	}

	_, _, device := getJSON(t, http.MethodPost, "/authorize", url.Values{
		"username": {"guest:bootstrap"},
		"scope":    {"bi"},
	})
	if device["status"] != float64(1) || device["accountType"] != "gllive" || device["operation"] != "login" {
		t.Fatalf("device auth shape: %v", device)
	}
	deviceUser, _ := device["username"].(string)
	deviceToken, _ := device["access_token"].(string)
	if !strings.HasPrefix(deviceUser, "device_") || deviceToken == "" || deviceToken == accounts.StableAccessToken {
		t.Fatalf("device auth collapsed to seed: %v", device)
	}
	if _, ok := device["forCredentials"].(string); !ok {
		t.Fatalf("forCredentials must be account id string: %T %v", device["forCredentials"], device["forCredentials"])
	}
	creds, ok := device["credentials"].([]any)
	if !ok || len(creds) != 2 {
		t.Fatalf("device credentials: %T %v", device["credentials"], device["credentials"])
	}
	_, _, deviceProfile := getJSON(t, http.MethodGet,
		"/profiles/me/myprofile?access_token="+url.QueryEscape(deviceToken), nil)
	if deviceProfile["username"] != deviceUser || deviceProfile["nickname"] == "Enterpries1" {
		t.Fatalf("device profile did not resolve its token: %v", deviceProfile)
	}
	_, _, device2 := getJSON(t, http.MethodPost, "/authorize", url.Values{
		"username": {"guest:second-device"},
		"scope":    {"tracking_bi"},
	})
	device2Token, _ := device2["access_token"].(string)
	_, _, linked := getJSON(t, http.MethodPost, "/authenticate", url.Values{
		"username":     {"LinkedNick"},
		"password":     {"pw"},
		"access_token": {device2Token},
		"scope":        {"auth"},
	})
	linkedAccount := accounts.Get("linkednick")
	if linked["nickname"] != "LinkedNick" || linkedAccount == nil || linkedAccount.Gateway != "172.16.57.2" {
		t.Fatalf("new account did not inherit device gateway: body=%v account=%+v", linked, linkedAccount)
	}
}

func loadEdgeAccounts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	raw := map[string]any{
		"accounts": map[string]any{
			"enterpries1": map[string]any{
				"username": "enterpries1", "password": "example-pass-1", "nickname": "Enterpries1",
				"user_id": 1000001, "account_id": "1000001", "gateway": "172.16.42.2",
			},
		},
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGaiaClientUserDataIsScopedToAuthenticatedNickname(t *testing.T) {
	dir := loadEdgeAccounts(t)
	t.Setenv("HOC_ROOT", dir)
	gaiaMu.Lock()
	gaiaStore = nil
	gaiaMu.Unlock()

	first, _, ok := accounts.AuthenticateOrCreate("BlueFox", "pw")
	if !ok {
		t.Fatal("first account registration failed")
	}
	second, _, ok := accounts.AuthenticateOrCreate("RedWolf", "pw")
	if !ok {
		t.Fatal("second account registration failed")
	}
	firstToken, _ := accounts.TokensFor(first)
	secondToken, _ := accounts.TokensFor(second)

	readSeed := func(token string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet,
			"/data/me/HOC%40ClientUserData_codex?access_token="+url.QueryEscape(token), nil)
		rr := httptest.NewRecorder()
		Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("gaia status=%d body=%q", rr.Code, rr.Body.String())
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rr.Body.String()))
		if err != nil {
			t.Fatalf("gaia base64 %q: %v", rr.Body.String(), err)
		}
		return string(decoded)
	}

	firstBlob := readSeed(firstToken)
	secondBlob := readSeed(secondToken)
	if !strings.Contains(firstBlob, "BlueFox") || strings.Contains(firstBlob, "RedWolf") {
		t.Fatalf("first Gaia nick blob=%q", firstBlob)
	}
	if !strings.Contains(secondBlob, "RedWolf") || strings.Contains(secondBlob, "BlueFox") {
		t.Fatalf("second Gaia nick blob=%q", secondBlob)
	}
}

func TestSmallM1RouteParity(t *testing.T) {
	tests := []struct {
		path  string
		check func(*testing.T, map[string]any)
	}{
		{"/encrypt_token", func(t *testing.T, got map[string]any) {
			if got["token"] == "" || got["token"] != got["access_token"] {
				t.Fatalf("encrypt token shape: %v", got)
			}
		}},
		{"/beta/get_product_list", func(t *testing.T, got map[string]any) {
			if _, ok := got["product_list"].([]any); !ok {
				t.Fatalf("product_list missing/wrong: %T %v", got["product_list"], got)
			}
		}},
		{"/transports/wns/endpoints/mock", func(t *testing.T, got map[string]any) {
			if got["access_token"] == "" {
				t.Fatalf("WNS token missing: %v", got)
			}
		}},
		{"/chk_ver", checkNoUpdate},
		{"/download_idx", checkNoUpdate},
		{"/all_dlc", checkNoUpdate},
		{"/collect", checkOK},
		{"/telemetry", checkOK},
		{"/metrics", checkOK},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			code, header, got := getJSON(t, http.MethodGet, tt.path, nil)
			if code != http.StatusOK || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
				t.Fatalf("code/content-type: %d %q", code, header.Get("Content-Type"))
			}
			tt.check(t, got)
		})
	}
}

func checkOK(t *testing.T, got map[string]any) {
	t.Helper()
	if got["status"] != float64(1) || got["error"] != float64(0) {
		t.Fatalf("not OK: %v", got)
	}
}

func checkNoUpdate(t *testing.T, got map[string]any) {
	t.Helper()
	checkOK(t, got)
	if got["update_available"] != float64(0) || got["version"] != "1.6.1" {
		t.Fatalf("update shape: %v", got)
	}
	result, ok := got["result"].(map[string]any)
	if !ok {
		t.Fatalf("update result: %T %v", got["result"], got)
	}
	if _, ok := result["files"].([]any); !ok {
		t.Fatalf("update files: %T %v", result["files"], result)
	}
}

func TestFeedShapeAndBlacklistRegression(t *testing.T) {
	_, _, got := getJSON(t, http.MethodGet, "/feeds", nil)
	feed, ok := got["feed"].(map[string]any)
	if !ok {
		t.Fatalf("feed object: %T %v", got["feed"], got)
	}
	entries, ok := feed["entry"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("feed entries: %T %v", feed["entry"], got)
	}
	if got["total"] != float64(1) {
		t.Fatalf("feed total: %v", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/data/me/HOC%40UserBlackList_v100", nil)
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound || rr.Body.Len() != 0 {
		t.Fatalf("blacklist regression: code=%d body=%q", rr.Code, rr.Body.String())
	}
}

func TestHestiaDoesNotFallThroughToProfile(t *testing.T) {
	_, _, got := getJSON(t, http.MethodGet, "/configs/users/me?profile_name=hoc", nil)
	if _, ok := got["deactivated_events"].([]any); !ok {
		t.Fatalf("not Hestia config: %v", got)
	}
	game, ok := got["game"].(map[string]any)
	if !ok || len(game) == 0 {
		t.Fatalf("empty Hestia game: %v", got)
	}
	if _, profile := got["birthdate"]; profile {
		t.Fatalf("Hestia became profile JSON: %v", got)
	}
}

// Regression lock (2026-08-16, EDGE_FIX_2026-08-01 §3/§7): HTTP/TLS surfaces
// must never advertise a bare IP, even on a LAN edge. A raw IP breaks TLS
// SNI/cert validation (mock cert has DNS SANs only) and the client loops on
// /locate forever — the "checking update..." hang. The APK's DNS remap cave
// patch resolves gameloft.com to the LAN box, so hostname is both correct and
// required. Raw IP stays on the non-TLS lobby/GS path (ipport :20001, e02d gs=).
func TestLocateNeverReturnsBareIPOnLANEdge(t *testing.T) {
	oldHost, oldMode, oldGw := config.EdgeHost, config.EdgeMode, config.GSGateway
	defer func() { config.EdgeHost, config.EdgeMode, config.GSGateway = oldHost, oldMode, oldGw }()
	config.EdgeHost, config.EdgeMode, config.GSGateway = "192.168.68.111", "lan", "192.168.68.111"

	if !config.IsLANEdge() {
		t.Fatal("precondition: expected LAN edge")
	}
	if config.ClientHost() != config.DefaultEdgeHost {
		t.Fatalf("ClientHost=%q, want hostname %q", config.ClientHost(), config.DefaultEdgeHost)
	}

	for _, svc := range []string{"auth", "asset", "config", "storage", "etsv2", "ads_agency", "message", "unknownsvc"} {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/locate?service="+svc, nil))
		body := strings.TrimSpace(rec.Body.String())
		if strings.Contains(body, "192.168.68.111") {
			t.Fatalf("/locate?service=%s returned bare IP %q", svc, body)
		}
		if !strings.Contains(body, config.DefaultEdgeHost) {
			t.Fatalf("/locate?service=%s = %q, want hostname", svc, body)
		}
	}

	// datacenters: url (TLS) = hostname, ip/ipport (plain TCP) = LAN IP.
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/datacenters", nil))
	var dcs []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dcs); err != nil || len(dcs) == 0 {
		t.Fatalf("datacenters unmarshal err=%v body=%s", err, rec.Body.String())
	}
	if url, _ := dcs[0]["url"].(string); !strings.Contains(url, config.DefaultEdgeHost) {
		t.Fatalf("datacenters url=%q, want hostname", url)
	}
	if ip, _ := dcs[0]["ip"].(string); ip != "192.168.68.111" {
		t.Fatalf("datacenters ip=%q, want LAN IP", ip)
	}
	if ipport, _ := dcs[0]["ipport"].(string); ipport != "192.168.68.111:20001" {
		t.Fatalf("datacenters ipport=%q, want LAN IP:20001", ipport)
	}

	// GS relay keeps the raw IP (no TLS, no DNS on that path).
	if got := config.EffectiveGSGateway(); got != "192.168.68.111" {
		t.Fatalf("EffectiveGSGateway=%q, want LAN IP", got)
	}
}
