package match

import (
	"net"
	"sync"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/netx"
	"hoc-server/internal/session"
)

// WAN resilience, part 2 (AGENTS5 problem H): unbounded writes.
//
// TestSlowPeerMustNotBlockRoomLock already proves a congested peer cannot hold
// room.mu. But the wire lock is still held across the sends, and the sends
// themselves have no deadline:
//
//	sendFrames: room.WireLock() ... for _, s := range sends { c.send(...) }
//	gs.sendSyn:  conn.Write(...)          <- no SetWriteDeadline anywhere
//
// So a peer whose TCP receive window is full (phone on a dead link, mobile
// data stall, laptop suspended) makes Write block indefinitely. It no longer
// freezes room STATE, but it does hold the room's wire slot forever, which
// stalls the frame clock for that room -- everyone in that match stops
// receiving op11.
//
// This is invisible on LAN: net.Pipe and a local socket always accept the
// write. The test below emulates the WAN case with a peer that never reads.

// blackholeConn accepts a bounded amount of data and then blocks forever,
// exactly like a real TCP socket whose peer stopped ACKing.
type blackholeConn struct {
	net.Conn
	mu       sync.Mutex
	accepted int
	limit    int
	deadline time.Time
	blocked  chan struct{}
	once     sync.Once
}

func newBlackholeConn(limit int) *blackholeConn {
	c, _ := net.Pipe()
	return &blackholeConn{Conn: c, limit: limit, blocked: make(chan struct{})}
}

func (b *blackholeConn) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.accepted += len(p)
	over := b.accepted > b.limit
	dl := b.deadline
	b.mu.Unlock()

	if !over {
		return len(p), nil
	}
	b.once.Do(func() { close(b.blocked) })

	// Past the window: block until the write deadline fires. With no deadline
	// set (dl zero) this blocks forever -- which is the bug under test.
	if dl.IsZero() {
		select {}
	}
	d := time.Until(dl)
	if d <= 0 {
		return 0, timeoutErr{}
	}
	time.Sleep(d)
	return 0, timeoutErr{}
}

func (b *blackholeConn) SetWriteDeadline(t time.Time) error {
	b.mu.Lock()
	b.deadline = t
	b.mu.Unlock()
	return nil
}
func (b *blackholeConn) Close() error { return nil }

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

// TestStalledPeerMustNotHoldWireSlotForever pins the fix for the remaining WAN
// hazard: a peer that stops reading must not park the room's wire lock, or the
// frame clock for that match stops for everybody.
//
// Expected behaviour: the send path sets a write deadline, so the stuck write
// returns an error within a bounded time and the wire slot is released.
func TestStalledPeerMustNotHoldWireSlotForever(t *testing.T) {
	prevTicker := config.FrameTicker
	config.FrameTicker = true
	defer func() { config.FrameTicker = prevTicker }()

	host := &session.Session{Username: "stuck", Account: &accounts.Account{Username: "stuck"}}
	room := &session.Room{Host: host, Members: []*session.Session{host}}
	host.SetMatchPlaying(true)

	stuck := newBlackholeConn(0) // block on the very first frame
	host.AttachGS(stuck)
	room.MatchClockArmed = true

	// Mirror the production send path: whatever the server does before the
	// raw Write is what decides whether a stalled peer can be shed.
	clock := NewClock(func(c net.Conn, _, _ uint16, body []byte, _ bool) {
		netx.ApplyMatchWriteDeadline(c)
		_, _ = c.Write(body)
		netx.ClearMatchWriteDeadline(c)
	})

	done := make(chan struct{})
	go func() {
		clock.sendFrames(room, 1, "wan-stall")
		close(done)
	}()

	select {
	case <-stuck.blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("write hook never reached the blocking state")
	}

	// The room's wire slot must come back without human intervention.
	freed := make(chan struct{})
	go func() {
		room.WireLock()
		room.WireUnlock()
		close(freed)
	}()

	select {
	case <-freed:
		<-done
	case <-time.After(3 * time.Second):
		t.Fatal("wire lock still held by a stalled peer after 3s: " +
			"one dead WAN connection stops the frame clock for the whole room " +
			"(missing SetWriteDeadline on the in-match send path)")
	}
}
