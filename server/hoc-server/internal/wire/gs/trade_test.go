package gs_test

import (
	"encoding/base64"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/domain/kitabe"
	"hoc-server/internal/domain/talent"
	wiregs "hoc-server/internal/wire/gs"
	"hoc-server/internal/wire/msgpack"
)

func TestBuyItemEmptyLayout(t *testing.T) {
	b := wiregs.BuildBuyItemEmpty(&accounts.Account{Emblem: 1, Rune: 2, Gems: 3})
	// LoginBuyItemKitabe=true → array16 18 (0xdc); else fixarray 13 (0x9d)
	if b[0] != 0xdc && b[0] != 0x9d {
		t.Fatalf("hdr=%#x", b[0])
	}
	// [1] must be empty fixstr 0xa0 not int — after array header
	off := 1
	if b[0] == 0xdc {
		off = 3
	}
	if b[off+1] != 0xa0 { // after [0] int 0
		t.Fatalf("elem[1] type %#x — must be fixstr (crash class)", b[off+1])
	}
}

func TestBuyItemDualOwnershipAndWallet(t *testing.T) {
	a := &accounts.Account{
		Emblem: 11, Rune: 22, Gems: 33,
		Heroes: []int{131, 158},
		Skins:  map[string][]int{"131": {9001}},
	}
	b := wiregs.BuildBuyItem(a, wiregs.BuyItemOptions{Ownership: true, Kitabe: true})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	top := v.([]any)
	if len(top) != 20 {
		t.Fatalf("top len=%d", len(top))
	}
	// Client wallet path reads [4]=rune, [5]=emblem (see BuildBuyItem comment).
	if top[4] != int64(22) || top[5] != int64(11) {
		t.Fatalf("wallet [4]=rune/[5]=emblem got %v/%v", top[4], top[5])
	}
	if top[2] != int64(11) || top[3] != int64(22) {
		t.Fatalf("legacy [2]/[3]=%v/%v", top[2], top[3])
	}
	owned := top[11].([]any)
	valid := top[12].([]any)
	if len(owned) != 2 || len(valid) != 2 {
		t.Fatalf("dual lengths owned=%d valid=%d", len(owned), len(valid))
	}
	for i := range owned {
		oh := owned[i].([]any)
		vh := valid[i].([]any)
		if oh[0] != vh[0] || oh[4] != int64(4) || vh[4] != int64(4) {
			t.Fatalf("hero[%d] owned=%#v valid=%#v", i, oh, vh)
		}
	}
	if skins := owned[0].([]any)[5].([]any); len(skins) != 1 {
		t.Fatalf("skin count=%d", len(skins))
	}
	if _, ok := top[17].([]any); !ok {
		t.Fatalf("[17] type=%T, want GESub array", top[17])
	}
}

func TestBuyItemCustomLightOwnership(t *testing.T) {
	a := &accounts.Account{Heroes: []int{131, 158}}
	b := wiregs.BuildBuyItem(a, wiregs.BuyItemOptions{Ownership: false, Kitabe: true})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	top := v.([]any)
	if len(top[11].([]any)) != 0 || len(top[12].([]any)) != 0 {
		t.Fatal("custom-light response carried ownership")
	}
}

func TestUserInfoCarriesRevivalRunes(t *testing.T) {
	b := wiregs.BuildUserInfo(&accounts.Account{Items: map[string]int{"141": 99}})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	items := v.([]any)[7].([]any)
	if len(items) != 1 {
		t.Fatalf("GetUserInfo items=%d, want one Revival Rune entry", len(items))
	}
	item := items[0].([]any)
	if len(item) != 7 || item[0] != int64(141) || item[3] != int64(99) {
		t.Fatalf("Revival Rune ItemInfo=%#v, want id=141 quantity=99 at client-consumed field [3]", item)
	}
}

func TestBuyItemReplaysRevivalRunes(t *testing.T) {
	b := wiregs.BuildBuyItem(&accounts.Account{Items: map[string]int{"141": 99}}, wiregs.BuyItemOptions{
		Ownership: true,
		Kitabe:    true,
	})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	items := v.([]any)[13].([]any)
	if len(items) != 1 {
		t.Fatalf("BuyItem items=%d, want one Revival Rune entry", len(items))
	}
	item := items[0].([]any)
	if len(item) != 7 || item[0] != int64(141) || item[3] != int64(99) {
		t.Fatalf("Revival Rune ItemInfo=%#v, want id=141 quantity=99 at client-consumed field [3]", item)
	}
}

func TestUserInfoCarriesTabletCapacity(t *testing.T) {
	b := wiregs.BuildUserInfo(&accounts.Account{TabletPacketSize: 75})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	ints := v.([]any)[3].([]any)
	if got := ints[0x88]; got != int64(75) {
		t.Fatalf("tablet capacity=%v", got)
	}
}

func TestTradeResultAck(t *testing.T) {
	b := wiregs.TradeResultAck(26, 0)
	if b[0] != 0x92 {
		t.Fatalf("want fixarray2 got %#x", b[0])
	}
}

func TestUserInfoHasTalentMap(t *testing.T) {
	b := wiregs.BuildUserInfo(&accounts.Account{Level: 40, Username: "enterpries1", Nickname: "Enterpries1"})
	// 17 elems → array16 (0xdc) or fixarray
	if b[0] != 0xdc && b[0] != 0xa1 {
		t.Fatalf("top hdr=%#x", b[0])
	}
	if len(b) < 100 {
		t.Fatalf("too short %d", len(b))
	}
}

