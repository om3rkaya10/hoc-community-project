package match

import (
	"net"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/netx"
	"hoc-server/internal/session"
)

// WAN resilience, part 3: does ONE dead peer stop the match for the SURVIVOR?
//
// This is the scenario the user reported for problem B (mid-match reconnect):
// "when a player dropped, BOTH sides broke -- the dropper could not get back
// in, and the players still in the match broke too".
//
// The second half of that (survivors breaking) is a server-side property and
// can be measured without a game: give a two-player room one healthy peer and
// one peer that stopped reading, then check the healthy peer keeps receiving
// frames.
//
// Without a write deadline the stalled peer parks the room's wire slot and the
// survivor's frame stream stops dead -- which matches the archived symptom
// ("op7 paused / yürüyüş kırıldı", AGENTS5 §2.1.1).
func TestDeadPeerMustNotStopFramesForSurvivor(t *testing.T) {
	prevTicker := config.FrameTicker
	config.FrameTicker = true
	defer func() { config.FrameTicker = prevTicker }()

	dead := &session.Session{Username: "dead", Account: &accounts.Account{Username: "dead"}}
	alive := &session.Session{Username: "alive", Account: &accounts.Account{Username: "alive"}}
	room := &session.Room{Host: alive, Members: []*session.Session{alive, dead}}
	dead.SetMatchPlaying(true)
	alive.SetMatchPlaying(true)

	stuck := newBlackholeConn(0) // stops accepting immediately
	dead.AttachGS(stuck)

	healthy := newCountingConn()
	alive.AttachGS(healthy)

	room.MatchClockArmed = true

	clock := NewClock(func(c net.Conn, _, _ uint16, body []byte, _ bool) {
		netx.ApplyMatchWriteDeadline(c)
		_, _ = c.Write(body)
		netx.ClearMatchWriteDeadline(c)
	})

	// Drive several frames, as the ticker would.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 3; i++ {
			clock.sendFrames(room, 1, "")
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("frame loop wedged: a dead peer is blocking the whole room")
	}

	if got := healthy.count(); got < 3 {
		t.Fatalf("survivor received %d frames, want >=3: "+
			"one dead peer is starving the healthy player's frame stream", got)
	}
}

// countingConn accepts every write instantly, like a healthy local peer, and
// records how many frames actually reached it.
type countingConn struct {
	net.Conn
	n chan struct{}
}

func newCountingConn() *countingConn {
	c, _ := net.Pipe()
	return &countingConn{Conn: c, n: make(chan struct{}, 256)}
}

func (c *countingConn) Write(p []byte) (int, error) {
	select {
	case c.n <- struct{}{}:
	default:
	}
	return len(p), nil
}
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *countingConn) Close() error                     { return nil }
func (c *countingConn) count() int                       { return len(c.n) }
