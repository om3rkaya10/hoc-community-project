package config

import (
	"encoding/json"
	"strconv"
	"strings"
)

// CustomRoomOptions carries the "advanced room options" a custom-room host
// picks in the UI. The client sends them inside the 0x1014 room JSON at
// create time, and the engine expects them back in the LoadMap 0x2002 body
// (the trailing block that is only present when gameModeParam == 4, i.e.
// GAME_MODE_PARAM_CUSTOMIZE).
//
// Field order below is the wire order, recovered from the client's own
// LoadMap debug template in libAndroid.so:
//
//	[randSeed:] [gameMode:] [gameModeParam:] [gamePlayMode:] [isLeaguePVP:]
//	[m_initial_gold:] [m_get_gold:] [m_initial_level:] [m_get_exp:]
//	[m_talent:] [m_inscription:] [m_lasthithint:] [m_revive:]
//	[m_is_kill_streak_reward_reduce:]
//
// which is 4 x int32 followed by 5 x bool = 21 bytes.
//
// In-match these values surface in Lua as GetCustomInfo():
//
//	result.InitGold / .InitLevel / .GoldPerSec / .ExpPerSec / .KSPunishment
//
// Sentinel: -1 means "leave the map default alone". Lua checks
// `if initgold ~= -1 then ...`, so -1 is NOT the same as 0 -- sending 0 for
// GetGold would mean "no passive income" instead of "use the map's rate".
// The client sends "-1" for the UI's "TANIMLI" (default) setting.
type CustomRoomOptions struct {
	InitialGold  int32
	GetGold      int32 // passive gold per second, -1 = map default
	InitialLevel int32
	GetExp       int32 // passive xp per second, -1 = map default

	Talent      bool
	Inscription bool
	LastHitHint bool
	Revive      bool

	// KillStreakReduce mirrors "kill_streak_reward" from the room JSON.
	// The client's member is m_is_kill_streak_reward_reduce ("reduce the
	// kill-streak reward"), and the UI checkbox is "ÖLDÜRME SERİSİ
	// ÖDÜLÜNÜ AZALT", so the JSON value maps straight through.
	KillStreakReduce bool
}

// Engine-side clamps, taken from Reset_Custom_Elemets() in Map_5V5.lua /
// Map_3V3.lua / Map_5V5_Vault.lua. The scripts clamp anyway; we clamp too so
// the wire never carries a value the client would refuse or wrap.
const (
	MaxInitialGold  = 10000
	MaxInitialLevel = 15
	MaxGetGold      = 10
	MaxGetExp       = 20
)

// DefaultCustomRoomOptions is what a room that specifies nothing gets: keep
// every map default untouched.
func DefaultCustomRoomOptions() CustomRoomOptions {
	return CustomRoomOptions{
		InitialGold:  -1,
		GetGold:      -1,
		InitialLevel: -1,
		GetExp:       -1,
	}
}

// jsonBool accepts the client's "True"/"False" spelling (it sends capitalised
// Python-style strings, not JSON booleans).
func jsonBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

// jsonInt accepts the client's quoted integers ("5000"), returning def when
// the value is absent or unparseable.
func jsonInt(s string, def int32) int32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return def
	}
	return int32(n)
}

func clamp(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// clampOption preserves the protocol's -1 sentinel, normalises every other
// negative value to that sentinel, and clamps real values to the range the
// map scripts accept.
func clampOption(v, lo, hi int32) int32 {
	if v < 0 {
		return -1
	}
	return clamp(v, lo, hi)
}

// CustomRoomOptionsFromJSON parses the 0x1014 room blob.
//
// Live sample (host set "başlangıç altını" to 5000):
//
//	{"advanced_options":"True","creator":"gllive:phone","get_gold":"-1",
//	 "get_xp":"-1","initial_gold":"5000","initial_level":"1",
//	 "inscription":"True","kill_streak_reward":"False",
//	 "last_hit_hint":"True","level":"40","locked":"False","map":"5V5",
//	 "nick_name":"Phone","password":"","revive":"True","talent":"True"}
//
// Note "level" is the room's *entry requirement* (GEREKEN SEVİYE), not the
// starting hero level -- that one is "initial_level". Do not confuse them.
func CustomRoomOptionsFromJSON(raw []byte) CustomRoomOptions {
	opts := DefaultCustomRoomOptions()
	if len(raw) == 0 {
		return opts
	}
	var kv struct {
		Advanced     string `json:"advanced_options"`
		InitialGold  string `json:"initial_gold"`
		GetGold      string `json:"get_gold"`
		InitialLevel string `json:"initial_level"`
		GetExp       string `json:"get_xp"`
		Talent       string `json:"talent"`
		Inscription  string `json:"inscription"`
		LastHitHint  string `json:"last_hit_hint"`
		Revive       string `json:"revive"`
		KillStreak   string `json:"kill_streak_reward"`
	}
	if err := json.Unmarshal(raw, &kv); err != nil {
		return opts
	}

	// The toggles on the room-create screen apply whether or not the host
	// opened the "advanced" dialog, so they are read unconditionally.
	opts.Talent = jsonBool(kv.Talent, false)
	opts.Inscription = jsonBool(kv.Inscription, false)
	opts.LastHitHint = jsonBool(kv.LastHitHint, false)
	opts.Revive = jsonBool(kv.Revive, false)
	opts.KillStreakReduce = jsonBool(kv.KillStreak, false)

	opts.InitialGold = jsonInt(kv.InitialGold, -1)
	opts.GetGold = jsonInt(kv.GetGold, -1)
	opts.InitialLevel = jsonInt(kv.InitialLevel, -1)
	opts.GetExp = jsonInt(kv.GetExp, -1)

	// Preserve -1 (map default), normalise invalid negatives to -1, and clamp
	// real values. Zero is valid for gold/rates; starting level starts at 1.
	opts.InitialGold = clampOption(opts.InitialGold, 0, MaxInitialGold)
	opts.GetGold = clampOption(opts.GetGold, 0, MaxGetGold)
	opts.GetExp = clampOption(opts.GetExp, 0, MaxGetExp)
	if opts.InitialLevel < 0 {
		opts.InitialLevel = -1
	} else {
		opts.InitialLevel = clamp(opts.InitialLevel, 1, MaxInitialLevel)
	}
	return opts
}
