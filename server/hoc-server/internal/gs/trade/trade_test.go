package trade

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/domain/kitabe"
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
	// SendDeleteTabletRequest (live): [26, name, ownedIndex, 1, uid, page1];
	// 453 is owned index 0 of the grant list.
	if idx := a.OwnedTabletIndex(453); idx != 0 {
		t.Fatalf("owned index of 453 = %d, want 0", idx)
	}
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(0),
		msgpack.Int(1), msgpack.Int(6), msgpack.Int(1),
	)
	applyKitabeMutation(0x53, body, a)
	if len(a.EquippedTablets()) != 0 {
		t.Fatalf("tablet still equipped: %#v", a.EquippedTablets())
	}
	if a.OwnedTabletIndex(453) != -1 {
		t.Fatal("deleted tablet still in the owned vector")
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

func inscriptionQty(a *accounts.Account, itemID int) int {
	for _, pair := range a.InscriptionPairs() {
		if pair[0] == 1 && pair[2] == itemID {
			return pair[3]
		}
	}
	return 0
}

// capturedMergeBody is the live 0x50 request: [26, "tester", 0, 4×InscriptionInfo(494), 6].
func capturedMergeBody(t *testing.T) []byte {
	t.Helper()
	body, err := hex.DecodeString("951aa674657374657200949dcd01ee00000000c300ca0000000000000190909dcd01ee00000000c300ca0000000000000190909dcd01ee00000000c300ca0000000000000190909dcd01ee00000000c300ca00000000000001909006")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// setMergeSource rewrites the n-th (1-based, 0 = all) source id in
// capturedMergeBody (all four are msgpack uint16 494 = cd 01 ee).
func setMergeSource(body []byte, n int, id int) {
	hits := 0
	for i := 0; i+3 < len(body); i++ {
		// each row is a 13-elem array (0x9d) whose first field is a uint16
		if body[i] == 0x9d && body[i+1] == 0xcd {
			hits++
			if n == 0 || hits == n {
				body[i+2] = byte(id >> 8)
				body[i+3] = byte(id)
			}
		}
	}
}

func fixedRandomInscription(t *testing.T, pick int) {
	t.Helper()
	prev := randomInscription
	randomInscription = func(pool []int) int {
		for _, id := range pool {
			if id == pick {
				return pick
			}
		}
		t.Fatalf("pick %d not in pool %v", pick, pool)
		return 0
	}
	t.Cleanup(func() { randomInscription = prev })
}

func replyField6(t *testing.T, reply []byte) int64 {
	t.Helper()
	v, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	top, ok := v.([]any)
	if !ok || len(top) != 11 {
		t.Fatalf("reply is not the 11-elem Unlock family: %#v", v)
	}
	n, _ := top[6].(int64)
	return n
}

func TestMergeInscriptionCapturedSilverToGold(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"inscriptions": map[string]int{"494": 8},
	})
	fixedRandomInscription(t, 483) // any gold; the exchange result is random
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: capturedMergeBody(t), Sub: 0x50,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if got := inscriptionQty(a, 494); got != 4 {
		t.Fatalf("source 494 qty=%d, want 4", got)
	}
	if got := inscriptionQty(a, 483); got != 1 {
		t.Fatalf("target 483 qty=%d, want 1", got)
	}
	// [6] carries the produced item: onMergeInscriptionResponse shows it.
	if got := replyField6(t, reply); got != 483 {
		t.Fatalf("reply[6]=%d, want 483", got)
	}
}

func TestMergeInscriptionBronzeToSilver(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"inscriptions": map[string]int{"455": 4},
	})
	fixedRandomInscription(t, 504)
	body := capturedMergeBody(t)
	setMergeSource(body, 0, 455)
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x50,
		Send: func(_ uint16, _ []byte) {},
	})
	if got := inscriptionQty(a, 455); got != 0 {
		t.Fatalf("source 455 qty=%d, want 0", got)
	}
	if got := inscriptionQty(a, 504); got != 1 {
		t.Fatalf("target 504 qty=%d, want 1", got)
	}
}

