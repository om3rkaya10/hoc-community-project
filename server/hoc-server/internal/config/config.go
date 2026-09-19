package config

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRoomID = 1
	DefaultTskCID = 2
	GSPort        = 9999
	FrameLead     = 1
	GSIGameMode   = 4
	GSIModeParam  = 4
	MapUnlockMask = 0xFFFF
	CatchupMax    = 4

	// MatchWriteTimeout bounds a single in-match socket write (op7/op11).
	//
	// In-match writes are serialized per room by room.WireLock(), so a write
	// that never returns parks the room's wire slot and stops the frame clock
	// for EVERY player in that match. On WAN a peer that stops reading (lost
	// signal, suspended device, full receive window) does exactly that, and
	// default TCP timeouts can hold the write for minutes.
	//
	// 2s is ~60 frames at 30 Hz: far beyond any healthy RTT/jitter, so a live
	// player is never dropped by it, while a dead connection is shed quickly.
	// Set HOC_MATCH_WRITE_TIMEOUT_MS=0 to restore the old unbounded behaviour.
	DefaultMatchWriteTimeout = 2 * time.Second

	// DefaultEdgeHost is the Nox DNAT hostname path (LIVE default).
	DefaultEdgeHost = "game-portal.gameloft.com"
)

// Custom-room map selection.
//
// The client sends its chosen map as a string in the 0xe038 room JSON
// (field 0x1014), e.g. {"map":"3V3"}. That string selects GAME_MODE, which
// the client turns into a world + Lua script:
//
//	EnterGame@0x00BEC43C -> GetGameMode(GSI+4) -> table@0x023FED80 -> mapInfoId
//	Mode_Selection.lua   -> LoadMapScript() by the same GAME_MODE
//
//	JSON "map" | GAME_MODE constant             | GSI+4 | mapInfoId | script
//	-----------+--------------------------------+-------+-----------+------------------
//	3V3        | GAME_MODE_DOTA_3V3             |     3 |         6 | Map_3V3.lua
//	5V5        | GAME_MODE_DOTA                 |     0 |         7 | Map_5V5.lua
//	5V5_UR     | GAME_MODE_DOTA_UNDERREALMRUINS |     4 |        12 | Map_5V5_Vault.lua
//
// GSIModeParam (GSI+8) is a DIFFERENT axis: it stays 4
// (GAME_MODE_PARAM_CUSTOMIZE) for every custom room and must not be
// confused with the map. Likewise the field the LoginAck docs call
// "map_id" is really CSConnCtrl::mode and is unrelated to the map
// (see AGENTS2 "TUZAK").
const (
	GameModeDota5v5   = 0 // GAME_MODE_DOTA
	GameMode3v3       = 3 // GAME_MODE_DOTA_3V3
	GameMode5v5Vault  = 4 // GAME_MODE_DOTA_UNDERREALMRUINS
	DefaultCustomMode = GameMode5v5Vault
)

// GameModeForMapName maps the client's 0x1014 "map" string to GAME_MODE.
// Unknown/absent values fall back to the previous hardcoded behaviour so a
// future map name cannot break room creation.
//
// VERIFIED LIVE 2026-08-15 (all three maps loaded and played):
//
//	room "3V3"    -> mode=3 -> 3v3 desert
//	room "5V5"    -> mode=0 -> classic Sinskaald Rift
//	room "5V5_UR" -> mode=4 -> Under Realm Ruins
//
// Before this, GSIGameMode was pinned to 4, so every custom room loaded
// Under Realm Ruins no matter what the host picked -- that was the bug.
//
// NOTE (client-side, not fixable here): the 3v3 map is unstable on HIGH
// graphics on some devices -- it segfaults in TerrainTiled::GetHeight
// during the first frame. It loads and plays fine on LOW. This is an
// asset/render issue in the client; the server data is identical either
// way, so do not "fix" it by withholding mode=3.
func GameModeForMapName(name string) int {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "3V3":
		return GameMode3v3
	case "5V5":
		return GameModeDota5v5
	case "5V5_UR":
		return GameMode5v5Vault
	default:
		return DefaultCustomMode
	}
}

