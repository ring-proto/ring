package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitLoops is the empirical witness of loop-dissipativity (Thm 5.7) on the live async mesh. It is a
// READ-ONLY probe: at each sample it snapshots every node's current forwarding via toward a single
// fixed source (Node.Route), assembles the directed via-graph node → via, and COUNTS the directed
// cycles in it by DFS. A directed cycle in the via-graph is a transient forwarding loop — a packet
// caught in it would circulate (a BlackholeLoop). Thm 5.7 says such loops can flicker into existence
// when neighbours relax at different async steps and momentarily cross on stale advertisements, but
// can NEVER persist: the screened potential is a discrete subharmonic with no spurious interior
// maximum, so once the field re-settles every via points strictly up-gradient and the only fixed
// point of the via-map is the source.
//
// We FORCE the mixed-step comparisons the theorem must survive by (a) skewing every node's clock on a
// strong gradient, so advertisements are maximally stale relative to a neighbour, and (b) churning the
// mesh — killing the whole interior and reviving it, repeatedly — so the field collapses and re-grows
// and adjacent nodes briefly disagree about who is up-gradient. We sample the cycle count through each
// transition. The story the data tells: transient via-cycles spike at each disturbance and DISSIPATE
// to 0 within a few rounds, never lingering.
//
// Writes loops.dat (t mean p90 max maxlen) and loops_kill.dat / loops_revive.dat (the disturbance
// instants, split so kills and revives can be styled apart) so the figure draws the event markers
// from data, not constants. Averaged over R reps with a max envelope, because a single async run is
// noisy and the loops are stochastic per rep.
func emitLoops(dir string) {
	const (
		W, H   = 9, 9 // 81-node grid; corner source, large enough for re-routing disagreement
		R      = 21   // reps to average the noisy, stochastic async timeline
		tickMs = 20   // calmer tick so the scenario runs cheaply (cfg.Tick = 20ms)
		stepMs = 4.0  // sample cadence (ms): loops live <1 tick, so we must sample sub-tick
	)
	cfg := proto.DefaultConfig()
	cfg.Tick = tickMs * time.Millisecond
	id := func(x, y int) proto.NodeID { return proto.NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	src := id(0, 0)

	// the disturbance schedule (ms), shared by every rep so the spikes align and the statistics are
	// meaningful: kill the whole interior (field collapses), revive it (field re-grows). Three rounds.
	type event struct {
		atMs  float64
		label string
		kill  bool // true=kill interior, false=revive interior
	}
	plan := []event{
		{atMs: 800, label: "kill", kill: true},
		{atMs: 1500, label: "revive", kill: false},
		{atMs: 2600, label: "kill", kill: true},
		{atMs: 3300, label: "revive", kill: false},
		{atMs: 4400, label: "kill", kill: true},
		{atMs: 5100, label: "revive", kill: false},
	}
	const totalMs = 6200.0
	nSteps := int(totalMs/stepMs) + 1

	// per-step accumulation across reps
	mean := make([]float64, nSteps)      // mean cycle count
	maxEnv := make([]float64, nSteps)    // max cycle count seen at this step over all reps
	maxlenAcc := make([]float64, nSteps) // mean longest cycle length
	dist := make([][]float64, nSteps)    // full distribution per step (for p90)
	for i := range dist {
		dist[i] = make([]float64, 0, R)
	}

	for rep := 0; rep < R; rep++ {
		m := proto.NewMesh(cfg)
		proto.BuildGrid(m, W, H)
		// strong clock-skew gradient BEFORE start: neighbours relax very out of step, maximizing the
		// staleness that lets two adjacent advertisements momentarily cross into a 2-cycle.
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				m.SetSkew(id(x, y), time.Duration(7*(x+y))*time.Millisecond)
			}
		}
		interior := make([]proto.NodeID, 0, (W-2)*(H-2))
		for y := 1; y < H-1; y++ {
			for x := 1; x < W-1; x++ {
				interior = append(interior, id(x, y))
			}
		}
		m.Start()

		start := time.Now()
		// fire each scheduled disturbance off the sampling loop
		for _, ev := range plan {
			ev := ev
			go func() {
				time.Sleep(time.Duration(ev.atMs) * time.Millisecond)
				for _, nd := range interior {
					if ev.kill {
						m.Kill(nd)
					} else {
						m.Revive(nd)
					}
				}
			}()
		}

		// sample the via-graph on the shared time grid
		for s := 0; s < nSteps; s++ {
			target := start.Add(time.Duration(float64(s)*stepMs) * time.Millisecond)
			if d := time.Until(target); d > 0 {
				time.Sleep(d)
			}
			cnt, maxlen := countViaCycles(m, W, H, id, src)
			mean[s] += float64(cnt)
			maxlenAcc[s] += float64(maxlen)
			if float64(cnt) > maxEnv[s] {
				maxEnv[s] = float64(cnt)
			}
			dist[s] = append(dist[s], float64(cnt))
		}
		m.Stop()
	}

	var b strings.Builder
	b.WriteString("t mean p90 max maxlen\n")
	for s := 0; s < nSteps; s++ {
		t := float64(s) * stepMs
		fmt.Fprintf(&b, "%.1f %.4f %.3f %.3f %.4f\n",
			t, mean[s]/float64(R), percentileL(dist[s], 0.90), maxEnv[s], maxlenAcc[s]/float64(R))
	}
	write(filepath.Join(dir, "loops.dat"), b.String())

	// disturbance event markers, split into two tables so the figure can style kills and revives
	// differently: kills collapse the field monotonically (no loop) while revives re-grow it (the
	// spike). Each is blank-line-separated 2-point vertical segments (t y) → one \addplot of verticals.
	var ek, er strings.Builder
	ek.WriteString("t y\n")
	er.WriteString("t y\n")
	for _, ev := range plan {
		dst := &er
		if ev.kill {
			dst = &ek
		}
		fmt.Fprintf(dst, "%.1f 0\n%.1f 1\n\n", ev.atMs, ev.atMs)
	}
	write(filepath.Join(dir, "loops_kill.dat"), ek.String())
	write(filepath.Join(dir, "loops_revive.dat"), er.String())

	// report key result numbers to stderr for the integrator: peak, where, and the post-settle floor.
	var peak, peakT float64
	var maxFloor float64 // max mean cycle count in the final settle window (>700ms past last disturbance)
	for s := 0; s < nSteps; s++ {
		mu := mean[s] / float64(R)
		if mu > peak {
			peak, peakT = mu, float64(s)*stepMs
		}
		if float64(s)*stepMs > 5800 { // 700ms past final revive at 5100
			if mu > maxFloor {
				maxFloor = mu
			}
		}
	}
	fmt.Printf("  loops: peak mean cycles/snapshot=%.3f at t=%.0fms; settle-floor (>5800ms)=%.4f (R=%d)\n",
		peak, peakT, maxFloor, R)
}

