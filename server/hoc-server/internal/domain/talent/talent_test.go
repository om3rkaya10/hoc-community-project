package talent

import (
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/wire/msgpack"
)

func TestGroupInfoPreservesZeroRemaining(t *testing.T) {
	body := GroupInfo(accounts.TalentGroupRec{
		Echo: 0, Unlocked: true, Limit: 0,
		F14: 0, F18: 0, F20: 0,
	})
	v, err := msgpack.Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	arr := v.([]any)
	for _, index := range []int{0, 3, 4, 5, 6} {
		if got := arr[index]; got != int64(0) {
			t.Fatalf("field[%d]=%v, want zero remaining preserved", index, got)
		}
	}
}
