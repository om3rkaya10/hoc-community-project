package accounts

import "testing"

func TestEquipTabletMovesSameCopyInsteadOfDuplicating(t *testing.T) {
	a := &Account{Tablets: map[string]TabletRec{
		"0:1": {ID: 465},
	}}
	uid := a.EquippedTablets()[[2]int{0, 1}].UID
	if uid == 0 {
		t.Fatalf("legacy slot not bound to an instance: %#v", a.Tablets)
	}
	a.EquipTablet(1, 2, uid)
	if _, ok := a.Tablets["0:1"]; ok {
		t.Fatalf("old slot retained the copy: %#v", a.Tablets)
	}
	if got := a.Tablets["1:2"]; got.ID != 465 || got.UID != uid {
		t.Fatalf("new slot=%#v, want uid %d / 465", got, uid)
	}
}

// Two copies of one tablet are independent: each keeps its own sockets and
// ascension and can sit in its own slot.
func TestTwoCopiesOfOneTabletAreIndependent(t *testing.T) {
	a := &Account{Emblem: 10000, OwnedTablets: []int{465}}
	if !a.Purchase(465, 1, PayEmblem, 100) {
		t.Fatal("second copy purchase failed")
	}
	views := a.OwnedTabletViews()
	if len(views) != 2 || views[0].ID != 465 || views[1].ID != 465 || views[0].UID == views[1].UID {
		t.Fatalf("views=%#v", views)
	}
	a.SetBackpackSockets(views[0].UID, map[int][2]int{0: {494, 1}})
	a.SetTabletAwake(views[0].UID, true)
	a.EquipTablet(0, 0, views[0].UID)
	a.EquipTablet(0, 1, views[1].UID)
	views = a.OwnedTabletViews()
	if !views[0].Awake || views[1].Awake || len(views[1].Sockets) != 0 || views[0].Sockets[0][0] != 494 {
		t.Fatalf("copies share state: %#v", views)
	}
	eq := a.EquippedTablets()
	if eq[[2]int{0, 0}].UID != views[0].UID || eq[[2]int{0, 1}].UID != views[1].UID {
		t.Fatalf("equipped=%#v", eq)
	}
	// Deleting the first copy returns its inscription and shifts the second
	// copy to index 0 while keeping its slot.
	a.DeleteTablet(views[0].UID)
	if a.Inscriptions["494"] != 1 {
		t.Fatalf("inscription not returned: %v", a.Inscriptions)
	}
	views = a.OwnedTabletViews()
	if len(views) != 1 || views[0].UID != eq[[2]int{0, 1}].UID || views[0].Index != 0 {
		t.Fatalf("after delete views=%#v", views)
	}
	if _, ok := a.EquippedTablets()[[2]int{0, 0}]; ok {
		t.Fatal("deleted copy still equipped")
	}
	if a.EquippedTablets()[[2]int{0, 1}].Index != 0 {
		t.Fatalf("surviving copy index not updated: %#v", a.EquippedTablets())
	}
}

func TestNormalizeDuplicateTabletsKeepsLowestSlot(t *testing.T) {
	a := &Account{OwnedTablets: []int{465, 467}, Tablets: map[string]TabletRec{
		"1:2": {ID: 465},
		"0:1": {ID: 465},
		"0:2": {ID: 467},
	}}
	if !a.normalizeDuplicateTabletsLocked() {
		t.Fatal("duplicate normalization reported no change")
	}
	if _, ok := a.Tablets["1:2"]; ok {
		t.Fatalf("higher duplicate slot survived: %#v", a.Tablets)
	}
	if a.Tablets["0:1"].ID != 465 || a.Tablets["0:2"].ID != 467 {
		t.Fatalf("normalization damaged canonical slots: %#v", a.Tablets)
	}
	if len(a.TabletInstances) != 2 {
		t.Fatalf("legacy same-id slots must not mint copies: %#v", a.TabletInstances)
	}
}

// Legacy records migrate 1:1 — owned order, sockets and awake land on the
// instances and the equipped slots get the matching copy.
func TestLegacyTabletRecordMigratesToInstances(t *testing.T) {
	a := &Account{
		OwnedTablets:    []int{453, 464, 465},
		BackpackSockets: map[string]map[string][]int{"464": {"1": {494, 1}}},
		AwakeTabletIDs:  []int{464},
		Tablets:         map[string]TabletRec{"0:0": {ID: 464, Sockets: map[string][]int{"1": {494, 1}}}},
	}
	views := a.OwnedTabletViews()
	if len(views) != 3 || views[1].ID != 464 || !views[1].Awake || views[1].Sockets[1][0] != 494 || views[0].Awake {
		t.Fatalf("views=%#v", views)
	}
	eq := a.EquippedTablets()[[2]int{0, 0}]
	if eq.UID != views[1].UID || eq.Index != 1 || !eq.Awake {
		t.Fatalf("equipped=%#v", eq)
	}
	// Mirror fields stay readable by an older server.
	if a.OwnedTablets[1] != 464 || a.AwakeTabletIDs[0] != 464 || a.BackpackSockets["464"]["1"][0] != 494 || a.Tablets["0:0"].UID != eq.UID {
		t.Fatalf("mirror out of sync: owned=%v awake=%v sockets=%v tablets=%#v", a.OwnedTablets, a.AwakeTabletIDs, a.BackpackSockets, a.Tablets)
	}
}