// LIVE pin flags (AGENTS3 / GO_MIGRATION_PIN).
const (
	PushGotData100B     = true
	PushPlayerReady100A = true
	CustomNoLoadMap     = true
	CustomReadyLoadMap  = true
	CustomSoloRoster    = true
	SoftAIFill          = false
	SynConsume          = true
	FrameClock          = true
	AuthStrict          = true
	PushGSOnStartGame   = true
	PushHostUserList    = true

	// Account systems (Python pin names).
	ServerKitabe        = true
	LoginBuyItemKitabe  = true // BuyItem[17] GESub on login (user-requested)
	KitabeSeedAfterMenu = true
	KitabeSeedDelayMS   = 500
	// Every account is granted the full tablet set (beta economy); deleted
	// tablets can be bought back from the shop.
	KitabeGrantAllTablets = true
	// Loft capacity (GetUserInfo iv[0x88]) before any 0x5d expansion. The
	// grant-all set is 50 tablets, so leave room for extra copies bought
	// from the shop.
	KitabeTabletCapacityDefault = 75
	// Tablet reopen ("Uyut" 0x4e) prices. The client falls back to 750/20 when
	// GetUserInfo iv[0x4f]/iv[0x50] are 0 (DlgTabletPage::onClicked
	// @0x1229940); they are sent explicitly so both sides agree.
	KitabeSleepEmblem = 750
	KitabeSleepRune   = 20
	// Gold 1-for-1 inscription exchange (trade 0x59, EXCHANGE page third
	// panel). GetUserInfo iv[0x6b]/iv[0x6c] → UserInfo+0x310/+0x314;
	// DlgInscriptionPage::OnClickedConfirm1V1Exchange sends the price back
	// with payType 5 (emblem) or 2 (rune). Original prices unknown; kept
	// below the 750/20 tablet unlock.
	KitabeExchangeGoldEmblem = 300
	KitabeExchangeGoldRune   = 10
	ServerTalent             = true
	ServerTalentLoginSeed    = false
	ServerTalentSeatSeed     = false
	TalentPointsDefault      = 40
)

