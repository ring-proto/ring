package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitCongestion measures the self-driven load↔charge loop (§5.7–5.8) on a barbell: two fully-connected
// clusters joined by ONE bridge edge, so every cross-cluster route funnels through the bridge hub. The
// hub self-measures the up-gradient current it forwards (Load()) and glides its screening charge µ
// (Charge()) toward Crowd·Load via the non-overshoot integrator reg() — no injected stress, the loop is
// entirely internal. Two products, both measured from the async reference implementation:
//
//   - congestion_ts.dat: the bottleneck's charge trajectory under the three named couplings
//     (Crowd = 0.3, 1.0, 2.0), raw µ and µ normalised to its own equilibrium µ∞. The charge glides
//     monotonically to its fixed point with no overshoot — the damped settle the integrator guarantees.
//     Columns: t mu0p3 mu1 mu2 n0p3 n1 n2.
//   - congestion_gain.dat: the realized loop gain Γ(c)=c·s'(µ*) at the operating point µ*=c·s(µ*), swept
//     over coupling c. Because the up-gradient load SATURATES (s'→0 as µ grows, the resolvent flattens),
//     the realized gain stays far below the Γ=1 instability threshold at EVERY coupling — the loop is
//     monotone-stable everywhere, with no basin boundary. We also emit the "naive" gain c·s'(µ_light) a
//     fixed light-load slope would predict (which crosses 1), to show why saturation defeats it.
//     Columns: crowd gamma_real gamma_naive mu_star s_star.
//
// NOTE on the diagonal sign (§5.7): the self-term ∂s_i/∂µ_i is NON-negative (raising µ_i sinks f_i and
// widens the up-gradient gap the node measures), i.e. the self-feedback is positive, not dissipative.
// It does not run away because s saturates: c·s'(µ*) stays < 1 at every coupling (this figure).
func emitCongestion(dir string) {
	emitCongestionTimeseries(filepath.Join(dir, "congestion_ts.dat"))
	emitCongestionGain(filepath.Join(dir, "congestion_gain.dat"))
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
	cfg := figCfg()
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

// settleEndogenous runs one barbell at the given coupling to steady state (endogenous loop, no stress)
// and returns the hub's settled charge µ* and carried load s*, each averaged over a tail window and over
// `reps` independent runs (the async sim is jittery). At the fixed point µ* = c·s* (the reg() target).
func settleEndogenous(crowd float64, reps int) (muStar, sStar float64) {
	for r := 0; r < reps; r++ {
		cfg := figCfg()
		cfg.Crowd = crowd
		cfg.Tick = 20 * time.Millisecond
		m := proto.NewMesh(cfg)
		hub := barbell(m, 4)
		m.Start()
		time.Sleep(2500 * time.Millisecond) // let the cold-start transient pass and µ settle
		var mu, s float64
		const navg = 16
		for k := 0; k < navg; k++ {
			mu += m.Node(hub).Charge()
			s += m.Node(hub).Load()
			time.Sleep(50 * time.Millisecond)
		}
		m.Stop()
		muStar += mu / navg
		sStar += s / navg
	}
	return muStar / float64(reps), sStar / float64(reps)
}

// fitSaturating fits s(µ) ≈ sInf − A/(µ+B) (A>0: increasing, saturating; s'(µ)=A/(µ+B)²>0 decreasing)
// by a grid search over B with a closed-form linear least-squares for (sInf, A) at each B. This smooths
// the jittery measured (µ*,s*) samples so the derivative s'(µ) is stable.
func fitSaturating(mu, s []float64) (sInf, A, B float64) {
	bestErr := math.Inf(1)
	for Bc := 0.05; Bc <= 40; Bc *= 1.12 {
		var s11, s12, s22, t1, t2 float64
		for i := range mu {
			x1 := 1.0
			x2 := -1.0 / (mu[i] + Bc)
			s11 += x1 * x1
			s12 += x1 * x2
			s22 += x2 * x2
			t1 += x1 * s[i]
			t2 += x2 * s[i]
		}
		det := s11*s22 - s12*s12
		if math.Abs(det) < 1e-12 {
			continue
		}
		si := (t1*s22 - t2*s12) / det
		a := (s11*t2 - s12*t1) / det
		var err float64
		for i := range mu {
			d := (si - a/(mu[i]+Bc)) - s[i]
			err += d * d
		}
		if err < bestErr {
			bestErr, sInf, A, B = err, si, a, Bc
		}
	}
	return
}

// emitCongestionGain sweeps the coupling c, measures the operating point (µ*, s*) of the endogenous loop
// at each, fits the saturating load response s(µ), and reports the realized loop gain Γ(c)=c·s'(µ*). The
// gain stays far below the Γ=1 instability threshold at every coupling because s saturates — the honest
// replacement for the old "overshoot basin" panel, which measured a cold-start field-convergence spike,
// not a stability boundary. We also emit the naive gain c·s'(µ_light) a fixed light-load slope predicts.
func emitCongestionGain(path string) {
	const reps = 6
	crowds := []float64{0.15, 0.2, 0.3, 0.5, 0.7, 1.0, 1.5, 2.0, 3.0, 4.0, 6.0, 8.0, 12.0, 16.0, 24.0, 32.0}

	mu := make([]float64, len(crowds))
	s := make([]float64, len(crowds))
	for i, c := range crowds {
		mu[i], s[i] = settleEndogenous(c, reps)
		fmt.Fprintf(os.Stderr, "congestion gain: c=%.2f µ*=%.3f s*=%.4f (µ*/c=%.4f)\n",
			c, mu[i], s[i], mu[i]/c)
	}

	sInf, A, B := fitSaturating(mu, s)
	fmt.Fprintf(os.Stderr, "congestion gain: fit s(µ)=%.4f − %.4f/(µ+%.4f)\n", sInf, A, B)
	sprime := func(m float64) float64 { return A / ((m + B) * (m + B)) }
	spLight := sprime(mu[0]) // the lightest-load slope — the "naive" fixed slope

	var b strings.Builder
	b.WriteString("crowd gamma_real gamma_naive mu_star s_star\n")
	for i, c := range crowds {
		gr := c * sprime(mu[i])
		gn := c * spLight
		fmt.Fprintf(&b, "%.3f %.5f %.5f %.4f %.5f\n", c, gr, gn, mu[i], s[i])
		fmt.Fprintf(os.Stderr, "congestion gain: c=%.2f Γ_real=%.4f Γ_naive=%.4f\n", c, gr, gn)
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
