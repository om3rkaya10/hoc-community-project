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

// useLegacyPump pins a test to the pre-2026-08-16 recv-timeout pump path.
// Those tests assert pump-specific semantics (master gating, StartPlay
// barrier) that are intentionally inert while the dedicated ticker owns a
// room, so they must opt out of the ticker explicitly.
func useLegacyPump(t *testing.T) {
	t.Helper()
	prev := config.FrameTicker
	config.FrameTicker = false
	t.Cleanup(func() { config.FrameTicker = prev })
}

func TestSharedClockMasterFailoverAndIdleDisarm(t *testing.T) {
	useLegacyPump(t)
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	guest := &session.Session{Username: "guest", Account: &accounts.Account{Username: "guest"}}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}}
	host.SetMatchPlaying(true)
	guest.SetMatchPlaying(true)
	guestConn, guestPeer := net.Pipe()
	defer guestConn.Close()
	defer guestPeer.Close()
	guest.AttachGS(guestConn)

	var mu sync.Mutex
	sends := 0
	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {
		mu.Lock()
		sends++
		mu.Unlock()
	})
	clock.Arm(room)
	if !clock.IsMaster(room, host) || clock.IsMaster(room, guest) {
		t.Fatalf("host master=%v guest master=%v", clock.IsMaster(room, host), clock.IsMaster(room, guest))
	}

	host.SetMatchPlaying(false)
	if clock.IsMaster(room, host) || !clock.IsMaster(room, guest) {
		t.Fatalf("failover host master=%v guest master=%v", clock.IsMaster(room, host), clock.IsMaster(room, guest))
	}
	clock.PumpOnTimeoutFor(room, host)
	clock.PumpOnTimeoutFor(room, guest)
	mu.Lock()
	got := sends
	mu.Unlock()
	if got != 1 {
		t.Fatalf("master-gated sends=%d want=1", got)
	}

	guest.SetMatchPlaying(false)
	if clock.DisarmIfIdle(room) != true || room.MatchClockArmed {
		t.Fatalf("idle disarm armed=%v", room.MatchClockArmed)
	}
	clock.PumpOnTimeoutFor(room, guest)
	mu.Lock()
	got = sends
	mu.Unlock()
	if got != 1 {
		t.Fatalf("idle clock emitted frame sends=%d", got)
	}
}

func TestSharedClockWaitsForLateStartPlay(t *testing.T) {
	useLegacyPump(t)
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	guest := &session.Session{Username: "guest", Account: &accounts.Account{Username: "guest"}}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}}
	host.SetMatchPlaying(true)
	guest.SetMatchLoading(true)
	hostConn, hostPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	host.AttachGS(hostConn)

	sends := 0
	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) { sends++ })
	clock.Arm(room)
	clock.PumpOnTimeoutFor(room, host)
	if sends != 0 || room.MatchSFrame != 0 || room.MatchSynNext != 0 {
		t.Fatalf("late guest advanced shared clock sends=%d frame=%d syn=%d",
			sends, room.MatchSFrame, room.MatchSynNext)
	}

	guest.SetMatchLoading(false)
	guest.SetMatchPlaying(true)
	clock.PumpOnTimeoutFor(room, host)
	if sends != 1 || room.MatchSFrame != 1 || room.MatchSynNext != 1 {
		t.Fatalf("clock did not open after both StartPlay sends=%d frame=%d syn=%d",
			sends, room.MatchSFrame, room.MatchSynNext)
	}
}

func TestSharedClockContinuesWhenPeerHeldOrSilent(t *testing.T) {
	useLegacyPump(t)
	// Pre-reconnect pin: survivor walk/op11 must not freeze for peer hold/stall.
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	guest := &session.Session{Username: "guest", Account: &accounts.Account{Username: "guest"}}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}}
	host.SetMatchPlaying(true)
	guest.MatchHold = true
	hostConn, hostPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	host.AttachGS(hostConn)

	sends := 0
	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) { sends++ })
	clock.Arm(room)
	clock.PumpOnTimeoutFor(room, host)
	if sends != 1 || room.MatchSFrame != 1 {
		t.Fatalf("hold froze survivor clock sends=%d frame=%d", sends, room.MatchSFrame)
	}

	guest.MatchHold = false
	guest.MatchAwaitResumePing = true
	guest.SetMatchPlaying(true)
	guest.LastGSActivity = time.Now().Add(-time.Hour)
	clock.PumpOnTimeoutFor(room, host)
	if sends != 2 || room.MatchSFrame != 2 {
		t.Fatalf("await/stall froze survivor clock sends=%d frame=%d", sends, room.MatchSFrame)
	}
}

