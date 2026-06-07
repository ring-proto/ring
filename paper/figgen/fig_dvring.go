package main

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/baseline"
	"github.com/ring-proto/ring/proto"
)

// emitDVRing is the headline count-to-infinity contrast on the SAME line A–B–C–D–E with the
// destination at one end. Two routers face the identical event (the destination dies) under the
// identical min-plus recurrence, and diverge in what they DO with the diverging metric.
//
// LEFT (baseline.DV, dv.dat): a textbook SYNCHRONOUS distance-vector router set with no split-horizon
// and no poison reverse — the canonical count-to-infinity foil. After Kill(dst) the far node's hop
// metric to the dead destination is stale and circulates upward between routers that each believe the
// other still has a path, climbing one hop per round toward the cap ("infinity"). It is reproducible to
// the integer: the rounds are synchronous, the trajectory is fixed.
//
// RIGHT (RING, ring.dat): the SAME line under the async RING protocol. RING obeys the same min-plus
// recurrence in U = 1/V (U = 1 + alpha*delta, which genuinely diverges — it IS the count-to-infinity
// metric), but the far observer READS the bounded reciprocal V. After Kill(dst) the data-plane-read V
// at the far node ages smoothly through the action threshold phi to 0 — graceful severance, no flap,
// no spurious "16 hops away" advertisement. U=1/V is plotted alongside to show the divergence is real;
// V just stays in [0,1]. Because the protocol is async + real-time, the RING trajectory is MEASURED
// (median + a 10–90% band over R reps), not reproducible to the byte.
//
// Emits dv.dat (round metric Ucap), ring.dat (t V U phi), ring_band.dat (t Vlo Vhi the inter-rep band),
// and ring_kill.dat / dv_kill.dat (the death-event markers, as 2-point vertical segments from data).
func emitDVRing(dir string) {
	const N = 5 // A B C D E
	cfg := proto.DefaultConfig()
	cfg.Tick = 20 * time.Millisecond // calmer cadence: cheap async runs, same per-hop behaviour

	// ---- LEFT: synchronous distance-vector, counts to infinity (reproducible) ----
	// Build the DV adjacency to MATCH the RING line n0–n1–n2–n3–n4. Destination = far end n4;
	// observer = n0. (The names line up index-for-index with proto.BuildLine.)
	adj := map[string][]string{}
	for i := 0; i < N; i++ {
		var nbrs []string
		if i > 0 {
			nbrs = append(nbrs, fmt.Sprintf("n%d", i-1))
		}
		if i+1 < N {
			nbrs = append(nbrs, fmt.Sprintf("n%d", i+1))
		}
		adj[fmt.Sprintf("n%d", i)] = nbrs
	}
	dst := fmt.Sprintf("n%d", N-1) // destination at the far end
	obs := "n0"                    // observer at the near end (4 hops away)

	g := baseline.NewDV(adj, 16)
	g.Converge(50)
	pre := g.Metric(obs, dst) // should be N-1 = 4 hops

	var dv strings.Builder
	dv.WriteString("round metric Ucap\n")
	// record the converged pre-death metric at round 0, then kill and run rounds to the cap.
	fmt.Fprintf(&dv, "%d %d %d\n", 0, pre, g.Cap)
	g.Kill(dst)
	dvCapRound := -1
	for r := 1; r <= 60; r++ {
		g.Round()
		mm := g.Metric(obs, dst)
		fmt.Fprintf(&dv, "%d %d %d\n", r, mm, g.Cap)
		if mm >= g.Cap && dvCapRound < 0 {
			dvCapRound = r
		}
		if mm >= g.Cap && r >= dvCapRound+3 { // a few rounds past the cap to flatten the plot, then stop
			break
		}
	}
	write(filepath.Join(dir, "dv.dat"), dv.String())
	// death marker for the DV axis: at round 0 (the kill happens between the converged read and round 1).
	write(filepath.Join(dir, "dv_kill.dat"),
		fmt.Sprintf("round metric\n%d %d\n%d %d\n", 0, 0, 0, g.Cap+2))

	// ---- RIGHT: async RING on the same line, V fades through phi (measured, R reps) ----
	const (
		reps      = 21
		converge  = 1200 * time.Millisecond // settle the field before the kill
		decay     = 5000 * time.Millisecond // observe V fall well below phi after the kill
		sampleEvy = 25 * time.Millisecond
	)
	// a common time grid (relative to the kill instant) so reps can be aggregated point-for-point.
	nConv := int(converge / sampleEvy)
	nDecay := int(decay / sampleEvy)
	total := nConv + nDecay
	grid := make([]float64, total)            // wall time (ms) since run start, kill at index nConv
	samples := make([][]float64, total)       // samples[k] = the V readings across reps at grid point k
	for k := range samples {
		grid[k] = float64(k) * sampleEvy.Seconds() * 1000
		samples[k] = make([]float64, 0, reps)
	}
	killMs := float64(nConv) * sampleEvy.Seconds() * 1000

	for rep := 0; rep < reps; rep++ {
		m := proto.NewMesh(cfg)
		ids := proto.BuildLine(m, N)
		rsrc, robs := ids[N-1], ids[0] // destination far end, observer near end — same roles as DV
		m.Start()
		// converge: wait until the observer actually carries a route to the destination.
		waitUntil(4*time.Second, func() bool { _, _, r := m.Node(robs).Route(rsrc); return r })

		start := time.Now()
		killed := false
		for k := 0; k < total; k++ {
			// fire the kill once, at the converge/decay boundary.
			if !killed && k >= nConv {
				m.Kill(rsrc)
				killed = true
			}
			_, v, _ := m.Node(robs).Route(rsrc)
			samples[k] = append(samples[k], v)
			// pace to the common grid (real-time async: sleep the inter-sample gap).
			target := start.Add(time.Duration(k+1) * sampleEvy)
			if d := time.Until(target); d > 0 {
				time.Sleep(d)
			}
		}
		m.Stop()
	}

	// aggregate: median and the 10–90% inter-rep band at each grid point.
	pct := func(s []float64, p float64) float64 {
		if len(s) == 0 {
			return 0
		}
		cp := append([]float64(nil), s...)
		sort.Float64s(cp)
		idx := int(p * float64(len(cp)-1))
		return cp[idx]
	}
	var ring, band strings.Builder
	ring.WriteString("t V U phi\n")
	band.WriteString("t Vlo Vhi\n")
	for k := 0; k < total; k++ {
		med := pct(samples[k], 0.5)
		lo := pct(samples[k], 0.10)
		hi := pct(samples[k], 0.90)
		u := math.Inf(1)
		if med > 1e-9 {
			u = 1.0 / med // U = 1/V: the divergent min-plus metric. Caps in the plot via ymax.
		}
		// shift the time axis so 0 = the kill instant (negative = pre-death converged phase).
		t := grid[k] - killMs
		if math.IsInf(u, 1) {
			fmt.Fprintf(&ring, "%.1f %.6f %s %.3f\n", t, med, "inf", cfg.Phi)
		} else {
			fmt.Fprintf(&ring, "%.1f %.6f %.6f %.3f\n", t, med, u, cfg.Phi)
		}
		fmt.Fprintf(&band, "%.1f %.6f %.6f\n", t, lo, hi)
	}
	write(filepath.Join(dir, "ring.dat"), ring.String())
	write(filepath.Join(dir, "ring_band.dat"), band.String())
	// death marker for the RING axis at t=0 (the shifted kill instant).
	write(filepath.Join(dir, "ring_kill.dat"), "t V\n0 0\n0 1.05\n")

	// report the story numbers to stderr so the integrator can sanity-check.
	medFinal := pct(samples[total-1], 0.5)
	// find the time V first crosses below phi (median).
	crossMs := math.NaN()
	for k := nConv; k < total; k++ {
		if pct(samples[k], 0.5) < cfg.Phi {
			crossMs = grid[k] - killMs
			break
		}
	}
	fmt.Printf("  dvring: DV %s->%s pre=%d cap=%d capRound=%d | RING preV=%.3f phiCrossing=%.0fms finalV=%.4f (reps=%d)\n",
		obs, dst, pre, g.Cap, dvCapRound, pct(samples[nConv-1], 0.5), crossMs, medFinal, reps)
}