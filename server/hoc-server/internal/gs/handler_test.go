package gs

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

func withReconnectHold(t *testing.T) {
	t.Helper()
	prev := config.MatchReconnectHold
	config.MatchReconnectHold = true
	t.Cleanup(func() { config.MatchReconnectHold = prev })
}

func TestHandleUnitActionStampsSharedClock(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sess := &session.Session{}
	room := &session.Room{
		Members:         []*session.Session{sess},
		MatchClockArmed: true,
		MatchSFrame:     30,
		MatchSynNext:    30,
	}
	sess.Room = room
	sess.SetMatchPlaying(true)
	st := &connState{conn: server, sess: sess, playing: true, seq: 100}
	setState(server, st)
	defer delState(server)

	readCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		pkt := make([]byte, 14+24)
		if _, err := io.ReadFull(client, pkt); err != nil {
			errCh <- err
			return
		}
		readCh <- pkt
	}()

	act := make([]byte, 24)
	binary.LittleEndian.PutUint32(act[0:4], 29) // stale client frame
	binary.LittleEndian.PutUint32(act[4:8], 0)  // server-owned sqid
	binary.LittleEndian.PutUint32(act[8:12], 1234)
	handleUnitAction(server, st, 0, 1, act)

	var pkt []byte
	select {
	case pkt = <-readCh:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for op7 relay")
	}
	if pkt[0] != 0x40 {
		t.Fatalf("magic=%#x", pkt[0])
	}
	if got := binary.LittleEndian.Uint16(pkt[9:11]); got != 0x70 {
		t.Fatalf("opslot=%#x", got)
	}
	if got := binary.LittleEndian.Uint16(pkt[11:13]); got != 1 {
		t.Fatalf("sub=%#x", got)
	}
	body := pkt[14:]
	if got := binary.LittleEndian.Uint32(body[0:4]); got != 31 {
		t.Fatalf("frame=%d", got)
	}
	if got := binary.LittleEndian.Uint32(body[4:8]); got != 30 {
		t.Fatalf("syn=%d", got)
	}
	if room.MatchSynNext != 31 {
		t.Fatalf("next syn=%d", room.MatchSynNext)
	}
	if binary.LittleEndian.Uint32(act[0:4]) != 29 || binary.LittleEndian.Uint32(act[4:8]) != 0 {
		t.Fatal("input action mutated")
	}
}

