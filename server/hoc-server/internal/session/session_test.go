package session

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
)

func TestRoomListingAndLobbyHostTeardown(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "enterpries1", 1000001)
	guest := newTestSession(t, "enterpries2", 1000002)

	room := CreateRoom(host, RoomOptions{
		Name: "M2 Room", Capacity: 10, Flag1012: 1, Flag1013: 2,
		JSON1014: []byte(`{"mode":4}`), Param103E: 9,
	})
	if room == nil || room.ID != config.DefaultRoomID {
		t.Fatalf("room=%v", room)
	}
	joined, why := JoinRoom(guest, room.ID, nil)
	if joined != room || why != "ok" || guest.Seat != 1 {
		t.Fatalf("join room=%v why=%q seat=%d", joined, why, guest.Seat)
	}

	list := ListOpenRooms()
	if len(list) != 1 || list[0].Members != 2 || list[0].Name != "M2 Room" {
		t.Fatalf("list=%+v", list)
	}
	if list[0].Flag1012 != 1 || list[0].Flag1013 != 2 || list[0].Param103E != 9 {
		t.Fatalf("list metadata=%+v", list[0])
	}

	host.SeatHeroID, host.SeatSkinID, host.SeatSpell1, host.SeatSpell2 = 381, 7, 599, 600
	guest.SeatHeroID, guest.SeatSkinID, guest.SeatSpell1, guest.SeatSpell2 = 340, 2, 601, 602
	guest.SeatReady = true
	guest.ReadyCancelled = true
	guest.CustomMatchArmed = true
	guest.CustomRoomIntent = true

	LeaveRoom(host, "quit_e02e")
	if GetRoom(room.ID) != room {
		t.Fatalf("host seat-lobby leave destroyed room")
	}
	if room.Host != guest || guest.Room != room {
		t.Fatalf("survivor not promoted host=%v guestRoom=%v", room.Host, guest.Room)
	}
	if host.Room != nil {
		t.Fatalf("leaver still linked room=%v", host.Room)
	}
	if host.SeatHeroID != 0 || host.CustomMatchArmed || host.CustomRoomIntent {
		t.Fatalf("leaver latch survived hero=%d armed=%v intent=%v",
			host.SeatHeroID, host.CustomMatchArmed, host.CustomRoomIntent)
	}
	// Survivor keeps seat pick / ready latches — room still live.
	if guest.SeatHeroID != 340 || !guest.SeatReady || !guest.CustomMatchArmed {
		t.Fatalf("survivor wiped hero=%d ready=%v armed=%v",
			guest.SeatHeroID, guest.SeatReady, guest.CustomMatchArmed)
	}
	LeaveRoom(guest, "cleanup")
	if GetRoom(room.ID) != nil {
		t.Fatal("last member leave did not destroy room")
	}
}

func TestReadyCancelBlocksTrailing2001UntilReadyOne(t *testing.T) {
	sess := &Session{SeatHeroID: 333, SeatReady: true}
	if !sess.ApplyReadyFromSkill(false) || sess.SeatReady || !sess.ReadyCancelled {
		t.Fatalf("READY=0 state ready=%v cancelled=%v", sess.SeatReady, sess.ReadyCancelled)
	}
	if ok, why := sess.AcceptReadyStart(); ok || why != "cancelled" || sess.SeatReady {
		t.Fatalf("cancelled 0x2001 ok=%v why=%q ready=%v", ok, why, sess.SeatReady)
	}
	if !sess.ApplyReadyFromSkill(true) || !sess.SeatReady || sess.ReadyCancelled {
		t.Fatalf("READY=1 state ready=%v cancelled=%v", sess.SeatReady, sess.ReadyCancelled)
	}
	if ok, why := sess.AcceptReadyStart(); !ok || why != "ok" || !sess.SeatReady {
		t.Fatalf("re-armed 0x2001 ok=%v why=%q ready=%v", ok, why, sess.SeatReady)
	}
}

