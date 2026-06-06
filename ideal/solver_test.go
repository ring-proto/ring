package ideal

import (
	"fmt"
	"testing"
)

func buildLine(g *Graph, n int) {
	for i := range n {
		g.Add(fmt.Sprintf("v%d", i), float64(i), 0)
	}
	for i := 0; i+1 < n; i++ {
		g.Link(fmt.Sprintf("v%d", i), fmt.Sprintf("v%d", i+1))
	}
}

// A single pinned source produces a field that decays strictly and monotonically away from it,
// bounded in [0, F0].
func TestSolveSingleSourceMonotone(t *testing.T) {
	g := NewGraph(1, 0.05)
	buildLine(g, 9)
	f := g.Solve(map[string]float64{"v0": 1}, nil, 20000, 1e-12)
	prev := f.Pot("v0")
	if prev != 1 {
		t.Fatalf("source not pinned: v0=%.6f", prev)
	}
	for i := 1; i < 9; i++ {
		p := f.Pot(fmt.Sprintf("v%d", i))
		if p <= 0 || p >= prev {
			t.Errorf("v%d=%.6f not strictly in (0, %.6f)", i, p, prev)
		}
		prev = p
	}
}

// Maximum principle (§5.4): no non-pinned node exceeds its largest neighbour — the structural reason
// there are no interior maxima, hence no black holes. Tested on an irregular graph.
func TestMaxPrinciple(t *testing.T) {
	g := NewGraph(1, 0.05)
	RandomGeometric(g, 40, 0.30, 1)
	f := g.Solve(map[string]float64{"v0": 1}, nil, 20000, 1e-12)
	for i, nd := range g.Nodes {
		if nd.ID == "v0" || len(g.adj[i]) == 0 {
			continue
		}
		maxNbr := 0.0
		for _, e := range g.adj[i] {
			if f.F[e.to] > maxNbr {
				maxNbr = f.F[e.to]
			}
		}
		if f.F[i] > maxNbr+1e-9 {
			t.Errorf("%s = %.6f exceeds its max neighbour %.6f (interior maximum!)", nd.ID, f.F[i], maxNbr)
		}
	}
}

// Two sources create two attracting basins with a watershed: the field is symmetric on a symmetric
// line and bottoms out in the middle.
func TestTwoBasinsWatershed(t *testing.T) {
	g := NewGraph(1, 0.05)
	buildLine(g, 9) // v0..v8
	f := g.Solve(map[string]float64{"v0": 1, "v8": 1}, nil, 20000, 1e-12)
	for i := 0; i < 9; i++ {
		a, b := f.Pot(fmt.Sprintf("v%d", i)), f.Pot(fmt.Sprintf("v%d", 8-i))
		if d := a - b; d < -1e-9 || d > 1e-9 {
			t.Errorf("not symmetric: v%d=%.6f vs v%d=%.6f", i, a, 8-i, b)
		}
	}
	mid := f.Pot("v4")
	if mid >= f.Pot("v3") || mid >= f.Pot("v5") {
		t.Errorf("watershed not at the middle: v3=%.6f v4=%.6f v5=%.6f", f.Pot("v3"), mid, f.Pot("v5"))
	}
}

// Every steepest-ascent streamline flows to a source: from the far end of a line it climbs to v0.
func TestStreamlineReachesSource(t *testing.T) {
	g := NewGraph(1, 0.05)
	buildLine(g, 9)
	f := g.Solve(map[string]float64{"v0": 1}, nil, 20000, 1e-12)
	path := f.Streamline("v8", 64)
	if len(path) == 0 || path[len(path)-1] != "v0" {
		t.Errorf("streamline from v8 did not reach the source v0: %v", path)
	}
}
