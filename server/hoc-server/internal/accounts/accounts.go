package accounts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hoc-server/internal/config"
	"hoc-server/internal/domain/items"
)

const (
	DefaultAge       = 23
	DefaultGender    = 1 // male
	DefaultBirthdate = "2003-07-25 01:59:14Z"
)

// TabletRec is one filled page slot. UID references the TabletInstance
// (inventory v2); ID and Sockets mirror that instance for older readers.
type TabletRec struct {
	UID     int              `json:"uid,omitempty"`
	ID      int              `json:"id"`
	Sockets map[string][]int `json:"sockets"`
}

type TalentGroupRec struct {
	Echo     int     `json:"echo"`
	Unlocked bool    `json:"unlocked"`
	Limit    int     `json:"limit"`
	Talents  [][]int `json:"talents"`
	Layers   []int   `json:"layers"`
	F14      int     `json:"f14"`
	F18      int     `json:"f18"`
	F20      int     `json:"f20"`
}

type Account struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	Nickname     string `json:"nickname"`
	UserID       int64  `json:"user_id"`
	AccountID    string `json:"account_id"`
	Temporary    bool   `json:"temporary,omitempty"`
	Device       bool   `json:"device,omitempty"`
	Level        int    `json:"level"`
	Exp          int    `json:"exp"`
	Rune         int    `json:"rune"`
	Emblem       int    `json:"emblem"`
	Gems         int    `json:"gems"`
	RevivalRunes int    `json:"revival_runes"`
	Gateway      string `json:"gateway"`
	GSHost       string `json:"gs_host"`

	// DlgAge / Gaia profiles/me (Python pin 2026-07-25): empty birthdate ⇒ yaş sorar every login.
	Age        int              `json:"age"`
	Gender     int              `json:"gender"` // 1=male 2=female (GS IntIdx)
	IsSavedAge int              `json:"is_saved_age"`
	GenderStr  string           `json:"gender_str"` // Gaia wire: "male"/"female"
	Birthdate  string           `json:"birthdate"`  // Gaia wire: "YYYY-MM-DD HH:MM:SSZ"
	Collection string           `json:"collection,omitempty"`
	Heroes     []int            `json:"heroes,omitempty"`
	Skins      map[string][]int `json:"skins,omitempty"`

	Inscriptions        map[string]int              `json:"inscriptions"`
	Tablets             map[string]TabletRec        `json:"tablets"`
	BackpackSockets     map[string]map[string][]int `json:"backpack_sockets"`
	UnlockedPages       []int                       `json:"unlocked_pages,omitempty"`
	SlotStates          map[string]int              `json:"slot_states,omitempty"`
	TabletPacketSize    int                         `json:"tablet_packet_size,omitempty"`
	TalentPoints        int                         `json:"talent_points"`
	Talents             map[string]TalentGroupRec   `json:"talents"`
	SelectedTabletGroup int                         `json:"selected_tablet_group"`
	AwakeTabletIDs      []int                       `json:"awake_tablets"`

	// Inventory v1 (2026-09-13). Items is the ItemInfo consumable inventory
	// (GetUserInfo[7] / BuyItem[13]); OwnedTablets is the Kitabe backpack in
	// wire order — the client addresses tablets by their index in this vector
	// (TabletSlot indexIDInPacket, sleep/delete/wake requests). Poles and
	// Patterns feed GESub5Member18 (HocPole/HocFlag maps); Flag* is the
	// SelectFlag choice. InventoryVersion gates the one-shot migration.
	Items        map[string]int `json:"items"`
	OwnedTablets []int          `json:"owned_tablets"` // mirror of TabletInstances ids
	// Inventory v2 (2026-09-18): TabletInstances is the owned vector in wire
	// order, one entry per copy (see tablets.go). NextTabletUID allocates
	// instance ids.
	TabletInstances  []TabletInstance `json:"tablet_instances"`
	NextTabletUID    int              `json:"next_tablet_uid,omitempty"`
	Poles            map[string]int   `json:"poles"`
	Patterns         map[string]int   `json:"patterns"`
	FlagPole         int              `json:"flag_pole"`
	FlagPattern      int              `json:"flag_pattern"`
	FlagType         int              `json:"flag_type"`
	InventoryVersion int              `json:"inventory_version"`

	// Social (Gaia Osiris, 2026-09-15). Friends is the bilateral friend set
	// (usernames, Norm'd); FriendRequests are pending incoming
	// connection_approval requests (see friends.go). IconSign is the raw
	// Trade_UserData blob the client PUTs to profiles/me/myprofile/_hoc_icon_sign
	// and reads back for friends through /profiles?credentials=.
	Friends        []string           `json:"friends,omitempty"`
	FriendRequests []FriendRequestRec `json:"friend_requests,omitempty"`
	IconSign       string             `json:"icon_sign,omitempty"`
	// SummonerSpells is the last valid pair the client reported in a 0x100C
	// SkillAck. The original service seeded the match-setup PlayerInfo from
	// the profile; without it a fresh client sends 0/0 until the player opens
	// the spell picker and the match then rolls random spells (LIVE journal
	// 2026-09-18: 195 of ~400 SkillAcks carried 0/0, 8 matches started so).
	SummonerSpells []int `json:"summoner_spells,omitempty"`
}

type storeFile struct {
	Accounts map[string]*Account `json:"accounts"`
}

var (
	mu             sync.RWMutex
	byUN           map[string]*Account
	byID           map[string]*Account
	byToken        map[string]*Account
	loadPath       string
	defaultHeroIDs []int
)

type collectionCatalog struct {
	Heroes []int `json:"heroes"`
}

func Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f storeFile
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	catalogHeroes := loadCollectionHeroes(filepath.Join(filepath.Dir(path), "collection_catalog.json"))
	mu.Lock()
	defer mu.Unlock()
	loadPath = path
	defaultHeroIDs = catalogHeroes
	byUN = make(map[string]*Account)
	byID = make(map[string]*Account)
	byToken = make(map[string]*Account)
	dirty := false
	for k, a := range f.Accounts {
		if a == nil {
			continue
		}
		key := Norm(k)
		a.Username = Norm(a.Username)
		if a.Username == "" {
			a.Username = key
		}
		if a.AccountID == "" && a.UserID != 0 {
			a.AccountID = fmt.Sprintf("%d", a.UserID)
		}
		if a.SelectedTabletGroup <= 0 {
			a.SelectedTabletGroup = 1
		}
		if a.TalentPoints <= 0 {
			a.TalentPoints = config.TalentPointsDefault
			dirty = true
		}
		if a.normalizeDuplicateTabletsLocked() {
			dirty = true
		}
		if a.migrateInventoryLocked() {
			dirty = true
		}
		if a.ensureAgeGateLocked() {
			dirty = true
		}
		byUN[key] = a
		indexIdentityLocked(a)
	}
	if dirty {
		_ = saveLocked()
	}
	return nil
}

func accountIDValue(a *Account) string {
	if a == nil {
		return ""
	}
	if aid := strings.TrimSpace(a.AccountID); aid != "" {
		return aid
	}
	if a.UserID != 0 {
		return strconv.FormatInt(a.UserID, 10)
	}
	return ""
}

func tokenStrings(a *Account) (access, janus string) {
	if a == nil || Norm(a.Username) == "enterpries1" {
		return StableAccessToken, StableJanusToken
	}
	suffix := accountIDValue(a)
	if suffix == "" || len(suffix) > 20 {
		sum := sha256.Sum256([]byte(Norm(a.Username) + "|" + suffix))
		suffix = fmt.Sprintf("%x", sum[:6])
	}
	return "mock_at_" + suffix, "mock_jt_" + suffix
}

func indexIdentityLocked(a *Account) {
	if a == nil {
		return
	}
	if aid := accountIDValue(a); aid != "" {
		byID[aid] = a
	}
	access, janus := tokenStrings(a)
	byToken[access] = a
	byToken[janus] = a
}

func removeIdentityLocked(a *Account) {
	if a == nil {
		return
	}
	if aid := accountIDValue(a); aid != "" && byID[aid] == a {
		delete(byID, aid)
	}
	access, janus := tokenStrings(a)
	if byToken[access] == a {
		delete(byToken, access)
	}
	if byToken[janus] == a {
		delete(byToken, janus)
	}
}

func ensureIndexesLocked() {
	if byUN == nil {
		byUN = make(map[string]*Account)
	}
	if byID == nil {
		byID = make(map[string]*Account)
	}
	if byToken == nil {
		byToken = make(map[string]*Account)
	}
}