func TestGSLeave3001ClearsStateWithoutReply(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sess := &session.Session{
		AwaitingGS:       true,
		CustomMatchArmed: true,
		MatchLoading:     true,
	}
	sess.SetMatchPlaying(true)
	st := &connState{
		conn:    server,
		sess:    sess,
		custom:  true,
		loading: true,
		playing: true,
	}

	pkt := wiregs.BuildReply(1, 9, 0x3001, nil, 0x48)
	handlePkt(server, st, 1, pkt)

	if st.custom || st.loading || st.playing {
		t.Fatalf("connection state not idle: custom=%v loading=%v playing=%v", st.custom, st.loading, st.playing)
	}
	if sess.AwaitingGS || sess.CustomMatchArmed || sess.MatchLoading || sess.IsMatchPlaying() {
		t.Fatalf("session state not idle: awaiting=%v armed=%v loading=%v playing=%v",
			sess.AwaitingGS, sess.CustomMatchArmed, sess.MatchLoading, sess.IsMatchPlaying())
	}

	if err := client.SetReadDeadline(time.Now().Add(25 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("0x3001 unexpectedly produced a reply")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("reply check error=%v", err)
	}
}

func TestHeldReLoginReqSendsOnlyAckAndPreservesClock(t *testing.T) {
	withReconnectHold(t)
	lobbyConn := &captureConn{}
	sess := session.Create(lobbyConn, "relogin")
	sess.BindAccount(&accounts.Account{
		Username: "enterpries1", Nickname: "Enterpries", UserID: 1000001,
		AccountID: "1000001", Gateway: "127.0.0.1",
	})
	room := session.CreateRoom(sess, session.RoomOptions{Name: "shared", Capacity: 10})
	sess.SeatHeroID = 333
	sess.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.MatchSFrame = 2348
	room.MatchSynNext = 2432
	room.MatchFramesSent = 2348
	room.Unlock()
	oldGS := &captureConn{}
	sess.AttachGS(oldGS)
	held, _, _ := sess.DetachGSForDisconnect(oldGS, true, true, time.Second)
	if !held {
		t.Fatal("match disconnect did not enter hold")
	}

	newGS := &captureConn{}
	st := &connState{conn: newGS, roomID: 1, tskcid: 2}
	setState(newGS, st)
	t.Cleanup(func() {
		delState(newGS)
		sess.MarkGSLeave()
		sess.DetachGS(newGS)
		session.Destroy(lobbyConn)
	})
	// Server syn==client _syn must bump to req+1 (duplicate syn → Dev|3005).
	room.Lock()
	room.MatchSynNext = 2431
	room.Unlock()
	body := reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 2431, 4)
	if !handleReLogin(newGS, st, 50, body) {
		t.Fatal("valid held ReLoginReq rejected")
	}
	writes := newGS.takeWrites()
	if len(writes) != 1 {
		t.Fatalf("resume writes=%d want only ReLoginAck", len(writes))
	}
	ack := writes[0]
	if len(ack) < 14 || binary.LittleEndian.Uint16(ack[11:13]) != 5 {
		t.Fatalf("resume packet=%x", ack)
	}
	if want := wiregs.ReLoginAck(room.ID, sess.Seat, claimTskCID(sess)); string(ack[14:]) != string(want) {
		t.Fatalf("resume body=%x want=%x", ack[14:], want)
	}
	if hasGSSub(writes, 2) || hasGSSub(writes, 0x1002) || hasGSSub(writes, 0x100B) || hasGSSub(writes, 0x2002) || hasGSSub(writes, 0x2004) {
		t.Fatalf("resume emitted forbidden fresh/start packets: %x", writes)
	}
	if sess.MatchHold || sess.MatchResumeGrace() || !sess.IsMatchPlaying() || sess.IsMatchLoading() ||
		!st.custom || !st.peerReady || !st.playing || st.loading {
		t.Fatalf("resume state hold=%v grace=%v session=%v/%v conn custom=%v ready=%v play=%v load=%v",
			sess.MatchHold, sess.MatchResumeGrace(), sess.IsMatchPlaying(), sess.IsMatchLoading(),
			st.custom, st.peerReady, st.playing, st.loading)
	}
	room.Lock()
	armed, frame, syn, framesSent := room.MatchClockArmed, room.MatchSFrame, room.MatchSynNext, room.MatchFramesSent
	room.Unlock()
	// Next syn must be req+1 (2432). Clock stays frozen until rejoiner op3.
	if !armed || frame != 2348 || syn != 2432 || framesSent != 2348 {
		t.Fatalf("resume clock armed=%v frame=%d syn=%d sent=%d want syn=2432", armed, frame, syn, framesSent)
	}
	if !sess.IsMatchClockFrozen() {
		t.Fatal("resume should await op3 before shared clock advances")
	}
}

func TestHandleDispatchesFirstCommand4WithoutFreshLogin(t *testing.T) {
	withReconnectHold(t)
	lobbyConn := &captureConn{}
	sess := session.Create(lobbyConn, "relogin-dispatch")
	sess.BindAccount(&accounts.Account{Username: "enterpries1", Nickname: "Enterpries"})
	room := session.CreateRoom(sess, session.RoomOptions{Name: "shared", Capacity: 10})
	sess.SeatHeroID = 333
	sess.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.MatchSFrame = 40
	room.MatchSynNext = 41
	room.Unlock()
	oldGS := &captureConn{}
	sess.AttachGS(oldGS)
	if held, _, _ := sess.DetachGSForDisconnect(oldGS, true, true, time.Second); !held {
		t.Fatal("match disconnect did not enter hold")
	}

	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		Handle(server)
		close(done)
	}()
	body := reLoginReqBody("hoc_r"+strconv.Itoa(room.ID), sess.GUID, 40, 4)
	inner := wiregs.BuildReply(0, 9, 4, body, 0x48)
	if _, err := client.Write(encryptC2SPacket(50, inner, 0x2a)); err != nil {
		t.Fatal(err)
	}
	wantLen := 14 + len(wiregs.ReLoginAck(room.ID, sess.Seat, claimTskCID(sess)))
	ack := make([]byte, wantLen)
	if _, err := io.ReadFull(client, ack); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(ack[11:13]); got != 5 {
		t.Fatalf("first response sub=%#x want ReLoginAck(5); packet=%x", got, ack)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("GS handler did not exit")
	}
	session.Destroy(lobbyConn)
}

