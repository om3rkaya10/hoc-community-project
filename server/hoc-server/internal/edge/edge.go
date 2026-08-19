package edge

import (
	"crypto/md5"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

// edgeHTTPRoot is the base URL clients fetch over HTTP/TLS. Hostname only —
// see config.ClientHost (raw IP breaks TLS SNI/cert; APK DNS remap handles
// the LAN redirect).
func edgeHTTPRoot() string {
	return "http://" + config.ClientHost() + ":8080"
}

func edgeHostPort(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// urlsResponse is built per request so runtime config changes apply without a
// restart. All values are hostname-based (see config.ClientHost).
func urlsResponsePayload() map[string]any {
	return map[string]any{
		"status": "none", "maintenance": false, "required": false, "optional": false,
		"CN": "US", "country_code": "US", "IGP_shortcode": "HOC",
		"client_id": "55674", "product_id": "55674",
		"bundle_id":      "GAMELOFTSA.HeroesofOrderChaosOffline_0pp20fcewvvtj",
		"ecomm_api_root": edgeHTTPRoot(),
		"pandora":        edgeHTTPRoot(),
		"login":          edgeHTTPRoot() + "/login",
		"chk_ver":        edgeHTTPRoot() + "/chk_ver",
		"download_idx":   edgeHTTPRoot() + "/download_idx",
		"all_dlc":        edgeHTTPRoot() + "/all_dlc",
		"offline_items":  edgeHTTPRoot() + "/offline_items",
		"gllive-ope":     edgeHTTPRoot() + "/gllive-ope",
		"result": map[string]any{
			"ecomm_api_root": edgeHTTPRoot(),
			"pandora":        edgeHTTPRoot(),
			"login":          edgeHTTPRoot() + "/login",
			"chk_ver":        edgeHTTPRoot() + "/chk_ver",
			"download_idx":   edgeHTTPRoot() + "/download_idx",
			"all_dlc":        edgeHTTPRoot() + "/all_dlc",
			"offline_items":  edgeHTTPRoot() + "/offline_items",
			"gllive-ope":     edgeHTTPRoot() + "/gllive-ope",
			"bdc":            "North_America",
			"hoc_vac":        0,
		},
	}
}

// Proven pin (mock_server_grok / CHECKPOINT datacenters_SOLVED).
// Built per request so runtime config changes apply without a restart.
// "url" is fetched over TLS → hostname (config.ClientHost). "ip"/"ipport" is
// the lobby TCP endpoint (:20001, no TLS) → raw LAN IP is correct there.
func datacentersResponse() []map[string]any {
	return []map[string]any{
		{
			"name": "North_America", "status": "active", "preferred": true,
			"country_code": "US", "_datacenter_id": "NA_DC_01",
			"ip": config.EdgeHost, "port": 20001, "ipport": config.EdgeHost + ":20001",
			"url": "http://" + config.ClientHost() + ":8443", "srvName": "Global",
			"network": 1, "roomName": "Lobby",
		},
	}
}

// DLC pin (session-4): size/hash JSON; bare body = base64(xml); Range → 206.
var dlcIndexXML = []byte(`<?xml version="1.0" encoding="UTF-8"?><dlc><version>1.6.1h</version><filelist><file dst="dummy.dat" size="2" /></filelist></dlc>`)
var dlcIndexB64 = []byte(base64.StdEncoding.EncodeToString(dlcIndexXML))
var widgetXML = []byte(`<?xml version="1.0" encoding="UTF-8"?><widget><version>1.0</version></widget>`)

func menuBannerURL() string {
	return edgeHTTPRoot() + "/menu_assets/hoc_banner.jpg"
}

var offlineItemsXML = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<response>
  <status>1</status>
  <error>0</error>
  <product>1903</product>
  <catalog_version>1.0</catalog_version>
  <version>1</version>
</response>
`)

func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handle)
	return mux
}

func handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	low := strings.ToLower(path)
	q := r.URL.Query()
	fmt.Printf(" [HTTP] %s %s ua=%q\n", r.Method, r.URL.RequestURI(), r.UserAgent())

	switch {
	case strings.HasSuffix(low, "/urls") || strings.Contains(low, "/urls"):
		respondJSON(w, r, urlsResponsePayload())
	case strings.HasSuffix(low, "/datacenters"):
		respondJSON(w, r, datacentersResponse())
	case strings.Contains(low, "hdloading"):
		respondJSON(w, r, map[string]any{"status": 1, "message": "success", "hitId": 0})
	case strings.Contains(low, "/menu_assets/"):
		handleMenuAsset(w, r)
	case strings.Contains(low, "ingamenews") || strings.EqualFold(q.Get("action"), "checkNews"):
		respondJSON(w, r, map[string]any{
			"success": true, "unread": 0, "current-id": 1,
			"feed": map[string]any{"entry": []map[string]any{{
				"title": "Heroes of Order & Chaos", "summary": menuBannerURL(),
				"link": menuBannerURL(), "id": "1", "updated": "2099-12-31T23:59:59Z",
			}}},
			"news": []map[string]any{{
				"id": 1, "title": "Heroes of Order & Chaos", "summary": menuBannerURL(),
				"image": menuBannerURL(), "link": menuBannerURL(),
			}},
		})
	case strings.Contains(low, "serverconfig") || (strings.Contains(low, "/ope") && !strings.Contains(low, "generic")):
		respondBytes(w, r, "text/plain", []byte("configured\nf|0|i|0|\nu|http://gllive.gameloft.com/|\n"))
	case strings.Contains(low, "banned_services"):
		respondBytes(w, r, "text/plain", []byte("status=1&error=0"))
	case strings.Contains(low, "locate") || strings.HasPrefix(low, "/locate"):
		svc := q.Get("service")
		if svc == "" {
			// /locate/asset style
			parts := strings.Split(strings.Trim(path, "/"), "/")
			if len(parts) >= 2 {
				svc = parts[1]
			}
		}
		handleLocate(w, r, svc)
	case strings.Contains(low, "game_object"):
		respondJSON(w, r, map[string]any{
			"status": 1, "error": 0,
			"game_object": map[string]any{
				"game": map[string]any{
					"_week_init": map[string]any{
						"free_hero": []int{6},
						"top_10":    []int{3},
					},
				},
			},
		})
	case strings.Contains(low, "dlc.index.bin"):
		handleDLCIndex(w, r, low)
	case strings.Contains(low, "widget.xml"):
		handleWidget(w, r, low)
	case strings.Contains(low, "dummy.dat"):
		if strings.Contains(low, "metadata/size") {
			respondBytes(w, r, "text/plain", []byte("2"))
		} else {
			respondBytes(w, r, "application/octet-stream", []byte("AB"))
		}
	case strings.Contains(low, "assets"):
		respondBytes(w, r, "text/plain", []byte("0"))
	case strings.Contains(low, "authorize"):
		handleAuth(w, r, "authorize")
	case strings.Contains(low, "authenticate"):
		handleAuth(w, r, "authenticate")
	case strings.Contains(low, "encrypt_token"):
		handleEncryptToken(w, r)
	case strings.Contains(low, "genericxplayer"):
		respondBytes(w, r, "text/plain", []byte("g|166|r|s|v|40002"))
	case strings.Contains(low, "get_product_list"):
		respondJSON(w, r, map[string]any{
			"status": 1, "error": 0, "products": []any{}, "product_list": []any{},
		})
	case strings.HasPrefix(low, "/transports/wns/endpoints/"):
		respondJSON(w, r, map[string]any{
			"status": 1, "error": 0, "access_token": accounts.StableAccessToken,
		})
	case strings.Contains(low, "transports") || strings.Contains(low, "wns") || strings.Contains(low, "endpoints"):
		respondJSON(w, r, map[string]any{"status": 1, "error": 0})
	// Hestia BEFORE bare users/me — configs/users/me must not become profile JSON.
	case strings.Contains(low, "configs/users/me") || strings.Contains(low, "/configs/") || strings.Contains(low, "hestia"):
		handleHestiaConfig(w, r)
	case strings.Contains(low, "users/me") || strings.Contains(low, "profiles/me") || strings.Contains(low, "seshat"):
		handleProfile(w, r)
	case strings.Contains(low, "games/mygame/alias") || strings.Contains(low, "games/mygame"):
		handleAlias(w, r)
	// Gaia /data/me/<key> — MUST NOT return JSON/plaintext garbage (SIGABRT Thread-29).
	// UserBlackList → 404 empty; other GET → raw base64 or 404; PUT → store + JSON ack.
	case strings.Contains(low, "/data/me/"):
		handleGaiaData(w, r)
	case strings.Contains(low, "/feeds") || strings.HasSuffix(low, "/feeds"):
		respondJSON(w, r, map[string]any{
			"status": 1, "error": 0, "success": true, "current-id": 1, "unread": 0,
			"feed": map[string]any{"entry": []map[string]any{{
				"title": "Heroes of Order & Chaos", "summary": menuBannerURL(),
				"link": menuBannerURL(), "id": "1",
			}}},
			"feeds": []map[string]any{{
				"id": 1, "title": "Heroes of Order & Chaos", "summary": menuBannerURL(),
				"image": menuBannerURL(), "link": menuBannerURL(),
			}},
			"offset": 0, "limit": 20, "total": 1,
		})
	case strings.Contains(low, "devices/mydevice") || strings.Contains(low, "/devices"):
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "device_id": "mock_device_hoc"})
	case strings.Contains(low, "accounts/me") || strings.Contains(low, "connection_approval"):
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "requests": []any{}})
	case strings.Contains(low, "alerts/me") || strings.Contains(low, "/alerts"):
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "alerts": []any{}})
	case strings.Contains(low, "chk_ver") || strings.Contains(low, "download_idx") || strings.Contains(low, "all_dlc"):
		respondJSON(w, r, map[string]any{
			"status": 1, "error": 0, "update_available": 0, "version": "1.6.1",
			"result": map[string]any{"version": "1.6.1", "update_available": 0, "files": []any{}},
		})
	case strings.Contains(low, "collect") || strings.Contains(low, "tracking") ||
		strings.Contains(low, "telemetry") || strings.Contains(low, "analytics") ||
		strings.Contains(low, "log") || strings.Contains(low, "stats") || strings.Contains(low, "metrics"):
		respondJSON(w, r, map[string]any{"status": 1, "error": 0})
	case strings.Contains(low, "offline_items") || r.URL.Query().Get("product") != "":
		respondBytes(w, r, "application/xml", offlineItemsXML)
	case strings.Contains(low, "login"):
		respondBytes(w, r, "text/plain", []byte("status=1&error=0"))
	default:
		respondBytes(w, r, "text/plain", []byte("status=1&error=0"))
	}
}

func handleMenuAsset(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	data, err := os.ReadFile(filepath.Join(config.RootDir(), "menu_assets", name))
	if err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing"))
		return
	}
	ctype := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		ctype = "image/jpeg"
	case ".png":
		ctype = "image/png"
	}
	fmt.Printf(" [MENU-ASSET] %s (%dB)\n", name, len(data))
	respondBytes(w, r, ctype, data)
}

func handleDLCIndex(w http.ResponseWriter, r *http.Request, low string) {
	body := dlcIndexB64
	switch {
	case strings.Contains(low, "metadata/size"):
		respondJSON(w, r, map[string]any{"size": len(body)})
	case strings.Contains(low, "metadata/hash"):
		sum := md5.Sum(body)
		respondJSON(w, r, map[string]any{"hash": hex.EncodeToString(sum[:])})
	default:
		respondBytes(w, r, "application/octet-stream", body)
	}
}

func handleWidget(w http.ResponseWriter, r *http.Request, low string) {
	switch {
	case strings.Contains(low, "metadata/size"):
		respondJSON(w, r, map[string]any{"size": len(widgetXML)})
	case strings.Contains(low, "metadata/hash"):
		sum := md5.Sum(widgetXML)
		respondJSON(w, r, map[string]any{"hash": hex.EncodeToString(sum[:])})
	default:
		respondBytes(w, r, "application/xml", widgetXML)
	}
}

// handleLocate answers the client's service discovery. All values are
// HOSTNAME:port — never a raw IP. The client opens TLS to whatever this
// returns; a bare IP fails SNI/cert validation and the client loops on
// /locate forever ("checking update..." hang). The APK's DNS remap sends the
// hostname to the LAN box. See config.ClientHost / EDGE_FIX_2026-08-01 §3, §7.
func handleLocate(w http.ResponseWriter, r *http.Request, service string) {
	s := strings.ToLower(service)
	host8443 := edgeHostPort(config.ClientHost(), 8443)
	host8080 := edgeHostPort(config.ClientHost(), 8080)
	switch {
	case s == "ets":
		respondJSON(w, r, map[string]any{
			"status": "none", "maintenance": false, "CN": "US", "country_code": "US",
			"result": map[string]any{"bdc": "North_America", "hoc_vac": 0},
			"ets":    map[string]any{"serviceName": "ets"},
		})
	case s == "auth" || s == "message" || s == "asset" || s == "config" || s == "storage" || s == "etsv2" || s == "ads_agency":
		respondBytes(w, r, "text/plain", []byte(host8443))
	case s == "gllive-ope" || s == "offline_items":
		respondJSON(w, r, map[string]any{"status": 1, "error": 0, "result": map[string]any{"host": config.ClientHost(), "port": 8443}})
	default:
		respondBytes(w, r, "text/plain", []byte(host8080))
	}
}

func handleAuth(w http.ResponseWriter, r *http.Request, scope string) {
	_ = r.ParseForm()
	user := first(r, "username", "user", "login")
	pass := first(r, "password", "pass")
	if user == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		for i, p := range parts {
			if strings.EqualFold(p, "authenticate") && i > 0 {
				user = parts[i-1]
			}
		}
	}
	sc := first(r, "scope")
	if sc == "" {
		sc = scope
	}
	// Bootstrap authorize (tracking_bi / empty user / GlWebTools device path).
	scopeL := strings.ToLower(strings.TrimSpace(sc))
	if user == "" || accounts.IsDeviceAuth(user) || strings.Contains(scopeL, "tracking") || scopeL == "bi" {
		if accounts.IsDeviceAuth(user) {
			acc, created := accounts.EnsureDevice(user)
			if acc != nil {
				verb := "ok"
				if created {
					verb = "REGISTERED"
				}
				fmt.Printf(" [AUTH] %s device %s user=%s nick=%s id=%s gateway=%s scope=%q\n",
					scope, verb, acc.Username, acc.Nickname, accounts.AccountIDString(acc), accounts.GatewayFor(acc), sc)
				respondJSON(w, r, accounts.AuthSuccessJSONScope(acc, sc))
				return
			}
		}
		respondJSON(w, r, accounts.DeviceAuthSuccessJSON(sc))
		fmt.Printf(" [AUTH] %s device/bootstrap ok scope=%q user=%q\n", scope, sc, user)
		return
	}
	sourceAccount, sourceHow := accountFromRequest(r)
	acc, created, ok := accounts.AuthenticateOrCreate(user, pass)
	if !ok || acc == nil {
		fmt.Printf(" [AUTH] %s REJECTED user=%q scope=%q\n", scope, user, sc)
		respondJSON(w, r, accounts.AuthFailJSON("invalid_credentials"))
		return
	}
	ingressGateway := requestGateway(r)
	if ingressGateway != "" {
		acc.SetTemporaryGateway(ingressGateway)
	} else if created && sourceAccount != nil && sourceAccount.Device {
		acc.SetTemporaryGateway(accounts.GatewayFor(sourceAccount))
	}
	verb := "ok"
	if created {
		verb = "REGISTERED"
	}
	fmt.Printf(" [AUTH] %s %s user=%s nick=%s id=%s gateway=%s ingress=%s source=%s scope=%q\n",
		scope, verb, acc.Username, acc.Nickname, accounts.AccountIDString(acc),
		accounts.GatewayFor(acc), ingressGateway, sourceHow, sc)
	respondJSON(w, r, accounts.AuthSuccessJSONScope(acc, sc))
}

func requestGateway(r *http.Request) string {
	if r == nil {
		return ""
	}
	addr, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	host = strings.Trim(host, "[]")
	if config.IsPlayerGateway(host) {
		return host
	}
	return ""
}

func accountFromRequest(r *http.Request) (*accounts.Account, string) {
	if r == nil {
		return nil, "none"
	}
	_ = r.ParseForm()
	if user := first(r, "username", "user", "login"); user != "" {
		if acc := accounts.Get(user); acc != nil {
			return acc, "username"
		}
	}
	if aid := first(r, "account_id", "user_id", "forCredentials"); aid != "" {
		if acc := accounts.GetByID(aid); acc != nil {
			return acc, "account_id"
		}
	}
	for _, key := range []string{
		"access_token", "janusToken", "janus_token", "token", "encrypted_token", "credential",
	} {
		if token := first(r, key); token != "" {
			if acc := accounts.ResolveToken(token); acc != nil {
				return acc, key
			}
		}
	}
	for _, header := range []string{"Authorization", "X-Authorization", "X-Access-Token"} {
		if token := strings.TrimSpace(r.Header.Get(header)); token != "" {
			if acc := accounts.ResolveToken(token); acc != nil {
				return acc, strings.ToLower(header)
			}
		}
	}
	return nil, "none"
}

func handleEncryptToken(w http.ResponseWriter, r *http.Request) {
	acc, how := accountFromRequest(r)
	access, janus := accounts.TokensFor(acc)
	respondJSON(w, r, map[string]any{
		"status": 1, "error": 0,
		"access_token": access, "janusToken": janus,
		"encrypted_token": access, "token": access,
	})
	fmt.Printf(" [AUTH] encrypt_token account=%q resolve=%s\n", accountUsername(acc), how)
}

func handleAlias(w http.ResponseWriter, r *http.Request) {
	acc, how := accountFromRequest(r)
	if acc == nil {
		acc = accounts.Get("enterpries1")
		how = "enterpries1-fallback"
	}
	alias := accountNickname(acc)
	respondJSON(w, r, map[string]any{"status": 1, "error": 0, "alias": alias})
	fmt.Printf(" [ACCOUNT] alias user=%q nick=%q resolve=%s\n", accountUsername(acc), alias, how)
}

func handleHestiaConfig(w http.ResponseWriter, r *http.Request) {
	// Python pin: telemetry + non-empty game._week_init (empty game:{} → SIGABRT class).
	respondJSON(w, r, map[string]any{
		"deactivated_events":             []any{},
		"network_send_interval":          60,
		"network_max_events_per_package": 10,
		"max_events_of_one_type":         5,
		"game": map[string]any{
			"_week_init": map[string]any{
				"free_hero": []map[string]string{
					{"item_id": "131"}, {"item_id": "158"}, {"item_id": "159"},
					{"item_id": "160"}, {"item_id": "161"}, {"item_id": "162"},
				},
				"top_10": []map[string]string{
					{"item_id": "131"}, {"item_id": "158"}, {"item_id": "159"},
				},
			},
		},
	})
	fmt.Printf(" [HESTIA-CONFIG] telemetry+game\n")
}

// --- Gaia /data/me KV (Trade_UserData) ---

type gaiaRec struct {
	Data       string `json:"data"`
	Visibility string `json:"visibility"`
}

var (
	gaiaMu    sync.Mutex
	gaiaStore map[string]gaiaRec
)

func gaiaStorePath() string {
	return filepath.Join(config.RootDir(), "gaia_data_store.json")
}

func gaiaLoad() {
	if gaiaStore != nil {
		return
	}
	gaiaStore = map[string]gaiaRec{}
	b, err := os.ReadFile(gaiaStorePath())
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &gaiaStore)
}

func gaiaSave() {
	b, err := json.MarshalIndent(gaiaStore, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(gaiaStorePath(), b, 0o644)
}

func gaiaKeyFromPath(path string) string {
	idx := strings.Index(strings.ToLower(path), "/data/me/")
	if idx < 0 {
		return ""
	}
	rest := path[idx+len("/data/me/"):]
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	k, err := url.PathUnescape(rest)
	if err != nil {
		k = rest
	}
	return strings.Trim(k, "/")
}

// Minimal ClientUserData: msgpack [ [], ["icon","sign","nick"] ] then base64.
func gaiaClientUserDataSeed(acc *accounts.Account) string {
	nick := accountNickname(acc)
	if nick == "" {
		nick = "Player"
	}
	icon := "basic_user_icon_1.png"
	sign := "Onurumuz için savaşalım."
	// fixarray-2 + empty fixarray-0 + fixarray-3 of fixstrs
	var blob []byte
	blob = append(blob, 0x92, 0x90) // [[], ...]
	blob = append(blob, 0x93)
	for _, s := range []string{icon, sign, nick} {
		b := []byte(s)
		if len(b) < 32 {
			blob = append(blob, byte(0xa0|len(b)))
			blob = append(blob, b...)
		}
	}
	return base64.StdEncoding.EncodeToString(blob)
}

func handleGaiaData(w http.ResponseWriter, r *http.Request) {
	key := gaiaKeyFromPath(r.URL.Path)
	keyL := strings.ToLower(key)
	_ = r.ParseForm()
	acc, how := accountFromRequest(r)
	if acc == nil {
		acc = accounts.Get("enterpries1")
		how = "enterpries1-fallback"
	}
	storeKey := gaiaAccountKey(acc, key)

	// UserBlackList: garbage 200 → SIGABRT Thread-29 (Python pin: 404 empty).
	if strings.Contains(keyL, "blacklist") || strings.Contains(keyL, "userblacklist") {
		fmt.Printf(" [GAIA-DATA 404 empty] %s\n", key)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	gaiaMu.Lock()
	defer gaiaMu.Unlock()
	gaiaLoad()

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		dataStr := r.FormValue("data")
		vis := r.FormValue("visibility")
		if vis == "" {
			vis = "public"
		}
		if dataStr == "" && r.Body != nil {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if !strings.Contains(string(raw), "data=") {
				dataStr = strings.TrimSpace(string(raw))
			}
		}
		if dataStr != "" {
			gaiaStore[storeKey] = gaiaRec{Data: dataStr, Visibility: vis}
			gaiaSave()
			fmt.Printf(" [GAIA-DATA PUT] %s user=%q resolve=%s data=%dB\n",
				key, accountUsername(acc), how, len(dataStr))
		}
		respondJSON(w, r, map[string]any{"status": 1, "error": 0})
		return
	}

	rec, ok := gaiaStore[storeKey]
	if !ok && strings.Contains(keyL, "clientuserdata") {
		seed := gaiaClientUserDataSeed(acc)
		rec = gaiaRec{Data: seed, Visibility: "public"}
		gaiaStore[storeKey] = rec
		gaiaSave()
		fmt.Printf(" [GAIA-DATA SEED] %s user=%q nick=%q resolve=%s\n",
			key, accountUsername(acc), accountNickname(acc), how)
		ok = true
	}
	if !ok || rec.Data == "" {
		fmt.Printf(" [GAIA-DATA 404 empty] %s\n", key)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	// RAW base64 body — NOT JSON (Trade_UserData::Deserialize).
	fmt.Printf(" [GAIA-DATA GET raw-b64] %s user=%q resolve=%s len=%d\n",
		key, accountUsername(acc), how, len(rec.Data))
	respondBytes(w, r, "text/plain", []byte(rec.Data))
}

func gaiaAccountKey(acc *accounts.Account, key string) string {
	if acc == nil || accounts.Norm(acc.Username) == "enterpries1" {
		return key
	}
	aid := accounts.AccountIDString(acc)
	if aid == "" {
		aid = accounts.Norm(acc.Username)
	}
	return aid + "|" + key
}

func handleProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	path := strings.ToLower(strings.TrimRight(r.URL.Path, "/"))
	acc, how := accountFromRequest(r)
	if acc == nil {
		acc = accounts.Get("enterpries1")
		how = "enterpries1-fallback"
	}
	if acc == nil {
		respondJSON(w, r, map[string]any{"status": 0, "error": 1})
		return
	}

	// DlgAge: POST .../birthdate object="YYYY-MM-DD HH:MM:SSZ" + .../gender object="female"|"male"
	obj := first(r, "object")
	if obj == "" {
		obj = r.FormValue("object")
	}
	switch {
	case strings.HasSuffix(path, "/birthdate"):
		if obj != "" {
			acc.SetBirthdate(obj)
			fmt.Printf(" [ACCOUNT] ★birthdate SET %q age=%d\n", obj, acc.Age)
		}
	case strings.HasSuffix(path, "/gender"):
		if obj != "" {
			acc.SetGenderStr(obj)
			fmt.Printf(" [ACCOUNT] ★gender SET %q\n", obj)
		}
	}

	aid := accounts.AccountIDString(acc)
	bd := acc.BirthdateStr()
	gstr := acc.GenderWire()
	nick := accountNickname(acc)
	access, _ := accounts.TokensFor(acc)
	fmt.Printf(" [ACCOUNT] profile OK user=%q nick=%q resolve=%s birthdate=%q gender=%q\n",
		acc.Username, nick, how, bd, gstr)
	respondJSON(w, r, map[string]any{
		"status": 1, "error": 0,
		"account_id": aid, "user_id": aid, // string pin (playground)
		"username": acc.Username, "nickname": nick,
		"display_name": nick,
		"access_token": access,
		"storage":      "48hNPwsS7/4=",
		"level":        acc.Level, "xp": acc.Exp,
		"country": "TR", "language": "tr",
		"banned_from":        []any{},
		"total_transactions": 0,
		"birthdate":          bd,
		"gender":             gstr,
		"_guest_linked":      false,
		"_testdlc":           0,
	})
}

func accountUsername(acc *accounts.Account) string {
	if acc == nil {
		return ""
	}
	return acc.Username
}

func accountNickname(acc *accounts.Account) string {
	if acc == nil {
		return ""
	}
	if nick := strings.TrimSpace(acc.Nickname); nick != "" {
		return nick
	}
	return strings.TrimSpace(acc.Username)
}

func first(r *http.Request, keys ...string) string {
	for _, k := range keys {
		if v := r.FormValue(k); v != "" {
			return v
		}
		if v := r.URL.Query().Get(k); v != "" {
			return v
		}
	}
	return ""
}

func respondJSON(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "json", 500)
		return
	}
	respondBytes(w, r, "application/json", b)
}

func respondBytes(w http.ResponseWriter, r *http.Request, ctype string, body []byte) {
	total := len(body)
	rng := r.Header.Get("Range")
	if rng != "" && strings.HasPrefix(strings.TrimSpace(rng), "bytes=") {
		spec := strings.TrimSpace(rng)[len("bytes="):]
		if i := strings.IndexByte(spec, ','); i >= 0 {
			spec = spec[:i]
		}
		spec = strings.TrimSpace(spec)
		parts := strings.SplitN(spec, "-", 2)
		if len(parts) == 2 {
			start, end := 0, total-1
			if parts[0] != "" {
				if v, err := strconv.Atoi(parts[0]); err == nil {
					start = v
				}
			}
			if parts[1] != "" {
				if v, err := strconv.Atoi(parts[1]); err == nil {
					end = v
				}
			}
			if start < 0 {
				start = 0
			}
			if end >= total {
				end = total - 1
			}
			if start <= end && start < total {
				chunk := body[start : end+1]
				sum := md5.Sum(body)
				w.Header().Set("Content-Type", ctype)
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
				w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
				w.Header().Set("Accept-Ranges", "bytes")
				w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Connection", "close")
				w.WriteHeader(206)
				_, _ = w.Write(chunk)
				fmt.Printf("  -> 206 %s bytes %d-%d/%d\n", ctype, start, end, total)
				return
			}
		}
	}
	sum := md5.Sum(body)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(total))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func ListenHTTP(addr string) error {
	s := &http.Server{Addr: addr, Handler: Handler(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf(" HTTP %s listening...\n", addr)
	return s.ListenAndServe()
}

func ListenTLS(addr, crt, key string) error {
	cert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return fmt.Errorf("load cert: %w (CWD must be Desktop\\Hoc)", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS10,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf(" HTTPS %s listening...\n", addr)
	s := &http.Server{Handler: Handler(), TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second}
	return s.Serve(tls.NewListener(ln, cfg))
}

func ServeDual8080(addr string, crt, key string, lobby func(net.Conn, string)) error {
	cert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		return err
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS10}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf(" DUAL %s (HTTP/TLS/lobby) listening...\n", addr)
	httpSrv := &http.Server{Handler: Handler(), ReadHeaderTimeout: 10 * time.Second}
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go func(conn net.Conn) {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			buf := make([]byte, 2)
			n, err := io.ReadFull(conn, buf)
			_ = conn.SetReadDeadline(time.Time{})
			if err != nil || n < 2 {
				_ = conn.Close()
				return
			}
			peeked := &prefixConn{Conn: conn, prefix: append([]byte(nil), buf...)}
			switch {
			case buf[0] == 0x16 && buf[1] == 0x03:
				tlsConn := tls.Server(peeked, tlsCfg)
				if err := tlsConn.Handshake(); err != nil {
					_ = conn.Close()
					return
				}
				_ = httpSrv.Serve(&onceListener{c: tlsConn})
			case buf[0] == 0x16 && buf[1] == 0x01:
				lobby(peeked, conn.RemoteAddr().String())
			case (buf[0] >= 'A' && buf[0] <= 'Z') || (buf[0] >= 'a' && buf[0] <= 'z'):
				_ = httpSrv.Serve(&onceListener{c: peeked})
			default:
				lobby(peeked, conn.RemoteAddr().String())
			}
		}(c)
	}
}

type prefixConn struct {
	net.Conn
	prefix []byte
}

func (p *prefixConn) Read(b []byte) (int, error) {
	if len(p.prefix) > 0 {
		n := copy(b, p.prefix)
		p.prefix = p.prefix[n:]
		return n, nil
	}
	return p.Conn.Read(b)
}

type onceListener struct {
	c    net.Conn
	done bool
}

func (s *onceListener) Accept() (net.Conn, error) {
	if s.done || s.c == nil {
		return nil, io.EOF
	}
	s.done = true
	c := s.c
	s.c = nil
	return c, nil
}
func (s *onceListener) Close() error { return nil }
func (s *onceListener) Addr() net.Addr {
	if s.c != nil {
		return s.c.LocalAddr()
	}
	return &net.TCPAddr{}
}