func loadCollectionHeroes(path string) []int {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c collectionCatalog
	if json.Unmarshal(b, &c) != nil {
		return nil
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(c.Heroes))
	for _, id := range c.Heroes {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func saveLocked() error {
	if loadPath == "" {
		return fmt.Errorf("accounts: no load path")
	}
	f := storeFile{Accounts: byUN}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(loadPath, b, 0o644)
}

func Save() error {
	mu.Lock()
	defer mu.Unlock()
	return saveLocked()
}

func Norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Client paths/forms use gllive:enterpries1 / gllive%3Aenterpries1 — strip federation prefix.
	for _, pfx := range []string{"gllive:", "glive:", "hoc:"} {
		if strings.HasPrefix(s, pfx) {
			s = strings.TrimPrefix(s, pfx)
			break
		}
	}
	return strings.TrimSpace(s)
}

func Get(username string) *Account {
	mu.RLock()
	defer mu.RUnlock()
	return byUN[Norm(username)]
}

func GetByID(accountID string) *Account {
	mu.RLock()
	defer mu.RUnlock()
	return byID[strings.TrimSpace(accountID)]
}

func ResolveToken(raw string) *Account {
	token := strings.TrimSpace(strings.Trim(raw, `"`))
	if fields := strings.Fields(token); len(fields) == 2 && strings.EqualFold(fields[0], "bearer") {
		token = fields[1]
	}
	mu.RLock()
	defer mu.RUnlock()
	return byToken[token]
}

func AccountIDString(a *Account) string {
	mu.RLock()
	defer mu.RUnlock()
	return accountIDValue(a)
}

func Authenticate(username, password string) (*Account, bool) {
	a := Get(username)
	if a == nil {
		return nil, false
	}
	if config.AuthStrict && a.Password != password {
		return nil, false
	}
	return a, true
}

// AuthenticateOrCreate keeps strict passwords for existing accounts while
// turning an unknown login into a play-ready temporary account. A lobby-first
// account has an empty password and the first HTTP login claims it.
func AuthenticateOrCreate(username, password string) (a *Account, created, ok bool) {
	key := Norm(username)
	if !validUsername(key) {
		return nil, false, false
	}
	display := loginDisplayName(username, key)

	mu.Lock()
	defer mu.Unlock()
	ensureIndexesLocked()
	if a = byUN[key]; a != nil {
		if config.AuthStrict && a.Password != password {
			if !a.Temporary || a.Password != "" {
				return nil, false, false
			}
			oldPassword := a.Password
			a.Password = password
			if err := saveLocked(); err != nil {
				a.Password = oldPassword
				return nil, false, false
			}
		}
		return a, false, true
	}

	a = newTemporaryAccountLocked(key, display, password, false)
	byUN[key] = a
	indexIdentityLocked(a)
	if err := saveLocked(); err != nil {
		delete(byUN, key)
		removeIdentityLocked(a)
		return nil, false, false
	}
	return a, true, true
}

// EnsureTemporary is the lobby-side safety net when HTTP authentication was
// skipped or raced the GLBlock login. It never aliases an unknown user to a
// seed account.
func EnsureTemporary(username string) (a *Account, created bool) {
	key := Norm(username)
	if !validUsername(key) {
		return nil, false
	}
	display := loginDisplayName(username, key)

	mu.Lock()
	defer mu.Unlock()
	ensureIndexesLocked()
	if a = byUN[key]; a != nil {
		return a, false
	}
	a = newTemporaryAccountLocked(key, display, "", false)
	byUN[key] = a
	indexIdentityLocked(a)
	if err := saveLocked(); err != nil {
		delete(byUN, key)
		removeIdentityLocked(a)
		return nil, false
	}
	return a, true
}

// EnsureDevice gives each real android:/guest:/anonymous: bootstrap identity
// its own account and tokens instead of collapsing every device onto enterpries1.
func EnsureDevice(raw string) (a *Account, created bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	sum := sha256.Sum256([]byte(raw))
	suffix := fmt.Sprintf("%x", sum[:6])
	key := "device_" + suffix
	nickname := "Guest-" + suffix[:6]

	mu.Lock()
	defer mu.Unlock()
	ensureIndexesLocked()
	if a = byUN[key]; a != nil {
		return a, false
	}
	a = newTemporaryAccountLocked(key, nickname, "", true)
	byUN[key] = a
	indexIdentityLocked(a)
	if err := saveLocked(); err != nil {
		delete(byUN, key)
		removeIdentityLocked(a)
		return nil, false
	}
	return a, true
}

func validUsername(username string) bool {
	return username != "" && len(username) <= 64 && utf8.ValidString(username)
}

func loginDisplayName(raw, fallback string) string {
	display := strings.TrimSpace(raw)
	lower := strings.ToLower(display)
	for _, prefix := range []string{"gllive:", "glive:", "hoc:"} {
		if strings.HasPrefix(lower, prefix) {
			display = strings.TrimSpace(display[len(prefix):])
			break
		}
	}
	if display == "" {
		display = fallback
	}
	return capUTF8Bytes(display, 32)
}

func capUTF8Bytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for s != "" && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func newTemporaryAccountLocked(username, nickname, password string, device bool) *Account {
	var maxID int64 = 1000000
	playerCount := 0
	deviceCount := 0
	for _, existing := range byUN {
		if existing == nil {
			continue
		}
		if existing.UserID > maxID {
			maxID = existing.UserID
		}
		if existing.Temporary && existing.Device {
			deviceCount++
		} else if existing.Temporary {
			playerCount++
		}
	}
	userID := maxID + 1
	gateway := config.EffectiveGSGateway()
	if len(config.PlayerGateways) > 0 && gateway == config.DefaultGSIP {
		index := playerCount
		if device {
			index = deviceCount
		}
		gateway = config.PlayerGateways[index%len(config.PlayerGateways)]
	}
	a := &Account{
		Username: username, Password: password, Nickname: nickname,
		UserID: userID, AccountID: strconv.FormatInt(userID, 10), Temporary: true, Device: device,
		Level: 40, Rune: 9999, Emblem: 99999, Gems: 99999, RevivalRunes: 99, Gateway: gateway,
		Age: DefaultAge, Gender: DefaultGender, IsSavedAge: 1,
		GenderStr: genderIntToStr(DefaultGender), Birthdate: BirthdateFromAge(DefaultAge),
		Collection: "full", TalentPoints: config.TalentPointsDefault, SelectedTabletGroup: 1,
		Inscriptions: map[string]int{}, Tablets: map[string]TabletRec{},
		BackpackSockets: map[string]map[string][]int{}, Talents: map[string]TalentGroupRec{},
	}
	a.migrateInventoryLocked()
	return a
}

// GatewayFor returns the GS relay IP advertised to this account (e02d gs=).
//
// On a LAN edge (phone deploy) any persisted Nox-pool gateway is stale: it was
// written when the account was first created on Nox and is unreachable from a
// phone. In that case fall back to the effective LAN gateway. The account's
// identity — temporary auto-create, nickname = username — is untouched; only
// the relay IP is re-resolved (AGENTS5 §2.2.1 D2).
func GatewayFor(a *Account) string {
	if a == nil {
		return config.EffectiveGSGateway()
	}
	if a.Gateway != "" && !config.StaleNoxGateway(a.Gateway) {
		return a.Gateway
	}
	if a.GSHost != "" && !config.StaleNoxGateway(a.GSHost) {
		return a.GSHost
	}
	if gw, ok := config.DefaultGateways[Norm(a.Username)]; ok && !config.StaleNoxGateway(gw) {
		return gw
	}
	return config.EffectiveGSGateway()
}

func (a *Account) SetTemporaryGateway(gateway string) bool {
	if a == nil || !config.IsPlayerGateway(gateway) {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if !a.Temporary || a.Gateway == gateway {
		return false
	}
	a.Gateway = gateway
	_ = saveLocked()
	return true
}

const (
	StableAccessToken = "mock_token_hoc_2026"
	StableJanusToken  = "mock_janus_token_2026"
)

func TokensFor(a *Account) (access, janus string) {
	mu.Lock()
	defer mu.Unlock()
	access, janus = tokenStrings(a)
	if a != nil && byUN[Norm(a.Username)] == a {
		if byToken == nil {
			byToken = make(map[string]*Account)
		}
		byToken[access] = a
		byToken[janus] = a
	}
	return access, janus
}

func AuthSuccessJSON(a *Account) map[string]any {
	return AuthSuccessJSONScope(a, "auth")
}

func AuthSuccessJSONScope(a *Account, scope string) map[string]any {
	tok, janus := TokensFor(a)
	aid := AccountIDString(a)
	if scope == "" {
		scope = "auth"
	}
	nick := a.Nickname
	if nick == "" {
		nick = a.Username
	}
	// Shape matches Python account_auth_success_json (LIVE Seshat → profiles/me).
	return map[string]any{
		"status":         1,
		"error":          0,
		"access_token":   tok,
		"janusToken":     janus,
		"refresh_token":  "mock_refresh_token_2026",
		"token_type":     "bearer",
		"expires_in":     86400,
		"account_id":     aid,
		"user_id":        aid, // string — playground pin
		"client_id":      "55674",
		"session_id":     "mock_session_2026",
		"device_id":      "mock_device_hoc",
		"scope":          scope,
		"username":       a.Username,
		"nickname":       nick,
		"display_name":   nick,
		"accountType":    "gllive",
		"key":            "mock_key_2026",
		"operation":      "login",
		"forCredentials": aid,
		"credentials":    []string{tok, janus},
	}
}

func DeviceAuthSuccessJSON(scope string) map[string]any {
	if scope == "" {
		scope = "tracking_bi"
	}
	a := Get("enterpries1")
	if a == nil {
		a = &Account{
			Username: "enterpries1", Nickname: "Enterpries",
			UserID: 1000001, AccountID: "1000001",
		}
	}
	return AuthSuccessJSONScope(a, scope)
}

func AuthFailJSON(msg string) map[string]any {
	if msg == "" {
		msg = "invalid_credentials"
	}
	return map[string]any{
		"status":        0,
		"error":         1,
		"error_code":    1,
		"error_message": msg,
		"message":       msg,
	}
}

func IsDeviceAuth(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	for _, prefix := range []string{"android:", "anonymous:", "guest:"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	u := Norm(s)
	if len(u) < 40 {
		return false
	}
	for _, c := range u {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || strings.ContainsRune("+/=_-.", c) {
			continue
		}
		return false
	}
	return true
}

// OwnedHeroIDs returns the account's clean collection roster. Missing heroes
// means the full catalog from collection_catalog.json; skins stay opt-in.
func (a *Account) OwnedHeroIDs() []int {
	if a == nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	src := a.Heroes
	full := strings.ToLower(strings.TrimSpace(a.Collection))
	if src == nil || full == "full" || full == "all" || full == "true" || full == "1" {
		src = defaultHeroIDs
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(src))
	for _, id := range src {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (a *Account) OwnedSkinIDs(heroID int) []int {
	if a == nil {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	ids := a.Skins[strconv.Itoa(heroID)]
	if len(ids) > 512 {
		ids = ids[:512]
	}
	return append([]int(nil), ids...)
}

func (a *Account) Wallet() (emblem, runeV, gems int) {
	emblem, runeV, gems = 99999, 9999, 99999
	if a == nil {
		return
	}
	mu.RLock()
	defer mu.RUnlock()
	return a.Emblem, a.Rune, a.Gems
}

func persistMutation(a *Account, mutate func()) {
	if a == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	mutate()
	_ = saveLocked()
}

// InscriptionPairs expands account inscriptions under outer tabs 1..3 (Python pin).
func (a *Account) InscriptionPairs() [][4]int {
	mu.RLock()
	defer mu.RUnlock()
	return inscriptionPairsLocked(a)
}

func inscriptionPairsLocked(a *Account) [][4]int {
	if a == nil || len(a.Inscriptions) == 0 {
		return nil
	}
	var out [][4]int
	for k, qty := range a.Inscriptions {
		var iid int
		if _, err := fmt.Sscanf(k, "%d", &iid); err != nil || iid == 0 {
			continue
		}
		q := qty
		if q < 1 {
			q = 1
		}
		for outer := 1; outer <= 3; outer++ {
			out = append(out, [4]int{outer, iid, iid, q})
		}
	}
	return out
}

func (a *Account) SelectedPage0() int {
	if a == nil {
		return 0
	}
	mu.RLock()
	defer mu.RUnlock()
	p := a.SelectedTabletGroup - 1
	if p < 0 || p > 6 {
		return 0
	}
	return p
}

func (a *Account) UnlockedPageSet() map[int]bool {
	mu.RLock()
	defer mu.RUnlock()
	return unlockedPageSetLocked(a)
}

func unlockedPageSetLocked(a *Account) map[int]bool {
	out := map[int]bool{}
	if a == nil || len(a.UnlockedPages) == 0 {
		for page := 1; page <= 7; page++ {
			out[page] = true
		}
		return out
	}
	for _, page := range a.UnlockedPages {
		if page >= 1 && page <= 7 {
			out[page] = true
		}
	}
	if len(out) == 0 {
		out[1] = true
	}
	return out
}

func (a *Account) SlotStateSnapshot() map[string]int {
	out := map[string]int{}
	if a == nil {
		return out
	}
	mu.RLock()
	defer mu.RUnlock()
	for key, state := range a.SlotStates {
		out[key] = state
	}
	return out
}

func (a *Account) AddInscription(itemID, qty int) {
	if itemID <= 0 {
		return
	}
	if qty < 1 {
		qty = 1
	}
	persistMutation(a, func() {
		if a.Inscriptions == nil {
			a.Inscriptions = map[string]int{}
		}
		key := strconv.Itoa(itemID)
		a.Inscriptions[key] += qty
	})
}

func (a *Account) RemoveInscription(itemID, qty int) {
	if itemID <= 0 {
		return
	}
	if qty < 1 {
		qty = 1
	}
	persistMutation(a, func() {
		if a.Inscriptions == nil {
			return
		}
		key := strconv.Itoa(itemID)
		left := a.Inscriptions[key] - qty
		if left > 0 {
			a.Inscriptions[key] = left
		} else {
			delete(a.Inscriptions, key)
		}
	})
}

// ExchangeInscriptions atomically consumes sourceQty identical inscriptions and
// grants one target inscription. Insufficient inventory leaves state untouched.
func (a *Account) ExchangeInscriptions(sourceID, targetID, sourceQty int) bool {
	if a == nil || sourceID <= 0 || targetID <= 0 || sourceID == targetID || sourceQty < 1 {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if a.Inscriptions == nil {
		return false
	}
	sourceKey := strconv.Itoa(sourceID)
	if a.Inscriptions[sourceKey] < sourceQty {
		return false
	}
	left := a.Inscriptions[sourceKey] - sourceQty
	if left == 0 {
		delete(a.Inscriptions, sourceKey)
	} else {
		a.Inscriptions[sourceKey] = left
	}
	targetKey := strconv.Itoa(targetID)
	a.Inscriptions[targetKey]++
	_ = saveLocked()
	return true
}

// ExchangeInscriptionSet atomically consumes one of each source inscription
// (duplicates count multiple times) and grants one target. Used by the 4→1
// tier exchange (0x50) and the gold 1↔1 exchange (0x59). When payType is
// PayEmblem or PayRune the price is debited in the same critical section and
// the whole exchange is rejected on an insufficient balance.
func (a *Account) ExchangeInscriptionSet(sources []int, targetID, payType, price int) bool {
	if a == nil || targetID <= 0 || len(sources) == 0 || price < 0 {
		return false
	}
	mu.Lock()
	defer mu.Unlock()
	if a.Inscriptions == nil {
		return false
	}
	need := map[string]int{}
	for _, id := range sources {
		if id <= 0 {
			return false
		}
		need[strconv.Itoa(id)]++
	}
	for key, n := range need {
		if a.Inscriptions[key] < n {
			return false
		}
	}
	switch payType {
	case PayEmblem:
		if a.Emblem < price {
			return false
		}
		a.Emblem -= price
	case PayRune:
		if a.Rune < price {
			return false
		}
		a.Rune -= price
	default:
		if price != 0 {
			return false
		}
	}
	for key, n := range need {
		left := a.Inscriptions[key] - n
		if left == 0 {
			delete(a.Inscriptions, key)
		} else {
			a.Inscriptions[key] = left
		}
	}
	a.Inscriptions[strconv.Itoa(targetID)]++
	_ = saveLocked()
	return true
}

func (a *Account) UnlockSlot(page, slot int) {
	if a == nil || page < 0 || page > 6 || slot < 0 || slot > 2 {
		return
	}
	persistMutation(a, func() {
		if a.SlotStates == nil {
			a.SlotStates = map[string]int{}
		}
		a.SlotStates[fmt.Sprintf("%d:%d", page, slot)] = 1
	})
}

func (a *Account) UnlockPage(page int) []int {
	if a == nil {
		return nil
	}
	if page < 1 || page > 7 {
		page = 1
	}
	var pages []int
	persistMutation(a, func() {
		set := unlockedPageSetLocked(a)
		for p := 1; p <= page; p++ {
			set[p] = true
		}
		pages = make([]int, 0, len(set))
		for p := range set {
			pages = append(pages, p)
		}
		sort.Ints(pages)
		a.UnlockedPages = append([]int(nil), pages...)
	})
	return pages
}

func (a *Account) Debit(payType, amount int) (emblem, runeV, gems int) {
	if amount < 0 {
		amount = 0
	}
	persistMutation(a, func() {
		switch payType {
		case 5:
			a.Emblem = maxInt(0, a.Emblem-amount)
		case 2:
			a.Rune = maxInt(0, a.Rune-amount)
		case 1, 3, 4:
			a.Gems = maxInt(0, a.Gems-amount)
		}
	})
	return a.Wallet()
}

// PayEmblem / PayRune are the BuyMethod codes shared by the shop (0x6e), the
// Kitabe reopen dialog (0x4e) and page unlock (0x7c).
const (
	PayRune   = 2
	PayEmblem = 5
)

const revivalRuneItemID = 141

// Purchase debits total from the wallet and grants qty of the shop item by
// prototype type. Unsupported or already-owned unique items are not charged
// (ok=false) so the BuyItem reply carries an unchanged wallet.
func (a *Account) Purchase(itemID, qty, payType, total int) (ok bool) {
	if a == nil || itemID <= 0 {
		return false
	}
	if qty < 1 {
		qty = 1
	}
	if total < 0 {
		total = 0
	}
	mu.Lock()
	defer mu.Unlock()
	if !a.canGrantLocked(itemID) {
		return false
	}
	debit := total
	switch payType {
	case PayEmblem:
		a.Emblem = maxInt(0, a.Emblem-debit)
	case PayRune:
		a.Rune = maxInt(0, a.Rune-debit)
	case 1, 3, 4:
		a.Gems = maxInt(0, a.Gems-debit)
	}
	a.grantLocked(itemID, qty, 0)
	_ = saveLocked()
	return true
}

// Grant adds an item to the account without charging (rewards, migration).
func (a *Account) Grant(itemID, qty int) bool {
	if a == nil || itemID <= 0 {
		return false
	}
	if qty < 1 {
		qty = 1
	}
	mu.Lock()
	defer mu.Unlock()
	if !a.canGrantLocked(itemID) {
		return false
	}
	a.grantLocked(itemID, qty, 0)
	_ = saveLocked()
	return true
}

func (a *Account) canGrantLocked(itemID int) bool {
	switch items.TypeOf(itemID) {
	case items.TypeInscription, items.TypeConsumable, items.TypePole, items.TypePattern,
		items.TypePack, items.TypeEmblemCurrency, items.TypeRuneCurrency, items.TypeGemCurrency,
		items.TypeHero:
		return true
	case items.TypeTablet:
		return a.canGrantTabletLocked()
	default:
		return false
	}
}

// grantLocked routes one item into the matching inventory. Packs expand
// recursively (depth-limited); currency rows credit the wallet.
func (a *Account) grantLocked(itemID, qty, depth int) {
	if qty < 1 || depth > 4 {
		return
	}
	key := strconv.Itoa(itemID)
	switch items.TypeOf(itemID) {
	case items.TypeInscription:
		if a.Inscriptions == nil {
			a.Inscriptions = map[string]int{}
		}
		a.Inscriptions[key] += qty
	case items.TypeConsumable:
		if a.Items == nil {
			a.Items = map[string]int{}
		}
		a.Items[key] += qty
	case items.TypeTablet:
		a.grantTabletLocked(itemID, qty)
	case items.TypePole:
		if a.Poles == nil {
			a.Poles = map[string]int{}
		}
		a.Poles[key] += qty
	case items.TypePattern:
		if a.Patterns == nil {
			a.Patterns = map[string]int{}
		}
		if items.PatternUnlimited(itemID) {
			a.Patterns[key] = 1
		} else {
			a.Patterns[key] += qty
		}
	case items.TypePack:
		it, _ := items.Lookup(itemID)
		for _, p := range it.Pack {
			a.grantLocked(p.ID, p.Count*qty, depth+1)
		}
	case items.TypeEmblemCurrency, items.TypeRuneCurrency, items.TypeGemCurrency:
		typ, amount, _ := items.CurrencyAmount(itemID)
		switch typ {
		case items.TypeEmblemCurrency:
			a.Emblem += amount * qty
		case items.TypeRuneCurrency:
			a.Rune += amount * qty
		case items.TypeGemCurrency:
			a.Gems += amount * qty
		}
	case items.TypeHero:
		if a.Heroes == nil {
			return // full collection already owns every hero
		}
		for _, h := range a.Heroes {
			if h == itemID {
				return
			}
		}
		a.Heroes = append(a.Heroes, itemID)
	}
}

// migrateInventoryLocked brings pre-inventory-v1 records forward:
//   - Items seeded from the legacy revival_runes counter;
//   - tablet instances built from the legacy owned/backpack/awake fields
//     (or the grant list for a brand-new record), see tablets.go;
//   - non-inscription ids that earlier builds stored in Inscriptions
//     (emblem packs, banners, poles, potions, bundles) are re-routed through
//     grantLocked so players receive what they paid for.
func (a *Account) migrateInventoryLocked() bool {
	if a == nil {
		return false
	}
	dirty := false
	if a.Items == nil {
		a.Items = map[string]int{}
		n := a.RevivalRunes
		if n <= 0 {
			n = 99
		}
		a.Items[strconv.Itoa(revivalRuneItemID)] = n
		dirty = true
	}
	if ensureTabletInstancesLocked(a) {
		dirty = true
	}
	if a.Poles == nil {
		a.Poles = map[string]int{}
		dirty = true
	}
	if a.Patterns == nil {
		a.Patterns = map[string]int{}
		dirty = true
	}
	if a.InventoryVersion < 1 {
		for key, qty := range a.Inscriptions {
			id, err := strconv.Atoi(key)
			if err != nil || items.IsInscription(id) {
				continue
			}
			delete(a.Inscriptions, key)
			if items.TypeOf(id) >= 0 {
				a.grantLocked(id, qty, 0)
			}
			dirty = true
		}
		a.InventoryVersion = 1
		dirty = true
	}
	if a.InventoryVersion < inventoryVersionTabletInstances {
		a.InventoryVersion = inventoryVersionTabletInstances
		dirty = true
	}
	return dirty
}

// ItemCounts returns the consumable inventory (item id → quantity).
func (a *Account) ItemCounts() map[int]int {
	out := map[int]int{}
	if a == nil {
		out[revivalRuneItemID] = 99
		return out
	}
	mu.RLock()
	defer mu.RUnlock()
	for k, q := range a.Items {
		if id, err := strconv.Atoi(k); err == nil && id > 0 && q > 0 {
			out[id] = q
		}
	}
	return out
}

func (a *Account) PoleCounts() map[int]int {
	return a.countMap(func() map[string]int { return a.Poles })
}

func (a *Account) PatternCounts() map[int]int {
	return a.countMap(func() map[string]int { return a.Patterns })
}

func (a *Account) countMap(get func() map[string]int) map[int]int {
	out := map[int]int{}
	if a == nil {
		return out
	}
	mu.RLock()
	defer mu.RUnlock()
	for k, q := range get() {
		if id, err := strconv.Atoi(k); err == nil && id > 0 && q > 0 {
			out[id] = q
		}
	}
	return out
}

// FlagSelection returns the persisted SelectFlag choice (0 = client default).
func (a *Account) FlagSelection() (pole, pattern, flagType int) {
	if a == nil {
		return 0, 0, 0
	}
	mu.RLock()
	defer mu.RUnlock()
	return a.FlagPole, a.FlagPattern, a.FlagType
}

func (a *Account) SetFlagSelection(pole, pattern, flagType int) {
	persistMutation(a, func() {
		a.FlagPole, a.FlagPattern, a.FlagType = pole, pattern, flagType
	})
}

// UsePattern consumes one banner charge (in-match UseFlag 0x5b). Unlimited
// banners are never decremented.
func (a *Account) UsePattern(patternID int) {
	if a == nil || patternID <= 0 || items.PatternUnlimited(patternID) {
		return
	}
	persistMutation(a, func() {
		key := strconv.Itoa(patternID)
		if a.Patterns == nil || a.Patterns[key] <= 0 {
			return
		}
		a.Patterns[key]--
		if a.Patterns[key] <= 0 {
			delete(a.Patterns, key)
		}
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *Account) SetSelectedPage(gid1Based int) {
	if a == nil {
		return
	}
	if gid1Based < 1 {
		gid1Based = 1
	}
	if gid1Based > 7 {
		gid1Based = 7
	}
	persistMutation(a, func() {
		a.SelectedTabletGroup = gid1Based
	})
}

// ensureAgeGateLocked fills missing DlgAge fields (caller holds mu).
// Returns true if the record was mutated.
func (a *Account) ensureAgeGateLocked() bool {
	if a == nil {
		return false
	}
	changed := false
	if a.IsSavedAge == 0 || strings.TrimSpace(a.Birthdate) == "" {
		a.IsSavedAge = 1
		changed = true
	}
	if a.Age < 13 {
		a.Age = DefaultAge
		changed = true
	}
	if a.Gender != 1 && a.Gender != 2 {
		a.Gender = DefaultGender
		changed = true
	}
	if strings.TrimSpace(a.GenderStr) == "" {
		a.GenderStr = genderIntToStr(a.Gender)
		changed = true
	}
	if strings.TrimSpace(a.Birthdate) == "" {
		a.Birthdate = BirthdateFromAge(a.Age)
		changed = true
	}
	return changed
}

func genderIntToStr(g int) string {
	if g == 2 {
		return "female"
	}
	return "male"
}

func genderStrToInt(s string) int {
	switch strings.ToLower(strings.TrimSpace(strings.Trim(s, `"`))) {
	case "female", "f", "2":
		return 2
	default:
		return 1
	}
}

// BirthdateFromAge — Gaia wire 'YYYY-MM-DD HH:MM:SSZ' (LIVE POST 2026-07-25).
func BirthdateFromAge(age int) string {
	if age < 13 {
		age = DefaultAge
	}
	if age > 100 {
		age = 100
	}
	now := time.Now().UTC()
	y := now.Year() - age
	bd := time.Date(y, now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), 0, time.UTC)
	// Clamp invalid day (e.g. Feb 29).
	if bd.Month() != now.Month() {
		bd = time.Date(y, now.Month(), 28, 1, 59, 14, 0, time.UTC)
	}
	return bd.Format("2006-01-02 15:04:05Z")
}

func (a *Account) BirthdateStr() string {
	if a == nil {
		return DefaultBirthdate
	}
	mu.RLock()
	defer mu.RUnlock()
	if s := strings.TrimSpace(strings.Trim(a.Birthdate, `"`)); len(s) >= 8 {
		return s
	}
	age := a.Age
	if age < 13 {
		age = DefaultAge
	}
	return BirthdateFromAge(age)
}

func (a *Account) GenderWire() string {
	if a == nil {
		return "male"
	}
	mu.RLock()
	defer mu.RUnlock()
	if s := strings.TrimSpace(a.GenderStr); s != "" {
		return strings.ToLower(s)
	}
	return genderIntToStr(a.Gender)
}

// AgeFields — GetUserInfo IntIdx 0x10d / 0x12a / 0x12e.
func (a *Account) AgeFields() (age, gender, saved int) {
	age, gender, saved = DefaultAge, DefaultGender, 1
	if a == nil {
		return
	}
	mu.RLock()
	defer mu.RUnlock()
	if a.Age >= 13 {
		age = a.Age
	}
	if a.Gender == 1 || a.Gender == 2 {
		gender = a.Gender
	}
	if a.IsSavedAge != 0 {
		saved = 1
	} else {
		saved = 0
	}
	return
}

func (a *Account) SetBirthdate(raw string) {
	if a == nil {
		return
	}
	s := strings.TrimSpace(strings.Trim(raw, `"`))
	if s == "" {
		return
	}
	age := 0
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[0:4]); err == nil {
			age = time.Now().UTC().Year() - y
			if age < 13 {
				age = 13
			}
			if age > 100 {
				age = 100
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	a.Birthdate = s
	a.IsSavedAge = 1
	if age > 0 {
		a.Age = age
	}
	_ = saveLocked()
}

func (a *Account) SetGenderStr(raw string) {
	if a == nil {
		return
	}
	g := genderStrToInt(raw)
	mu.Lock()
	defer mu.Unlock()
	a.Gender = g
	a.GenderStr = genderIntToStr(g)
	a.IsSavedAge = 1
	_ = saveLocked()
}

func (a *Account) SetAge(age, gender int) {
	if a == nil {
		return
	}
	if age < 13 {
		age = DefaultAge
	}
	if gender != 1 && gender != 2 {
		gender = DefaultGender
	}
	mu.Lock()
	defer mu.Unlock()
	a.Age = age
	a.Gender = gender
	a.GenderStr = genderIntToStr(gender)
	a.IsSavedAge = 1
	if strings.TrimSpace(a.Birthdate) == "" {
		a.Birthdate = BirthdateFromAge(age)
	}
	_ = saveLocked()
}

// SetNickname persists display nick (trade 0x70 C2S). Cap 32 like Python.
func (a *Account) SetNickname(nick string) {
	if a == nil {
		return
	}
	nick = strings.TrimSpace(nick)
	if nick == "" {
		return
	}
	nick = capUTF8Bytes(nick, 32)
	persistMutation(a, func() {
		a.Nickname = nick
	})
}

func (a *Account) EnsureTalentPages() map[int]TalentGroupRec {
	if a == nil {
		out := map[int]TalentGroupRec{}
		for gid := 1; gid <= 7; gid++ {
			out[gid] = defaultTalentGroup(config.TalentPointsDefault)
		}
		return out
	}
	mu.Lock()
	defer mu.Unlock()
	return ensureTalentPagesLocked(a)
}

func defaultTalentGroup(budget int) TalentGroupRec {
	return TalentGroupRec{
		Echo: budget, Unlocked: true, Limit: budget,
		Talents: [][]int{}, Layers: []int{},
		F14: budget, F18: budget, F20: budget,
	}
}

func normalizeTalentInfos(raw [][]int) [][]int {
	out := make([][]int, 0, len(raw))
	for _, row := range raw {
		if len(row) == 0 {
			continue
		}
		id, rank, extra := row[0], 0, 0
		if len(row) > 1 {
			rank = row[1]
		}
		if len(row) > 2 {
			extra = row[2]
		}
		out = append(out, []int{id, rank, extra})
	}
	return out
}

func talentSpent(rows [][]int) int {
	spent := 0
	for _, row := range normalizeTalentInfos(rows) {
		if row[1] > 0 {
			spent += row[1]
		}
	}
	return spent
}

// clampTalentInfos enforces the one shared budget inside a talent preset.
// Rows are kept in deterministic talent-ID order; the final positive rank is
// truncated if necessary and all later ranks are dropped.
func clampTalentInfos(rows [][]int, budget int) [][]int {
	byID := map[int][]int{}
	for _, row := range normalizeTalentInfos(rows) {
		if row[0] > 0 && row[1] > 0 {
			byID[row[0]] = row
		}
	}
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	remaining := maxInt(0, budget)
	out := make([][]int, 0, len(ids))
	for _, id := range ids {
		if remaining == 0 {
			break
		}
		row := append([]int(nil), byID[id]...)
		if row[1] > remaining {
			row[1] = remaining
		}
		remaining -= row[1]
		out = append(out, row)
	}
	return out
}

func cloneTalentGroup(g TalentGroupRec) TalentGroupRec {
	g.Talents = normalizeTalentInfos(g.Talents)
	g.Layers = append([]int(nil), g.Layers...)
	return g
}

func ensureTalentPagesLocked(a *Account) map[int]TalentGroupRec {
	budget := config.TalentPointsDefault
	if a.TalentPoints > 0 {
		budget = a.TalentPoints
	}
	if a.Talents == nil {
		a.Talents = map[string]TalentGroupRec{}
	}
	out := map[int]TalentGroupRec{}
	for gid := 1; gid <= 7; gid++ {
		g, ok := a.Talents[strconv.Itoa(gid)]
		if !ok {
			g = defaultTalentGroup(budget)
		}
		g = cloneTalentGroup(g)
		g.Talents = clampTalentInfos(g.Talents, budget)
		g.Unlocked = true
		g.Limit = budget
		g.Echo = maxInt(0, budget-talentSpent(g.Talents))
		// SwitchToStyle maps the four visible talent classes to the four
		// per-class budgets in TalentGroupInfo: F14, F18, Limit, F20.
		// Leaving F14/F20 at zero locks Support/Guardian client-side before
		// any C2S unlock request is emitted.
		g.F14 = budget
		g.F18 = budget
		g.F20 = budget
		a.Talents[strconv.Itoa(gid)] = g
		out[gid] = cloneTalentGroup(g)
	}
	a.TalentPoints = budget
	return out
}

func (a *Account) UnlockTalent(groupID int, layerID *int, runeCost int) map[int]TalentGroupRec {
	if a == nil {
		return nil
	}
	if groupID < 1 || groupID > 7 {
		groupID = 1
	}
	var out map[int]TalentGroupRec
	persistMutation(a, func() {
		groups := ensureTalentPagesLocked(a)
		g := groups[groupID]
		g.Unlocked = true
		if layerID != nil {
			seen := false
			for _, id := range g.Layers {
				seen = seen || id == *layerID
			}
			if !seen {
				g.Layers = append(g.Layers, *layerID)
			}
		}
		a.Talents[strconv.Itoa(groupID)] = g
		if runeCost > 0 {
			a.Rune = maxInt(0, a.Rune-runeCost)
		}
		out = ensureTalentPagesLocked(a)
	})
	return out
}

func (a *Account) ApplyTalentUpdate(groupID int, incoming [][]int, reset bool) (map[int]TalentGroupRec, TalentGroupRec) {
	if a == nil {
		return nil, defaultTalentGroup(config.TalentPointsDefault)
	}
	if groupID < 1 || groupID > 7 {
		groupID = 1
	}
	var out map[int]TalentGroupRec
	var selected TalentGroupRec
	persistMutation(a, func() {
		groups := ensureTalentPagesLocked(a)
		g := groups[groupID]
		if reset {
			g.Talents = [][]int{}
		} else {
			byID := map[int][]int{}
			for _, row := range normalizeTalentInfos(g.Talents) {
				byID[row[0]] = row
			}
			for _, row := range normalizeTalentInfos(incoming) {
				byID[row[0]] = row
			}
			ids := make([]int, 0, len(byID))
			for id, row := range byID {
				if id > 0 && row[1] != 0 {
					ids = append(ids, id)
				}
			}
			sort.Ints(ids)
			g.Talents = make([][]int, 0, len(ids))
			for _, id := range ids {
				g.Talents = append(g.Talents, byID[id])
			}
		}
		a.Talents[strconv.Itoa(groupID)] = g
		out = ensureTalentPagesLocked(a)
		selected = out[groupID]
	})
	return out, selected
}

// SetSummonerSpells remembers the pair the client reported (both non-zero).
func (a *Account) SetSummonerSpells(s1, s2 int) {
	if a == nil || s1 <= 0 || s2 <= 0 {
		return
	}
	mu.RLock()
	same := len(a.SummonerSpells) == 2 && a.SummonerSpells[0] == s1 && a.SummonerSpells[1] == s2
	mu.RUnlock()
	if same {
		return
	}
	persistMutation(a, func() {
		a.SummonerSpells = []int{s1, s2}
	})
}

// SummonerSpellPair returns the remembered pair, or ok=false when none.
func (a *Account) SummonerSpellPair() (s1, s2 int, ok bool) {
	if a == nil {
		return 0, 0, false
	}
	mu.RLock()
	defer mu.RUnlock()
	if len(a.SummonerSpells) != 2 || a.SummonerSpells[0] <= 0 || a.SummonerSpells[1] <= 0 {
		return 0, 0, false
	}
	return a.SummonerSpells[0], a.SummonerSpells[1], true
}
