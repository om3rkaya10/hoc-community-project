package netx

import (
	"net"
	"time"

	"hoc-server/internal/config"
)

// ApplyMatchWriteDeadline bounds a single in-match socket write (op7/op11).
//
// WAN hazard (AGENTS5 problem H). In-match writes are serialized per room by
// room.WireLock() so op7 and op11 cannot overtake each other (AGENTS4 §11.7).
// That lock is correct, but it means a write that never returns parks the
// room's wire slot forever: the frame clock for that match stops for EVERY
// player because one peer stopped reading.
//
// A blocked write is normal on WAN and impossible to reproduce on LAN: if a
// phone loses signal, suspends, or its receive window fills, the kernel send
// buffer backs up and Write blocks until the socket is torn down — which can
// be minutes with default TCP timeouts.
//
// With a deadline the stuck write returns a timeout error, the send loop moves
// on, the wire slot is released, and the dead peer is reaped by the normal
// EOF/leave path instead of taking the room down with it.
//
// Deliberately NOT applied to lobby/trade sockets: those are request/response,
// so a slow client there only delays itself.
func ApplyMatchWriteDeadline(c net.Conn) {
	if c == nil || config.MatchWriteTimeout <= 0 {
		return
	}
	_ = c.SetWriteDeadline(time.Now().Add(config.MatchWriteTimeout))
}

// ClearMatchWriteDeadline removes the bound again so a healthy connection is
// not left carrying a stale deadline into a later, unrelated write.
func ClearMatchWriteDeadline(c net.Conn) {
	if c == nil || config.MatchWriteTimeout <= 0 {
		return
	}
	_ = c.SetWriteDeadline(time.Time{})
}