func TestUserInfoAgeGateInts(t *testing.T) {
	b := wiregs.BuildUserInfo(&accounts.Account{
		Level: 40, Age: 23, Gender: 2, IsSavedAge: 1,
		Birthdate: "2003-07-25 01:59:14Z", GenderStr: "female",
	})
	if len(b) < 50 {
		t.Fatalf("short %d", len(b))
	}
	// Smoke: InputAge ack shape
	ack := wiregs.InputAgeAck(23, 2)
	if ack[0] != 0x92 {
		t.Fatalf("ack hdr %#x", ack[0])
	}
}

func TestKitabeUnlockShape(t *testing.T) {
	b := kitabe.UnlockResponse(&accounts.Account{
		Emblem: 100, Rune: 50,
		Inscriptions: map[string]int{"513": 1},
	})
	if b[0] != 0x9b { // fixarray 11
		t.Fatalf("hdr=%#x want 0x9b", b[0])
	}
	if len(b) < 200 {
		t.Fatalf("kitabe body too small %d", len(b))
	}
}

func TestTalentMapNonEmpty(t *testing.T) {
	m := talent.MapFromAccount(nil)
	if m[0]&0xf0 != 0x80 && m[0] != 0xde {
		t.Fatalf("not a map hdr %#x", m[0])
	}
	// 7 pages → fixmap 7 = 0x87
	if m[0] != 0x87 {
		t.Fatalf("want 7-entry map got %#x", m[0])
	}
}

func TestLoadMapSharedLocalSeat(t *testing.T) {
	b := wiregs.LoadMapShared(2, 6, 42, 4, 4, config.DefaultCustomRoomOptions(), []wiregs.LoadMapMember{
		{Seat0: 0, Hero: 100, IsOwner: true, Nick: "h", GUID: "gllive:h"},
		{Seat0: 5, Hero: 200, Nick: "g", GUID: "gllive:g"},
	})
	if binaryLE32(b[0:4]) != 2 {
		t.Fatal("tskcid")
	}
	if b[4] != 0x01 {
		t.Fatal("using_decode")
	}
	if binaryLE32(b[5:9]) != 6 {
		t.Fatalf("local seat=%d", binaryLE32(b[5:9]))
	}
	if binaryLE32(b[9:13]) != 42 {
		t.Fatal("seed")
	}
}

func binaryLE32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

func TestUserInfoLoginInjectRuneDelta(t *testing.T) {
	a := &accounts.Account{Emblem: 11, Rune: 22, Gems: 33}
	dec := func(b []byte) []any {
		v, err := msgpack.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		return v.([]any)[3].([]any)
	}
	if iv := dec(wiregs.BuildUserInfo(a)); iv[2] != int64(22) {
		t.Fatalf("plain iv[2]=%v", iv[2])
	}
	if iv := dec(wiregs.BuildUserInfoLoginInject(a)); iv[2] != int64(21) {
		t.Fatalf("inject iv[2]=%v want 21", iv[2])
	}
	if iv := dec(wiregs.BuildUserInfoLoginInject(&accounts.Account{})); iv[2] != int64(0) {
		t.Fatalf("zero rune must not go negative: %v", iv[2])
	}
}

func TestBuyItemCarriesFlagOwnership(t *testing.T) {
	a := &accounts.Account{
		Poles: map[string]int{"568": 1}, Patterns: map[string]int{"563": 4, "860": 1},
		FlagPole: 568, FlagPattern: 860, FlagType: 0x11,
	}
	b := wiregs.BuildBuyItem(a, wiregs.BuyItemOptions{Ownership: true, Kitabe: true})
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	top := v.([]any)
	if top[18] != int64(0) {
		t.Fatalf("[18]=%v", top[18])
	}
	ge := top[19].([]any)
	if len(ge) != 6 {
		t.Fatalf("GESub5Member18 len=%d", len(ge))
	}
	poles := ge[1].(map[any]any)
	pole := poles[int64(568)].([]any)
	if len(pole) != 7 || pole[0] != int64(568) || pole[2] != int64(1) {
		t.Fatalf("HocPole=%#v", pole)
	}
	flags := ge[2].(map[any]any)
	if flags[int64(563)].([]any)[2] != int64(4) {
		t.Fatalf("HocFlag 563=%#v", flags[int64(563)])
	}
	if ge[3] != int64(568) || ge[4] != int64(860) || ge[5] != int64(0x11) {
		t.Fatalf("current pole/pattern/type=%v/%v/%v", ge[3], ge[4], ge[5])
	}
}

func TestUserInfoCarriesFlagBlobAndSleepPrices(t *testing.T) {
	a := &accounts.Account{Poles: map[string]int{"568": 1}}
	b := wiregs.BuildUserInfo(a)
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	top := v.([]any)
	iv := top[3].([]any)
	if iv[0x4f] != int64(config.KitabeSleepEmblem) || iv[0x50] != int64(config.KitabeSleepRune) {
		t.Fatalf("sleep prices iv[0x4f]=%v iv[0x50]=%v", iv[0x4f], iv[0x50])
	}
	strs := top[16].([]any)
	if len(strs) < 0x13 {
		t.Fatalf("strvec len=%d", len(strs))
	}
	blob := strs[0x12].(string)
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatalf("strvec[0x12] not base64: %v", err)
	}
	ge, err := msgpack.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if poles := ge.([]any)[1].(map[any]any); poles[int64(568)] == nil {
		t.Fatal("pole 568 missing from blob")
	}
}
