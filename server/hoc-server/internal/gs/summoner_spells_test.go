package gs

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

// skillAckBody mirrors the client's 0x100C layout (cid, three UTFs, 139 LE
// ints starting with READY, PI+0x40, PI+0x48, spell1, spell2).
func skillAckBody(ready, spell1, spell2 int) []byte {
	b := wiregs.CIDOnly(2)
	for _, s := range []string{"gllive:enterpries1", "gllive:enterpries1", "Enterpries"} {
		b = append(b, wiregs.UTF(s)...)
	}
	ints := make([]int32, 139)
	ints[0] = int32(ready)
	ints[1], ints[2] = 3, 5
	ints[3], ints[4] = int32(spell1), int32(spell2)
	for i := 5; i < len(ints); i++ {
		ints[i] = int32(i + 9)
	}
	tmp := make([]byte, 4)
	for _, v := range ints {
		binary.LittleEndian.PutUint32(tmp, uint32(v))
		b = append(b, tmp...)
	}
	return b
}

func loadGSAccount(t *testing.T, record map[string]any) *accounts.Account {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	b, err := json.Marshal(map[string]any{"accounts": map[string]any{"enterpries1": record}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Load(path); err != nil {
		t.Fatal(err)
	}
	a := accounts.Get("enterpries1")
	if a == nil {
		t.Fatal("test account missing")
	}
	return a
}

// LIVE 2026-09-18: a fresh client sends 0/0 summoner spells in every SkillAck
// until the player opens the picker; LoadMap then carried 0/0 and the match
// rolled random spells. The server must remember the last real pair per
// account and fall back to it (or the default) when the SkillAck has none.
func TestSkillAckRemembersAndRestoresSummonerSpells(t *testing.T) {
	acc := loadGSAccount(t, map[string]any{"username": "enterpries1", "password": "pw"})
	conn := &captureConn{}
	sess := &session.Session{Username: "enterpries1", GUID: "gllive:enterpries1", SeatHeroID: 333, Account: acc}
	room := &session.Room{Host: sess, Members: []*session.Session{sess}, TskCID: 2, State: "open"}
	sess.Room = room
	sess.AttachGS(conn)
	st := &connState{conn: conn, sess: sess, custom: true, peerReady: true, seq: 10, tskcid: 2}
	setState(conn, st)
	t.Cleanup(func() { delState(conn) })

	// Fresh account, no pair yet, client reports 0/0 → default pair.
	handlePkt(conn, st, 1, wiregs.BuildReply(1, 9, 0x100C, skillAckBody(0, 0, 0), 0x48))
	_ = conn.takeWrites()
	if sess.SeatSpell1 != config.DefaultSummonerSpell1 || sess.SeatSpell2 != config.DefaultSummonerSpell2 {
		t.Fatalf("fresh 0/0 → %d/%d, want default %d/%d", sess.SeatSpell1, sess.SeatSpell2,
			config.DefaultSummonerSpell1, config.DefaultSummonerSpell2)
	}

	// Player picks 598/600 → applied and persisted.
	handlePkt(conn, st, 2, wiregs.BuildReply(2, 9, 0x100C, skillAckBody(0, 598, 600), 0x48))
	_ = conn.takeWrites()
	if sess.SeatSpell1 != 598 || sess.SeatSpell2 != 600 {
		t.Fatalf("picked pair not applied: %d/%d", sess.SeatSpell1, sess.SeatSpell2)
	}
	if s1, s2, ok := acc.SummonerSpellPair(); !ok || s1 != 598 || s2 != 600 {
		t.Fatalf("pair not remembered on the account: %d/%d ok=%v", s1, s2, ok)
	}

	// Next session (seat selection reset, e.g. after relogin): client sends
	// 0/0 again → the remembered pair, and LoadMap carries it.
	sess.SeatSpell1, sess.SeatSpell2 = 0, 0
	handlePkt(conn, st, 3, wiregs.BuildReply(3, 9, 0x100C, skillAckBody(1, 0, 0), 0x48))
	_ = conn.takeWrites()
	if sess.SeatSpell1 != 598 || sess.SeatSpell2 != 600 {
		t.Fatalf("remembered pair not restored: %d/%d", sess.SeatSpell1, sess.SeatSpell2)
	}
	handlePkt(conn, st, 4, wiregs.BuildReply(4, 9, 0x2001, nil, 0x48))
	writes := conn.takeWrites()
	i := gsSubIndex(writes, 0x2002)
	if i < 0 {
		t.Fatalf("no LoadMap: %x", writes)
	}
	if !containsBytes(writes[i], spellPairBytes(598, 600)) {
		t.Fatalf("LoadMap does not carry 598/600: %x", writes[i])
	}
}

func spellPairBytes(s1, s2 int) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], uint32(s1))
	binary.LittleEndian.PutUint32(b[4:8], uint32(s2))
	return b
}

func containsBytes(hay, needle []byte) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		match := true
		for j := range needle {
			if hay[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
