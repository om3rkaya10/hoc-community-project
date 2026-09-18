package trade

import (
	"testing"

	"hoc-server/internal/session"
	"hoc-server/internal/wire/msgpack"
)

// Two copies of one tablet are addressed by their owned-vector index: wear,
// fill/ascend and delete must act on the copy the client tapped, never on
// "the tablet with that id".
func TestTabletCopiesAreAddressedByOwnedIndex(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 5000,
		"owned_tablets": []int{464, 464, 453},
		"inscriptions":  map[string]int{"494": 8},
	})
	send := func(sub uint16, body []byte) []byte {
		var reply []byte
		handleKitabeFamily(&Ctx{
			Sess: &session.Session{Account: a}, Body: body, Sub: sub,
			Send: func(_ uint16, b []byte) { reply = b },
		})
		return reply
	}
	// Wear copy #2 (index 1) on page 1 slot 0 and copy #1 (index 0) on slot 1.
	wear := func(slot, idx, page1 int) []byte {
		return tradeArray(
			msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(int64(slot)),
			msgpack.Int(int64(idx)), msgpack.Int(6), msgpack.Int(int64(page1)),
		)
	}
	send(0x4b, wear(0, 1, 1))
	send(0x4b, wear(1, 0, 1))
	eq := a.EquippedTablets()
	views := a.OwnedTabletViews()
	if eq[[2]int{0, 0}].UID != views[1].UID || eq[[2]int{0, 1}].UID != views[0].UID {
		t.Fatalf("wear bound the wrong copies: eq=%#v views=%#v", eq, views)
	}
	// Ascend copy #2 only (index 1), with four sockets.
	socks := map[int][2]int{0: {494, 1}, 1: {494, 1}, 2: {494, 1}, 3: {494, 1}}
	reply := send(0x4f, fillInscriptionBody(464, 1, socks, 1, 1))
	views = a.OwnedTabletViews()
	if !views[1].Awake || views[0].Awake || len(views[0].Sockets) != 0 || len(views[1].Sockets) != 4 {
		t.Fatalf("fill/ascend hit the wrong copy: %#v", views)
	}
	if got := replyField7(t, reply); got != 1 {
		t.Fatalf("reply[7]=%d", got)
	}
	// The reply's equipped slots point at the right copies and carry their
	// own wake flags.
	top, _ := msgpack.Decode(reply)
	slots := top.([]any)[4].([]any)
	if len(slots) != 2 {
		t.Fatalf("equipped slots=%d", len(slots))
	}
	s0, s1 := slots[0].([]any), slots[1].([]any)
	if s0[4].([]any)[0] != int64(1) || s0[0].([]any)[8] != int64(2) {
		t.Fatalf("slot 0 should be copy index 1 awake: idx=%v wake=%v", s0[4], s0[0].([]any)[8])
	}
	if s1[4].([]any)[0] != int64(0) || s1[0].([]any)[8] != int64(0) {
		t.Fatalf("slot 1 should be copy index 0 asleep: idx=%v wake=%v", s1[4], s1[0].([]any)[8])
	}
	// Delete copy #1 (index 0): copy #2 shifts to index 0, keeps its
	// sockets, ascension and slot; the reply's slot index follows.
	del := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(0),
		msgpack.Int(1), msgpack.Int(6), msgpack.Int(1),
	)
	reply = send(0x53, del)
	views = a.OwnedTabletViews()
	if len(views) != 2 || views[0].ID != 464 || !views[0].Awake || len(views[0].Sockets) != 4 || views[1].ID != 453 {
		t.Fatalf("delete removed the wrong copy: %#v", views)
	}
	eq = a.EquippedTablets()
	if _, ok := eq[[2]int{0, 1}]; ok {
		t.Fatalf("deleted copy still equipped: %#v", eq)
	}
	if eq[[2]int{0, 0}].UID != views[0].UID || eq[[2]int{0, 0}].Index != 0 {
		t.Fatalf("surviving copy not re-indexed: %#v", eq)
	}
	top, _ = msgpack.Decode(reply)
	slots = top.([]any)[4].([]any)
	if len(slots) != 1 || slots[0].([]any)[4].([]any)[0] != int64(0) {
		t.Fatalf("reply slot index after delete: %#v", slots)
	}
	if a.OwnedTabletCount(464) != 1 {
		t.Fatalf("count=%d", a.OwnedTabletCount(464))
	}
}

// Buying a tablet the player already owns adds a second copy and charges.
func TestBuyItemCRMAddsSecondTabletCopy(t *testing.T) {
	a := loadTradeAccount(t, map[string]any{
		"username": "tester", "password": "pw", "emblem": 5000,
		"owned_tablets": []int{478},
	})
	body := tradeArray(
		msgpack.Int(26), msgpack.RawStr([]byte("tester")), msgpack.Int(478),
		msgpack.Int(1), msgpack.Int(5), msgpack.Int(0), msgpack.Int(2000),
	)
	handleBuyItemCRM(&Ctx{
		Sess: &session.Session{Account: a}, Body: body, Sub: 0x6e,
		Send: func(uint16, []byte) {},
	})
	if emblem, _, _ := a.Wallet(); emblem != 3000 {
		t.Fatalf("emblem=%d, want 3000", emblem)
	}
	if a.OwnedTabletCount(478) != 2 {
		t.Fatalf("copies=%d, want 2", a.OwnedTabletCount(478))
	}
}