// Four different inscriptions of one tier are a valid exchange (the EXCHANGE
// page lets the player mix stats; only the tier must match).
func TestMergeInscriptionAcceptsMixedStatsOfOneTier(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"inscriptions": map[string]int{"494": 2, "498": 1, "504": 1},
	})
	fixedRandomInscription(t, 495)
	body := capturedMergeBody(t)
	setMergeSource(body, 3, 498)
	setMergeSource(body, 4, 504)
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x50,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if inscriptionQty(a, 494) != 0 || inscriptionQty(a, 498) != 0 || inscriptionQty(a, 504) != 0 {
		t.Fatalf("sources not consumed: %#v", a.InscriptionPairs())
	}
	if got := inscriptionQty(a, 495); got != 1 {
		t.Fatalf("target 495 qty=%d, want 1", got)
	}
	if got := replyField6(t, reply); got != 495 {
		t.Fatalf("reply[6]=%d, want 495", got)
	}
}

func TestMergeInscriptionRejectsMixedTiers(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"inscriptions": map[string]int{"494": 3, "455": 1},
	})
	body := capturedMergeBody(t)
	setMergeSource(body, 4, 455) // bronze among silvers
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x50,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if inscriptionQty(a, 494) != 3 || inscriptionQty(a, 455) != 1 {
		t.Fatalf("mixed-tier request mutated inventory: %#v", a.InscriptionPairs())
	}
	if got := replyField6(t, reply); got != 0 {
		t.Fatalf("reply[6]=%d, want 0", got)
	}
}

func TestMergeInscriptionRejectsInsufficientInventory(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"inscriptions": map[string]int{"494": 3},
	})
	fixedRandomInscription(t, 495)
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: capturedMergeBody(t), Sub: 0x50,
		Send: func(_ uint16, _ []byte) {},
	})
	if inscriptionQty(a, 494) != 3 || inscriptionQty(a, 495) != 0 {
		t.Fatalf("insufficient request mutated inventory: %#v", a.InscriptionPairs())
	}
}

// exchangeGoldBody builds 0x59 as captured live (2026-09-18):
// 961aa57465737461cd01ff02919dcd01e3... = [26, name, targetID, payType,
// vector[InscriptionInfo(source)], uid].
func exchangeGoldBody(payType, source, target int) []byte {
	row := msgpack.FixArray(13)
	row = append(row, msgpack.Int(int64(source))...)
	for i := 0; i < 4; i++ {
		row = append(row, msgpack.Int(0)...)
	}
	row = append(row, msgpack.Bool(true)...)
	row = append(row, msgpack.Int(0)...)
	row = append(row, msgpack.Float32(0)...)
	row = append(row, msgpack.Int(0)...)
	row = append(row, msgpack.Int(0)...)
	row = append(row, msgpack.Int(1)...)
	row = append(row, msgpack.EmptyArray()...)
	row = append(row, msgpack.EmptyArray()...)
	vec := append(msgpack.FixArray(1), row...)
	return tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(int64(target)),
		msgpack.Int(int64(payType)), vec, msgpack.Int(6),
	)
}

func TestExchangeGoldCapturedWireLayout(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 1000, "rune": 50,
		"inscriptions": map[string]int{"483": 1, "511": 1},
	})
	// Live capture, username swapped for the test account.
	body, err := hex.DecodeString("961aa674657374657" + "2" + "cd01ff02919dcd01e300000000c300ca00000000000001909006")
	if err != nil {
		t.Fatal(err)
	}
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x59,
		Send: func(_ uint16, _ []byte) {},
	})
	if _, runeV, _ := a.Wallet(); runeV != 50-config.KitabeExchangeGoldRune {
		t.Fatalf("rune=%d", runeV)
	}
	if inscriptionQty(a, 483) != 0 || inscriptionQty(a, 511) != 2 {
		t.Fatalf("captured exchange not applied: %#v", a.InscriptionPairs())
	}
}

func TestExchangeGoldInscriptionDebitsAndSwaps(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 1000, "rune": 50,
		"inscriptions": map[string]int{"495": 1},
	})
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a},
		Body: exchangeGoldBody(accounts.PayEmblem, 495, 483), Sub: 0x59,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if emblem, runeV, _ := a.Wallet(); emblem != 1000-config.KitabeExchangeGoldEmblem || runeV != 50 {
		t.Fatalf("wallet emblem=%d rune=%d", emblem, runeV)
	}
	if inscriptionQty(a, 495) != 0 || inscriptionQty(a, 483) != 1 {
		t.Fatalf("exchange not applied: %#v", a.InscriptionPairs())
	}
	if got := replyField6(t, reply); got != 483 {
		t.Fatalf("reply[6]=%d, want 483", got)
	}
}

