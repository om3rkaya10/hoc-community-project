// Package prof is the HOC_PROFILE=1 lag-hunt instrumentation (2026-09-19).
//
// It answers the questions the VPS journal could not: the op11 jitter report
// only times the tick FIRING, not the writes behind it. Everything here is
// off unless HOC_PROFILE=true, in which case one block per 10 s window is
// printed with:
//
//   - op11 frame send: wall time of the whole per-tick write loop (p50/p95/
//     max), the slowest single socket write in the window and whose it was,
//     and how long the ticker waited for the room wire lock (an op7 relay in
//     progress holds it).
//   - op7 relay: recv → last peer write (p50/p95/max), wire-lock wait, and
//     the cost of the per-relay fmt.Printf (journald suspect).
//   - in-match write errors, split into deadline timeouts vs other.
//   - accounts.json rewrites: count, max duration, size.
//   - Go runtime: GC cycles, GC pause max, GC CPU share, heap, goroutines,
//     process CPU (Linux getrusage; runtime estimate elsewhere).
//
// HOC_PPROF_ADDR=127.0.0.1:6060 additionally serves net/http/pprof; the
// address must be loopback, anything else is refused (never public).
package prof

import (
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"runtime/metrics"
	"sort"
	"sync"
	"time"

	"hoc-server/internal/config"
)

const window = 10 * time.Second

type hist struct {
	n   int
	sum time.Duration
	max time.Duration
	v   []time.Duration // capped sample for percentiles
}

func (h *hist) add(d time.Duration) {
	h.n++
	h.sum += d
	if d > h.max {
		h.max = d
	}
	if len(h.v) < 8192 {
		h.v = append(h.v, d)
	}
}