func TestInvalidReLoginReqNeverFallsBackToLoginAck(t *testing.T) {
	conn := &captureConn{}
	st := &connState{conn: conn, roomID: 1, tskcid: 2}
	if handleReLogin(conn, st, 10, reLoginReqBody("hoc_r999", "gllive:missing", 10, 4)) {
		t.Fatal("invalid ReLoginReq accepted")
	}
	if writes := conn.takeWrites(); len(writes) != 0 {
		t.Fatalf("invalid ReLoginReq writes=%d loginAck=%v", len(writes), hasGSSub(writes, 2))
	}
}

func TestReconnectHoldTimeoutEmitsOneLeave3003(t *testing.T) {
	hostLobby := &captureConn{}
	guestLobby := &captureConn{}
	host := session.Create(hostLobby, "host")
	guest := session.Create(guestLobby, "guest")
	host.BindAccount(&accounts.Account{Username: "enterpries1", Nickname: "Enterpries"})
	guest.BindAccount(&accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"})
	room := session.CreateRoom(host, session.RoomOptions{Name: "shared", Capacity: 10})
	if joined, why := session.JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("guest join room=%v why=%q", joined, why)
	}
	host.SetMatchPlaying(true)
	guest.SetMatchPlaying(true)
	guest.SeatHeroID = 340
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.Unlock()
	hostGS := &captureConn{}
	guestGS := &captureConn{}
	host.AttachGS(hostGS)
	guest.AttachGS(guestGS)
	hostState := &connState{conn: hostGS, sess: host, custom: true, peerReady: true, playing: true, seq: 20}
	setState(hostGS, hostState)
	t.Cleanup(func() {
		delState(hostGS)
		host.MarkGSLeave()
		host.DetachGS(hostGS)
		session.Destroy(guestLobby)
		session.Destroy(hostLobby)
	})
	held, _, _ := guest.DetachGSForDisconnect(guestGS, true, true, 20*time.Millisecond)
	if !held {
		t.Fatal("guest hold not entered")
	}
	deadline := time.Now().Add(time.Second)
	for guest.CurrentRoom() != nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	writes := hostGS.takeWrites()
	if guest.CurrentRoom() != nil || len(writes) != 1 || !hasGSSub(writes, 0x3003) {
		t.Fatalf("timeout room=%v writes=%d leave=%v", guest.CurrentRoom(), len(writes), hasGSSub(writes, 0x3003))
	}
	session.LeaveRoom(guest, "late_cleanup")
	if writes := hostGS.takeWrites(); len(writes) != 0 {
		t.Fatalf("timeout duplicated leave writes=%d", len(writes))
	}
}

func TestCustomSeatLoginSkipsBuyItemAndPreservesHeroesLatch(t *testing.T) {
	lobbyConn := &captureConn{}
	sess := session.Create(lobbyConn, "custom-login")
	t.Cleanup(func() { session.Destroy(lobbyConn) })
	sess.BindAccount(&accounts.Account{
		Username: "enterpries1", Nickname: "Enterpries", UserID: 1000001,
		AccountID: "1000001", Gateway: "127.0.0.1",
	})
	sess.CustomMatchArmed = true
	sess.HeroesDelivered = false
	token := sess.EnsureToken()

	gsConn := &captureConn{}
	st := &connState{conn: gsConn, roomID: 1, tskcid: 2}
	handleLogin(gsConn, st, 10, []byte("gllive:enterpries1 "+token))
	t.Cleanup(func() { sess.DetachGS(gsConn) })

	writes := gsConn.takeWrites()
	if !st.custom || !hasGSSub(writes, 1) || !hasGSSub(writes, 0x100A) || !hasGSSub(writes, 0x100B) {
		t.Fatalf("custom login state/writes custom=%v userinfo=%v ready=%v gotdata=%v writes=%d",
			st.custom, hasGSSub(writes, 1), hasGSSub(writes, 0x100A),
			hasGSSub(writes, 0x100B), len(writes))
	}
	if gsSubIndex(writes, 0x100A) > gsSubIndex(writes, 0x100B) {
		t.Fatal("custom login sent 0x100B before 0x100A")
	}
	if hasGSSub(writes, 5) {
		t.Fatal("custom login emitted forbidden BuyItem sub5")
	}
	if sess.HeroesDelivered {
		t.Fatal("custom login changed the main-menu HeroesDelivered latch")
	}
}

