package gs

import (
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

// matchSession builds a session that is mid-match in its own room with an
// armed clock, the way a live player looks between LoadMap and the end.
func matchSession(t *testing.T, name string) (*session.Session, *session.Room, *captureConn) {
	t.Helper()
	withReconnectHold(t)
	lobbyConn := &captureConn{}
	sess := session.Create(lobbyConn, name)
	sess.BindAccount(&accounts.Account{Username: "enterpries1", Nickname: "Enterpries", UserID: 1000001, AccountID: "1000001"})
	room := session.CreateRoom(sess, session.RoomOptions{Name: "shared", Capacity: 10})
	sess.SeatHeroID = 333
	sess.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.MatchSFrame = 35824
	room.MatchSynNext = 44602
	room.MatchFramesSent = 35824
	room.Unlock()
	t.Cleanup(func() { session.Destroy(lobbyConn) })
	return sess, room, lobbyConn
}

// LIVE 2026-09-18 20:50: the phone's network dropped without RST, the old GS
// socket stayed attached, and every ReLoginReq died with "no-hold". A
// ReLoginReq for a still-attached owner must take the seat over: hold the
// session, close the stale transport out of band, and Ack on the new one.
func TestReLoginReqTakesOverHalfOpenSocket(t *testing.T) {
	sess, room, _ := matchSession(t, "takeover")

	// Stale transport driven by the real Handle loop so its deferred
	// disconnect path runs when we close it.
	staleServer, staleClient := net.Pipe()
	defer staleClient.Close()
	done := make(chan struct{})
	go func() {
		Handle(staleServer)
		close(done)
	}()
	var stale *connState
	for i := 0; i < 200 && stale == nil; i++ {
		stale = getState(staleServer)
		if stale == nil {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if stale == nil {
		t.Fatal("Handle did not register the stale connection")
	}
	stale.mu.Lock()
	stale.sess = sess
	stale.playing = true
	stale.custom = true
	stale.mu.Unlock()
	sess.AttachGS(staleServer)

	newGS := &captureConn{}
	st := &connState{conn: newGS, roomID: 1, tskcid: 2}
	setState(newGS, st)
	t.Cleanup(func() { delState(newGS); sess.MarkGSLeave(); sess.DetachGS(newGS) })

	body := reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 44567, 4)
	if !handleReLogin(newGS, st, 95, body) {
		t.Fatal("ReLoginReq for a still-attached owner was rejected (no-hold)")
	}
	writes := newGS.takeWrites()
	if len(writes) != 1 || !hasGSSub(writes, 5) {
		t.Fatalf("takeover writes=%x want exactly one ReLoginAck", writes)
	}

	// The stale Handle goroutine must exit (socket closed) without touching
	// the session: no hold clear, no leave, no soft-fail count.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stale GS handler did not exit after takeover")
	}
	if sess.Room != room {
		t.Fatalf("takeover left the room: room=%v", sess.Room)
	}
	if sess.MatchHold || !sess.IsMatchPlaying() || !st.playing {
		t.Fatalf("post-resume state hold=%v playing=%v conn.playing=%v", sess.MatchHold, sess.IsMatchPlaying(), st.playing)
	}
	conns := sess.SnapshotGSConns()
	if len(conns) != 1 || conns[0] != newGS {
		t.Fatalf("session transports after takeover=%v want only the new socket", conns)
	}
	if n := sess.SoftResumeFailCount(); n != 0 {
		t.Fatalf("stale close counted as soft-resume fail: %d", n)
	}
	room.Lock()
	armed := room.MatchClockArmed
	room.Unlock()
	if !armed {
		t.Fatal("takeover disarmed the room clock")
	}
}

// Takeover must never fire for a foreign GUID or a session that is not in a
// running match — "no-hold" stays the answer there.
func TestReLoginReqTakeoverRequiresLiveOwner(t *testing.T) {
	sess, room, _ := matchSession(t, "takeover-guard")
	old := &captureConn{}
	sess.AttachGS(old)
	t.Cleanup(func() { sess.MarkGSLeave(); sess.DetachGS(old) })

	newGS := &captureConn{}
	st := &connState{conn: newGS, roomID: 1, tskcid: 2}
	setState(newGS, st)
	t.Cleanup(func() { delState(newGS) })
	if handleReLogin(newGS, st, 10, reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), "gllive:somebody-else", 1, 4)) {
		t.Fatal("foreign GUID took over a live seat")
	}
	if conns := sess.SnapshotGSConns(); len(conns) != 1 || conns[0] != old {
		t.Fatalf("foreign ReLoginReq detached the owner: %v", conns)
	}
	if sess.MatchHold {
		t.Fatal("foreign ReLoginReq created a hold")
	}
}

