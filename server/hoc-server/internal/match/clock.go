package match

import (
	"fmt"
	"net"
	"sort"
	"sync"
	"time"

	"hoc-server/internal/config"
	"hoc-server/internal/session"
	wiregs "hoc-server/internal/wire/gs"
)

type Sender func(conn net.Conn, opcode, sub uint16, body []byte, inmatch bool)

// Clock drives the op11 gameplay frame stream for a room.
//
// Two mutually exclusive sources, never both (double pump = "kopma sınıfı",
// AGENTS4:549):
//
//   - config.FrameTicker=true  → one owner goroutine per room, time.Ticker at
//     FrameHZ. PumpOnTimeout* becomes a no-op for ticker-driven rooms.
//   - config.FrameTicker=false → legacy recv-timeout pump (pre-2026-08-16).
type Clock struct {
	mu      sync.Mutex
	armed   map[*session.Room]time.Time // arm wall time / last pump anchor
	tickers map[*session.Room]*roomTicker
	send    Sender
}

// roomTicker owns frame generation for exactly one room.
type roomTicker struct {
	stop chan struct{}
	done chan struct{}
	// stats guarded by Clock.mu
	ticks   int
	lateSum time.Duration
	lateMax time.Duration
	gaps    []time.Duration
}

func NewClock(send Sender) *Clock {
	return &Clock{
		armed:   map[*session.Room]time.Time{},
		tickers: map[*session.Room]*roomTicker{},
		send:    send,
	}
}

// Stop tears down every running room ticker (server shutdown).
func (c *Clock) Stop() {
	c.mu.Lock()
	rooms := make([]*session.Room, 0, len(c.tickers))
	for room := range c.tickers {
		rooms = append(rooms, room)
	}
	c.mu.Unlock()
	for _, room := range rooms {
		c.stopTicker(room)
	}
}

func (c *Clock) Arm(room *session.Room) {
	if room == nil || !config.FrameClock {
		return
	}
	room.Lock()
	if !room.MatchClockArmed {
		room.MatchSynNext = 0
		room.MatchSFrame = 0
		room.MatchFramesSent = 0
		room.MatchClockArmed = true
	}
	room.Unlock()
	c.mu.Lock()
	c.armed[room] = time.Now()
	c.mu.Unlock()
	if config.FrameTicker {
		c.startTicker(room)
		return
	}
	fmt.Printf(" [MATCH] op11 clock ARMED hz=%d (recv-timeout pump, no thread)\n", config.FrameHZ)
}

// startTicker launches the single owner goroutine for a room (idempotent).
func (c *Clock) startTicker(room *session.Room) {
	c.mu.Lock()
	if _, running := c.tickers[room]; running {
		c.mu.Unlock()
		return
	}
	rt := &roomTicker{stop: make(chan struct{}), done: make(chan struct{})}
	c.tickers[room] = rt
	c.mu.Unlock()

	period := time.Second / time.Duration(config.FrameHZ)
	fmt.Printf(" [MATCH] op11 clock ARMED hz=%d period=%s (dedicated room ticker)\n",
		config.FrameHZ, period)

	go func() {
		defer close(rt.done)
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		prev := time.Now()

		for {
			select {
			case <-rt.stop:
				return
			case now := <-ticker.C:
				// Wall-clock catch-up: if the tick was delayed (GC, scheduler,
				// blocking send) emit the frames we owe, capped at CatchupMax.
				gap := now.Sub(prev)
				prev = now
				due := int(gap / period)
				if due < 1 {
					due = 1
				}
				if due > config.CatchupMax {
					due = config.CatchupMax
				}

				late := gap - period
				if late < 0 {
					late = 0
				}
				c.mu.Lock()
				rt.ticks++
				rt.lateSum += late
				if late > rt.lateMax {
					rt.lateMax = late
				}
				if len(rt.gaps) < 4096 {
					rt.gaps = append(rt.gaps, gap)
				}
				stats := rt.ticks%(config.FrameHZ*10) == 0
				snapshot := *rt
				c.mu.Unlock()

				if stats {
					c.reportJitter(&snapshot, period)
				}

				if !c.tickOnce(room, due) {
					return // room finished → owner exits
				}
			}
		}
	}()
}

