package proto

import (
	"testing"
	"time"
)

// Per-link conductance (bandwidth) steers routing: with two equal-length paths S–A–D and S–B–D, the
// source prefers whichever path is fatter — the max-flux current split follows bandwidth × gradient,
// not just the gradient. Tested both ways so it is the conductance deciding, not a node-name tiebreak.
func TestPerLinkConductanceSteersRouting(t *testing.T) {
	build := func(gA, gB float64) (*Mesh, NodeID, NodeID) {
		m := NewMesh(DefaultConfig())
		S, A, B, D := NodeID("S"), NodeID("A"), NodeID("B"), NodeID("D")
		for _, id := range []NodeID{S, A, B, D} {
			m.Add(id)
		}
		m.LinkW(S, A, gA)
		m.LinkW(A, D, gA)
		m.LinkW(S, B, gB)
		m.LinkW(B, D, gB)
		m.Start()
		return m, S, D
	}
	routesVia := func(m *Mesh, src, dst, want NodeID) bool {
		return waitUntil(5*time.Second, func() bool { via, _, r := m.Node(src).Route(dst); return r && via == want })
	}

	mA, sA, dA := build(5, 1) // fat A path
	okA := routesVia(mA, sA, dA, "A")
	mA.Stop()

	mB, sB, dB := build(1, 5) // fat B path
	okB := routesVia(mB, sB, dB, "B")
	mB.Stop()

	if !okA {
		t.Error("fat A path: expected S to route via A")
	}
	if !okB {
		t.Error("fat B path: expected S to route via B")
	}
}
