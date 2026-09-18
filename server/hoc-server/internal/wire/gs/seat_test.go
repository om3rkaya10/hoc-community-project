package gs_test

import (
	"encoding/binary"
	"testing"

	wiregs "hoc-server/internal/wire/gs"
)

func TestSeatRoster1002TwoPlayers(t *testing.T) {
	got := wiregs.SeatRoster1002(2, 1, []wiregs.SeatRosterMember{
		{Seat0: 0, Hero: 346, IsOwner: true},
		{Seat0: 1, Hero: 378, Ready: true},
	})
	if binary.LittleEndian.Uint32(got[:4]) != 2 || got[4] != 2 {
		t.Fatalf("header cid=%d count=%d", binary.LittleEndian.Uint32(got[:4]), got[4])
	}
	first := 5
	second := first + wiregs.PISize
	if len(got) != 4+1+2*wiregs.PISize+1 {
		t.Fatalf("len=%d", len(got))
	}
	if binary.LittleEndian.Uint32(got[first:first+4]) != 1 || binary.LittleEndian.Uint32(got[first+4:first+8]) != 346 {
		t.Fatalf("first PI seat/hero=%d/%d", binary.LittleEndian.Uint32(got[first:first+4]), binary.LittleEndian.Uint32(got[first+4:first+8]))
	}
	if binary.LittleEndian.Uint32(got[first+12:first+16]) != 1 {
		t.Fatal("host owner flag missing")
	}
	if binary.LittleEndian.Uint32(got[second:second+4]) != 2 || binary.LittleEndian.Uint32(got[second+4:second+8]) != 378 {
		t.Fatalf("second PI seat/hero=%d/%d", binary.LittleEndian.Uint32(got[second:second+4]), binary.LittleEndian.Uint32(got[second+4:second+8]))
	}
	if binary.LittleEndian.Uint32(got[second+8:second+12]) != 1 {
		t.Fatal("ready flag missing")
	}
	if got[len(got)-1] != 2 {
		t.Fatalf("local seat=%d", got[len(got)-1])
	}
}

func TestPlayerLeaveRoom3003(t *testing.T) {
	got := wiregs.PlayerLeaveRoom3003(2, 1, "Omer")
	want := []byte{2, 0, 0, 0, 2, 4, 0, 'O', 'm', 'e', 'r'}
	if string(got) != string(want) {
		t.Fatalf("got=%x want=%x", got, want)
	}
}

// skillBody mirrors the client's 0x100C body: cid + UTF(session guid) + UTF(PI guid) +
// UTF(nick) + 139 LE ints (READY, PI+0x40, PI+0x48, spell1, spell2, ...).
func skillBody(sessGUID, piGUID, nick string, ready, spell1, spell2 int) []byte {
	b := wiregs.CIDOnly(7)
	for _, s := range []string{sessGUID, piGUID, nick} {
		b = append(b, wiregs.UTF(s)...)
	}
	ints := make([]int32, 139)
	ints[0] = int32(ready)
	ints[1], ints[2] = 3, 5 // PI+0x40 / PI+0x48
	ints[3], ints[4] = int32(spell1), int32(spell2)
	for i := 5; i < len(ints); i++ {
		ints[i] = int32(i + 9) // small consecutive values (14, 15, ...) like the live bodies
	}
	tmp := make([]byte, 4)
	for _, v := range ints {
		binary.LittleEndian.PutUint32(tmp, uint32(v))
		b = append(b, tmp...)
	}
	return b
}

func TestParseSummonerSpellsFixedLayout(t *testing.T) {
	cases := []struct {
		sess, guid, nick string
		s1, s2           int
	}{
		{"1000277", "", "koshar23", 600, 941},
		{"1000277", "", "hmhm1234", 600, 602},
		{"1000253", "", "enterpries", 597, 600},
		{"1000261", "", "edlonbloper@gmail", 602, 594},
		{"1000307", "ed26f1e7d165", "samsungrtl", 603, 600},
		{"", "", "a", 594, 600},
		{"1000001", "x", "deneme99999", 941, 600},
	}
	for _, c := range cases {
		body := skillBody(c.sess, c.guid, c.nick, 1, c.s1, c.s2)
		if want := 4 + 6 + len(c.sess) + len(c.guid) + len(c.nick) + 139*4; len(body) != want {
			t.Fatalf("%s: body %dB want %d", c.nick, len(body), want)
		}
		s1, s2, ok := wiregs.ParseSummonerSpells(body)
		if !ok || s1 != c.s1 || s2 != c.s2 {
			t.Fatalf("%s: spells=%d/%d ok=%v want %d/%d", c.nick, s1, s2, ok, c.s1, c.s2)
		}
		rdy, ok := wiregs.ParseReadyFromSkill(body)
		if !ok || rdy != 1 {
			t.Fatalf("%s: ready=%d ok=%v", c.nick, rdy, ok)
		}
		rdy, ok = wiregs.ParseReadyFromSkill(skillBody(c.sess, c.guid, c.nick, 0, c.s1, c.s2))
		if !ok || rdy != 0 {
			t.Fatalf("%s: ready(0)=%d ok=%v", c.nick, rdy, ok)
		}
	}
}

func TestParseSummonerSpellsNoSpellsNotLatched(t *testing.T) {
	if _, _, ok := wiregs.ParseSummonerSpells(skillBody("1000277", "", "koshar23", 1, 0, 0)); ok {
		t.Fatal("0/0 spells must not latch")
	}
	if _, _, ok := wiregs.ParseSummonerSpells([]byte{1, 2, 3}); ok {
		t.Fatal("short body must not latch")
	}
}

// Nox 2026-09-19: the client reports level-1 Heal (34) + level-1 Mana Regen
// (593) before the player touches the picker; ids below 100 must parse.
func TestParseSummonerSpellsAcceptsLowIDs(t *testing.T) {
	s1, s2, ok := wiregs.ParseSummonerSpells(skillBody("gllive:x", "gllive:x", "Testab", 0, 34, 593))
	if !ok || s1 != 34 || s2 != 593 {
		t.Fatalf("34/593 parsed as %d/%d ok=%v", s1, s2, ok)
	}
	if _, _, ok := wiregs.ParseSummonerSpells(skillBody("gllive:x", "gllive:x", "Testab", 1, 0, 593)); ok {
		t.Fatal("0/593 accepted as a valid pair")
	}
}