// tickOnce emits due frames; reports false when the room is no longer active.
func (c *Clock) tickOnce(room *session.Room, due int) bool {
	c.mu.Lock()
	_, armed := c.armed[room]
	c.mu.Unlock()
	if !armed {
		return false
	}
	if !c.AllPlayersStarted(room) {
		return true // barrier: hold the counter, keep the owner alive
	}
	c.sendFrames(room, due, "")
	room.Lock()
	stillArmed := room.MatchClockArmed
	room.Unlock()
	return stillArmed
}

// reportJitter logs measured spacing so "feel" claims can be checked against
// numbers (target = 1/FrameHZ).
func (c *Clock) reportJitter(rt *roomTicker, period time.Duration) {
	if len(rt.gaps) == 0 {
		return
	}
	gaps := append([]time.Duration(nil), rt.gaps...)
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	p := func(q float64) time.Duration {
		idx := int(float64(len(gaps)-1) * q)
		return gaps[idx]
	}
	fmt.Printf(" [MATCH] op11 jitter ticks=%d target=%s p50=%s p95=%s max=%s lateAvg=%s lateMax=%s\n",
		rt.ticks, period.Round(time.Microsecond),
		p(0.50).Round(time.Microsecond), p(0.95).Round(time.Microsecond),
		gaps[len(gaps)-1].Round(time.Microsecond),
		(rt.lateSum / time.Duration(rt.ticks)).Round(time.Microsecond),
		rt.lateMax.Round(time.Microsecond))
}

// stopTicker halts a room's owner goroutine and waits for it to exit.
func (c *Clock) stopTicker(room *session.Room) {
	c.mu.Lock()
	rt, running := c.tickers[room]
	if running {
		delete(c.tickers, room)
	}
	c.mu.Unlock()
	if !running {
		return
	}
	close(rt.stop)
	<-rt.done
}

func (c *Clock) Disarm(room *session.Room) {
	if room == nil {
		return
	}
	room.Lock()
	room.MatchClockArmed = false
	room.Unlock()
	c.mu.Lock()
	delete(c.armed, room)
	c.mu.Unlock()
	c.stopTicker(room)
}

// DisarmIfIdle keeps the shared clock alive when one peer's GS socket closes.
// The room is only disarmed after the last match-playing member is gone.
func (c *Clock) DisarmIfIdle(room *session.Room) bool {
	if room == nil {
		return true
	}
	room.Lock()
	alive := false
	for _, member := range room.Members {
		if member != nil && member.IsMatchPlaying() {
			alive = true
			break
		}
	}
	if alive {
		room.Unlock()
		return false
	}
	room.MatchClockArmed = false
	room.Unlock()
	c.mu.Lock()
	delete(c.armed, room)
	c.mu.Unlock()
	c.stopTicker(room)
	return true
}

// IsMaster applies the LIVE shared-clock rule: the room host pumps while
// playing; after host leave, the first remaining playing member takes over.
func (c *Clock) IsMaster(room *session.Room, source *session.Session) bool {
	if room == nil || source == nil {
		return false
	}
	room.Lock()
	defer room.Unlock()
	if room.Host != nil && room.Host.IsMatchPlaying() {
		return room.Host == source
	}
	for _, member := range room.Members {
		if member != nil && member.IsMatchPlaying() {
			return member == source
		}
	}
	return false
}

// PumpOnTimeoutFor is the recv-timeout pump gated by the shared clock master.
// No-op when the dedicated ticker owns this room — frames must have exactly
// one source (ticker + pump = double advance, "kopma sınıfı" AGENTS4:549).
func (c *Clock) PumpOnTimeoutFor(room *session.Room, source *session.Session) {
	if c.tickerOwns(room) {
		return
	}
	if !c.IsMaster(room, source) {
		return
	}
	if !c.AllPlayersStarted(room) {
		return
	}
	c.PumpOnTimeout(room)
}

// tickerOwns reports whether a dedicated owner goroutine drives this room.
func (c *Clock) tickerOwns(room *session.Room) bool {
	if room == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, running := c.tickers[room]
	return running
}

// AllPlayersStarted is the shared LoadMap/StartPlay barrier. A member still
// marked loading must receive its first StartPlay before the room syn counter
// advances; a member that already left GS is no longer a barrier.
func (c *Clock) AllPlayersStarted(room *session.Room) bool {
	if room == nil {
		return true
	}
	room.Lock()
	defer room.Unlock()
	members := make([]*session.Session, 0, len(room.Members))
	for _, member := range room.Members {
		if member != nil && member.Account != nil {
			members = append(members, member)
		}
	}
	if len(members) < 2 {
		return true
	}
	for _, member := range members {
		if member.IsMatchPlaying() {
			continue
		}
		if member.IsMatchLoading() {
			return false
		}
		// Pre-reconnect: dropped peers are !playing and must not freeze the
		// survivor clock. Hold/stall room-wide freeze removed (broke walk).
	}
	return true
}

