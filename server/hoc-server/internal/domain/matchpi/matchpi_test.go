package matchpi

import (
	"encoding/binary"
	"testing"

	"hoc-server/internal/accounts"
	wiregs "hoc-server/internal/wire/gs"
)

func TestExtraFromAccount(t *testing.T) {
	a := &accounts.Account{
		Tablets: map[string]accounts.TabletRec{
			"0:0": {ID: 453, Sockets: map[string][]int{"0": {447, 1}, "1": {480, 1}}},
			"0:1": {ID: 464},
		},
		AwakeTabletIDs: []int{453},
		Patterns:       map[string]int{"573": 7},
		FlagPole:       568, FlagPattern: 573, FlagType: 0x11,
		TalentPoints: 40,
		Talents: map[string]accounts.TalentGroupRec{
			"1": {Talents: [][]int{{1001, 1, 0}}},
			"3": {Talents: [][]int{{3001, 3, 0}, {3002, 2, 0}}},
		},
	}
	e := Extra(a)
	// 453 -> 1125, 447 -> 1178, 480 -> 1162 (proto+0x28); 464 is not awake.
	want := []int32{1125, 1178, 1162}
	if len(e.AwakeWire) != len(want) {
		t.Fatalf("awake=%v", e.AwakeWire)
	}
	for i := range want {
		if e.AwakeWire[i] != want[i] {
			t.Fatalf("awake=%v want %v", e.AwakeWire, want)
		}
	}
	if e.TalentPage != 3 || len(e.Talents) != 2 {
		t.Fatalf("talent page=%d pairs=%v", e.TalentPage, e.Talents)
	}
	if e.Pole != 568 || e.Pattern != 573 || e.PatternUses != 7 {
		t.Fatalf("banner=%d/%d x%d", e.Pole, e.Pattern, e.PatternUses)
	}
}

func TestBannerDefaults(t *testing.T) {
	pole, pattern, uses := Banner(&accounts.Account{})
	if pole != 562 || pattern != 572 || uses != 0 {
		t.Fatalf("defaults=%d/%d x%d", pole, pattern, uses)
	}
}

// Flat layout: 10 ints, 4 UTF (empty), then ints[10..]. PI+0x358 = ints[107],
// pole/pattern/uses = ints[127..129], talent page = ints[8].
func TestEncodePIExtraFlatIndexes(t *testing.T) {
	e := &wiregs.PIExtra{
		AwakeWire: []int32{1125, 1178}, TalentPage: 3,
		Talents: [][2]int32{{3001, 3}},
		Pole:    568, Pattern: 573, PatternUses: 7,
	}
	pi := wiregs.EncodePIExtra(131, 0, 1, 2, 0, true, true, true, "", "", false, e)
	if len(pi) != wiregs.PISize {
		t.Fatalf("len=%d want %d", len(pi), wiregs.PISize)
	}
	at := func(flat int) int32 {
		off := flat * 4
		if flat >= 10 {
			off += 8 // four empty UTF (u16 len each)
		}
		return int32(binary.LittleEndian.Uint32(pi[off:]))
	}
	if at(8) != 3 || at(107) != 1125 || at(108) != 1178 || at(127) != 568 || at(128) != 573 || at(129) != 7 {
		t.Fatalf("page=%d awake=%d,%d banner=%d/%d/%d", at(8), at(107), at(108), at(127), at(128), at(129))
	}
	if at(13) != 3001 || at(53) != 3 {
		t.Fatalf("talent slot1 id=%d rank=%d", at(13), at(53))
	}
}
