//go:build linux

package prof

import "syscall"

// processCPU returns cumulative user/system CPU seconds of this process.
func processCPU() (user, sys float64) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return -1, -1
	}
	tv := func(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
	return tv(ru.Utime), tv(ru.Stime)
}