func (h *hist) pct(q float64) time.Duration {
	if len(h.v) == 0 {
		return 0
	}
	v := append([]time.Duration(nil), h.v...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[int(float64(len(v)-1)*q)]
}

func (h *hist) String() string {
	if h.n == 0 {
		return "n=0"
	}
	return fmt.Sprintf("n=%d p50=%s p95=%s max=%s", h.n,
		h.pct(0.5).Round(time.Microsecond), h.pct(0.95).Round(time.Microsecond),
		h.max.Round(time.Microsecond))
}

type stats struct {
	frameSend   hist // whole write loop of one tick
	frameWrites int  // socket writes issued by the ticker
	frameSlow   time.Duration
	frameSlowU  string
	frameWait   hist // ticker waiting for WireLock

	op7      hist // recv → last peer write
	op7Wait  hist // op7 waiting for WireLock
	op7Log   hist // fmt.Printf per relay
	op7Peers int

	wrTimeout int
	wrErr     int

	saves    int
	saveMax  time.Duration
	saveSize int
}

var (
	enabled bool
	mu      sync.Mutex
	cur     stats
)

// Enabled reports whether HOC_PROFILE is on; callers skip the timing calls
// (and the time.Now() pairs) when it is off.
func Enabled() bool { return enabled }

// Enable switches the instrumentation on or off without the env (tests).
func Enable(on bool) { enabled = on }

// Snapshot is the part of the current window a test can assert on.
type Snapshot struct {
	FrameTicks       int
	FrameWrites      int
	FrameSlowest     time.Duration
	FrameSlowestUser string
	WriteTimeouts    int
	WriteErrors      int
}

// Peek returns the current window's counters without resetting them.
func Peek() Snapshot {
	mu.Lock()
	defer mu.Unlock()
	return Snapshot{
		FrameTicks: cur.frameSend.n, FrameWrites: cur.frameWrites,
		FrameSlowest: cur.frameSlow, FrameSlowestUser: cur.frameSlowU,
		WriteTimeouts: cur.wrTimeout, WriteErrors: cur.wrErr,
	}
}

// Start reads the env flags and, when enabled, launches the 10 s reporter and
// the optional loopback pprof listener. Call once from main.
func Start() {
	enabled = config.Profile
	if addr := config.PprofAddr; addr != "" {
		host, _, err := net.SplitHostPort(addr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() {
			fmt.Printf(" [PROF] HOC_PPROF_ADDR=%q refused: must be a loopback host:port\n", addr)
		} else {
			go func() {
				fmt.Printf(" [PROF] pprof on http://%s/debug/pprof/\n", addr)
				if err := http.ListenAndServe(addr, nil); err != nil {
					fmt.Printf(" [PROF] pprof listener: %v\n", err)
				}
			}()
		}
	}
	if !enabled {
		return
	}
	fmt.Printf(" [PROF] HOC_PROFILE on: window=%s GOMAXPROCS=%d\n", window, runtime.GOMAXPROCS(0))
	go reporter()
}

// FrameSend records one op11 tick: the wall time of the write loop, how many
// socket writes it issued, the slowest single write and its owner, and how
// long the ticker waited for the room wire lock before it could start.
func FrameSend(total time.Duration, writes int, slowest time.Duration, slowestUser string, wireWait time.Duration) {
	mu.Lock()
	cur.frameSend.add(total)
	cur.frameWrites += writes
	if slowest > cur.frameSlow {
		cur.frameSlow, cur.frameSlowU = slowest, slowestUser
	}
	cur.frameWait.add(wireWait)
	mu.Unlock()
}

// Op7Relay records one unit-action relay: recv → last peer write, the wait
// for the wire lock inside it, the cost of its log line, and the peer count.
func Op7Relay(total, wireWait, logCost time.Duration, peers int) {
	mu.Lock()
	cur.op7.add(total)
	cur.op7Wait.add(wireWait)
	cur.op7Log.add(logCost)
	cur.op7Peers += peers
	mu.Unlock()
}

// WriteErr counts a failed in-match socket write, separating deadline
// timeouts (MatchWriteTimeout fired: a peer stopped reading) from the rest.
func WriteErr(err error) {
	if err == nil {
		return
	}
	mu.Lock()
	// os.ErrDeadlineExceeded and *net.OpError both carry Timeout(); the
	// narrower interface also matches the test doubles.
	if te, ok := err.(interface{ Timeout() bool }); ok && te.Timeout() {
		cur.wrTimeout++
	} else {
		cur.wrErr++
	}
	mu.Unlock()
}

// Save records one accounts.json rewrite.
func Save(d time.Duration, size int) {
	mu.Lock()
	cur.saves++
	if d > cur.saveMax {
		cur.saveMax = d
	}
	cur.saveSize = size
	mu.Unlock()
}

// runtime/metrics samples read every window; deltas are printed.
var samples = []metrics.Sample{
	{Name: "/gc/cycles/total:gc-cycles"},
	{Name: "/gc/pauses:seconds"},
	{Name: "/cpu/classes/gc/total:cpu-seconds"},
	{Name: "/cpu/classes/total:cpu-seconds"},
	{Name: "/memory/classes/heap/objects:bytes"},
	{Name: "/sched/goroutines:goroutines"},
}

func reporter() {
	t := time.NewTicker(window)
	defer t.Stop()
	var prevGC uint64
	var prevGCCPU, prevCPU float64
	var prevPause *metrics.Float64Histogram
	prevUser, prevSys := processCPU()
	for range t.C {
		mu.Lock()
		s := cur
		cur = stats{}
		mu.Unlock()

		metrics.Read(samples)
		gc := samples[0].Value.Uint64()
		pause := samples[1].Value.Float64Histogram()
		gcCPU := samples[2].Value.Float64()
		cpu := samples[3].Value.Float64()
		heap := samples[4].Value.Uint64()
		gor := samples[5].Value.Uint64()
		pauseMax := histMaxDelta(pause, prevPause)
		prevPause = pause

		user, sys := processCPU()
		procCPU := ""
		if user >= 0 {
			procCPU = fmt.Sprintf(" proc=%.1f%%(usr %.1f%% sys %.1f%%)",
				100*((user-prevUser)+(sys-prevSys))/window.Seconds(),
				100*(user-prevUser)/window.Seconds(), 100*(sys-prevSys)/window.Seconds())
		}

		fmt.Printf(" [PROF] op11 send %s writes=%d slowest=%s(%s) wireWait=%s\n",
			s.frameSend.String(), s.frameWrites, s.frameSlow.Round(time.Microsecond),
			s.frameSlowU, s.frameWait.String())
		fmt.Printf(" [PROF] op7 relay %s peers=%d wireWait=%s printf=%s\n",
			s.op7.String(), s.op7Peers, s.op7Wait.String(), s.op7Log.String())
		fmt.Printf(" [PROF] write errors timeout=%d other=%d | accounts saves=%d max=%s size=%dB\n",
			s.wrTimeout, s.wrErr, s.saves, s.saveMax.Round(time.Microsecond), s.saveSize)
		fmt.Printf(" [PROF] runtime gc=%d pauseMax=%s gcCPU=%.1f%% goCPU=%.1f%%%s heap=%.1fMB goroutines=%d\n",
			gc-prevGC, time.Duration(pauseMax*float64(time.Second)).Round(time.Microsecond),
			100*(gcCPU-prevGCCPU)/window.Seconds(), 100*(cpu-prevCPU)/window.Seconds(),
			procCPU, float64(heap)/(1<<20), gor)
		prevGC, prevGCCPU, prevCPU, prevUser, prevSys = gc, gcCPU, cpu, user, sys
	}
}

// histMaxDelta returns the upper bound of the highest bucket that gained a
// count since prev (the worst GC pause in this window), in seconds.
func histMaxDelta(h, prev *metrics.Float64Histogram) float64 {
	if h == nil {
		return 0
	}
	for i := len(h.Counts) - 1; i >= 0; i-- {
		c := h.Counts[i]
		if prev != nil && i < len(prev.Counts) {
			c -= prev.Counts[i]
		}
		if c > 0 {
			if i+1 < len(h.Buckets) {
				return h.Buckets[i+1]
			}
			return h.Buckets[i]
		}
	}
	return 0
}