func TestExchangeGoldInscriptionRejectsBadRequests(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 1000, "rune": 50,
		"inscriptions": map[string]int{"495": 1, "494": 1},
	})
	for name, body := range map[string][]byte{
		"silver source": exchangeGoldBody(accounts.PayRune, 494, 483),
		"silver target": exchangeGoldBody(accounts.PayRune, 495, 494),
		"same item":     exchangeGoldBody(accounts.PayRune, 495, 495),
		"unknown pay":   exchangeGoldBody(9, 495, 483),
	} {
		handleKitabeFamily(&Ctx{
			Sess: &session.Session{Account: a}, Body: body, Sub: 0x59,
			Send: func(_ uint16, _ []byte) {},
		})
		if emblem, runeV, _ := a.Wallet(); emblem != 1000 || runeV != 50 {
			t.Fatalf("%s: wallet changed emblem=%d rune=%d", name, emblem, runeV)
		}
		if inscriptionQty(a, 495) != 1 || inscriptionQty(a, 494) != 1 || inscriptionQty(a, 483) != 0 {
			t.Fatalf("%s: inventory changed: %#v", name, a.InscriptionPairs())
		}
	}
}

func TestSleepTabletDebitsEmblemAndReturnsTargetIndex(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 2000,
		"tablets": map[string]any{
			"0:0": map[string]any{"id": 464},
			"0:1": map[string]any{"id": 465},
			"0:2": map[string]any{"id": 467},
			"1:1": map[string]any{"id": 469},
		},
		"awake_tablets": []int{464, 465, 467, 469},
	})
	// Captured shape: [26,user,1,ownedIndex=3,payType=5,6,page=2].
	// ownedIndex addresses the owned TabletInfo vector (467 is index 3 of the
	// grant list), not an equipped position or a page-local slot.
	body, err := hex.DecodeString("971aa67465737465720103050602")
	if err != nil {
		t.Fatal(err)
	}
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x4e,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if emblem, _, _ := a.Wallet(); emblem != 1250 {
		t.Fatalf("emblem=%d, want 1250", emblem)
	}
	if a.AwakeTablets()[467] {
		t.Fatal("tablet 467 remained awake")
	}
	v, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	top := v.([]any)
	if got := top[1]; got != int64(1250) {
		t.Fatalf("response emblem=%v, want 1250", got)
	}
	if top[9] != int64(5) || top[10] != int64(3) {
		t.Fatalf("callback target=%v/%v, want payType=5 owned-index=3", top[9], top[10])
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

func TestSleepTabletAcceptsRunePayment(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 2000, "rune": 100,
		"tablets":       map[string]any{"0:0": map[string]any{"id": 464}},
		"awake_tablets": []int{464},
	})
	// [26,user,1,ownedIndex=1 (464),payType=2 rune,6,page=1]
	body, err := hex.DecodeString("971aa67465737465720101020601")
	if err != nil {
		t.Fatal(err)
	}
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x4e,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	emblem, runeV, _ := a.Wallet()
	if emblem != 2000 || runeV != 80 {
		t.Fatalf("wallet emblem=%d rune=%d, want 2000/80", emblem, runeV)
	}
	if a.AwakeTablets()[464] {
		t.Fatal("tablet 464 remained awake")
	}
	top, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	if arr := top.([]any); arr[0] != int64(0) || arr[9] != int64(2) || arr[10] != int64(1) {
		t.Fatalf("reply result/callback = %v/%v/%v", arr[0], arr[9], arr[10])
	}
}

func TestSleepTabletRejectsBackpackDefaultIndex(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 2000,
		"tablets":       map[string]any{"0:0": map[string]any{"id": 464}},
		"awake_tablets": []int{464},
	})
	// ownedIndex=-1 (TabletButton default when no index was assigned).
	body, err := hex.DecodeString("971aa674657374657201ff050601")
	if err != nil {
		t.Fatal(err)
	}
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x4e,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if emblem, _, _ := a.Wallet(); emblem != 2000 {
		t.Fatalf("rejected sleep charged: emblem=%d", emblem)
	}
	top, _ := msgpack.Decode(reply)
	if top.([]any)[0] != int64(1) {
		t.Fatal("expected result=1")
	}
}

