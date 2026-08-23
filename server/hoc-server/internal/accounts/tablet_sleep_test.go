package accounts

import "testing"

func TestSleepTabletWithEmblemIsAtomicAndIdempotent(t *testing.T) {
	a := &Account{Emblem: 2000, AwakeTabletIDs: []int{464}}
	if !a.SleepTabletWithEmblem(464, 750) {
		t.Fatal("first sleep rejected sufficient balance")
	}
	if a.Emblem != 1250 {
		t.Fatalf("emblem=%d, want 1250", a.Emblem)
	}
	for _, id := range a.AwakeTabletIDs {
		if id == 464 {
			t.Fatal("tablet remained awake")
		}
	}
	if !a.SleepTabletWithEmblem(464, 750) {
		t.Fatal("idempotent repeated sleep was rejected")
	}
	if a.Emblem != 1250 {
		t.Fatalf("repeated sleep charged again: emblem=%d", a.Emblem)
	}
}

func TestSleepTabletWithEmblemRejectsInsufficientBalance(t *testing.T) {
	a := &Account{Emblem: 749, AwakeTabletIDs: []int{464}}
	if a.SleepTabletWithEmblem(464, 750) {
		t.Fatal("sleep succeeded with insufficient emblem")
	}
	if a.Emblem != 749 || len(a.AwakeTabletIDs) != 1 || a.AwakeTabletIDs[0] != 464 {
		t.Fatalf("failed sleep mutated state: emblem=%d awake=%v", a.Emblem, a.AwakeTabletIDs)
	}
}
