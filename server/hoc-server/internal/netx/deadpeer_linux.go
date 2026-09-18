//go:build linux

package netx

import (
	"net"
	"syscall"
	"time"
)

// tcpUserTimeout is TCP_USER_TIMEOUT from <linux/tcp.h> (Linux ≥ 2.6.37);
// the value is milliseconds. Kept as a literal to avoid pulling x/sys.
const tcpUserTimeout = 18

func setUserTimeout(tc *net.TCPConn, timeout time.Duration) {
	raw, err := tc.SyscallConn()
	if err != nil {
		return
	}
	ms := int(timeout / time.Millisecond)
	_ = raw.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpUserTimeout, ms)
	})
}
