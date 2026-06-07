package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitCongestion measures the self-driven load↔charge loop (§5.7–5.8, the stability basin of Thm 5.9)
// on a barbell: two fully-connected clusters joined by ONE bridge edge, so every cross-cluster route
// funnels through the bridge hub. The hub self-measures that carried current (Load()) and glides its
// screening charge µ (Charge()) toward Crowd·Load via the non-overshoot integrator reg() — no injected
// stress, the loop is entirely internal. Two products, both averaged over reps (the sim is async/real-
// time, so a single run is noisy):
//
//   - congestion_ts.dat: the bottleneck's charge trajectory under the three named couplings
//     (Crowd = 0.3, 1.0, 2.0), as raw µ and as µ normalised to its own equilibrium µ∞. All three are
//     INSIDE the basin: the charge glides monotonically to its fixed point with no overshoot — the
//     damped settle the integrator guarantees. Columns: t mu0p3 mu1 mu2 n0p3 n1 n2.
//   - congestion_basin.dat: a wide coupling sweep of the transient overshoot (peak µ above equilibrium,
//     %), median with a 10–90 band over reps. It stays ~0 across the named couplings (the wide basin)
//     and climbs only once the gain is large enough to amplify the convergence load-spike — the basin
//     boundary made empirical. Columns: crowd over_med over_lo over_hi.
//
// Measured from the reference implementation; never a single run.
func emitCongestion(dir string) {
	emitCongestionTimeseries(filepath.Join(dir, "congestion_ts.dat"))
	emitCongestionBasin(filepath.Join(dir, "congestion_basin.dat"))
}

// barbell wires two K-cliques joined by a single bridge edge a0—b0 and returns the bridge hub a0.
// Within each cluster every pair is linked, so the ONLY A↔B path is across the bridge: all
// cross-cluster current concentrates on the hub, which therefore self-congests.
func barbell(m *proto.Mesh, K int) proto.NodeID {
	idA := func(i int) proto.NodeID { return proto.NodeID(fmt.Sprintf("a%d", i)) }
	idB := func(i int) proto.NodeID { return proto.NodeID(fmt.Sprintf("b%d", i)) }
	for i := 0; i < K; i++ {
		m.Add(idA(i))
	}
	for i := 0; i < K; i++ {
		m.Add(idB(i))
	}
	for i := 0; i < K; i++ {
		for j := i + 1; j < K; j++ {
			m.Link(idA(i), idA(j))
			m.Link(idB(i), idB(j))
		}
	}
	m.Link(idA(0), idB(0)) // the single bridge — every A↔B route crosses here
	return idA(0)
}

// chargeTrace runs one barbell to wall-clock T, sampling the hub's charge µ every `step` and returning
// the trajectory on a fixed time grid (so reps can be averaged sample-for-sample). The loop is
// endogenous: no stress is injected, only Crowd is set.
func chargeTrace(crowd float64, samples int, step time.Duration) []float64 {
	cfg := proto.DefaultConfig()
	cfg.Crowd = crowd
	cfg.Tick = 20 * time.Millisecond // calmer tick so the async run is cheap
	m := proto.NewMesh(cfg)
	hub := barbell(m, 4)
	m.Start()
	defer m.Stop()
	trace := make([]float64, samples)
	t0 := time.Now()
	for i := 0; i < samples; i++ {
		trace[i] = m.Node(hub).Charge()
		// sleep to the next sample instant on a fixed grid (drift-free), as emitTimeseries does
		if next := t0.Add(time.Duration(i+1) * step); time.Now().Before(next) {
			time.Sleep(time.Until(next))
		}
	}
	return trace
}

