package kitabe

import (
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/wire/msgpack"
)

// The client links equipped cards to the backpack through TabletSlot[4][0] =
// index in the owned TabletInfo vector (DlgTabletPage::RefreshTabletButtonGroup
// @0x122a5fc compares getIndexIDInPacket() with the backpack loop index).

func testEquipped() map[[2]int]accounts.EquippedTablet {
	// owned vector: [453, 464, 465, 467, 468, 469]
	return map[[2]int]accounts.EquippedTablet{
		{0, 0}: {UID: 2, ID: 464, Index: 1},
		{0, 1}: {UID: 3, ID: 465, Index: 2},
		{0, 2}: {UID: 4, ID: 467, Index: 3},
		{1, 1}: {UID: 6, ID: 469, Index: 5},
	}
}

func TestEquippedSlotsCarryOwnedIndexes(t *testing.T) {
	v, err := msgpack.Decode(EquippedSlotsVector(testEquipped()))
	if err != nil {
		t.Fatal(err)
	}
	slots := v.([]any)
	want := []int64{1, 2, 3, 5}
	if len(slots) != len(want) {
		t.Fatalf("slots=%d, want %d", len(slots), len(want))
	}
	for i, raw := range slots {
		slot := raw.([]any)
		if got := slot[4].([]any)[0]; got != want[i] || slot[5] != want[i] {
			t.Fatalf("slot %d owned index [4][0]=%v [5]=%v, want %d", i, got, slot[5], want[i])
		}
	}
}

func TestFullGroupsFilledSlotsCarryOwnedIndexes(t *testing.T) {
	b := FullGroups(2, 3, true, testEquipped(), map[int]bool{1: true, 2: true}, nil)
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	groups := v.(map[any]any)
	checks := []struct {
		page, slot, want int
	}{{1, 0, 1}, {1, 1, 2}, {1, 2, 3}, {2, 1, 5}}
	for _, c := range checks {
		group := groups[int64(c.page)].([]any)
		slots := group[1].([]any)
		slot := slots[c.slot].([]any)
		if got := slot[4].([]any)[0]; got != int64(c.want) {
			t.Fatalf("page=%d slot=%d owned index [4][0]=%v, want %d", c.page, c.slot, got, c.want)
		}
	}
}

func TestEquippedTabletMissingFromBackpackUsesMinusOne(t *testing.T) {
	eq := map[[2]int]accounts.EquippedTablet{{0, 0}: {UID: 9, ID: 464, Index: -1}}
	v, err := msgpack.Decode(EquippedSlotsVector(eq))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range v.([]any) {
		if got := raw.([]any)[4].([]any)[0]; got != int64(-1) {
			t.Fatalf("owned index=%v, want -1", got)
		}
	}
}

// Two copies of one tablet are two entries with their own sockets and
// ascension; the equipped card points at the exact copy by index.
func TestOwnedTabletsVectorKeepsOrderAndCopies(t *testing.T) {
	owned := []accounts.TabletView{
		{UID: 1, ID: 873, Index: 0},
		{UID: 2, ID: 453, Index: 1, Awake: true, Sockets: map[int][2]int{0: {494, 1}}},
		{UID: 3, ID: 453, Index: 2},
	}
	v, err := msgpack.Decode(OwnedTabletsVector(owned))
	if err != nil {
		t.Fatal(err)
	}
	list := v.([]any)
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	for i, want := range owned {
		info := list[i].([]any)
		if info[0] != int64(want.ID) {
			t.Fatalf("index %d id=%v want %d", i, info[0], want.ID)
		}
		wantWake := int64(0)
		if want.Awake {
			wantWake = 2
		}
		if info[8] != wantWake {
			t.Fatalf("index %d wake=%v want %d", i, info[8], wantWake)
		}
		filled := info[1].(map[any]any)[int64(0)].([]any)[0].(bool)
		if filled != (len(want.Sockets) > 0) {
			t.Fatalf("index %d socket 0 filled=%v", i, filled)
		}
	}
	eq := map[[2]int]accounts.EquippedTablet{{0, 0}: {UID: 3, ID: 453, Index: 2}}
	s, _ := msgpack.Decode(EquippedSlotsVector(eq))
	if got := s.([]any)[0].([]any)[4].([]any)[0]; got != int64(2) {
		t.Fatalf("equipped second copy index=%v, want 2", got)
	}
}
