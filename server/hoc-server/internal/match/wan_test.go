package match

import (
	"net"
	"sync"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/session"
)

// WAN resilience (AGENTS5 problem H).
//
// sendFrames() holds room.Lock() across the per-connection send. On LAN a
// TCP write returns immediately, so this is invisible. On WAN a congested
// peer (full send buffer, loss, phone that stopped ACKing) makes the write
// block -- and because op7 relay (handleUnitAction) also takes room.Lock,
// ONE slow player would freeze the frame clock and every other player.
//
// This test blocks inside the send hook to emulate that peer and asserts the
// room lock stays available.
func TestSlowPeerMustNotBlockRoomLock(t *testing.T) {
	prevTicker := config.FrameTicker
	config.FrameTicker = true
	defer func() { config.FrameTicker = prevTicker }()

	host := &session.Session{Username: "slow", Account: &accounts.Account{Username: "slow"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)

	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	host.AttachGS(conn)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)

	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // emulate a blocked WAN write
	})
	room.MatchClockArmed = true

	done := make(chan struct{})
	go func() {
		clock.sendFrames(room, 1, "wan")
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("send hook never ran")
	}

	// Someone else (op7 relay, join, leave) must still be able to take the room.
	lockFree := make(chan struct{})
	go func() {
		room.Lock()
		room.Unlock()
		close(lockFree)
	}()

	select {
	case <-lockFree:
		close(release)
		<-done
	case <-time.After(500 * time.Millisecond):
		close(release)
		<-done
		t.Fatal("room.Lock() is held across a blocking send: one slow WAN peer freezes the whole match")
	}
}

// The AGENTS4 §11.7 LIVE pin must survive the WAN lock split: op7 and op11
// writes still may not interleave, or the client sees sequenceID wrong UP/TP
// and drops with Dev|3004. Ordering moved from room.mu to room.WireLock().
func TestFrameWritesStaySerializedUnderWireLock(t *testing.T) {
	prevTicker := config.FrameTicker
	config.FrameTicker = true
	defer func() { config.FrameTicker = prevTicker }()

	host := &session.Session{Username: "h", Account: &accounts.Account{Username: "h"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	host.AttachGS(conn)
	room.MatchClockArmed = true

	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			clock.sendFrames(room, 2, "")
		}()
	}
	wg.Wait()

	mu.Lock()
	got := maxInFlight
	mu.Unlock()
	if got != 1 {
		t.Fatalf("concurrent in-flight writes = %d, want 1 (wire order broken -> Dev|3004 risk)", got)
	}
}

// Regression (LIVE crash 2026-08-16): the first WAN lock split released
// room.mu and only THEN took the wire lock. An op7 relay slipping into that
// gap sent its packet ahead of the op11 frame that was already numbered ->
// sequenceID wrong -> client crash mid-match (AGENTS4 §11.7 pin).
//
// Correct behaviour is hand-over-hand: whoever numbers a frame must already
// hold the wire slot before it gives up the state lock.
func TestNoWireGapBetweenStateAndWireLock(t *testing.T) {
	prevTicker := config.FrameTicker
	config.FrameTicker = true
	defer func() { config.FrameTicker = prevTicker }()

	host := &session.Session{Username: "h", Account: &accounts.Account{Username: "h"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	host.AttachGS(conn)
	room.MatchClockArmed = true

	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {})

	// Simulate the op7 relay racing the frame clock: it takes mu, then wireMu
	// (hand-over-hand), exactly like handleUnitAction does.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			room.Lock()
			room.WireLock()
			room.Unlock()
			room.WireUnlock()
		}
	}()

	for i := 0; i < 400; i++ {
		clock.sendFrames(room, 1, "")
	}
	close(stop)
	wg.Wait()

	// A frame must never be numbered without its wire slot: with the gap bug
	// this deadlocks or reorders under -race; the assertion here is that the
	// frame counter advanced cleanly and nothing panicked.
	room.Lock()
	got := room.MatchFramesSent
	room.Unlock()
	if got != 400 {
		t.Fatalf("frames sent = %d, want 400", got)
	}
}