// emitCongestionTimeseries writes the median charge trajectory at the three named couplings, raw and
// normalised to each curve's own equilibrium µ∞ (mean of the last quarter of the median trace). The
// normalised columns let the three couplings — whose absolute µ differ by an order of magnitude — be
// overlaid on one axis where 1.0 is equilibrium, so "monotone approach, no overshoot above 1" reads off
// directly. Median is taken across reps at each time index (robust to async jitter).
func emitCongestionTimeseries(path string) {
	const (
		reps    = 15
		samples = 120
	)
	step := 20 * time.Millisecond // 120 * 20ms = 2.4 s window
	crowds := []float64{0.3, 1.0, 2.0}

	// med[c][i] = median over reps of the charge at sample i for coupling c
	med := make([][]float64, len(crowds))
	for ci, crowd := range crowds {
		traces := make([][]float64, reps)
		for r := 0; r < reps; r++ {
			traces[r] = chargeTrace(crowd, samples, step)
		}
		med[ci] = make([]float64, samples)
		col := make([]float64, reps)
		for i := 0; i < samples; i++ {
			for r := 0; r < reps; r++ {
				col[r] = traces[r][i]
			}
			med[ci][i] = median(col)
		}
	}

	// equilibrium per coupling = mean of the last quarter of the median trace
	eq := make([]float64, len(crowds))
	for ci := range crowds {
		s, n := 0.0, 0
		for i := samples * 3 / 4; i < samples; i++ {
			s += med[ci][i]
			n++
		}
		eq[ci] = s / float64(n)
	}
	fmt.Fprintf(os.Stderr, "congestion ts: equilibria µ∞ = {0.3:%.3f, 1.0:%.3f, 2.0:%.3f}\n",
		eq[0], eq[1], eq[2])

	var b strings.Builder
	b.WriteString("t mu0p3 mu1 mu2 n0p3 n1 n2\n")
	for i := 0; i < samples; i++ {
		t := float64(i) * step.Seconds() * 1000
		fmt.Fprintf(&b, "%.1f %.5f %.5f %.5f %.5f %.5f %.5f\n",
			t, med[0][i], med[1][i], med[2][i],
			med[0][i]/eq[0], med[1][i]/eq[1], med[2][i]/eq[2])
	}
	write(path, b.String())
}

// emitCongestionBasin sweeps the congestion coupling and reports the transient overshoot of the hub's
// charge — the percentage by which the peak µ exceeds its equilibrium µ∞ — as median + a 10–90 band
// over reps. Overshoot ~0 means the self-driven loop glided in cleanly (inside the basin); a rising
// overshoot is the loop ringing as the gain amplifies the load-spike during field convergence. The
// named couplings (0.3, 1.0, 2.0) sit in the flat, well-damped floor; the basin boundary is where the
// curve lifts off.
func emitCongestionBasin(path string) {
	const reps = 15
	crowds := []float64{0.3, 0.6, 1.0, 1.5, 2.0, 3.0, 4.0, 6.0, 8.0, 12.0, 16.0, 20.0, 24.0}

	var b strings.Builder
	b.WriteString("crowd over_med over_lo over_hi\n")
	for _, crowd := range crowds {
		overs := make([]float64, reps)
		for r := 0; r < reps; r++ {
			// 2.5 s, sampled fine (5 ms) so the early peak is captured
			tr := chargeTrace(crowd, 500, 5*time.Millisecond)
			eq, n := 0.0, 0
			for i := len(tr) * 3 / 4; i < len(tr); i++ {
				eq += tr[i]
				n++
			}
			eq /= float64(n)
			peak := 0.0
			for _, v := range tr {
				if v > peak {
					peak = v
				}
			}
			overs[r] = 100 * (peak - eq) / (eq + 1e-9)
		}
		md, lo, hi := median(overs), percentile(overs, 0.1), percentile(overs, 0.9)
		fmt.Fprintf(&b, "%.2f %.4f %.4f %.4f\n", crowd, md, lo, hi)
		fmt.Fprintf(os.Stderr, "congestion basin: Crowd=%.1f overshoot med=%.2f%% [%.2f,%.2f]\n",
			crowd, md, lo, hi)
	}
	write(path, b.String())
}

func median(xs []float64) float64 { return percentile(xs, 0.5) }

func percentile(xs []float64, p float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s) == 0 {
		return 0
	}
	i := int(p * float64(len(s)-1))
	return s[i]
}
