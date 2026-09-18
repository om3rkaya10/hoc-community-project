package accounts

import "testing"

func awakeUID(t *testing.T, a *Account, tabletID int) int {
	t.Helper()
	for _, v := range a.OwnedTabletViews() {
		if v.ID == tabletID {
			return v.UID
		}
	}
	t.Fatalf("tablet %d not owned", tabletID)
	return 0
}

func TestSleepTabletWithEmblemIsAtomicAndIdempotent(t *testing.T) {
	a := &Account{Emblem: 2000, OwnedTablets: []int{464}, AwakeTabletIDs: []int{464}}
	uid := awakeUID(t, a, 464)
	if !a.SleepTabletWithEmblem(uid, 750) {
		t.Fatal("first sleep rejected sufficient balance")
	}
	if a.Emblem != 1250 {
		t.Fatalf("emblem=%d, want 1250", a.Emblem)
	}
	if a.AwakeTablets()[464] {
		t.Fatal("tablet remained awake")
	}
	if !a.SleepTabletWithEmblem(uid, 750) {
		t.Fatal("idempotent repeated sleep was rejected")
	}
	if a.Emblem != 1250 {
		t.Fatalf("repeated sleep charged again: emblem=%d", a.Emblem)
	}
}

func TestSleepTabletWithEmblemRejectsInsufficientBalance(t *testing.T) {
	a := &Account{Emblem: 749, OwnedTablets: []int{464}, AwakeTabletIDs: []int{464}}
	uid := awakeUID(t, a, 464)
	if a.SleepTabletWithEmblem(uid, 750) {
		t.Fatal("sleep succeeded with insufficient emblem")
	}
	if a.Emblem != 749 || !a.AwakeTablets()[464] {
		t.Fatalf("failed sleep mutated state: emblem=%d awake=%v", a.Emblem, a.AwakeTablets())
	}
}
