package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/domain/items"
)

func loadStore(t *testing.T, records map[string]any) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	b, err := json.Marshal(map[string]any{"accounts": records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
}

// Pre-v1 builds stored every 0x6e purchase in the inscription map. The
// migration hands out what those ids really were (VPS 2026-09-13 snapshot:
// 515 ×39 emblem packs, 563 ×100 jest banners, 568 poles, 594 bundles ...).
func TestMigrationReroutesPollutedInscriptions(t *testing.T) {
	loadStore(t, map[string]any{
		"legacy": map[string]any{
			"username": "legacy", "password": "pw", "emblem": 1000, "rune": 50,
			"revival_runes": 42,
			"inscriptions": map[string]int{
				"513": 3,  // real inscription stays
				"515": 2,  // Emblem pack (5000) ×2 → +10000 emblems
				"563": 10, // Jest Banner - Thumbs Down ×10
				"568": 1,  // Chieftain's Flagstaff
				"860": 1,  // League Banner - Diamond II (unlimited)
				"594": 1,  // Hero's Haul: potions, tickets, 1000 emblems, 2 inscriptions, 15 banners
				"146": 1,  // Seeker's Insight (consumable)
			},
			"tablets": map[string]any{"0:0": map[string]any{"id": 464}},
		},
	})
	a := Get("legacy")
	if a == nil {
		t.Fatal("account missing")
	}
	if a.Inscriptions["515"] != 0 || a.Inscriptions["563"] != 0 || a.Inscriptions["594"] != 0 {
		t.Fatalf("pollution survived: %v", a.Inscriptions)
	}
	if a.Inscriptions["513"] != 3 || a.Inscriptions["480"] != 1 || a.Inscriptions["482"] != 1 {
		t.Fatalf("inscriptions=%v", a.Inscriptions)
	}
	if a.Emblem != 1000+10000+1000 {
		t.Fatalf("emblem=%d", a.Emblem)
	}
	if a.Patterns["563"] != 10 || a.Patterns["860"] != 1 || a.Patterns["578"] != 15 {
		t.Fatalf("patterns=%v", a.Patterns)
	}
	if a.Poles["568"] != 1 {
		t.Fatalf("poles=%v", a.Poles)
	}
	if a.Items["146"] != 1+3 || a.Items["518"] != 2 || a.Items["141"] != 42 {
		t.Fatalf("items=%v", a.Items)
	}
	if a.InventoryVersion != 1 {
		t.Fatalf("version=%d", a.InventoryVersion)
	}
	grant := items.GrantableTabletIDs()
	if len(a.OwnedTablets) != len(grant) || a.OwnedTablets[1] != 464 {
		t.Fatalf("owned tablets=%v", a.OwnedTablets)
	}
}

func TestMigrationKeepsEquippedTabletOwnedAndIsIdempotent(t *testing.T) {
	loadStore(t, map[string]any{
		"p": map[string]any{
			"username": "p", "password": "pw",
			"inventory_version": 1,
			"owned_tablets":     []int{453},
			"inscriptions":      map[string]int{"515": 1}, // must NOT be re-routed once versioned
			"tablets":           map[string]any{"0:0": map[string]any{"id": 600}},
		},
	})
	a := Get("p")
	if len(a.OwnedTablets) != 2 || a.OwnedTablets[1] != 600 {
		t.Fatalf("owned=%v", a.OwnedTablets)
	}
	if a.Inscriptions["515"] != 1 || a.Emblem != 0 {
		t.Fatal("versioned account was migrated twice")
	}
}

func TestPurchaseRoutesByType(t *testing.T) {
	a := &Account{Emblem: 10000, Rune: 1000, Gems: 100}
	a.migrateInventoryLocked()
	if !a.Purchase(513, 2, PayEmblem, 1350) || a.Emblem != 10000-1350 || a.Inscriptions["513"] != 2 {
		t.Fatalf("inscription purchase: emblem=%d insc=%v", a.Emblem, a.Inscriptions)
	}
	if !a.Purchase(514, 1, PayRune, 65) || a.Rune != 935 || a.Emblem != 10000-1350+500 {
		t.Fatalf("emblem pack: rune=%d emblem=%d", a.Rune, a.Emblem)
	}
	if !a.Purchase(141, 5, PayEmblem, 10) || a.Items["141"] != 99+5 {
		t.Fatalf("consumable: items=%v", a.Items)
	}
	// Every grant-list tablet is already owned: no charge, no duplicate.
	before := a.Emblem
	if a.Purchase(453, 1, PayEmblem, 500) || a.Emblem != before {
		t.Fatal("owned tablet must not be charged")
	}
	a.DeleteTablet(453)
	if a.OwnedTabletIndex(453) != -1 {
		t.Fatal("delete did not remove ownership")
	}
	if !a.Purchase(453, 1, PayEmblem, 500) || a.OwnedTabletIndex(453) != len(a.OwnedTablets)-1 {
		t.Fatalf("re-buy: owned=%v", a.OwnedTablets)
	}
	if a.Purchase(238, 1, PayEmblem, 100) { // skin: unsupported → not charged
		t.Fatal("unsupported item was charged")
	}
}