var (
	// PlayerGateways is the ordered Nox gateway pool used by temporary accounts.
	// A request/session that arrives on one of these addresses may override the
	// fallback assignment, so the same nickname can be used from either Nox.
	PlayerGateways = []string{"172.16.42.2", "172.16.57.2"}

	// DefaultGateways Nox guest → host DNAT gateway.
	DefaultGateways = map[string]string{
		"enterpries1": "172.16.42.2",
		"enterpries2": "172.16.57.2",
		"phone":       "192.168.68.111", // parked; seed only
	}
	DefaultGSIP = "172.16.42.2"

	// EdgeHost is the runtime edge hostname/IP exposed to clients.
	// EdgeMode is "hostname" (default, Nox DNAT path) or "lan" (bare IP).
	// GSGateway is the GS/TCP game relay IP advertised to clients.
	//
	// Resolution order for each field: env var > edge_runtime.json > default.
	// Default preserves the Nox DNAT path (game-portal.gameloft.com).
	// Env: HOC_EDGE_HOST / HOC_EDGE_MODE / HOC_GS_GATEWAY.
	// File: RootDir()/edge_runtime.json {edge_host, mode, gs_gateway}
	// (written by HOC_Android/phone_apk_build/patch_edge_client.py).
	EdgeHost, EdgeMode, GSGateway = resolveEdge()

	// MatchReconnectHold — mid-match soft ReLogin hold. The old room-wide
	// freeze implementation broke walk and is gone; the current path keeps the
	// survivor clock running, skips only the reconnecting peer, then replays the
	// exact missed op7/op11 syn stream from the room ring before rejoining it.
	// Default is now LIVE; set HOC_MATCH_RECONNECT_HOLD=false for rollback.
	MatchReconnectHold    = envBool("HOC_MATCH_RECONNECT_HOLD", true)
	MatchReconnectHoldTTL = envSeconds("HOC_MATCH_RECONNECT_HOLD_TTL_SEC", 90*time.Second)
	MatchReloginFailMax   = envInt("HOC_MATCH_RELOGIN_FAIL_MAX", 2)
	// MatchReloginFailCooldown — after MatchReloginFailMax Ack→quick-EOF
	// cycles the next ReLoginReq is refused (no Ack) only while the last fail
	// is younger than this; once the client slows down the counter resets and
	// it gets a real Ack again. The seat is kept for the hold TTL either way
	// (LIVE 2026-09-18: refusal used to drop the seat 4 s into a 90 s hold).
	MatchReloginFailCooldown = envSeconds("HOC_MATCH_RELOGIN_FAIL_COOLDOWN_SEC", 5*time.Second)
	// DefaultSummonerSpells is used for a match-setup PlayerInfo whose SkillAck
	// carries 0/0 and whose account has no remembered pair yet (first match
	// of a fresh account): level-1 Heal (34) and level-1 Mana Regen (593),
	// the pair the client itself reports before the player touches the
	// picker (operator decision 2026-09-19).
	DefaultSummonerSpell1 = envInt("HOC_DEFAULT_SUMMONER_SPELL1", 34)
	DefaultSummonerSpell2 = envInt("HOC_DEFAULT_SUMMONER_SPELL2", 593)
	// GSUserTimeout — Linux TCP_USER_TIMEOUT on GS sockets: unacked data
	// older than this aborts the connection, so a phone that vanished without
	// RST (Wi-Fi→4G, signal loss) surfaces as EOF in seconds instead of the
	// ~15 min retransmit ceiling. 30 B frames never fill the send buffer, so
	// the 2 s write deadline alone never fires for a silent peer. Keepalive
	// (same period) covers the idle direction. 0 disables both.
	GSUserTimeout = envSeconds("HOC_GS_USER_TIMEOUT_SEC", 10*time.Second)
	// Stall freeze disabled (0): any positive value paused op7/op11 for the
	// whole room and destroyed movement. Do not re-enable without a per-peer
	// skip (not room-wide freeze).
	MatchStallFreeze = envDurationMS("HOC_MATCH_STALL_FREEZE_MS", 0)
	MatchResumeGrace = envSeconds("HOC_MATCH_RESUME_GRACE_SEC", 0)

	// FrameTicker — dedicated per-room op11 frame clock (Go migration payoff).
	//
	// The legacy path pumps one frame per GS recv timeout. While a player is
	// walking the client sends ~28 packets/s, so Read() returns with data
	// instead of timing out and the clock only advances as a side effect of
	// packet handling. Measured LIVE 2026-08-16: 20.0 frame/s against
	// FrameHZ=30 — the client expects 30, so movement micro-stutters (AGENTS5
	// problem C; SYSTEM_PROMPT_WORKER already documented "op11 @ ~20Hz").
	//
	// The old Python FRAME_THREAD ban does NOT apply here. Its two documented
	// causes were Python-specific: (1) a dedicated thread contending on
	// room.lock with op7 (AGENTS4 §10.12/§10.14) and (2) a conn.sendall
	// monkeypatch that hung LoginAck (M6_CUTOVER). This implementation uses
	// neither: one owner goroutine per room drives frames, and the recv-timeout
	// pump is disabled while it runs so frames have exactly ONE source
	// (AGENTS4:549 warns that ticker+catchup double pump is a "kopma sınıfı"
	// hazard).
	//
	// Set HOC_FRAME_TICKER=false to fall back to the legacy recv-timeout pump.
	FrameTicker = envBool("HOC_FRAME_TICKER", true)

	// ClientHostOverride — DNS name served to clients in HTTP/TLS URLs on a
	// WAN/VPS deploy (env HOC_CLIENT_HOST). Empty keeps the shipped default
	// game-portal.gameloft.com, which every current APK already resolves via
	// the SO DNS-remap cave. Setting this REQUIRES a cert that covers the
	// name; bare IPs are rejected by ClientHost() (TLS SNI/SAN break).
	ClientHostOverride = envString("HOC_CLIENT_HOST", "")

	// MatchWriteTimeout — per-write deadline for in-match sockets. See
	// DefaultMatchWriteTimeout for the rationale. HOC_MATCH_WRITE_TIMEOUT_MS=0
	// disables it and restores the pre-2026-08-16 unbounded behaviour.
	MatchWriteTimeout = envDuration("HOC_MATCH_WRITE_TIMEOUT_MS", DefaultMatchWriteTimeout)

	// FrameHZ / FramePeriod — the op11 lockstep rate. LIVE is 30 Hz: the
	// client pushes a hard-coded 33 ms dt per received frame
	// (NGDataPtl::HandleGamePlayFrame, `mov r8,#0x21` @0x12a1bf4), so any
	// other rate needs a client whose dt constant matches the period.
	//
	// HOC_FRAME_MS=16 sets the period directly (60 Hz experiment, pairs with
	// tools/patch_frame_dt.py --ms 16); HOC_FRAME_HZ=60 is the same thing
	// rounded to a whole rate. Unset = exactly the old time.Second/30.
	FrameHZ, FramePeriod = frameRate()

	// Build60Hz / FramePeriod60Hz — the 60 Hz lockstep client. The client
	// reports its build string in the GS LoginReq and tags every custom room
	// it creates or searches with the attribute custom_<build> (Gameloft's
	// own version matchmaking, which this server used to ignore). The 60 Hz
	// APK returns Build60Hz from GetSimpleGameBuildVersion, so its rooms
	// only ever meet other 60 Hz clients and run at FramePeriod60Hz; stock
	// clients ("3.5.2a") keep FramePeriod. Never put both in one match.
	Build60Hz       = envString("HOC_BUILD_60HZ", "3.5.2b")
	FramePeriod60Hz = envDurationMS("HOC_FRAME_MS_60HZ", 16*time.Millisecond)

	// Profile — HOC_PROFILE=true turns on the lag-hunt instrumentation in
	// internal/prof (per-tick write timing, op7 relay latency, GC/CPU) as one
	// log block per 10 s. Off by default: it costs a time.Now() pair per
	// socket write.
	Profile = envBool("HOC_PROFILE", false)

	// PprofAddr — HOC_PPROF_ADDR=127.0.0.1:6060 serves net/http/pprof for the
	// profiling rig. Loopback only; prof.Start refuses anything else.
	PprofAddr = envString("HOC_PPROF_ADDR", "")
)

