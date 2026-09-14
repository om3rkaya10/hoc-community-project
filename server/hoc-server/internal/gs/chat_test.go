package gs

import (
	"bytes"
	"encoding/binary"
	"testing"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

type chatFixture struct {
	room               *session.Room
	host, guest, enemy *session.Session
	hostConn           *captureConn
	guestConn          *captureConn
	enemyConn          *captureConn
	hostState          *connState
}

func newChatFixture(t *testing.T) *chatFixture {
	t.Helper()
	f := &chatFixture{hostConn: &captureConn{}, guestConn: &captureConn{}, enemyConn: &captureConn{}}
	f.host = &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	f.guest = &session.Session{
		Username: "enterpries2", GUID: "gllive:enterpries2", Seat: 1,
		Account: &accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"},
	}
	f.enemy = &session.Session{
		Username: "enterpries3", GUID: "gllive:enterpries3", Seat: 5,
		Account: &accounts.Account{Username: "enterpries3", Nickname: "Enterpries3"},
	}
	f.room = &session.Room{Host: f.host, Members: []*session.Session{f.host, f.guest, f.enemy}, TskCID: 2}
	f.host.Room, f.guest.Room, f.enemy.Room = f.room, f.room, f.room
	f.host.AttachGS(f.hostConn)
	f.guest.AttachGS(f.guestConn)
	f.enemy.AttachGS(f.enemyConn)
	f.hostState = &connState{conn: f.hostConn, sess: f.host, custom: true, peerReady: true, seq: 10}
	setState(f.hostConn, f.hostState)
	setState(f.guestConn, &connState{conn: f.guestConn, sess: f.guest, custom: true, peerReady: true, seq: 20})
	setState(f.enemyConn, &connState{conn: f.enemyConn, sess: f.enemy, custom: true, peerReady: true, seq: 30})
	t.Cleanup(func() {
		delState(f.hostConn)
		delState(f.guestConn)
		delState(f.enemyConn)
	})
	return f
}

func lobbyChatBody(cid uint32, slot byte, text string) []byte {
	return lobbyChatBodyAim(cid, slot, chatAimTeam, text)
}

func lobbyChatBodyAim(cid uint32, slot byte, aim uint32, text string) []byte {
	b := make([]byte, 0, 32+len(text))
	b = binary.LittleEndian.AppendUint32(b, cid)
	b = append(b, wiregs.UTF("gllive:enterpries1")...)
	b = append(b, slot)
	b = binary.LittleEndian.AppendUint32(b, aim)
	b = binary.LittleEndian.AppendUint32(b, 7)
	return append(b, wiregs.UTF(text)...)
}

func TestLobbyChatTeamScopeEchoesBodyVerbatim(t *testing.T) {
	f := newChatFixture(t)
	body := lobbyChatBody(2, 1, "Enterpries(9:42): hello")
	handleLobbyChat(f.hostConn, f.hostState, 1, body)

	if n := len(f.enemyConn.takeWrites()); n != 0 {
		t.Fatalf("team lobby chat leaked to enemy half: %d", n)
	}
	want := body
	for name, c := range map[string]*captureConn{"host": f.hostConn, "guest": f.guestConn} {
		w := c.takeWrites()
		if len(w) != 1 {
			t.Fatalf("%s writes=%d", name, len(w))
		}
		hdr := w[0][:14]
		if hdr[0] != 0x24 || binary.LittleEndian.Uint16(hdr[9:11]) != (9<<4)|1 || binary.LittleEndian.Uint16(hdr[11:13]) != 0x1003 {
			t.Fatalf("%s header=%x", name, hdr)
		}
		if !bytes.Equal(w[0][14:], want) {
			t.Fatalf("%s body=%x want=%x", name, w[0][14:], want)
		}
	}
}

func TestLobbyChatAllScopeReachesBothHalves(t *testing.T) {
	f := newChatFixture(t)
	handleLobbyChat(f.hostConn, f.hostState, 1, lobbyChatBodyAim(2, 1, chatAimAll, "hi all"))
	for name, c := range map[string]*captureConn{"host": f.hostConn, "guest": f.guestConn, "enemy": f.enemyConn} {
		if n := len(c.takeWrites()); n != 1 {
			t.Fatalf("all-scope lobby chat %s writes=%d", name, n)
		}
	}
}

func TestLobbyChatRejectsShortBodyAndNoRoom(t *testing.T) {
	f := newChatFixture(t)
	handleLobbyChat(f.hostConn, f.hostState, 1, []byte{1, 2, 3})
	if n := len(f.guestConn.takeWrites()); n != 0 {
		t.Fatalf("short body relayed %d", n)
	}
	f.host.Room = nil
	handleLobbyChat(f.hostConn, f.hostState, 1, lobbyChatBody(2, 1, "x"))
	if n := len(f.guestConn.takeWrites()); n != 0 {
		t.Fatalf("roomless chat relayed %d", n)
	}
}

func unitAideBody(aim uint32, rest ...byte) []byte {
	b := binary.LittleEndian.AppendUint32(nil, 2)
	b = binary.LittleEndian.AppendUint32(b, aim)
	return append(b, rest...)
}

func TestUnitAideTeamScopeAndAllScope(t *testing.T) {
	f := newChatFixture(t)
	for _, s := range []*session.Session{f.host, f.guest, f.enemy} {
		s.MatchPlaying = true
	}
	f.hostState.playing = true
	f.room.MatchClockArmed = true
	f.room.MatchSFrame = 500

	body := unitAideBody(chatAimTeam, 2, 0, 0, 0)
	handleUnitAide(f.hostConn, f.hostState, 1, 2, body)
	if n := len(f.enemyConn.takeWrites()); n != 0 {
		t.Fatalf("team chat leaked to enemy: %d", n)
	}
	for name, c := range map[string]*captureConn{"host": f.hostConn, "guest": f.guestConn} {
		w := c.takeWrites()
		if len(w) != 1 {
			t.Fatalf("%s writes=%d", name, len(w))
		}
		hdr := w[0][:14]
		if hdr[0] != 0x40 || binary.LittleEndian.Uint16(hdr[9:11]) != (8<<4)|1 || binary.LittleEndian.Uint16(hdr[11:13]) != 2 {
			t.Fatalf("%s header=%x", name, hdr)
		}
		got := w[0][14:]
		if binary.LittleEndian.Uint32(got[0:4]) != uint32(500+config.FrameLead) || !bytes.Equal(got[4:], body[4:]) {
			t.Fatalf("%s body=%x want frame=%d + %x", name, got, 500+config.FrameLead, body[4:])
		}
	}

	handleUnitAide(f.hostConn, f.hostState, 1, 2, unitAideBody(chatAimAll, 1, 0, 0, 0))
	for name, c := range map[string]*captureConn{"host": f.hostConn, "guest": f.guestConn, "enemy": f.enemyConn} {
		if n := len(c.takeWrites()); n != 1 {
			t.Fatalf("all-chat %s writes=%d", name, n)
		}
	}

	// Ping (sub 3) is team-scoped like team chat.
	handleUnitAide(f.hostConn, f.hostState, 1, 3, unitAideBody(chatAimTeam, 0, 0, 0, 0, 0, 0, 0, 0))
	if n := len(f.enemyConn.takeWrites()); n != 0 {
		t.Fatalf("ping leaked to enemy: %d", n)
	}
	f.hostConn.takeWrites()
	f.guestConn.takeWrites()

	// Peers that are not in the match are skipped; the client would drop it anyway.
	f.guest.MatchPlaying = false
	handleUnitAide(f.hostConn, f.hostState, 1, 2, unitAideBody(chatAimAll, 1, 0, 0, 0))
	if n := len(f.guestConn.takeWrites()); n != 0 {
		t.Fatalf("non-playing guest got chat: %d", n)
	}
}

func TestUnitAideIgnoredBeforeMatch(t *testing.T) {
	f := newChatFixture(t)
	f.room.MatchClockArmed = true
	handleUnitAide(f.hostConn, f.hostState, 1, 2, unitAideBody(chatAimAll, 1, 0, 0, 0))
	for name, c := range map[string]*captureConn{"host": f.hostConn, "guest": f.guestConn, "enemy": f.enemyConn} {
		if n := len(c.takeWrites()); n != 0 {
			t.Fatalf("pre-match op8 relayed to %s: %d", name, n)
		}
	}
}