// LIVE 2026-09-18 20:33: after two Ack→quick-EOF cycles the refusal used to
// LeaveRoom, deleting the seat 4 s into a 90 s hold while the client kept
// retrying calmly for 50 s. Refusal must keep the hold, and once the client
// slows past the cooldown it must get an Ack again.
func TestReLoginRefusalKeepsHoldAndCoolsDown(t *testing.T) {
	sess, room, _ := matchSession(t, "refuse")
	prevMax, prevCool := config.MatchReloginFailMax, config.MatchReloginFailCooldown
	config.MatchReloginFailMax, config.MatchReloginFailCooldown = 2, 50*time.Millisecond
	t.Cleanup(func() { config.MatchReloginFailMax, config.MatchReloginFailCooldown = prevMax, prevCool })

	old := &captureConn{}
	sess.AttachGS(old)
	if held, _, _ := sess.DetachGSForDisconnect(old, true, true, 5*time.Second); !held {
		t.Fatal("match disconnect did not enter hold")
	}
	// Two quick Ack→EOF cycles.
	for i := 0; i < 2; i++ {
		sess.NoteSoftResumeAck()
		if n := sess.NoteSoftResumeEOF(); n != i+1 {
			t.Fatalf("fail count=%d want %d", n, i+1)
		}
	}

	body := reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 100, 4)
	refused := &captureConn{}
	st := &connState{conn: refused, roomID: 1, tskcid: 2}
	setState(refused, st)
	t.Cleanup(func() { delState(refused) })
	if handleReLogin(refused, st, 10, body) {
		t.Fatal("third quick attempt was Acked")
	}
	if w := refused.takeWrites(); len(w) != 0 {
		t.Fatalf("refusal wrote %x", w)
	}
	if !st.detached {
		t.Fatal("refused transport not marked detached")
	}
	if !sess.MatchHold || sess.Room != room {
		t.Fatalf("refusal dropped the seat: hold=%v room=%v", sess.MatchHold, sess.Room)
	}
	if conns := sess.SnapshotGSConns(); len(conns) != 0 {
		t.Fatalf("refused transport left attached: %v", conns)
	}
	if !sess.AwaitingGS {
		t.Fatal("held session not awaiting GS after refusal")
	}

	// Client slows down → cooldown elapses → real Ack, hold consumed.
	time.Sleep(80 * time.Millisecond)
	newGS := &captureConn{}
	st2 := &connState{conn: newGS, roomID: 1, tskcid: 2}
	setState(newGS, st2)
	t.Cleanup(func() { delState(newGS); sess.MarkGSLeave(); sess.DetachGS(newGS) })
	if !handleReLogin(newGS, st2, 20, body) {
		t.Fatal("attempt after cooldown still refused")
	}
	if w := newGS.takeWrites(); len(w) != 1 || !hasGSSub(w, 5) {
		t.Fatalf("post-cooldown writes=%x want one ReLoginAck", w)
	}
	if sess.MatchHold || !sess.IsMatchPlaying() {
		t.Fatalf("post-cooldown resume state hold=%v playing=%v", sess.MatchHold, sess.IsMatchPlaying())
	}
}

