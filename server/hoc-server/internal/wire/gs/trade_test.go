package gs_test

import (
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
	if len(top) != 18 {
		t.Fatalf("top len=%d", len(top))
	}
	if top[2] != int64(11) || top[3] != int64(22) || top[4] != int64(33) {
		t.Fatalf("wallet=%v/%v/%v", top[2], top[3], top[4])
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
