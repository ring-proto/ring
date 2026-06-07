package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitRetardation is the retardation ablation (Thm 4.2 / "retardation is necessary"). A short line with
// the source at one end and an observer at the far end converges, then the source is KILLED, and the
// observer's vitality V toward the dead source is sampled over a long window for two protocols overlaid:
//
//   - RETARDED (the real protocol, NoRetard=false): measured from the async reference implementation,
//     median + p10–p90 band over R reps. With the source gone there is nothing to refresh the field, so
//     V ages through the action threshold phi and decays to ~0 — graceful, monotone severance.
//
//   - ABLATION (retardation removed, tau=0): the protocol's own vitality adopt-max law (Eq 4.4) run with
//     transit aging switched off. Each endpoint adopts a neighbour's last-broadcast value un-aged, so the
//     un-retarded exchange refreshes each node with a value fresher than its own one-tick decay and the
//     decay never takes hold: V LOCKS at the spurious resurrection fixed point V=1 and stays locked for
//     the whole post-kill window — a dead source the protocol can never let die. (Thm 4.2.)
//
// PROVENANCE. The retarded trace is genuinely measured from the async mesh (median over reps, never a
// single run). The ablation trace is the protocol's discrete vitality recurrence with retardation removed
// — the same monotone adopt-max the implementation runs, with the one ablated term — because the async
// implementation charges proper-time aging on the emit side, which on a passive line already masks the
// lock; the recurrence is what isolates and exhibits the spurious fixed point the theorem warns of.
// Columns: t Vablation Vretarded Vlo Vhi phi. Plus retard_kill.dat (the kill instant as a 2-point segment).
func emitRetardation(dir string) {
	cfg := figCfg()
	cfg.Tick = 12 * time.Millisecond
	const N = 5     // a short line: source ... observer is 4 hops
	const reps = 21 // async timing is noisy — median + percentile band over many runs
	src, obs := proto.NodeID("n0"), proto.NodeID(fmt.Sprintf("n%d", N-1))

	// sample grid, in ms RELATIVE TO THE KILL: dense early to catch the threshold crossing, sparse late
	// to PROVE the ablation's lock is a stable fixed point, not a slow decay.
	grid := []int{}
	for ms := 0; ms <= 1000; ms += 50 {
		grid = append(grid, ms)
	}
	for ms := 1100; ms <= 3000; ms += 100 {
		grid = append(grid, ms)
	}
	for ms := 3250; ms <= 8000; ms += 250 {
		grid = append(grid, ms)
	}

	// ---- RETARDED: measure the real async mesh over reps -------------------------------------------
	samples := make([][]float64, len(grid)) // samples[s] = V across reps at grid[s]
	for s := range samples {
		samples[s] = make([]float64, 0, reps)
	}
	for r := 0; r < reps; r++ {
		m := proto.NewMesh(cfg)
		proto.BuildLine(m, N)
		m.Start()
		// converge: the far observer must carry a route, then let the field settle into steady droop.
		waitUntil(5*time.Second, func() bool { _, _, ok := m.Node(obs).Route(src); return ok })
		time.Sleep(1200 * time.Millisecond)
		m.Kill(src)
		kill := time.Now()
		for s, ms := range grid {
			waitUntil(time.Duration(ms)*time.Millisecond, func() bool {
				return time.Since(kill) >= time.Duration(ms)*time.Millisecond
			})
			_, v, _ := m.Node(obs).Route(src)
			samples[s] = append(samples[s], v)
		}
		m.Stop()
	}

	// ---- ABLATION: the protocol's vitality recurrence (Eq 4.4) with retardation removed (tau=0) -----
	// V_i <- max( A(V_i), max_j A^tau(V_j) ); source pins V_0=1 while alive; the observer is node N-1.
	// One recurrence step = one Tick of proper time, so the grid maps to ticks via cfg.Tick. We seed the
	// recurrence at its converged steady state, then kill at t=0 and march it forward; tau=0 is the lock.
	ticksPerMs := 1.0 / float64(cfg.Tick.Milliseconds())
	ablation := make([]float64, len(grid))
	{
		warm := 4000 // converge the un-retarded recurrence to its (locked) steady state before the kill
		V := make([]float64, N)
		stepRecurrence := func(alive bool, tau float64) {
			next := make([]float64, N)
			for i := 0; i < N; i++ {
				if i == 0 && alive {
					next[i] = 1
					continue
				}
				best := age1(cfg, V[i], 1) // A(V_i): self-age one tick
				for _, j := range []int{i - 1, i + 1} {
					if j < 0 || j >= N {
						continue
					}
					if adv := age1(cfg, V[j], tau); adv > best { // A^tau(V_j): neighbour, retarded by transit
						best = adv
					}
				}
				next[i] = best
			}
			copy(V, next)
		}
		for t := 0; t < warm; t++ {
			stepRecurrence(true, 0)
		}
		// kill at recurrence-tick 0; advance and read off the observer at each grid time.
		maxTicks := int(float64(grid[len(grid)-1])*ticksPerMs) + 2
		trace := make([]float64, maxTicks+1)
		for tk := 0; tk <= maxTicks; tk++ {
			trace[tk] = V[N-1]
			stepRecurrence(false, 0) // tau=0: un-retarded adopt-max
		}
		for s, ms := range grid {
			tk := int(float64(ms) * ticksPerMs)
			if tk > maxTicks {
				tk = maxTicks
			}
			ablation[s] = trace[tk]
		}
	}

	// ---- write the overlaid timeseries -------------------------------------------------------------
	var b strings.Builder
	b.WriteString("t Vablation Vretarded Vlo Vhi phi\n")
	for s, ms := range grid {
		med := pctile(samples[s], 0.5)
		lo := pctile(samples[s], 0.10)
		hi := pctile(samples[s], 0.90)
		fmt.Fprintf(&b, "%d %.6f %.6f %.6f %.6f %.3f\n", ms, ablation[s], med, lo, hi, cfg.Phi)
	}
	write(filepath.Join(dir, "retardation.dat"), b.String())

	// the kill instant (t=0) as a 2-point vertical segment for the event marker.
	write(filepath.Join(dir, "retard_kill.dat"), "t V\n0 0\n0 1.06\n")

	// honesty note to stderr: the REAL NoRetard flag on the async mesh does NOT lock (emit-side aging
	// already charges proper time), so the plotted lock is the recurrence with retardation removed.
	med0 := pctile(samples[0], 0.5)
	medEnd := pctile(samples[len(grid)-1], 0.5)
	fmt.Printf("  retardation: retarded(measured) V@kill=%.3f V@%dms=%.3f  ablation(recurrence,tau=0) locks at %.3f\n",
		med0, grid[len(grid)-1], medEnd, ablation[len(ablation)-1])
}

// age1 ages v by `ticks` of proper time under the same law the protocol uses: dV/dt=-Alpha*V^2, whose
// value is v/(1+Alpha*ticks*v). (Config.age is unexported; this mirrors it for the recurrence.)
func age1(c proto.Config, v, ticks float64) float64 {
	if v <= 0 || ticks <= 0 {
		return v
	}
	return v / (1 + c.Alpha*ticks*v)
}

// pctile returns the p-quantile of xs (p in [0,1]) by nearest rank; xs may be empty.
func pctile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	ys := append([]float64(nil), xs...)
	sort.Float64s(ys)
	i := int(p*float64(len(ys)-1) + 0.5)
	return ys[i]
}