func TestSharedClockSkipsOnlyAwaitingReconnectPeer(t *testing.T) {
	useLegacyPump(t)
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	guest := &session.Session{Username: "guest", Account: &accounts.Account{Username: "guest"}}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}}
	host.SetMatchPlaying(true)
	guest.SetMatchPlaying(true)
	guest.MatchAwaitResumePing = true

	hostConn, hostPeer := net.Pipe()
	guestConn, guestPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	defer guestConn.Close()
	defer guestPeer.Close()
	host.AttachGS(hostConn)
	guest.AttachGS(guestConn)

	var mu sync.Mutex
	byConn := map[net.Conn]int{}
	clock := NewClock(func(conn net.Conn, _ uint16, _ uint16, _ []byte, _ bool) {
		mu.Lock()
		byConn[conn]++
		mu.Unlock()
	})
	clock.Arm(room)
	clock.PumpOnTimeoutFor(room, host)

	mu.Lock()
	hostSends := byConn[hostConn]
	guestSends := byConn[guestConn]
	mu.Unlock()
	if hostSends != 1 || guestSends != 0 {
		t.Fatalf("awaiting reconnect peer received clock: host=%d guest=%d", hostSends, guestSends)
	}
	if room.MatchSFrame != 1 || room.MatchSynNext != 1 {
		t.Fatalf("survivor clock did not advance frame=%d syn=%d", room.MatchSFrame, room.MatchSynNext)
	}

	guest.NoteSoftResumeStable()
	clock.PumpOnTimeoutFor(room, host)
	mu.Lock()
	guestSends = byConn[guestConn]
	mu.Unlock()
	if guestSends != 1 {
		t.Fatalf("reconnected peer did not re-enter clock after op3: sends=%d", guestSends)
	}
}

func TestDisarmIfIdleKeepsClockForSurvivor(t *testing.T) {
	host := &session.Session{Username: "host"}
	guest := &session.Session{Username: "guest"}
	room := &session.Room{Host: host, Members: []*session.Session{host, guest}}
	host.SetMatchPlaying(true)
	guest.SetMatchPlaying(true)
	clock := NewClock(nil)
	clock.Arm(room)

	host.SetMatchPlaying(false)
	if clock.DisarmIfIdle(room) {
		t.Fatal("clock disarmed while guest was still playing")
	}
	if !room.MatchClockArmed {
		t.Fatal("shared clock was cleared with survivor")
	}
}

// --- Dedicated frame ticker (2026-08-16, AGENTS5 problem C) ---------------
//
// Root cause of the walk stutter: the legacy clock advanced one frame per GS
// recv timeout, so a walking client (~28 pkt/s) kept Read() busy and the
// clock ran at a measured 20.0 Hz against FrameHZ=30. These lock in the
// ticker contract.

// The ticker must advance frames on wall time even when the socket never
// times out (i.e. while the player is walking).
func TestFrameTickerAdvancesWithoutRecvTimeout(t *testing.T) {
	if !config.FrameTicker {
		t.Skip("HOC_FRAME_TICKER disabled")
	}
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)
	hostConn, hostPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	host.AttachGS(hostConn)

	var mu sync.Mutex
	sends := 0
	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {
		mu.Lock()
		sends++
		mu.Unlock()
	})
	clock.Arm(room)
	defer clock.Disarm(room)

	// One second of wall time with ZERO recv timeouts (no PumpOnTimeout call).
	time.Sleep(1000 * time.Millisecond)

	mu.Lock()
	got := sends
	mu.Unlock()

	// Expect ~FrameHZ; allow scheduler slack but reject the old ~20 Hz.
	if got < config.FrameHZ-7 || got > config.FrameHZ+7 {
		t.Fatalf("ticker produced %d frames/s, want ~%d (legacy recv-pump measured 20)", got, config.FrameHZ)
	}
	room.Lock()
	total := room.MatchFramesSent
	room.Unlock()
	if total == 0 {
		t.Fatal("room frame counter did not advance")
	}
}

// Frames must have exactly ONE source: while the ticker owns a room the
// recv-timeout pump must be inert (double pump = "kopma sınıfı", AGENTS4:549).
func TestFrameTickerSuppressesRecvTimeoutPump(t *testing.T) {
	if !config.FrameTicker {
		t.Skip("HOC_FRAME_TICKER disabled")
	}
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)
	hostConn, hostPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	host.AttachGS(hostConn)

	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {})
	clock.Arm(room)
	defer clock.Disarm(room)

	if !clock.tickerOwns(room) {
		t.Fatal("ticker should own the room after Arm")
	}

	room.Lock()
	before := room.MatchFramesSent
	room.Unlock()

	// Hammer the legacy pump: it must not add a single frame.
	for i := 0; i < 50; i++ {
		clock.PumpOnTimeoutFor(room, host)
	}

	room.Lock()
	after := room.MatchFramesSent
	room.Unlock()
	if after != before {
		t.Fatalf("recv-timeout pump advanced frames %d->%d while ticker owns room", before, after)
	}
}

// Disarm must stop the owner goroutine (no ghost clock after the match ends).
func TestFrameTickerStopsOnDisarm(t *testing.T) {
	if !config.FrameTicker {
		t.Skip("HOC_FRAME_TICKER disabled")
	}
	host := &session.Session{Username: "host", Account: &accounts.Account{Username: "host"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)
	hostConn, hostPeer := net.Pipe()
	defer hostConn.Close()
	defer hostPeer.Close()
	host.AttachGS(hostConn)

	clock := NewClock(func(net.Conn, uint16, uint16, []byte, bool) {})
	clock.Arm(room)
	time.Sleep(100 * time.Millisecond)
	clock.Disarm(room)

	if clock.tickerOwns(room) {
		t.Fatal("ticker still registered after Disarm")
	}
	room.Lock()
	frozen := room.MatchFramesSent
	room.Unlock()

	time.Sleep(150 * time.Millisecond)
	room.Lock()
	later := room.MatchFramesSent
	room.Unlock()
	if later != frozen {
		t.Fatalf("ghost clock kept running after Disarm: %d -> %d", frozen, later)
	}
}