// FramePeriodForBuild picks the room tick for a client build string.
func FramePeriodForBuild(build string) time.Duration {
	if build != "" && build == Build60Hz {
		return FramePeriod60Hz
	}
	return FramePeriod
}

// SameBuildClass reports whether two client builds may share a match: both
// unknown/legacy count as stock, and only an exact Build60Hz match is 60 Hz.
func SameBuildClass(a, b string) bool {
	return (a == Build60Hz) == (b == Build60Hz)
}

// frameRate resolves HOC_FRAME_MS / HOC_FRAME_HZ into (rate, period); the
// rate only drives log cadence, the period drives the room ticker.
func frameRate() (int, time.Duration) {
	if ms := envInt("HOC_FRAME_MS", 0); ms > 0 {
		return int(1000 / ms), time.Duration(ms) * time.Millisecond
	}
	hz := envInt("HOC_FRAME_HZ", 30)
	if hz <= 0 {
		hz = 30
	}
	return hz, time.Second / time.Duration(hz)
}

// envDuration reads a millisecond count from the environment. A value of 0 is
// meaningful (feature off), so it is preserved rather than treated as unset.
func envDuration(name string, def time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

func IsPlayerGateway(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return false
	}
	for _, gateway := range PlayerGateways {
		if host == gateway {
			return true
		}
	}
	return false
}

// ClientHost returns the host clients must put in HTTP/TLS URLs (locate,
// /urls, datacenters url, banners).
//
// This is ALWAYS a hostname, never a bare IP — even on a LAN edge. The phone
// APK carries a DNS remap cave patch (needle "gameloft.com" → LAN IP, see
// HOC_Android/EDGE_FIX_2026-08-01.md §7 Fix A2), so a hostname already
// resolves to the LAN box. Handing out a raw IP instead breaks TLS SNI /
// certificate validation (the mock cert has only DNS SANs under
// *.gameloft.com, no IP SAN) and the client silently retries /locate forever
// — the "checking update..." hang observed 2026-08-16.
//
// Keeping the hostname is also what makes a global release possible: one cert
// and one APK for everybody, with only DNS pointing at each host's box.
// Raw IPs belong on the GS/relay path only (no TLS, no DNS) —
// see EffectiveGSGateway.
//
// WAN/VPS (HOC_CLIENT_HOST): a public deploy may serve its own DNS name. The
// cert must then carry that name too, so this stays an explicit opt-in and
// still refuses bare IPs (an IP here would reintroduce the TLS break above).
func ClientHost() string {
	if ClientHostOverride != "" && !isBareIP(ClientHostOverride) {
		return ClientHostOverride
	}
	return DefaultEdgeHost
}

// isBareIP reports whether s is a literal IPv4/IPv6 address (not a hostname).
func isBareIP(s string) bool {
	return net.ParseIP(strings.Trim(strings.TrimSpace(s), "[]")) != nil
}

