package kitabe

import (
	"testing"

	"hoc-server/internal/wire/msgpack"
)

func TestFullGroupsFilledSlotsCarryGlobalPacketIndexes(t *testing.T) {
	equipped := map[[2]int]struct {
		ID      int
		Sockets map[int][2]int
	}{
		{0, 0}: {ID: 464},
		{0, 1}: {ID: 465},
		{0, 2}: {ID: 467},
		{1, 1}: {ID: 469},
	}
	b := FullGroups(2, 3, true, equipped, nil, false, map[int]bool{1: true, 2: true}, nil)
	v, err := msgpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	groups := v.(map[any]any)
	checks := []struct {
		page, slot, want int
	}{{1, 0, 0}, {1, 1, 1}, {1, 2, 2}, {2, 1, 3}}
	for _, c := range checks {
		group := groups[int64(c.page)].([]any)
		slots := group[1].([]any)
		slot := slots[c.slot].([]any)
		if got := slot[5]; got != int64(c.want) {
			t.Fatalf("page=%d slot=%d packet index=%v, want %d", c.page, c.slot, got, c.want)
		}
	}
}