// PumpOnTimeout — call from GS read timeout while match_playing (1 frame @ FrameHZ).
func (c *Clock) PumpOnTimeout(room *session.Room) {
	if room == nil || !config.FrameClock {
		return
	}
	c.mu.Lock()
	_, ok := c.armed[room]
	c.mu.Unlock()
	if !ok {
		return
	}
	// Exactly one frame per timeout wake — mirrors Python INMATCH_FRAMES_PER_HB=0 timer path.
	c.sendFrames(room, 1, "")
}

// CatchUp — only if we fell behind (e.g. long blocking work); cap CatchupMax.
func (c *Clock) CatchUp(room *session.Room, due int) {
	if due > config.CatchupMax {
		due = config.CatchupMax
	}
	if due > 1 {
		c.sendFrames(room, due-1, "catchup")
	}
}

func (c *Clock) sendFrames(room *session.Room, n int, tag string) {
	if room == nil || n <= 0 {
		return
	}
	// Frame numbering mutates room state and must stay under the lock, but the
	// actual writes must NOT: a congested WAN peer blocks in Write, and op7
	// relay (handleUnitAction) takes the same lock — one slow player would
	// freeze the clock and every other player in the match. Build the batch
	// under the lock, release it, then send.
	// See AGENTS5 §2.3.5 / TestSlowPeerMustNotBlockRoomLock.
	type frameSend struct {
		conn net.Conn
		body []byte
	}
	var sends []frameSend
	var f0, f1, syn0, syn1, total, alive int
	held := false

	func() {
		room.Lock()
		defer func() {
			if !held {
				room.Unlock()
			}
		}()
		if !room.MatchClockArmed {
			return
		}
		// No playing members → disarm (stop ghost clock after EOF).
		for _, m := range room.Members {
			if m != nil && m.IsMatchPlaying() {
				alive++
			}
		}
		if alive == 0 {
			room.MatchClockArmed = false
			return
		}

		f0 = room.MatchSFrame + 1
		syn0 = room.MatchSynNext
		var bodies [][]byte
		for i := 0; i < n; i++ {
			room.MatchSFrame++
			syn := room.MatchSynNext
			body := wiregs.GameplayFrame(int32(room.MatchSFrame), int32(syn), 1)
			room.AppendMatchReplayLocked(session.MatchReplayPacket{
				Syn: syn, Opcode: 11, Sub: 0, Body: body,
			})
			if config.SynConsume {
				room.MatchSynNext++
			}
			room.MatchFramesSent++
			bodies = append(bodies, body)
		}
		members := append([]*session.Session(nil), room.Members...)
		f1 = room.MatchSFrame
		syn1 = room.MatchSynNext - 1
		if syn1 < syn0 {
			syn1 = syn0
		}
		total = room.MatchFramesSent
		for _, body := range bodies {
			for _, dest := range members {
				// A held/disconnected peer must not freeze the survivor clock, but a
				// just-acked rejoiner must not receive the room's far-ahead op11
				// stream until its first op3 opens the soft-resume path. This is a
				// per-peer skip, never the forbidden room-wide reconnect freeze.
				if dest == nil || !dest.IsMatchPlaying() || dest.IsMatchClockFrozen() {
					continue
				}
				for _, gc := range dest.SnapshotGSConns() {
					sends = append(sends, frameSend{conn: gc, body: body})
				}
			}
		}
		if len(sends) > 0 {
			// Hand-over-hand: claim the wire slot BEFORE releasing state, so an
			// op7 relay cannot slip its packet ahead of the frame we just
			// numbered (sequenceID wrong -> Dev|3004, AGENTS4 §11.7).
			room.WireLock()
			held = true
			room.Unlock()
		}
	}()

	if len(sends) == 0 {
		return
	}
	for _, s := range sends {
		c.send(s.conn, 11, 0, s.body, true)
	}
	room.WireUnlock()
	if tag != "" || total <= config.FrameHZ || total%config.FrameHZ == 0 {
		fmt.Printf(" [MATCH] op11 SHARED x%d frames=%d..%d syn=%d..%d total=%d members=%d %s\n",
			n, f0, f1, syn0, syn1, total, alive, tag)
	}
}