func TestCustomJoinCorrelationUsesOnlyExactSingleAdvertisement(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	sess := newTestSession(t, "enterpries2", 1000002)
	sess.CustomRoomIntent = true

	sess.RememberCustomRoomSearch([]int{7})
	if got, ok := sess.CorrelateCustomRoomJoin(0); !ok || got != 7 {
		t.Fatalf("single advertisement correlation got=%d ok=%v", got, ok)
	}
	if got, ok := sess.CorrelateCustomRoomJoin(0); ok || got != 0 {
		t.Fatalf("correlation was not one-shot got=%d ok=%v", got, ok)
	}

	sess.RememberCustomRoomSearch([]int{7, 8})
	if got, ok := sess.CorrelateCustomRoomJoin(0); ok || got != 0 {
		t.Fatalf("ambiguous search guessed a room got=%d ok=%v", got, ok)
	}
	if got, ok := sess.CorrelateCustomRoomJoin(9); ok || got != 9 {
		t.Fatalf("explicit room id was changed got=%d ok=%v", got, ok)
	}
}

func TestSeatHopCollisionAndExclusiveHeroClaim(t *testing.T) {
	host := &Session{Username: "enterpries1", Seat: 0}
	guest := &Session{Username: "enterpries2", Seat: 5}
	room := &Room{Host: host, Members: []*Session{host, guest}, TskCID: 2}
	host.Room, guest.Room = room, room

	gotRoom, oldSeat, newSeat, changed, why := ChangeSeat(host, 5, "gllive:enterpries1")
	if gotRoom != room || oldSeat != 0 || newSeat != 6 || !changed || why != "ok" {
		t.Fatalf("hop room=%v old=%d new=%d changed=%v why=%q", gotRoom, oldSeat, newSeat, changed, why)
	}
	if host.Seat != 6 || host.GUID != "gllive:enterpries1" {
		t.Fatalf("host seat/guid=%d/%q", host.Seat, host.GUID)
	}

	if ok, owner := ClaimSeatHero(host, 333, 1); !ok || owner != "" {
		t.Fatalf("host claim ok=%v owner=%q", ok, owner)
	}
	// After hop host is seat 6; guest seat 5 → same team half → duplicate blocked.
	if ok, owner := ClaimSeatHero(guest, 333, 2); ok || owner != "enterpries1" {
		t.Fatalf("same-team duplicate claim ok=%v owner=%q", ok, owner)
	}
	if guest.SeatHeroID != 0 || guest.SeatSkinID != 0 {
		t.Fatalf("rejected claim mutated guest hero=%d skin=%d", guest.SeatHeroID, guest.SeatSkinID)
	}
	if ok, owner := ClaimSeatHero(guest, 340, 2); !ok || owner != "" {
		t.Fatalf("unique claim ok=%v owner=%q", ok, owner)
	}
}

func TestClaimSeatHeroAllowsEnemyTeamMirror(t *testing.T) {
	host := &Session{Username: "enterpries1", Seat: 0}
	guest := &Session{Username: "enterpries2", Seat: 5}
	room := &Room{Host: host, Members: []*Session{host, guest}, TskCID: 2}
	host.Room, guest.Room = room, room

	if ok, owner := ClaimSeatHero(host, 333, 1); !ok || owner != "" {
		t.Fatalf("host claim ok=%v owner=%q", ok, owner)
	}
	if ok, owner := ClaimSeatHero(guest, 333, 2); !ok || owner != "" {
		t.Fatalf("enemy-team mirror blocked ok=%v owner=%q", ok, owner)
	}
	if guest.SeatHeroID != 333 || guest.SeatSkinID != 2 {
		t.Fatalf("enemy mirror state hero=%d skin=%d", guest.SeatHeroID, guest.SeatSkinID)
	}
}

func TestHostLeaveInMatchPromotesSurvivor(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "enterpries1", 1000001)
	guest := newTestSession(t, "enterpries2", 1000002)
	room := CreateRoom(host, RoomOptions{Name: "shared", Capacity: 10, Flag1012: 1})
	if joined, why := JoinRoom(guest, room.ID, nil); joined == nil || why != "ok" {
		t.Fatalf("join failed: %v %s", joined, why)
	}
	room.Lock()
	room.MatchClockArmed = true
	room.Unlock()

	LeaveRoom(host, "lobby_disconnect")
	if GetRoom(room.ID) != room || room.Host != guest || guest.Room != room {
		t.Fatalf("survivor not promoted room=%v host=%v guestRoom=%v", GetRoom(room.ID), room.Host, guest.Room)
	}
	LeaveRoom(guest, "cleanup")
}

