package kitabe

import (
	"testing"

	"hoc-server/internal/wire/msgpack"
)

func TestEquippedSlotsCarryDistinctPacketIndexes(t *testing.T) {
	equipped := map[[2]int]struct {
		ID      int
		Sockets map[int][2]int
	}{
		{0, 0}: {ID: 464},
		{0, 1}: {ID: 465},
		{0, 2}: {ID: 467},
		{1, 1}: {ID: 469},
	}
	v, err := msgpack.Decode(EquippedSlotsVector(equipped, nil, false))
	if err != nil {
		t.Fatal(err)
	}
	slots := v.([]any)
	if len(slots) != 4 {
		t.Fatalf("slots=%d, want 4", len(slots))
	}
	for i, raw := range slots {
		slot := raw.([]any)
		if got := slot[5]; got != int64(i) {
			t.Fatalf("slot %d packet index=%v, want %d", i, got, i)
		}
	}
}
