package accounts

import "testing"

func TestEquipTabletMovesSameTabletInsteadOfDuplicating(t *testing.T) {
	a := &Account{Tablets: map[string]TabletRec{
		"0:1": {ID: 465},
	}}
	a.EquipTablet(1, 2, 465)
	if _, ok := a.Tablets["0:1"]; ok {
		t.Fatalf("old slot retained duplicate tablet: %#v", a.Tablets)
	}
	if got := a.Tablets["1:2"].ID; got != 465 {
		t.Fatalf("new slot tablet=%d, want 465", got)
	}
}

func TestNormalizeDuplicateTabletsKeepsLowestSlot(t *testing.T) {
	a := &Account{Tablets: map[string]TabletRec{
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
}
