package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitSkew sweeps the unsynchronized-clock robustness of the two retardation modes. For each maximum
// per-node skew magnitude (in Ticks) and each mode — clock-free hop-age (HopAge=true: a fixed one Tick
// of proper time per relay) vs measured cross-clock transit (HopAge=false: recv_clock − emit_clock,
// which needs NTP) — it runs R independent reps. Each rep draws a fresh adversarial skew pattern
// (random sign and magnitude per node up to the cap), converges a line, reads the live vitality at the
// far observer, kills the source, and waits for severance. A rep scores CORRECT iff the live source
// became routable AND the dead source severed. The figure plots fraction-correct vs skew: clock-free
// holds at ~1.0 by construction (transit never reads a clock); measured-transit degrades as the skew
// difference between emit and receive clocks dwarfs the true sub-Tick transit — over-aging V below φ
// (a live source that never lights up) or under-aging/negative transit (a dead source that resurrects).
//
// This is MEASURED from the reference implementation, async + real-time, median over R reps with a
// 10–90 percentile band on the steady live-V; never a single run. Columns of skew.dat:
//
//	skew fracHop fracMeas vMedHop vLoHop vHiHop vMedMeas vLoMeas vHiMeas
func emitSkew(dir string) {
	const (
		N    = 5  // line length; observer at one end, source at the other (4 hops)
		R    = 21 // reps per (skew, mode) cell — median + band, never one async run
		seed = 1
	)
	skews := []int{0, 2, 4, 6, 8, 10, 12, 14, 16} // max per-node skew magnitude, in Ticks

	// scoreRep runs one converge-then-sever cycle under a randomised per-node skew pattern and returns
	// whether the run was correct (live⇒routable AND dead⇒severed) and the steady live vitality observed.
	scoreRep := func(hopAge bool, maxSkew int, rng *rand.Rand) (correct bool, vLive float64) {
		cfg := proto.DefaultConfig()
		cfg.HopAge = hopAge
		cfg.Tick = 15 * time.Millisecond // a calmer Tick so the async sim runs cheaply
		m := proto.NewMesh(cfg)
		ids := proto.BuildLine(m, N)
		O, src := ids[0], ids[N-1]
		for _, id := range ids { // adversarial-ish: random sign and magnitude up to the cap, per node
			mag := rng.Intn(maxSkew + 1)
			sign := 1
			if rng.Intn(2) == 0 {
				sign = -1
			}
			m.SetSkew(id, time.Duration(sign*mag)*cfg.Tick)
		}
		m.Start()
		defer m.Stop()
		live := waitUntil(1500*time.Millisecond, func() bool { _, _, r := m.Node(O).Route(src); return r })
		_, vLive, _ = m.Node(O).Route(src) // steady live vitality at the observer (before the kill)
		m.Kill(src)
		severed := waitUntil(2500*time.Millisecond, func() bool { _, _, r := m.Node(O).Route(src); return !r })
		return live && severed, vLive
	}

	// sweep one mode across all skew levels; returns frac-correct and the V band per level.
	type cell struct{ frac, vMed, vLo, vHi float64 }
	sweep := func(hopAge bool) []cell {
		out := make([]cell, len(skews))
		for i, s := range skews {
			rng := rand.New(rand.NewSource(int64(s)*7 + seed)) // per-level seed: reproducible pattern set
			nCorrect := 0
			vs := make([]float64, 0, R)
			for r := 0; r < R; r++ {
				ok, v := scoreRep(hopAge, s, rng)
				if ok {
					nCorrect++
				}
				vs = append(vs, v)
			}
			sort.Float64s(vs)
			out[i] = cell{
				frac: float64(nCorrect) / R,
				vMed: vs[len(vs)/2],
				vLo:  vs[len(vs)/10],
				vHi:  vs[len(vs)*9/10],
			}
		}
		return out
	}

	hop := sweep(true)   // clock-free hop-age
	meas := sweep(false) // measured cross-clock transit

	var b strings.Builder
	b.WriteString("skew fracHop fracMeas vMedHop vLoHop vHiHop vMedMeas vLoMeas vHiMeas\n")
	for i, s := range skews {
		fmt.Fprintf(&b, "%d %.4f %.4f %.4f %.4f %.4f %.4f %.4f %.4f\n",
			s, hop[i].frac, meas[i].frac,
			hop[i].vMed, hop[i].vLo, hop[i].vHi,
			meas[i].vMed, meas[i].vLo, meas[i].vHi)
	}
	write(filepath.Join(dir, "skew.dat"), b.String())
	fmt.Fprintf(os.Stderr, "skew: clock-free frac %.2f→%.2f, measured frac %.2f→%.2f over skew %d→%d Ticks (R=%d)\n",
		hop[0].frac, hop[len(hop)-1].frac, meas[0].frac, meas[len(meas)-1].frac, skews[0], skews[len(skews)-1], R)
}