func TestBuyItemCRMEmblemPackCreditsEmblemsForRunes(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"emblem": 1000, "rune": 600, "gems": 300,
	})
	// Live capture: 515 "Emblem pack (5000)" pay=2 price=525.
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(515),
		msgpack.Int(1), msgpack.Int(2), msgpack.Int(525), msgpack.Int(525),
	)
	var reply []byte
	handleBuyItemCRM(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x6e,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	emblem, runeV, _ := a.Wallet()
	if emblem != 6000 || runeV != 75 {
		t.Fatalf("wallet emblem=%d rune=%d, want 6000/75", emblem, runeV)
	}
	if len(a.InscriptionPairs()) != 0 {
		t.Fatalf("pack leaked into inscriptions: %v", a.InscriptionPairs())
	}
	top, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	arr := top.([]any)
	if arr[4] != int64(75) || arr[5] != int64(6000) {
		t.Fatalf("reply wallet [4]=%v [5]=%v", arr[4], arr[5])
	}
	if len(arr) != 20 {
		t.Fatalf("reply len=%d, want 20 (GESub5Member18 at [19])", len(arr))
	}
}

func TestBuyItemCRMPoleAndPatternOwnership(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 1000, "rune": 2000,
	})
	for _, id := range []int{568, 860, 563} { // Chieftain's Flagstaff, League Banner (unlimited), Jest Banner x1
		body := tradeArray(
			msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(int64(id)),
			msgpack.Int(1), msgpack.Int(2), msgpack.Int(100), msgpack.Int(100),
		)
		handleBuyItemCRM(&Ctx{
			Sess: &session.Session{Account: a}, Body: body, Sub: 0x6e,
			Send: func(_ uint16, _ []byte) {},
		})
	}
	if a.PoleCounts()[568] != 1 || a.PatternCounts()[860] != 1 || a.PatternCounts()[563] != 1 {
		t.Fatalf("poles=%v patterns=%v", a.PoleCounts(), a.PatternCounts())
	}
	if len(a.InscriptionPairs()) != 0 {
		t.Fatal("flags leaked into inscriptions")
	}
	// In-match UseFlag consumes one jest banner charge but never an unlimited banner.
	for _, id := range []int{563, 860} {
		body := tradeArray(
			msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(int64(id)),
			msgpack.Int(1), msgpack.Bool(false), msgpack.Int(6),
		)
		handleUseFlag(&Ctx{Sess: &session.Session{Account: a}, Body: body, Sub: 0x5b})
	}
	if a.PatternCounts()[563] != 0 || a.PatternCounts()[860] != 1 {
		t.Fatalf("after use patterns=%v", a.PatternCounts())
	}
}

func TestSelectFlagPersistsSelectionAndRepliesResultZero(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{"username": "tester", "password": "pw"})
	// SendSelectFlagRequest: [26, name, pattern, pole, type, uid]
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(573),
		msgpack.Int(568), msgpack.Int(0x11), msgpack.Int(6),
	)
	var reply []byte
	handleSelectFlag(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x5c,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	pole, pattern, typ := a.FlagSelection()
	if pole != 568 || pattern != 573 || typ != 0x11 {
		t.Fatalf("selection=%d/%d/%#x", pole, pattern, typ)
	}
	v, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	arr := v.([]any)
	if len(arr) != 5 || arr[0] != int64(0) || arr[2] != int64(573) || arr[3] != int64(568) || arr[4] != int64(0x11) {
		t.Fatalf("reply=%#v", arr)
	}
}

func TestBuyItemCRMPackDebitsLineTotalOnce(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "rune": 9999,
	})
	// Live: 50× Artisan Banner - Peace, [5]=235 line total, [6]=235.
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(573),
		msgpack.Int(50), msgpack.Int(2), msgpack.Int(235), msgpack.Int(235),
	)
	handleBuyItemCRM(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x6e,
		Send: func(_ uint16, _ []byte) {},
	})
	if _, runeV, _ := a.Wallet(); runeV != 9999-235 {
		t.Fatalf("rune=%d, want %d", runeV, 9999-235)
	}
	if a.PatternCounts()[573] != 50 {
		t.Fatalf("patterns=%v", a.PatternCounts())
	}
}

// fillInscriptionBody builds SendFillInscriptionSlotRequest:
// [26, name, 1, ownedIndex, TabletInfo, ascend, uid, page1].
func fillInscriptionBody(tabletID, ownedIndex int, sockets map[int][2]int, ascend, page1 int) []byte {
	return tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(1),
		msgpack.Int(int64(ownedIndex)), kitabe.TabletInfo(tabletID, sockets, false),
		msgpack.Int(int64(ascend)), msgpack.Int(6), msgpack.Int(int64(page1)),
	)
}