func TestRoomSeatSyncAndPeerLeave(t *testing.T) {
	hostConn := &captureConn{}
	guestConn := &captureConn{}
	host := &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	guest := &session.Session{
		Username: "enterpries2", GUID: "gllive:enterpries2", Seat: 1,
		Account: &accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"},
	}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}, TskCID: 2}
	host.Room, guest.Room = room, room
	host.AttachGS(hostConn)
	guest.AttachGS(guestConn)
	hostState := &connState{conn: hostConn, sess: host, custom: true, peerReady: true, seq: 10}
	guestState := &connState{conn: guestConn, sess: guest, custom: true, peerReady: true, seq: 20}
	setState(hostConn, hostState)
	setState(guestConn, guestState)
	t.Cleanup(func() {
		delState(hostConn)
		delState(guestConn)
	})

	syncRoomSeats1002(room, "test")
	hostWrites := hostConn.takeWrites()
	guestWrites := guestConn.takeWrites()
	if len(hostWrites) != 1 || len(guestWrites) != 1 {
		t.Fatalf("seat-sync writes host=%d guest=%d", len(hostWrites), len(guestWrites))
	}
	checkSeatRosterPacket(t, hostWrites[0], 1)
	checkSeatRosterPacket(t, guestWrites[0], 2)

	skill := []byte{2, 0, 0, 0, 1, 2, 3}
	broadcastRoomSyn(room, guest, 0x100D, skill, "test-skill")
	hostWrites = hostConn.takeWrites()
	guestWrites = guestConn.takeWrites()
	if len(hostWrites) != 1 || len(guestWrites) != 0 {
		t.Fatalf("peer skill writes host=%d guest=%d", len(hostWrites), len(guestWrites))
	}
	if binary.LittleEndian.Uint16(hostWrites[0][11:13]) != 0x100D || string(hostWrites[0][14:]) != string(skill) {
		t.Fatalf("peer skill packet=%x", hostWrites[0])
	}

	broadcastRoomLeave3003(room, guest, "test-leave")
	hostWrites = hostConn.takeWrites()
	guestWrites = guestConn.takeWrites()
	if len(hostWrites) != 1 || len(guestWrites) != 0 {
		t.Fatalf("peer leave writes host=%d guest=%d", len(hostWrites), len(guestWrites))
	}
	leave := hostWrites[0]
	if binary.LittleEndian.Uint16(leave[11:13]) != 0x3003 || binary.LittleEndian.Uint16(leave[9:11])&0xF != 2 {
		t.Fatalf("leave header=%x", leave[:14])
	}
	if body := leave[14:]; len(body) < 7 || body[4] != 2 || string(body[7:]) != "Enterpries2" {
		t.Fatalf("leave body=%x", body)
	}
}

