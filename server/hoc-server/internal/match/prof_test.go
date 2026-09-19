package match

import (
	"net"
	"testing"
	"time"

	"hoc-server/internal/accounts"
	"hoc-server/internal/config"
	"hoc-server/internal/netx"
	"hoc-server/internal/prof"
	"hoc-server/internal/session"
)

// HOC_PROFILE lag hunt: the per-tick write timing must name the peer whose
// socket stalled, not just report a slow tick. One healthy reader and one
// black-hole peer; the deadline fires on the black hole and the profiler has
// to attribute the slowest write to it.
func TestProfileNamesSlowestFrameWrite(t *testing.T) {
	prevTimeout := config.MatchWriteTimeout
	config.MatchWriteTimeout = 50 * time.Millisecond
	defer func() { config.MatchWriteTimeout = prevTimeout }()
	prof.Enable(true)
	defer prof.Enable(false)

	fast := &session.Session{Username: "fast", Account: &accounts.Account{Username: "fast"}}
	stuck := &session.Session{Username: "stuck", Account: &accounts.Account{Username: "stuck"}}
	room := &session.Room{Host: fast, Members: []*session.Session{fast, stuck}}
	fast.SetMatchPlaying(true)
	stuck.SetMatchPlaying(true)
	room.MatchClockArmed = true

	srv, cli := net.Pipe()
	defer cli.Close()
	go func() { // healthy peer: drains everything
		buf := make([]byte, 4096)
		for {
			if _, err := cli.Read(buf); err != nil {
				return
			}
		}
	}()
	fast.AttachGS(srv)
	hole := newBlackholeConn(0)
	stuck.AttachGS(hole)

	before := prof.Peek()
	clock := NewClock(func(c net.Conn, _, _ uint16, body []byte, _ bool) {
		netx.ApplyMatchWriteDeadline(c)
		_, err := c.Write(body)
		netx.ClearMatchWriteDeadline(c)
		prof.WriteErr(err)
	})
	clock.sendFrames(room, 1, "prof")
	srv.Close()

	got := prof.Peek()
	if got.FrameTicks-before.FrameTicks != 1 || got.FrameWrites-before.FrameWrites != 2 {
		t.Fatalf("expected 1 tick / 2 writes recorded, got %+v (before %+v)", got, before)
	}
	if got.FrameSlowestUser != "stuck" {
		t.Fatalf("slowest write attributed to %q, want the black-hole peer \"stuck\" (%+v)", got.FrameSlowestUser, got)
	}
	if got.FrameSlowest < config.MatchWriteTimeout {
		t.Fatalf("slowest write %s shorter than the deadline %s", got.FrameSlowest, config.MatchWriteTimeout)
	}
	if got.WriteTimeouts-before.WriteTimeouts != 1 {
		t.Fatalf("expected exactly one deadline timeout counted, got %+v", got)
	}
}
