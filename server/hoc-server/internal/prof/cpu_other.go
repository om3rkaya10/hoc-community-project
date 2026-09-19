//go:build !linux

package prof

// processCPU is unavailable off Linux (Windows dev rig); only the runtime's
// own goCPU estimate is printed there.
func processCPU() (user, sys float64) { return -1, -1 }