// EffectiveGSGateway returns the GS relay IP advertised to clients. Explicit
// HOC_GS_GATEWAY / edge_runtime.json gs_gateway wins, then LAN EdgeHost, then
// the default Nox pool. Raw IP is correct here: the GS path is plain TCP with
// no TLS and no DNS lookup.
func EffectiveGSGateway() string {
	if GSGateway != "" {
		return GSGateway
	}
	if IsLANEdge() {
		return EdgeHost
	}
	return DefaultGSIP
}

// IsDirectEdge reports whether the edge is served on an operator-supplied
// address (LAN IP, public IP, or a self-hosted hostname) rather than the
// default Nox DNAT path.
//
// Modes: "hostname" = Nox DNAT default; "lan" = same-WiFi bare IP;
// "public" = WAN/VPS deploy (bare IP or own DNS name).
//
// WAN note (2026-08-16): "public" must behave like "lan" for GS gateway
// selection. Before this, mode=public fell through to the Nox pool and
// advertised gs=172.16.42.2 in e02d — unreachable for any internet player.
func IsDirectEdge() bool {
	if EdgeHost == "" || EdgeHost == DefaultEdgeHost {
		return false
	}
	return EdgeMode == "lan" || EdgeMode == "public"
}

// IsLANEdge is retained for older call sites; direct-edge semantics.
func IsLANEdge() bool { return IsDirectEdge() }

// StaleNoxGateway reports whether a persisted per-account gateway points at the
// Nox DNAT pool while the server is serving a LAN edge. Such a value would be
// advertised in e02d as gs=172.16.x.x and is unreachable from a phone, so
// callers must fall back to EffectiveGSGateway(). This does NOT change the
// temporary-account / nickname design — only which relay IP is handed out.
func StaleNoxGateway(gateway string) bool {
	if gateway == "" || !IsLANEdge() {
		return false
	}
	return IsPlayerGateway(gateway) || gateway == DefaultGSIP
}

func envString(name string, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	return raw
}

// edgeRuntime is the on-disk phone-deploy config written by
// phone_apk_build/patch_edge_client.py (write_runtime_edge).
type edgeRuntime struct {
	EdgeHost  string `json:"edge_host"`
	Mode      string `json:"mode"`
	GSGateway string `json:"gs_gateway"`
}

func readEdgeRuntime() edgeRuntime {
	var rt edgeRuntime
	raw, err := os.ReadFile(filepath.Join(RootDir(), "edge_runtime.json"))
	if err != nil {
		return rt
	}
	if json.Unmarshal(raw, &rt) != nil {
		return edgeRuntime{}
	}
	rt.EdgeHost = strings.TrimSpace(rt.EdgeHost)
	rt.Mode = strings.ToLower(strings.TrimSpace(rt.Mode))
	rt.GSGateway = strings.TrimSpace(rt.GSGateway)
	return rt
}

// resolveEdge applies env > edge_runtime.json > default for all three edge
// fields. Previously only edge_host was read from the file, so a file-only
// config (the APK pipeline's output) left mode=hostname and the GS gateway
// pinned to the Nox pool — the phone got LAN HTTP but an unreachable gs= in
// e02d (AGENTS5 §2.2.1 D1).
func resolveEdge() (host, mode, gateway string) {
	rt := readEdgeRuntime()

	host = envString("HOC_EDGE_HOST", rt.EdgeHost)
	if host == "" {
		host = DefaultEdgeHost
	}

	mode = strings.ToLower(envString("HOC_EDGE_MODE", rt.Mode))
	if mode == "" {
		// Infer: an explicit non-default host means a LAN/public edge.
		if host != DefaultEdgeHost {
			mode = "lan"
		} else {
			mode = "hostname"
		}
	}

	gateway = envString("HOC_GS_GATEWAY", rt.GSGateway)
	return host, mode, gateway
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func envSeconds(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func envDurationMS(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

// RootDir is CWD for certs/accounts (must be Desktop\Hoc).
func RootDir() string {
	if d := os.Getenv("HOC_ROOT"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func CertPaths() (crt, key string) {
	root := RootDir()
	return filepath.Join(root, "server.crt"), filepath.Join(root, "server.key")
}

func AccountsPath() string {
	return filepath.Join(RootDir(), "accounts.json")
}
