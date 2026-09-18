//go:build !linux

package netx

import (
	"net"
	"time"
)

// Only Linux exposes TCP_USER_TIMEOUT; elsewhere (Windows dev rig) keepalive
// alone has to do, which still catches the idle case.
func setUserTimeout(*net.TCPConn, time.Duration) {}