func TestReadyCancelEchoesAndBlocksTrailingLoadMap(t *testing.T) {
	conn := &captureConn{}
	sess := &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0,
		SeatHeroID: 333, SeatReady: true,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	room := &session.Room{Host: sess, Members: []*session.Session{sess}, TskCID: 2}
	sess.Room = room
	sess.AttachGS(conn)
	st := &connState{conn: conn, sess: sess, custom: true, peerReady: true, seq: 10, tskcid: 2}
	setState(conn, st)
	t.Cleanup(func() { delState(conn) })

	readyBody := make([]byte, 12)
	binary.LittleEndian.PutUint32(readyBody[0:4], 2)
	handlePkt(conn, st, 1, wiregs.BuildReply(1, 9, 0x100C, readyBody, 0x48))
	writes := conn.takeWrites()
	if sess.SeatReady || !sess.ReadyCancelled {
		t.Fatalf("READY=0 state ready=%v cancelled=%v", sess.SeatReady, sess.ReadyCancelled)
	}
	if !hasGSSub(writes, 0x100D) || !hasGSSub(writes, 0x1002) {
		t.Fatalf("READY=0 writes=%d skill=%v seat=%v", len(writes), hasGSSub(writes, 0x100D), hasGSSub(writes, 0x1002))
	}

	handlePkt(conn, st, 2, wiregs.BuildReply(2, 9, 0x2001, nil, 0x48))
	if writes = conn.takeWrites(); len(writes) != 0 || st.loading || sess.MatchLoading {
		t.Fatalf("cancelled 0x2001 writes=%d loading=%v/%v", len(writes), st.loading, sess.MatchLoading)
	}

	binary.LittleEndian.PutUint32(readyBody[4:8], 1)
	handlePkt(conn, st, 3, wiregs.BuildReply(3, 9, 0x100C, readyBody, 0x48))
	_ = conn.takeWrites()
	handlePkt(conn, st, 4, wiregs.BuildReply(4, 9, 0x2001, nil, 0x48))
	writes = conn.takeWrites()
	if !hasGSSub(writes, 0x2002) || !st.loading {
		t.Fatalf("re-armed 0x2001 writes=%d loadmap=%v loading=%v", len(writes), hasGSSub(writes, 0x2002), st.loading)
	}
}

func TestHostDestroyVacatesPeerOnceAndSkipsRoomNilSeatPaint(t *testing.T) {
	hostConn := &captureConn{}
	guestConn := &captureConn{}
	host := &session.Session{
		Username: "enterpries1", Seat: 0,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	guest := &session.Session{
		Username: "enterpries2", Seat: 1,
		Account: &accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"},
	}
	room := &session.Room{ID: 77, Host: host, Members: []*session.Session{host, guest}, State: "open", TskCID: 2}
	host.Room, guest.Room = room, room
	host.AttachGS(hostConn)
	guest.AttachGS(guestConn)
	hostState := &connState{conn: hostConn, sess: host, custom: true, peerReady: true, seq: 10, tskcid: 2}
	guestState := &connState{conn: guestConn, sess: guest, custom: true, peerReady: true, seq: 20, tskcid: 2}
	setState(hostConn, hostState)
	setState(guestConn, guestState)
	t.Cleanup(func() {
		delState(hostConn)
		delState(guestConn)
	})

	handlePkt(hostConn, hostState, 1, wiregs.BuildReply(1, 9, 0x3001, nil, 0x48))
	if writes := hostConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("host received self leave writes=%d", len(writes))
	}
	writes := guestConn.takeWrites()
	if len(writes) != 1 || !hasGSSub(writes, 0x3003) {
		t.Fatalf("peer vacate writes=%d leave=%v", len(writes), hasGSSub(writes, 0x3003))
	}

	session.LeaveRoom(host, "quit_e02e")
	if writes := guestConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("host promote duplicated peer vacate writes=%d", len(writes))
	}
	if host.Room != nil {
		t.Fatalf("leaver still linked room=%v", host.Room)
	}
	if guest.Room != room || room.Host != guest {
		t.Fatalf("survivor not promoted host=%v guestRoom=%v", room.Host, guest.Room)
	}

	// After last survivor leaves, room is gone — seat paint must stay quiet.
	session.LeaveRoom(guest, "late_cleanup")
	sendSeat1002(guestConn, guestState, "late-after-destroy")
	if writes := guestConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("room=nil emitted ghost seat paint writes=%d", len(writes))
	}
}

