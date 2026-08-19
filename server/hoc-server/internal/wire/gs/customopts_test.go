package gs_test

import (
	"encoding/binary"
	"testing"

	"hoc-server/internal/config"
	wiregs "hoc-server/internal/wire/gs"
)

// The custom-room options ride in LoadMap 0x2002 as a 21-byte block that is
// only emitted when gameModeParam == 4 (GAME_MODE_PARAM_CUSTOMIZE).
//
// Layout recovered from the client's own LoadMap debug template in
// libAndroid.so, in this exact order:
//
//	[m_initial_gold:] [m_get_gold:] [m_initial_level:] [m_get_exp:]   int32
//	[m_talent:] [m_inscription:] [m_lasthithint:] [m_revive:]
//	[m_is_kill_streak_reward_reduce:]                                 bool
//
// The block starts after: tskcid(4) + 0x01(1) + seat(4) + seed(4) +
// gameMode(4) + gameModeParam(4) + gamePlayMode(4) + isLeaguePVP(1) = 26.
const optsOffset = 26

func buildLoadMap(t *testing.T, param int, o config.CustomRoomOptions) []byte {
	t.Helper()
	return wiregs.LoadMapShared(2, 1, 42, 0, param, o, []wiregs.LoadMapMember{{
		Seat0: 0, Hero: 11, Skin: 1, Nick: "n", GUID: "g", IsOwner: true,
	}})
}

// TestLoadMapCustomOptionsEncoding pins the field order and endianness. A
// silent regression here means rooms start with the wrong gold/level, which
// is exactly the bug this block was added to fix.
func TestLoadMapCustomOptionsEncoding(t *testing.T) {
	o := config.CustomRoomOptions{
		InitialGold: 5000, GetGold: -1, InitialLevel: 3, GetExp: 7,
		Talent: true, Inscription: false, LastHitHint: true,
		Revive: false, KillStreakReduce: true,
	}
	b := buildLoadMap(t, 4, o)

	for i, want := range []int32{5000, -1, 3, 7} {
		got := int32(binary.LittleEndian.Uint32(b[optsOffset+i*4:]))
		if got != want {
			t.Fatalf("int field %d = %d, want %d", i, got, want)
		}
	}
	bools := b[optsOffset+16 : optsOffset+21]
	for i, want := range []byte{1, 0, 1, 0, 1} {
		if bools[i] != want {
			t.Fatalf("bool field %d = %d, want %d", i, bools[i], want)
		}
	}
}

// TestLoadMapDefaultsKeepMapValues guards the -1 sentinel: Lua checks
// `if initgold ~= -1`, so a default room must send -1, not 0. Sending 0
// would mean "start with 0 gold / no passive income" instead of "use the
// map's own defaults".
func TestLoadMapDefaultsKeepMapValues(t *testing.T) {
	b := buildLoadMap(t, 4, config.DefaultCustomRoomOptions())
	for i := 0; i < 4; i++ {
		got := int32(binary.LittleEndian.Uint32(b[optsOffset+i*4:]))
		if got != -1 {
			t.Fatalf("default int field %d = %d, want -1", i, got)
		}
	}
}

// TestLoadMapOptionsOnlyWhenCustomize pins the gating: the block exists only
// for gameModeParam == 4. Emitting it unconditionally would shift the whole
// PI array and desync the client's parser.
func TestLoadMapOptionsOnlyWhenCustomize(t *testing.T) {
	o := config.DefaultCustomRoomOptions()
	withBlock := buildLoadMap(t, 4, o)
	noBlock := buildLoadMap(t, 0, o)
	if len(withBlock)-len(noBlock) != 21 {
		t.Fatalf("custom block size = %d, want 21",
			len(withBlock)-len(noBlock))
	}
}
