package config

import "testing"

func TestCustomRoomOptionsFromJSON(t *testing.T) {
	raw := []byte(`{
		"advanced_options":"True",
		"initial_gold":"5000",
		"get_gold":"-1",
		"initial_level":"3",
		"get_xp":"7",
		"talent":"True",
		"inscription":"False",
		"last_hit_hint":"True",
		"revive":"False",
		"kill_streak_reward":"True"
	}`)
	got := CustomRoomOptionsFromJSON(raw)
	want := (CustomRoomOptions{
		InitialGold: 5000, GetGold: -1, InitialLevel: 3, GetExp: 7,
		Talent: true, LastHitHint: true, KillStreakReduce: true,
	})
	if got != want {
		t.Fatalf("options = %+v, want %+v", got, want)
	}
}

func TestCustomRoomOptionsDefaultsAndClamps(t *testing.T) {
	if got, want := CustomRoomOptionsFromJSON(nil), DefaultCustomRoomOptions(); got != want {
		t.Fatalf("empty options = %+v, want %+v", got, want)
	}

	got := CustomRoomOptionsFromJSON([]byte(`{
		"initial_gold":"99999",
		"get_gold":"99",
		"initial_level":"0",
		"get_xp":"-9"
	}`))
	if got.InitialGold != MaxInitialGold || got.GetGold != MaxGetGold ||
		got.InitialLevel != 1 || got.GetExp != -1 {
		t.Fatalf("clamped options = %+v", got)
	}
}

func TestCustomRoomLevelIsNotEntryRequirement(t *testing.T) {
	got := CustomRoomOptionsFromJSON([]byte(`{"level":"40"}`))
	if got.InitialLevel != -1 {
		t.Fatalf("room entry level leaked into starting level: %+v", got)
	}
}