func TestSeatHop1007BroadcastUsesOldSeatNibbleOnly(t *testing.T) {
	hostConn := &captureConn{}
	guestConn := &captureConn{}
	host := &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0, SeatHeroID: 333,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	guest := &session.Session{
		Username: "enterpries2", GUID: "gllive:enterpries2", Seat: 1,
		Account: &accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"},
	}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}, TskCID: 2}
	host.Room, guest.Room = room, room
	host.AttachGS(hostConn)
	guest.AttachGS(guestConn)
	hostState := &connState{conn: hostConn, sess: host, custom: true, peerReady: true, seq: 10, tskcid: 2}
	guestState := &connState{conn: guestConn, sess: guest, custom: true, peerReady: true, seq: 20, tskcid: 2}
	setState(hostConn, hostState)
	setState(guestConn, guestState)
	t.Cleanup(func() {
		delState(hostConn)
		delState(guestConn)
	})

	guid := "gllive:enterpries1"
	body := make([]byte, 4+2+len(guid)+1+4)
	binary.LittleEndian.PutUint32(body[0:4], 2)
	binary.LittleEndian.PutUint16(body[4:6], uint16(len(guid)))
	copy(body[6:], guid)
	body[6+len(guid)] = 6
	pkt := wiregs.BuildReply(1, 9, 0x1006, body, 0x48)
	handlePkt(hostConn, hostState, 1, pkt)

	if host.Seat != 5 || host.GUID != guid {
		t.Fatalf("host hop seat/guid=%d/%q", host.Seat, host.GUID)
	}
	hostWrites := hostConn.takeWrites()
	guestWrites := guestConn.takeWrites()
	if len(hostWrites) != 1 || len(guestWrites) != 1 {
		t.Fatalf("hop writes host=%d guest=%d", len(hostWrites), len(guestWrites))
	}
	for _, write := range [][]byte{hostWrites[0], guestWrites[0]} {
		if binary.LittleEndian.Uint16(write[11:13]) != 0x1007 {
			t.Fatalf("hop sub=%#x packet=%x", binary.LittleEndian.Uint16(write[11:13]), write)
		}
		if got := binary.LittleEndian.Uint16(write[9:11]) & 0xF; got != 1 {
			t.Fatalf("old-seat nibble=%d packet=%x", got, write[:14])
		}
		ack := write[14:]
		if len(ack) != 9 || ack[4] != 6 || binary.LittleEndian.Uint32(ack[5:9]) != 333 {
			t.Fatalf("hop body=%x", ack)
		}
	}
}

func TestHeroLockRejectsPeerDuplicateWithoutAck(t *testing.T) {
	hostConn := &captureConn{}
	guestConn := &captureConn{}
	host := &session.Session{
		Username: "enterpries1", GUID: "gllive:enterpries1", Seat: 0, SeatHeroID: 333,
		Account: &accounts.Account{Username: "enterpries1", Nickname: "Enterpries"},
	}
	// Same team half as host (seats 0..4) — duplicate must be rejected with no ACK.
	guest := &session.Session{
		Username: "enterpries2", GUID: "gllive:enterpries2", Seat: 1,
		Account: &accounts.Account{Username: "enterpries2", Nickname: "Enterpries2"},
	}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}, TskCID: 2}
	host.Room, guest.Room = room, room
	host.AttachGS(hostConn)
	guest.AttachGS(guestConn)
	hostState := &connState{conn: hostConn, sess: host, custom: true, peerReady: true, seq: 10, tskcid: 2}
	guestState := &connState{conn: guestConn, sess: guest, custom: true, peerReady: true, seq: 20, tskcid: 2}
	setState(hostConn, hostState)
	setState(guestConn, guestState)
	t.Cleanup(func() {
		delState(hostConn)
		delState(guestConn)
	})

	body := make([]byte, 12)
	binary.LittleEndian.PutUint32(body[0:4], 2)
	binary.LittleEndian.PutUint32(body[4:8], 333)
	pkt := wiregs.BuildReply(1, 9, 0x1008, body, 0x48)
	handlePkt(guestConn, guestState, 1, pkt)

	if guest.SeatHeroID != 0 || guest.SeatSkinID != 0 {
		t.Fatalf("duplicate pick mutated guest hero=%d skin=%d", guest.SeatHeroID, guest.SeatSkinID)
	}
	if writes := hostConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("duplicate pick wrote to host: %d", len(writes))
	}
	if writes := guestConn.takeWrites(); len(writes) != 0 {
		t.Fatalf("duplicate pick ACKed guest: %d", len(writes))
	}
}

func TestSendRoomSynSerializesSequenceWithWrite(t *testing.T) {
	conn := &captureConn{}
	st := &connState{conn: conn, custom: true, peerReady: true, seq: 100}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := sendRoomSyn(conn, st, 0x1002, []byte{1}, 1); err != nil {
				t.Errorf("sendRoomSyn: %v", err)
			}
		}()
	}
	wg.Wait()
	writes := conn.takeWrites()
	if len(writes) != 32 {
		t.Fatalf("writes=%d", len(writes))
	}
	for i, pkt := range writes {
		if got, want := binary.LittleEndian.Uint32(pkt[1:5]), uint32(100+i); got != want {
			t.Fatalf("write %d seq=%d want=%d", i, got, want)
		}
	}
}

