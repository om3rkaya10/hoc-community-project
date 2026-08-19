package config

import "testing"

// TestGameModeForMapName pins the custom-room map -> GAME_MODE mapping.
//
// Recovered from libAndroid.so:
//
//	EnterGame@0x00BEC43C -> GetGameMode(GSI+4) -> table@0x023FED80
//	mode 0 -> mapInfoId 7   mode 3 -> 6   mode 4 -> 12
//
// and cross-checked against script__core.lua:
//
//	GAME_MODE_DOTA = 0, GAME_MODE_DOTA_3V3 = 3,
//	GAME_MODE_DOTA_UNDERREALMRUINS = 4
//
// VERIFIED LIVE 2026-08-15: all three rooms loaded their correct map and
// played (5V5 reached StartPlay 0x2004 with a clean 30 Hz frame clock).
//
// Regression this guards: GSIGameMode used to be pinned to 4, so every
// custom room loaded Under Realm Ruins regardless of the host's choice.
func TestGameModeForMapName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"3v3 desert", "3V3", GameMode3v3},
		{"classic 5v5", "5V5", GameModeDota5v5},
		{"under realm ruins", "5V5_UR", GameMode5v5Vault},

		// The client has always sent these upper-case, but be forgiving:
		// a lower-case or padded value must not silently fall back to a
		// different map.
		{"lower case", "3v3", GameMode3v3},
		{"padded", "  5V5_UR  ", GameMode5v5Vault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GameModeForMapName(tc.in); got != tc.want {
				t.Fatalf("GameModeForMapName(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestGameModeForMapNameUnknown makes sure an unrecognised or missing map
// name degrades to the historical constant instead of returning something
// arbitrary -- a future map we have not decoded must not break room create.
func TestGameModeForMapNameUnknown(t *testing.T) {
	for _, in := range []string{"", "7V7", "garbage"} {
		if got := GameModeForMapName(in); got != DefaultCustomMode {
			t.Fatalf("GameModeForMapName(%q) = %d, want DefaultCustomMode(%d)",
				in, got, DefaultCustomMode)
		}
	}
}

// TestMapModeConstantsDistinct guards against a copy-paste collapse of the
// three modes onto one value, which is exactly what the original bug looked
// like from the outside (every room loading the same map).
func TestMapModeConstantsDistinct(t *testing.T) {
	seen := map[int]string{}
	for name, v := range map[string]int{
		"GameModeDota5v5":  GameModeDota5v5,
		"GameMode3v3":      GameMode3v3,
		"GameMode5v5Vault": GameMode5v5Vault,
	} {
		if prev, dup := seen[v]; dup {
			t.Fatalf("%s and %s share GAME_MODE %d", prev, name, v)
		}
		seen[v] = name
	}
}