func TestHostLeaveSeatLobbyPromotesSurvivor(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "alphaui", 1000010)
	guest := newTestSession(t, "betaui", 1000011)
	room := CreateRoom(host, RoomOptions{Name: "temp", Capacity: 10, Flag1012: 1})
	if joined, why := JoinRoom(guest, room.ID, nil); joined == nil || why != "ok" {
		t.Fatalf("join failed: %v %s", joined, why)
	}
	// Seat lobby: State=open, MatchClockArmed=false — old code destroyed room + e02f.
	LeaveRoom(host, "lobby_disconnect")
	if GetRoom(room.ID) != room {
		t.Fatal("seat-lobby host leave destroyed room")
	}
	if room.Host != guest || guest.Room != room || guest.Seat != 1 {
		t.Fatalf("survivor host=%v room=%v seat=%d", room.Host, guest.Room, guest.Seat)
	}
	if host.Room != nil {
		t.Fatalf("leaver still in room=%v", host.Room)
	}
	LeaveRoom(guest, "cleanup")
	if GetRoom(room.ID) != nil {
		t.Fatal("last member leave did not destroy room")
	}
}

func TestMatchDisconnectHoldKeepsSeatAndRejectsJoin(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "enterpries1", 1000001)
	guest := newTestSession(t, "enterpries2", 1000002)
	stranger := newTestSession(t, "phone", 1000003)
	room := CreateRoom(host, RoomOptions{Name: "shared", Capacity: 10})
	if joined, why := JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("guest join room=%v why=%q", joined, why)
	}
	guest.SeatHeroID = 340
	guest.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.MatchSFrame = 2348
	room.MatchSynNext = 2432
	room.Unlock()

	gs, peer := net.Pipe()
	defer gs.Close()
	defer peer.Close()
	guest.AttachGS(gs)
	held, last, gotRoom := guest.DetachGSForDisconnect(gs, true, true, time.Second)
	if !held || !last || gotRoom != room {
		t.Fatalf("hold held=%v last=%v room=%v", held, last, gotRoom)
	}
	if !guest.MatchHold || guest.MatchHoldRoomID != room.ID || guest.MatchHoldSeat != 1 || guest.MatchHoldHero != 340 {
		t.Fatalf("hold snapshot active=%v room=%d seat=%d hero=%d",
			guest.MatchHold, guest.MatchHoldRoomID, guest.MatchHoldSeat, guest.MatchHoldHero)
	}
	if guest.IsMatchPlaying() || guest.IsMatchLoading() || room.MemberCount() != 2 || guest.Room != room || guest.Seat != 1 {
		t.Fatalf("held membership playing=%v loading=%v members=%d room=%v seat=%d",
			guest.IsMatchPlaying(), guest.IsMatchLoading(), room.MemberCount(), guest.Room, guest.Seat)
	}
	room.Lock()
	armed, frame, syn := room.MatchClockArmed, room.MatchSFrame, room.MatchSynNext
	room.Unlock()
	if !armed || frame != 2348 || syn != 2432 {
		t.Fatalf("hold changed clock armed=%v frame=%d syn=%d", armed, frame, syn)
	}
	if joined, why := JoinRoom(stranger, room.ID, nil); joined != nil || why != "match" || stranger.Room != nil {
		t.Fatalf("stranger match join room=%v why=%q strangerRoom=%v", joined, why, stranger.Room)
	}

	LeaveRoom(guest, "quit_e02e")
	if guest.MatchHold || guest.Room != nil {
		t.Fatalf("intentional leave retained hold=%v room=%v", guest.MatchHold, guest.Room)
	}
}