func claimTskCID(sess *session.Session) int {
	if sess != nil && sess.Room != nil && sess.Room.TskCID > 0 {
		return sess.Room.TskCID
	}
	return config.DefaultTskCID
}

func reLoginReqBody(roomName, guid string, requestedSyn int32, mode byte) []byte {
	body := []byte{0}
	body = append(body, wiregs.UTF(roomName)...)
	body = append(body, wiregs.UTF(guid)...)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(requestedSyn))
	body = append(body, tmp...)
	body = append(body, mode)
	return body
}

func encryptC2SPacket(seq uint16, payload []byte, key byte) []byte {
	plain := make([]byte, 11+len(payload))
	plain[0] = 0x48
	binary.LittleEndian.PutUint16(plain[1:3], seq)
	binary.LittleEndian.PutUint16(plain[5:7], uint16(len(payload)))
	copy(plain[11:], payload)
	for i := range plain {
		plain[i] ^= key
	}
	return plain
}

func checkSeatRosterPacket(t *testing.T, pkt []byte, localSeat byte) {
	t.Helper()
	if len(pkt) < 20 || binary.LittleEndian.Uint16(pkt[11:13]) != 0x1002 {
		t.Fatalf("seat roster packet=%x", pkt)
	}
	if got := byte(binary.LittleEndian.Uint16(pkt[9:11]) & 0xF); got != localSeat {
		t.Fatalf("header local seat=%d want=%d", got, localSeat)
	}
	body := pkt[14:]
	if len(body) < 6 || body[4] != 2 || body[len(body)-1] != localSeat {
		t.Fatalf("roster count/local=%d/%d len=%d", body[4], body[len(body)-1], len(body))
	}
	first := 5
	second := nextPI(t, body, first)
	end := nextPI(t, body, second)
	if end != len(body)-1 {
		t.Fatalf("roster PI end=%d body=%d", end, len(body))
	}
	if binary.LittleEndian.Uint32(body[first:first+4]) != 1 || binary.LittleEndian.Uint32(body[second:second+4]) != 2 {
		t.Fatalf("roster seats=%d/%d", binary.LittleEndian.Uint32(body[first:first+4]), binary.LittleEndian.Uint32(body[second:second+4]))
	}
}

func hasGSSub(writes [][]byte, want uint16) bool {
	return gsSubIndex(writes, want) >= 0
}

func gsSubIndex(writes [][]byte, want uint16) int {
	for i, pkt := range writes {
		if len(pkt) >= 13 && binary.LittleEndian.Uint16(pkt[11:13]) == want {
			return i
		}
	}
	return -1
}

func nextPI(t *testing.T, body []byte, start int) int {
	t.Helper()
	off := start + 40
	for i := 0; i < 4; i++ {
		if off+2 > len(body) {
			t.Fatal("short PI UTF length")
		}
		n := int(binary.LittleEndian.Uint16(body[off : off+2]))
		off += 2 + n
		if off > len(body) {
			t.Fatal("short PI UTF body")
		}
	}
	return off + (146-10)*4
}

type captureConn struct {
	mu     sync.Mutex
	writes [][]byte
}

func (c *captureConn) Read([]byte) (int, error) { return 0, io.EOF }
func (c *captureConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), p...))
	c.mu.Unlock()
	return len(p), nil
}
func (c *captureConn) Close() error                     { return nil }
func (c *captureConn) LocalAddr() net.Addr              { return captureAddr("local") }
func (c *captureConn) RemoteAddr() net.Addr             { return captureAddr("remote") }
func (c *captureConn) SetDeadline(time.Time) error      { return nil }
func (c *captureConn) SetReadDeadline(time.Time) error  { return nil }
func (c *captureConn) SetWriteDeadline(time.Time) error { return nil }
func (c *captureConn) takeWrites() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([][]byte(nil), c.writes...)
	c.writes = nil
	return out
}

type captureAddr string

func (a captureAddr) Network() string { return "test" }
func (a captureAddr) String() string  { return string(a) }
