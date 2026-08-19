package accounts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

func TestAgeGateSeedAndProfileWire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	raw := map[string]any{
		"accounts": map[string]any{
			"enterpries1": map[string]any{
				"username": "enterpries1",
				"password": "example-pass-1",
				"nickname": "Enterpries1",
				"user_id":  1000001,
				"level":    40,
				// Missing birthdate / is_saved_age → EnsureAgeGate must fill.
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
	a := accounts.Get("enterpries1")
	if a == nil {
		t.Fatal("missing account")
	}
	age, gender, saved := a.AgeFields()
	if age < 13 || saved != 1 {
		t.Fatalf("age fields age=%d gender=%d saved=%d", age, gender, saved)
	}
	bd := a.BirthdateStr()
	if len(bd) < 19 || bd[len(bd)-1] != 'Z' {
		t.Fatalf("birthdate wire %q", bd)
	}
	gs := a.GenderWire()
	if gs != "male" && gs != "female" {
		t.Fatalf("gender wire %q", gs)
	}

	a.SetBirthdate(`"2003-07-25 01:59:14Z"`)
	if a.BirthdateStr() != "2003-07-25 01:59:14Z" {
		t.Fatalf("set birthdate %q", a.BirthdateStr())
	}
	a.SetGenderStr("female")
	if a.GenderWire() != "female" {
		t.Fatalf("gender %q", a.GenderWire())
	}
	_, g2, s2 := a.AgeFields()
	if g2 != 2 || s2 != 1 {
		t.Fatalf("after set gender=%d saved=%d", g2, s2)
	}
}

func TestNormStripsGllivePrefix(t *testing.T) {
	if accounts.Norm("gllive:enterpries1") != "enterpries1" {
		t.Fatalf("got %q", accounts.Norm("gllive:enterpries1"))
	}
	if accounts.Norm("GLLIVE:Enterpries2") != "enterpries2" {
		t.Fatalf("got %q", accounts.Norm("GLLIVE:Enterpries2"))
	}
}

func TestBirthdateFromAgeFormat(t *testing.T) {
	s := accounts.BirthdateFromAge(23)
	if len(s) < 19 {
		t.Fatalf("bad format %q", s)
	}
	if s[len(s)-1] != 'Z' {
		t.Fatalf("missing Z: %q", s)
	}
}

func TestCollectionCatalogDefaultAndExplicitEmpty(t *testing.T) {
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.json")
	catalogPath := filepath.Join(dir, "collection_catalog.json")
	raw := map[string]any{
		"accounts": map[string]any{
			"full": map[string]any{
				"username": "full", "password": "pw",
			},
			"none": map[string]any{
				"username": "none", "password": "pw", "heroes": []int{},
			},
		},
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(accountsPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, _ := json.Marshal(map[string]any{"heroes": []int{131, 158, 131, 0}})
	if err := os.WriteFile(catalogPath, catalog, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(accountsPath); err != nil {
		t.Fatal(err)
	}
	if got := accounts.Get("full").OwnedHeroIDs(); len(got) != 2 || got[0] != 131 || got[1] != 158 {
		t.Fatalf("default catalog=%v", got)
	}
	if got := accounts.Get("none").OwnedHeroIDs(); len(got) != 0 {
		t.Fatalf("explicit empty heroes=%v", got)
	}
}

func TestAwakeNilMeansLegacyAutoButEmptyMeansAsleep(t *testing.T) {
	tablets := map[string]accounts.TabletRec{
		"0:0": {ID: 453, Sockets: map[string][]int{}},
	}
	legacy := &accounts.Account{Tablets: tablets, AwakeTabletIDs: nil}
	if !legacy.AwakeTablets()[453] {
		t.Fatal("nil awake list must auto-wake equipped legacy tablet")
	}
	asleep := &accounts.Account{Tablets: tablets, AwakeTabletIDs: []int{}}
	if asleep.AwakeTablets()[453] {
		t.Fatal("explicit empty awake list must remain asleep")
	}
}

func TestTemporaryRegistrationPersistsDistinctIdentity(t *testing.T) {
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

	if _, _, ok := accounts.AuthenticateOrCreate("enterpries1", "wrong"); ok {
		t.Fatal("existing seed accepted a wrong password")
	}
	first, created, ok := accounts.AuthenticateOrCreate("BlueFox", "trash-pass")
	if !ok || !created || first == nil {
		t.Fatalf("first registration account=%v created=%v ok=%v", first, created, ok)
	}
	if first.Username != "bluefox" || first.Nickname != "BlueFox" || !first.Temporary {
		t.Fatalf("first identity=%+v", first)
	}
	if first.Level != 40 || first.Rune == 0 || first.Emblem == 0 || first.Gems == 0 {
		t.Fatalf("temporary account is not play-ready: %+v", first)
	}
	firstAccess, firstJanus := accounts.TokensFor(first)
	if accounts.ResolveToken(firstAccess) != first || accounts.ResolveToken(firstJanus) != first {
		t.Fatalf("tokens do not resolve first=%q/%q", firstAccess, firstJanus)
	}

	second, created, ok := accounts.AuthenticateOrCreate("RedWolf", "pw2")
	if !ok || !created || second == nil {
		t.Fatalf("second registration account=%v created=%v ok=%v", second, created, ok)
	}
	secondAccess, _ := accounts.TokensFor(second)
	if second.UserID == first.UserID || secondAccess == firstAccess || second.Nickname != "RedWolf" {
		t.Fatalf("identities collapsed first=%+v second=%+v tokens=%q/%q", first, second, firstAccess, secondAccess)
	}
	if first.Gateway == second.Gateway {
		t.Fatalf("dual-Nox fallback gateways did not alternate: %q", first.Gateway)
	}

	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	reloaded := accounts.Get("bluefox")
	if reloaded == nil || reloaded.Nickname != "BlueFox" || reloaded.Password != "trash-pass" {
		t.Fatalf("temporary account did not persist: %+v", reloaded)
	}
	if got := accounts.ResolveToken(firstAccess); got != reloaded {
		t.Fatalf("persisted token resolved to %v, want %v", got, reloaded)
	}
}

func TestLobbyFirstTemporaryAccountClaimsPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	a, created := accounts.EnsureTemporary("LobbyFirst")
	if a == nil || !created || a.Password != "" || a.Nickname != "LobbyFirst" {
		t.Fatalf("lobby account=%+v created=%v", a, created)
	}
	claimed, created, ok := accounts.AuthenticateOrCreate("lobbyfirst", "claimed")
	if !ok || created || claimed != a || claimed.Password != "claimed" {
		t.Fatalf("claim account=%+v created=%v ok=%v", claimed, created, ok)
	}
	if _, _, ok := accounts.AuthenticateOrCreate("lobbyfirst", "different"); ok {
		t.Fatal("claimed temporary account accepted a different password")
	}
}

func TestDeviceAccountsAreDistinctAndDoNotShiftPlayerGateways(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}

	device1, created := accounts.EnsureDevice("android:device-one-long-bootstrap-id")
	if device1 == nil || !created || !device1.Device || !device1.Temporary {
		t.Fatalf("device1=%+v created=%v", device1, created)
	}
	device2, created := accounts.EnsureDevice("android:device-two-long-bootstrap-id")
	if device2 == nil || !created || device2 == device1 || device2.UserID == device1.UserID {
		t.Fatalf("device identities collapsed first=%+v second=%+v", device1, device2)
	}
	device1Token, _ := accounts.TokensFor(device1)
	device2Token, _ := accounts.TokensFor(device2)
	if device1Token == device2Token || accounts.ResolveToken(device1Token) != device1 {
		t.Fatalf("device tokens first=%q second=%q", device1Token, device2Token)
	}

	player1, _, ok := accounts.AuthenticateOrCreate("PlayerOne", "pw1")
	if !ok {
		t.Fatal("player1 registration failed")
	}
	player2, _, ok := accounts.AuthenticateOrCreate("PlayerTwo", "pw2")
	if !ok {
		t.Fatal("player2 registration failed")
	}
	if player1.Gateway != "172.16.42.2" || player2.Gateway != "172.16.57.2" {
		t.Fatalf("device accounts shifted player gateways: %q/%q", player1.Gateway, player2.Gateway)
	}
}

// D2 regression (AGENTS5 §2.2.1): an account created on Nox keeps gateway
// 172.16.x.x in accounts.json. When the server serves a LAN edge (phone
// deploy) that pinned value is unreachable from the phone and must not be
// advertised in e02d. The temporary-account / nickname design is unchanged:
// only the relay IP is re-resolved.
func TestLANEdgeOverridesPinnedNoxGateway(t *testing.T) {
	oldHost, oldMode, oldGw := config.EdgeHost, config.EdgeMode, config.GSGateway
	defer func() { config.EdgeHost, config.EdgeMode, config.GSGateway = oldHost, oldMode, oldGw }()

	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte(`{"accounts":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}

	// Account first seen on Nox → gateway pinned to the DNAT pool.
	config.EdgeHost, config.EdgeMode, config.GSGateway = config.DefaultEdgeHost, "hostname", ""
	nox, created, ok := accounts.AuthenticateOrCreate("alphaui", "anything")
	if !ok || !created {
		t.Fatalf("nox account create ok=%v created=%v", ok, created)
	}
	if nox.Gateway != "172.16.42.2" {
		t.Fatalf("pinned nox gateway=%q", nox.Gateway)
	}
	if got := accounts.GatewayFor(nox); got != "172.16.42.2" {
		t.Fatalf("nox edge GatewayFor=%q, want Nox pool", got)
	}

	// Same account, now over a LAN edge (phone): must NOT hand out the Nox IP.
	config.EdgeHost, config.EdgeMode, config.GSGateway = "192.168.68.111", "lan", "192.168.68.111"
	same, created, ok := accounts.AuthenticateOrCreate("alphaui", "anything")
	if !ok || created || same != nox {
		t.Fatalf("re-login ok=%v created=%v same=%v", ok, created, same == nox)
	}
	if got := accounts.GatewayFor(same); got != "192.168.68.111" {
		t.Fatalf("lan edge GatewayFor=%q, want LAN IP (D2)", got)
	}
	// Identity untouched: nickname still equals the entered username.
	if same.Nickname != "alphaui" || !same.Temporary {
		t.Fatalf("temp identity changed: nick=%q temporary=%v", same.Nickname, same.Temporary)
	}
	// Persisted field itself is left alone; only resolution changes.
	if same.Gateway != "172.16.42.2" {
		t.Fatalf("persisted gateway mutated=%q", same.Gateway)
	}

	// A brand-new account on the LAN edge gets the LAN IP written directly.
	fresh, created, ok := accounts.AuthenticateOrCreate("phonefresh", "pw")
	if !ok || !created {
		t.Fatalf("fresh create ok=%v created=%v", ok, created)
	}
	if fresh.Gateway != "192.168.68.111" || accounts.GatewayFor(fresh) != "192.168.68.111" {
		t.Fatalf("fresh lan gateway=%q resolved=%q", fresh.Gateway, accounts.GatewayFor(fresh))
	}

	// Back on Nox: the pool must still work (no regression for dual-Nox).
	config.EdgeHost, config.EdgeMode, config.GSGateway = config.DefaultEdgeHost, "hostname", ""
	if got := accounts.GatewayFor(nox); got != "172.16.42.2" {
		t.Fatalf("nox regression GatewayFor=%q", got)
	}
}
