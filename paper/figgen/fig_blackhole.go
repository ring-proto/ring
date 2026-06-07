package main

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitBlackhole is the DATA-PLANE payoff: it forwards real packets through the live field and
// classifies every outcome with the one shared taxonomy (dataplane.go) — Delivered, Severed (the
// protocol correctly DECLINED, V<φ), or Blackholed (it ACCEPTED but failed: dead-hop / loop / ttl).
// Two panels, both MEASURED from the async reference implementation over many reps (median + band):
//
//	(a) blackhole_cdf_{nominal,lossy}.dat — delivery-latency CDF by hop count, for a nominal lossless
//	    fabric and a lossy one (LossProb>0); one file per condition (hops cdf delivered). Each CDF is
//	    SCALED by its delivered fraction, so the lossy curve plateaus below 1 — the gap to 1 is the
//	    physically-lost mass (Lost), never delivered, distinct from any routing failure.
//
//	(b) blackhole_ts.dat — a failure timeseries (t_ms, deliveryRatio, black-hole count AND ratio, each
//	    with a p25..p75 band). A steady stream of packets from random live srcs to a corner dst is
//	    injected in fixed windows; at t0 a node ON MANY PATHS is killed. The delivery ratio dips and the
//	    black-hole ratio spikes during the reconvergence window (stale routes forward INTO the corpse),
//	    then vitality ages the dead route past φ and forwarding cleanly SEVERS (not a black hole) —
//	    delivery recovers, black-holes fall back to zero. blackhole_ts_kill.dat carries the kill instant
//	    as a 2-point vertical segment for the event marker.
func emitBlackhole(dir string) {
	emitBlackholeCDF(dir)
	emitBlackholeTS(dir)
}