// Nox 2026-09-19: a solo room's LoadMap left room.State at "open", so
// ClaimMatchHold (State=="match") could never claim a solo player's hold.
func TestSoloLoadMapMarksRoomInMatch(t *testing.T) {
	conn := &captureConn{}
	sess := &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0,
		SeatHeroID: 333, SeatReady: true,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	room := &session.Room{Host: sess, Members: []*session.Session{sess}, TskCID: 2, State: "open"}
	sess.Room = room
	sess.AttachGS(conn)
	st := &connState{conn: conn, sess: sess, custom: true, peerReady: true, seq: 10, tskcid: 2}
	setState(conn, st)
	t.Cleanup(func() { delState(conn) })

	readyBody := make([]byte, 12)
	binary.LittleEndian.PutUint32(readyBody[0:4], 2)
	binary.LittleEndian.PutUint32(readyBody[4:8], 1)
	handlePkt(conn, st, 1, wiregs.BuildReply(1, 9, 0x100C, readyBody, 0x48))
	_ = conn.takeWrites()
	handlePkt(conn, st, 2, wiregs.BuildReply(2, 9, 0x2001, nil, 0x48))
	if writes := conn.takeWrites(); !hasGSSub(writes, 0x2002) {
		t.Fatalf("solo 0x2001 did not LoadMap: %x", writes)
	}
	room.Lock()
	state := room.State
	room.Unlock()
	if state != "match" {
		t.Fatalf("solo room state=%q want match", state)
	}
}

// Nox 2026-09-19: with the replay ring covering the requested syn, the
// resume must not rewind the room clock even when every peer is frozen
// (solo): rewind + replay handed the client the old packets and then a
// lower live sequence, which it rejected, looping until the fail guard.
func TestResumeDoesNotRewindWhenReplayCoversRequest(t *testing.T) {
	sess, room, _ := matchSession(t, "no-rewind")
	room.Lock()
	room.MatchSFrame = 1021
	room.MatchSynNext = 1045
	for syn := 480; syn < 1045; syn++ {
		room.AppendMatchReplayLocked(session.MatchReplayPacket{Syn: syn, Opcode: 11, Body: []byte{1}})
	}
	room.Unlock()
	old := &captureConn{}
	sess.AttachGS(old)
	if held, _, _ := sess.DetachGSForDisconnect(old, true, true, 5*time.Second); !held {
		t.Fatal("solo disconnect did not enter hold")
	}
	newGS := &captureConn{}
	st := &connState{conn: newGS, roomID: 1, tskcid: 2}
	setState(newGS, st)
	t.Cleanup(func() { delState(newGS); sess.MarkGSLeave(); sess.DetachGS(newGS) })
	if !handleReLogin(newGS, st, 30, reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 489, 4)) {
		t.Fatal("held solo ReLoginReq rejected")
	}
	room.Lock()
	frame, syn := room.MatchSFrame, room.MatchSynNext
	room.Unlock()
	if frame != 1021 || syn != 1045 {
		t.Fatalf("clock rewound: frame=%d syn=%d want 1021/1045", frame, syn)
	}

	// Gap older than the ring: the rewind fallback still applies for a
	// frozen room.
	sess.MarkGSLeave()
	sess.DetachGS(newGS)
	sess.SetMatchPlaying(true)
	old2 := &captureConn{}
	sess.AttachGS(old2)
	if held, _, _ := sess.DetachGSForDisconnect(old2, true, true, 5*time.Second); !held {
		t.Fatal("second disconnect did not enter hold")
	}
	newGS2 := &captureConn{}
	st2 := &connState{conn: newGS2, roomID: 1, tskcid: 2}
	setState(newGS2, st2)
	t.Cleanup(func() { delState(newGS2); sess.DetachGS(newGS2) })
	if !handleReLogin(newGS2, st2, 40, reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 100, 4)) {
		t.Fatal("held solo ReLoginReq (old gap) rejected")
	}
	room.Lock()
	syn = room.MatchSynNext
	room.Unlock()
	if syn != 101 {
		t.Fatalf("rewind fallback did not apply for a gap older than the ring: syn=%d want 101", syn)
	}
}
