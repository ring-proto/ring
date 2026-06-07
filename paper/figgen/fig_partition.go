package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitPartition stages a partition and heals it on a line, measuring the data-plane delivery dip
// against the control-plane state through the whole lifecycle. A line of N nodes converges; the
// bridge edge at the middle is CUT at t1 and RELINKED at t2. Throughout, a stream of packets is
// Forward()ed across the cut and each is classified (delivered / black-holed / severed), while the
// far side's routability is sampled. Because the sim is async + real-time, the cut→sever and
// relink→heal latencies jitter rep to rep; the figure therefore reports an ENSEMBLE over R reps,
// sampled on a wall-clock grid anchored at the cut, so each window's delivery ratio is the fraction
// of (reps × packets) delivered, with a min/max band — never a single run.
//
// The story the data tells, in three phases after the cut:
//  1. a transient BLACK-HOLE window — the stale field still routes packets, now into the dead edge,
//     forming a via-cycle (BlackholeLoop): the detection gap vitality exists to shrink;
//  2. correct SEVERANCE — once the far side's vitality ages below φ, routers decline to forward
//     (Severed), routable→0: the protocol working as intended, NOT a black hole;
//  3. fast RECOVERY after the relink — the field refreshes back through the healed edge and delivery
//     snaps to 1.0.
//
// Writes:
//   - partition.dat (t_ms deliv delivLo delivHi black sever routable) — the ensemble timeseries;
//   - partition_events.dat (label t_ms) — the cut/heal/recovery instants, for vertical markers.
func emitPartition(dir string) {
	const (
		N        = 9     // line length; bridge edge is (N/2-1)–(N/2)
		R        = 21    // reps to average the async transition jitter
		PktsPerS = 24    // packets forwarded per sample window
		preMs    = 120.0 // grid extent before the cut
		cutMs    = 700.0 // grid extent after the cut (covers black-hole + sever)
		healAtMs = 700.0 // wall-clock offset (from cut) at which we relink
		postMs   = 500.0 // grid extent after the heal
		stepMs   = 20.0  // sample-grid spacing
	)
	tick := 20 * time.Millisecond
	dp := proto.DataPlane{HopDelay: time.Millisecond, TTL: 64}

	// A fixed sampling grid in ms relative to the cut instant (t=0 at the cut). It spans a slice
	// before the cut, the whole partition window, and the recovery window after the heal.
	var grid []float64
	for t := -preMs; t <= healAtMs+postMs+1e-9; t += stepMs {
		grid = append(grid, t)
	}
	G := len(grid)
	healIdx := sort.SearchFloat64s(grid, healAtMs) // first grid index at/after the relink

	// per-window accumulators across all reps: counts of each classified outcome.
	deliv := make([]int, G)
	black := make([]int, G)
	sever := make([]int, G)
	rout := make([]int, G)      // reps with the far side routable in this window
	per := make([][]float64, G) // per-rep delivery ratio in each window (for the band)

	for rep := 0; rep < R; rep++ {
		m := proto.NewMesh(figCfg())
		ids := proto.BuildLine(m, N)
		dst, src := ids[0], ids[N-1] // forward across the whole line, over the bridge edge
		bridgeA, bridgeB := ids[N/2-1], ids[N/2]
		m.SetTick(tick)
		m.Start()

		routable := func() bool { _, _, r := m.Node(src).Route(dst); return r }
		if !waitUntil(8*time.Second, routable) {
			m.Stop()
			die(fmt.Errorf("partition: rep %d did not converge", rep))
		}
		time.Sleep(3 * tick) // let the field settle so the pre-cut baseline is clean

		cutAt := time.Now()
		cut, healed := false, false
		for gi, gt := range grid {
			// advance wall clock to this grid point relative to the cut
			target := cutAt.Add(time.Duration(gt * float64(time.Millisecond)))
			if d := time.Until(target); d > 0 {
				time.Sleep(d)
			}
			if !cut && gt >= 0 { // fire the cut exactly at t=0
				m.Cut(bridgeA, bridgeB)
				cut = true
			}
			if !healed && gt >= healAtMs { // relink at the heal offset
				m.Relink(bridgeA, bridgeB)
				healed = true
			}
			// inject a burst and classify each packet against the then-current field snapshot
			var nd int
			for p := 0; p < PktsPerS; p++ {
				switch o := m.Forward(src, dst, dp, nil).Outcome; {
				case o == proto.Delivered:
					deliv[gi]++
					nd++
				case o.Blackholed():
					black[gi]++
				case o == proto.Severed:
					sever[gi]++
				}
			}
			if _, _, r := m.Node(src).Route(dst); r {
				rout[gi]++
			}
			per[gi] = append(per[gi], float64(nd)/float64(PktsPerS))
		}
		m.Stop()
	}

	// reduce to the ensemble timeseries. delivery ratio = delivered / total; band = min/max per-rep.
	total := float64(R * PktsPerS)
	var b strings.Builder
	b.WriteString("t deliv delivLo delivHi black sever routable\n")
	for gi, gt := range grid {
		lo, hi := per[gi][0], per[gi][0]
		for _, v := range per[gi] {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		fmt.Fprintf(&b, "%.1f %.4f %.4f %.4f %.4f %.4f %.4f\n",
			gt,
			float64(deliv[gi])/total,
			lo, hi,
			float64(black[gi])/total,
			float64(sever[gi])/total,
			float64(rout[gi])/float64(R),
		)
	}
	write(filepath.Join(dir, "partition.dat"), b.String())

	// event markers as labelled instants (the cut is t=0 by construction; the heal is healAtMs).
	// Recovery time = the first grid window AFTER the heal whose ensemble delivery ratio climbs back
	// above 0.5 — the measured time for the data plane to recover once the edge is restored.
	recovMs := grid[G-1] - healAtMs // fallback: never recovered within the window
	for gi := healIdx; gi < G; gi++ {
		if float64(deliv[gi])/total >= 0.5 {
			recovMs = grid[gi] - healAtMs
			break
		}
	}
	var ev strings.Builder
	ev.WriteString("label t\n")
	fmt.Fprintf(&ev, "cut 0.0\n")
	fmt.Fprintf(&ev, "heal %.1f\n", healAtMs)
	fmt.Fprintf(&ev, "recovered %.1f\n", healAtMs+recovMs)
	write(filepath.Join(dir, "partition_events.dat"), ev.String())

	fmt.Printf("  partition: N=%d R=%d recovery=%.0fms (after relink)\n", N, R, recovMs)
}