func TestLobbyEOFDuringMatchHoldDoesNotVacate(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "enterpries1", 1000001)
	guestLobby, guestPeer := net.Pipe()
	defer guestPeer.Close()
	guest := Create(guestLobby, "guest-lobby")
	guest.BindAccount(&accounts.Account{Username: "enterpries2", Nickname: "Enterpries2", UserID: 1000002})
	room := CreateRoom(host, RoomOptions{Name: "shared", Capacity: 10})
	if joined, why := JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("guest join room=%v why=%q", joined, why)
	}
	guest.SeatHeroID = 340
	guest.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.Unlock()
	gs, gsPeer := net.Pipe()
	defer gs.Close()
	defer gsPeer.Close()
	guest.AttachGS(gs)
	if held, _, _ := guest.DetachGSForDisconnect(gs, true, true, time.Minute); !held {
		t.Fatal("hold not entered")
	}
	Destroy(guestLobby)
	_ = guestLobby.Close()
	if guest.CurrentRoom() != room || !guest.IsMatchHeld() || room.MemberCount() != 2 {
		t.Fatalf("lobby EOF vacated hold room=%v held=%v members=%d",
			guest.CurrentRoom(), guest.IsMatchHeld(), room.MemberCount())
	}
	newGS, newPeer := net.Pipe()
	defer newGS.Close()
	defer newPeer.Close()
	sess, claim, why := ClaimMatchHold(fmt.Sprintf("hoc_r%d", room.ID), guest.GUID, newGS, time.Now())
	if sess != guest || why != "ok" || claim.Seat != guest.Seat {
		t.Fatalf("reclaim after lobby EOF sess=%v why=%q seat=%d", sess, why, claim.Seat)
	}
}

func TestMatchDisconnectHoldTimeoutVacatesMember(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	host := newTestSession(t, "enterpries1", 1000001)
	guest := newTestSession(t, "enterpries2", 1000002)
	room := CreateRoom(host, RoomOptions{Name: "shared", Capacity: 10})
	if joined, why := JoinRoom(guest, room.ID, nil); joined != room || why != "ok" {
		t.Fatalf("guest join room=%v why=%q", joined, why)
	}
	guest.SeatHeroID = 340
	guest.SetMatchPlaying(true)
	room.Lock()
	room.State = "match"
	room.MatchClockArmed = true
	room.Unlock()
	gs, peer := net.Pipe()
	defer gs.Close()
	defer peer.Close()
	guest.AttachGS(gs)
	held, _, _ := guest.DetachGSForDisconnect(gs, true, true, 20*time.Millisecond)
	if !held {
		t.Fatal("hold not entered")
	}
	deadline := time.Now().Add(time.Second)
	for guest.CurrentRoom() != nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if guest.CurrentRoom() != nil || guest.IsMatchHeld() || room.MemberCount() != 1 {
		t.Fatalf("expired hold room=%v active=%v members=%d", guest.CurrentRoom(), guest.IsMatchHeld(), room.MemberCount())
	}
}

func TestTokenRotationKeepsOnlyLatestIndex(t *testing.T) {
	resetStateForTest()
	t.Cleanup(resetStateForTest)
	sess := newTestSession(t, "enterpries1", 1000001)
	first := sess.EnsureToken()
	second := sess.AllocToken()
	if first == second || len(first) != 10 || len(second) != 10 {
		t.Fatalf("tokens first=%q second=%q", first, second)
	}
	if got, _ := ResolveGS(first, ""); got != nil {
		t.Fatalf("old token still resolves: %v", got)
	}
	if got, how := ResolveGS(second, ""); got != sess || how != "token" {
		t.Fatalf("latest token resolve got=%v how=%q", got, how)
	}
}

func newTestSession(t *testing.T, username string, userID int64) *Session {
	t.Helper()
	server, client := net.Pipe()
	sess := Create(server, "test")
	sess.BindAccount(&accounts.Account{
		Username: username, Nickname: username, UserID: userID,
		AccountID: strconv.FormatInt(userID, 10), Gateway: "127.0.0.1",
	})
	t.Cleanup(func() {
		Destroy(server)
		_ = server.Close()
		_ = client.Close()
	})
	return sess
}

func resetStateForTest() {
	sessMu.Lock()
	byConn = map[net.Conn]*Session{}
	byToken = map[string]*Session{}
	sessMu.Unlock()
	peerSeq = 0
	tokenSeq = 0
	roomMu.Lock()
	rooms = map[int]*Room{}
	nextRoom = config.DefaultRoomID
	roomMu.Unlock()
}
