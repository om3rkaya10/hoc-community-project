package kitabe

import (
	"testing"

	"hoc-server/internal/wire/msgpack"
)

// The client links equipped cards to the backpack through TabletSlot[5] =
// index in the owned TabletInfo vector (DlgTabletPage::RefreshTabletButtonGroup
// @0x122a5fc compares getIndexIDInPacket() with the backpack loop index).

func testEquipped() map[[2]int]struct {
	ID      int
	Sockets map[int][2]int
} {
	return map[[2]int]struct {
		ID      int
		Sockets map[int][2]int
	}{
		{0, 0}: {ID: 464},
		{0, 1}: {ID: 465},
		{0, 2}: {ID: 467},
		{1, 1}: {ID: 469},
	}
}

func TestEquippedSlotsCarryOwnedIndexes(t *testing.T) {
	owned := []int{453, 464, 465, 467, 468, 469}
	v, err := msgpack.Decode(EquippedSlotsVector(testEquipped(), nil, false, OwnedIndexMap(owned)))
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
	owned := []int{453, 464, 465, 467, 468, 469}
	b := FullGroups(2, 3, true, testEquipped(), nil, false, map[int]bool{1: true, 2: true}, nil, OwnedIndexMap(owned))
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
	v, err := msgpack.Decode(EquippedSlotsVector(testEquipped(), nil, false, OwnedIndexMap([]int{453})))
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range v.([]any) {
		if got := raw.([]any)[4].([]any)[0]; got != int64(-1) {
			t.Fatalf("owned index=%v, want -1", got)
		}
	}
}

func TestOwnedTabletsVectorKeepsOrder(t *testing.T) {
	owned := []int{873, 453, 600}
	v, err := msgpack.Decode(OwnedTabletsVector(owned, nil, map[int]bool{453: true}))
	if err != nil {
		t.Fatal(err)
	}
	list := v.([]any)
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	for i, id := range owned {
		info := list[i].([]any)
		if info[0] != int64(id) {
			t.Fatalf("index %d id=%v want %d", i, info[0], id)
		}
		wantWake := int64(0)
		if id == 453 {
			wantWake = 2
		}
		if info[8] != wantWake {
			t.Fatalf("id %d wake=%v want %d", id, info[8], wantWake)
		}
	}
}