// countViaCycles snapshots the directed via-graph node→via toward src and counts the directed cycles
// in it by a three-colour DFS, returning (number of distinct cycles, longest cycle length). Read-only:
// it only calls Route. A node with no routable via (severed, or the source itself) is a sink in the
// graph and cannot lie on a cycle. Out-degree is ≤ 1, so each node lies on at most one cycle and the
// count is exact (no double-count).
func countViaCycles(m *proto.Mesh, W, H int, id func(x, y int) proto.NodeID, src proto.NodeID) (int, int) {
	n := W * H
	idx := func(x, y int) int { return y*W + x }
	via := make([]int, n) // via[i] = index of i's next hop, or -1 if no routable via
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			i := idx(x, y)
			if id(x, y) == src {
				via[i] = -1
				continue
			}
			v, _, routable := m.Node(id(x, y)).Route(src)
			if !routable || v == "" {
				via[i] = -1
				continue
			}
			var vx, vy int
			fmt.Sscanf(string(v), "g%d-%d", &vx, &vy)
			via[i] = idx(vx, vy)
		}
	}

	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make([]int, n)
	cycles, maxlen := 0, 0
	for s := 0; s < n; s++ {
		if colour[s] != white {
			continue
		}
		// walk the functional chain from s; a re-entry into a grey (on-stack) node is a cycle.
		stack := stack0(n)
		pos := make([]int, n)
		for i := range pos {
			pos[i] = -1
		}
		cur := s
		for cur != -1 && colour[cur] == white {
			colour[cur] = grey
			pos[cur] = len(stack)
			stack = append(stack, cur)
			cur = via[cur]
		}
		if cur != -1 && colour[cur] == grey {
			cycles++
			if l := len(stack) - pos[cur]; l > maxlen {
				maxlen = l
			}
		}
		for _, v := range stack {
			colour[v] = black
		}
	}
	return cycles, maxlen
}

// stack0 returns an empty slice with capacity for a worst-case chain.
func stack0(n int) []int { return make([]int, 0, n) }

// percentileL returns the linear-interpolated p-quantile (0..1) of xs (copies and sorts; xs untouched).
func percentileL(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := make([]float64, len(xs))
	copy(c, xs)
	for i := 1; i < len(c); i++ { // insertion sort (R is small)
		for j := i; j > 0 && c[j-1] > c[j]; j-- {
			c[j-1], c[j] = c[j], c[j-1]
		}
	}
	if p <= 0 {
		return c[0]
	}
	if p >= 1 {
		return c[len(c)-1]
	}
	idx := p * float64(len(c)-1)
	lo := int(idx)
	frac := idx - float64(lo)
	if lo+1 >= len(c) {
		return c[lo]
	}
	return c[lo]*(1-frac) + c[lo+1]*frac
}
