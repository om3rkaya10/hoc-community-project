package items

import "testing"

func TestCatalogTypes(t *testing.T) {
	cases := map[int]int{
		141: TypeConsumable, 453: TypeTablet, 513: TypeInscription,
		514: TypePack, 562: TypePole, 570: TypePattern, 517: TypeEmblemCurrency,
		595: TypeRuneCurrency, 160: TypeHero,
	}
	for id, want := range cases {
		if got := TypeOf(id); got != want {
			t.Errorf("TypeOf(%d)=%d want %d", id, got, want)
		}
	}
	if TypeOf(-5) != -1 {
		t.Error("unknown id must report -1")
	}
}

func TestPackContents(t *testing.T) {
	it, ok := Lookup(515)
	if !ok || len(it.Pack) != 1 || it.Pack[0] != (PackEntry{EmblemItemID, 5000}) {
		t.Fatalf("emblem pack 5000 contents = %+v", it.Pack)
	}
	it, _ = Lookup(594) // Hero's Haul: potions, tickets, 1000 emblems, inscriptions, 15 banners
	var emblems, banners int
	for _, p := range it.Pack {
		switch p.ID {
		case EmblemItemID:
			emblems = p.Count
		case 578:
			banners = p.Count
		}
	}
	if emblems != 1000 || banners != 15 {
		t.Fatalf("hero's haul emblems=%d banners=%d", emblems, banners)
	}
}

func TestPatternUnlimited(t *testing.T) {
	if !PatternUnlimited(860) { // League Banner - Diamond II
		t.Error("league banner must be unlimited")
	}
	if PatternUnlimited(563) { // Jest Banner - Thumbs Down (consumable)
		t.Error("jest banner must be consumable")
	}
	if PatternUnlimited(562) {
		t.Error("pole is not a pattern")
	}
}

func TestCurrencyAmount(t *testing.T) {
	typ, n, ok := CurrencyAmount(135) // "200 Emblems"
	if !ok || typ != TypeEmblemCurrency || n != 200 {
		t.Fatalf("135 → %d %d %v", typ, n, ok)
	}
	typ, n, ok = CurrencyAmount(517)
	if !ok || typ != TypeEmblemCurrency || n != 1 {
		t.Fatalf("517 → %d %d %v", typ, n, ok)
	}
	if _, _, ok := CurrencyAmount(513); ok {
		t.Fatal("inscription is not currency")
	}
}

func TestIDsOfTypeSortedTablets(t *testing.T) {
	ids := IDsOfType(TypeTablet)
	if len(ids) < 50 || ids[0] != 453 {
		t.Fatalf("tablets = %v", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatal("not sorted")
		}
	}
}