// emitBlackholeCDF forwards a large bundle of packets from random live srcs to a corner dst on a
// converged grid and emits the empirical CDF of delivery hop-count, for a lossless and a lossy
// fabric. Reps with independent seeds; the CDF is the pooled empirical distribution (the hop count of
// a delivered packet is a routing property, deterministic given the field, so pooling is honest).
func emitBlackholeCDF(dir string) {
	cfg := figCfg()
	cfg.Tick = 20 * time.Millisecond // calmer cadence: cheap async run
	cfg.Alpha = 0.025                // size the 12×10 grid INSIDE the vitality horizon so steady-state
	//                                  delivery is complete and the only severances/black-holes seen
	//                                  are the ones a FAILURE introduces (the horizon itself is the
	//                                  subject of the gridV / reciprocal figures, not this one).
	const W, H = 12, 10
	m := proto.NewMesh(cfg)
	proto.BuildGrid(m, W, H)
	id := func(x, y int) proto.NodeID { return proto.NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	dst := id(W-1, H-1)
	m.Start()
	defer m.Stop()
	// converge: every corner must carry a route to dst before we measure latency.
	waitUntil(12*time.Second, func() bool { _, _, r := m.Node(id(0, 0)).Route(dst); return r })
	time.Sleep(1500 * time.Millisecond)

	// the live source set: every node except the dst.
	var srcs []proto.NodeID
	for y := range H {
		for x := range W {
			if s := id(x, y); s != dst {
				srcs = append(srcs, s)
			}
		}
	}

	type cond struct {
		name string
		loss float64
	}
	conds := []cond{{"nominal", 0.0}, {"lossy", 0.04}}

	// one file per condition — two separately-styled CDF curves in the figure read cleanest from two
	// tables rather than one filtered by a string column. Each row is a step of the empirical CDF,
	// SCALED by the delivered fraction so a lossy curve plateaus below 1 (the gap = physical loss).
	for _, c := range conds {
		const reps = 24
		const perRep = 1500
		var hops []int
		var delivered, total int
		for r := 0; r < reps; r++ {
			rng := rand.New(rand.NewSource(int64(1000 + r)))
			for k := 0; k < perRep; k++ {
				s := srcs[rng.Intn(len(srcs))]
				d := m.Forward(s, dst, proto.DataPlane{HopDelay: time.Millisecond, LossProb: c.loss, TTL: 128}, rng)
				total++
				if d.Outcome == proto.Delivered {
					delivered++
					hops = append(hops, d.Hops)
				}
			}
		}
		sort.Ints(hops)
		frac := float64(delivered) / float64(total) // CDF top plateau (lossy < 1 by the lost mass)
		fmt.Printf("  blackhole CDF cond=%s delivered=%d/%d (%.3f) hops[min=%d med=%d max=%d]\n",
			c.name, delivered, total, frac, hops[0], hops[len(hops)/2], hops[len(hops)-1])

		var b strings.Builder
		b.WriteString("hops cdf delivered\n")
		// anchor at (0,0) so the step function starts from the floor, not the first delivered hop.
		fmt.Fprintf(&b, "0 0.000000 %.6f\n", frac)
		n := len(hops)
		for i := 0; i < n; {
			j := i
			for j < n && hops[j] == hops[i] {
				j++
			}
			cdf := float64(j) / float64(n) * frac // empirical CDF * delivered fraction
			fmt.Fprintf(&b, "%d %.6f %.6f\n", hops[i], cdf, frac)
			i = j
		}
		write(filepath.Join(dir, "blackhole_cdf_"+c.name+".dat"), b.String())
	}
}

// emitBlackholeTS injects a steady packet stream toward a corner dst, kills a node that sits on many
// paths to it, and records per-window delivery ratio + black-hole count across the failure. The
// window stream is the AVERAGED measure (median over reps) — a single async run is noisy because the
// reconvergence window jitters with the unsynchronized tick. Each rep rebuilds the mesh, converges,
// streams windows of packets (snapshot-walking the then-current field), and kills the cut node at a
// fixed window. We align reps on window INDEX (window 0 = first after warmup) and report the median
// ratio and median black-hole count per window, plus a band (p25..p75).
func emitBlackholeTS(dir string) {
	cfg := figCfg()
	cfg.Tick = 20 * time.Millisecond
	cfg.Alpha = 0.025 // grid inside the vitality horizon: steady-state delivery is complete (see CDF note)
	const W, H = 12, 10
	id := func(x, y int) proto.NodeID { return proto.NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	dst := id(W-1, H-1)

	const (
		reps       = 21
		windows    = 40 // windows per rep
		killWindow = 12 // kill at the start of this window (after warmup)
		perWindow  = 240
		windowMs   = 50.0 // nominal ms attributed to each window (for the time axis)
	)
	// the cut node: the single node directly above the corner dst. The dst's only two neighbours are
	// (W-2,H-1) and (W-1,H-2); the up-gradient flow of nearly the whole fabric funnels through this
	// one, so killing it black-holes almost every packet (stale routes forward INTO the corpse) until
	// vitality ages the dead route past φ and forwarding cleanly severs — a near-total dip, then a
	// sharp recovery as the field reconverges around the gap. The headline "node on many paths".
	cut := id(W-1, H-2)

	// per-window samples across reps
	ratios := make([][]float64, windows)
	bhs := make([][]float64, windows)   // black-hole COUNT per window
	bhrat := make([][]float64, windows) // black-hole RATIO (fraction of sent) — sits in [0,1]

	for r := 0; r < reps; r++ {
		m := proto.NewMesh(cfg)
		proto.BuildGrid(m, W, H)
		var srcs []proto.NodeID
		for y := range H {
			for x := range W {
				if s := id(x, y); s != dst {
					srcs = append(srcs, s)
				}
			}
		}
		m.Start()
		waitUntil(12*time.Second, func() bool { _, _, ok := m.Node(id(0, 0)).Route(dst); return ok })
		time.Sleep(1200 * time.Millisecond)

		rng := rand.New(rand.NewSource(int64(7000 + r)))
		killed := false
		for w := 0; w < windows; w++ {
			if w == killWindow && !killed {
				m.Kill(cut)
				killed = true
			}
			var delivered, blackhole, sev, sent int
			for k := 0; k < perWindow; k++ {
				s := srcs[rng.Intn(len(srcs))]
				if s == cut { // do not inject from the corpse itself
					continue
				}
				d := m.Forward(s, dst, proto.DataPlane{HopDelay: time.Millisecond, TTL: 128}, rng)
				sent++
				switch {
				case d.Outcome == proto.Delivered:
					delivered++
				case d.Outcome.Blackholed():
					blackhole++
				case d.Outcome == proto.Severed:
					sev++
				}
			}
			_ = sev
			ratios[w] = append(ratios[w], float64(delivered)/float64(sent))
			bhs[w] = append(bhs[w], float64(blackhole))
			bhrat[w] = append(bhrat[w], float64(blackhole)/float64(sent))
			// let the async field advance between windows so reconvergence shows across windows.
			time.Sleep(time.Duration(windowMs) * time.Millisecond)
		}
		m.Stop()
	}

	median := func(xs []float64) float64 {
		s := append([]float64(nil), xs...)
		sort.Float64s(s)
		return s[len(s)/2]
	}
	pct := func(xs []float64, p float64) float64 {
		s := append([]float64(nil), xs...)
		sort.Float64s(s)
		i := int(p * float64(len(s)-1))
		return s[i]
	}

	var b strings.Builder
	b.WriteString("t_ms deliveryRatio drLo drHi blackholes bhLo bhHi bhRatio bhrLo bhrHi\n")
	var minRatio, maxBH float64 = 1, 0
	for w := 0; w < windows; w++ {
		t := float64(w) * windowMs
		dr := median(ratios[w])
		drLo, drHi := pct(ratios[w], 0.25), pct(ratios[w], 0.75)
		bh := median(bhs[w])
		bhLo, bhHi := pct(bhs[w], 0.25), pct(bhs[w], 0.75)
		br := median(bhrat[w])
		brLo, brHi := pct(bhrat[w], 0.25), pct(bhrat[w], 0.75)
		fmt.Fprintf(&b, "%.1f %.6f %.6f %.6f %.3f %.3f %.3f %.6f %.6f %.6f\n",
			t, dr, drLo, drHi, bh, bhLo, bhHi, br, brLo, brHi)
		if dr < minRatio {
			minRatio = dr
		}
		if bh > maxBH {
			maxBH = bh
		}
	}
	write(filepath.Join(dir, "blackhole_ts.dat"), b.String())

	km := float64(killWindow) * windowMs
	write(filepath.Join(dir, "blackhole_ts_kill.dat"),
		fmt.Sprintf("t v\n%.1f 0\n%.1f 1.05\n", km, km))

	fmt.Printf("  blackhole TS: kill@%.0fms cut=%s  minDeliveryRatio=%.3f  peakBlackholes(median)=%.1f\n",
		km, cut, minRatio, maxBH)
}
