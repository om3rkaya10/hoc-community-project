package netx

import (
	"net"
	"time"

	"hoc-server/internal/config"
)

// ApplyGSDeadPeerDetection makes a silently vanished peer surface as a read
// error within roughly config.GSUserTimeout instead of the kernel's
// retransmit ceiling (~15 min on Linux).
//
// LIVE 2026-09-18: a phone changed networks without sending RST. The GS
// socket stayed ESTABLISHED on the server (unacked:1, retransmitting) for
// minutes; the 2 s match write deadline never fired because 30 B frames
// never fill the send buffer. No EOF → no reconnect hold → every ReLoginReq
// from the new socket was rejected with "no-hold".
//
// Two mechanisms, because they cover different states:
//   - TCP keepalive: probes an idle connection (nothing in flight).
//   - TCP_USER_TIMEOUT (Linux only, see deadpeer_linux.go): aborts when data
//     in flight stays unacked longer than the timeout — the in-match case,
//     where frames are always in flight.
func ApplyGSDeadPeerDetection(c net.Conn) {
	timeout := config.GSUserTimeout
	if c == nil || timeout <= 0 {
		return
	}
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	period := timeout / 2
	if period < time.Second {
		period = time.Second
	}
	_ = tc.SetKeepAlive(true)
	_ = tc.SetKeepAlivePeriod(period)
	setUserTimeout(tc, timeout)
}