func replyField7(t *testing.T, reply []byte) int64 {
	t.Helper()
	v, err := msgpack.Decode(reply)
	if err != nil {
		t.Fatal(err)
	}
	top, ok := v.([]any)
	if !ok || len(top) != 11 {
		t.Fatalf("reply is not the 11-elem Unlock family: %#v", v)
	}
	n, _ := top[7].(int64)
	return n
}

func TestWearDoesNotAscendTablet(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"owned_tablets": []int{453, 464},
		"tablets":       map[string]any{"0:0": map[string]any{"id": 453}},
		"awake_tablets": []int{},
	})
	// SendFillTabletSlotRequest (live): [26, name, slot, ownedIndex, uid, page1]
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(1),
		msgpack.Int(1), msgpack.Int(6), msgpack.Int(2),
	)
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x4b,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if eq, ok := a.EquippedTablets()[[2]int{1, 1}]; !ok || eq.ID != 464 {
		t.Fatalf("tablet 464 not equipped on page 2 slot 1: %#v", a.EquippedTablets())
	}
	if a.AwakeTablets()[464] {
		t.Fatal("wearing a tablet must not ascend it")
	}
	if got := replyField7(t, reply); got != 0 {
		t.Fatalf("reply[7]=%d, want 0 for a plain wear", got)
	}
}

func TestFillInscriptionSaveKeepsTabletUnascended(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"owned_tablets": []int{453},
		"tablets":       map[string]any{"0:0": map[string]any{"id": 453}},
		"awake_tablets": []int{},
		"inscriptions":  map[string]int{"494": 4},
	})
	socks := map[int][2]int{0: {494, 1}, 1: {494, 1}}
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: fillInscriptionBody(453, 0, socks, 0, 1), Sub: 0x4f,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if got := a.TabletSockets()[453]; len(got) != 2 || got[0][0] != 494 || got[1][0] != 494 {
		t.Fatalf("sockets not applied: %#v", got)
	}
	if a.AwakeTablets()[453] {
		t.Fatal("ascend=0 save must leave the tablet unascended")
	}
	if got := replyField7(t, reply); got != 0 {
		t.Fatalf("reply[7]=%d, want 0 (return to tablet page)", got)
	}
}

func TestFillInscriptionAscendFlagAscendsAndEchoesField7(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw",
		"owned_tablets": []int{453},
		"tablets":       map[string]any{"0:0": map[string]any{"id": 453}},
		"awake_tablets": []int{},
		"inscriptions":  map[string]int{"494": 4},
	})
	socks := map[int][2]int{0: {494, 1}, 1: {494, 1}, 2: {494, 1}, 3: {494, 1}}
	var reply []byte
	handleKitabeFamily(&Ctx{
		Sess: &session.Session{Account: a}, Body: fillInscriptionBody(453, 0, socks, 1, 1), Sub: 0x4f,
		Send: func(_ uint16, b []byte) { reply = b },
	})
	if !a.AwakeTablets()[453] {
		t.Fatal("ascend=1 save must ascend the tablet")
	}
	if got := replyField7(t, reply); got != 1 {
		t.Fatalf("reply[7]=%d, want 1 (client stays on the page and plays the ascension)", got)
	}
	// The ascended tablet is reported as awake in both the equipped and the
	// owned vectors, and the ascension survives a page change.
	v, _ := msgpack.Decode(reply)
	top := v.([]any)
	slots := top[4].([]any)
	if len(slots) != 1 || slots[0].([]any)[0].([]any)[8] != int64(2) {
		t.Fatalf("equipped TabletInfo[8] != 2: %#v", slots)
	}
	owned := top[5].([]any)
	if len(owned) != 1 || owned[0].([]any)[8] != int64(2) {
		t.Fatalf("owned TabletInfo[8] != 2: %#v", owned)
	}
	a.UnequipTablet(0, 0)
	if !a.AwakeTablets()[453] {
		t.Fatal("unequipping must not clear a paid-to-unlock ascension")
	}
	// Re-wearing on another page keeps it ascended, but does not ascend a
	// second, never-ascended tablet.
	a.EquipTablet(1, 0, 453)
	if !a.AwakeTablets()[453] {
		t.Fatal("re-equipped tablet lost its ascension")
	}
}
