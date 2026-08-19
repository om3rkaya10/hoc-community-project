package trade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/session"
	"hoc-server/internal/wire/msgpack"
)

func loadTradeAccount(t *testing.T, record map[string]any) *accounts.Account {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	raw := map[string]any{"accounts": map[string]any{"tester": record}}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	a := accounts.Get("tester")
	if a == nil {
		t.Fatal("test account missing")
	}
	return a
}

func tradeArray(values ...[]byte) []byte {
	out := msgpack.FixArray(len(values))
	for _, value := range values {
		out = append(out, value...)
	}
	return out
}

func TestBuyItemCRMFallsBackToUnitPrice(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"emblem": 1000, "rune": 200, "gems": 300,
	})
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(513),
		msgpack.Int(1), msgpack.Int(5), msgpack.Int(0), msgpack.Int(75),
	)
	var sentSub uint16
	handleBuyItemCRM(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x6e,
		Send: func(sub uint16, _ []byte) { sentSub = sub },
	})
	emblem, _, _ := a.Wallet()
	if emblem != 925 {
		t.Fatalf("emblem=%d, want 925", emblem)
	}
	found := false
	for _, pair := range a.InscriptionPairs() {
		if pair[2] == 513 && pair[3] == 1 {
			found = true
		}
	}
	if !found || sentSub != 0x6e {
		t.Fatalf("purchase found=%v sent=%#x", found, sentSub)
	}
}

func TestDeleteTabletReturnsEquippedSockets(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"tablets": map[string]any{
			"0:0": map[string]any{
				"id":      453,
				"sockets": map[string]any{"0": []int{494, 2}},
			},
		},
		"awake_tablets": []int{453},
	})
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(0),
		msgpack.Int(1), msgpack.Int(6), msgpack.Int(1),
	)
	applyKitabeMutation(0x53, body, a)
	if len(a.EquippedTablets()) != 0 {
		t.Fatalf("tablet still equipped: %#v", a.EquippedTablets())
	}
	qty := 0
	for _, pair := range a.InscriptionPairs() {
		if pair[0] == 1 && pair[2] == 494 {
			qty = pair[3]
		}
	}
	if qty != 2 {
		t.Fatalf("returned inscription qty=%d, want 2", qty)
	}
}

func TestTalentBatchUpsertsWithoutWipingOtherNodes(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "nickname": "Tester",
		"talent_points": 40,
		"talents": map[string]any{
			"1": map[string]any{
				"unlocked": true, "limit": 40, "echo": 32,
				"talents": [][]int{{27, 5, 0}, {28, 3, 0}},
			},
		},
	})
	rows := append(msgpack.FixArray(1), tradeArray(msgpack.Int(27), msgpack.Int(6), msgpack.Int(0))...)
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(1), rows,
	)
	var reply []byte
	handleTalentOp(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x3b,
		Send: func(_ uint16, body []byte) { reply = body },
	})
	g := a.EnsureTalentPages()[1]
	if g.Echo != 31 || len(g.Talents) != 2 {
		t.Fatalf("group echo=%d talents=%#v", g.Echo, g.Talents)
	}
	if g.Talents[0][0] != 27 || g.Talents[0][1] != 6 || g.Talents[1][0] != 28 {
		t.Fatalf("upsert wiped/reordered badly: %#v", g.Talents)
	}
	v, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.([]any)[4]; got != int64(31) {
		t.Fatalf("reply remaining=%v", got)
	}
}

func TestM5ResidualRegistryIsTyped(t *testing.T) {
	for _, sub := range []uint16{
		0x4b, 0x4c, 0x4d, 0x4e, 0x4f, 0x50, 0x51, 0x52, 0x53,
		0x59, 0x7c, 0x5c, 0x5d, 0x6e, 0x70, 0x1e, 0x1f, 0x3b, 0x3c,
	} {
		if registry[sub] == nil {
			t.Fatalf("sub %#x has no typed handler", sub)
		}
	}
}
