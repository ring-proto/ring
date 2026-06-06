package proto

import (
	"testing"
	"time"
)

func waitUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestAsyncConvergeAndSever runs the converged protocol with one goroutine per node, no global
// tick and no lock-step: convergence to a live source, then severance of a dead one purely
// through the retarded vitality decay. The retardation here ages by REAL wall-clock transit, so
// if it could resurrect (the un-retarded lock), V would never cross the threshold and this test
// would hang to its deadline.
func TestAsyncConvergeAndSever(t *testing.T) {
	m := NewMesh(DefaultConfig())
	ids := BuildLine(m, 5) // n0 .. n4
	O, src := ids[0], ids[4]
	m.Start()
	defer m.Stop()

	if !waitUntil(2*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return r }) {
		_, v, _ := m.Node(O).Route(src)
		t.Fatalf("O never became routable to the live source (V=%.3f)", v)
	}
	via, vit, _ := m.Node(O).Route(src)
	t.Logf("converged: O routes to %s via %s, V=%.3f (4 hops; unit-transit ideal 1/(1+0.04·4)=%.3f)",
		src, via, vit, 1.0/(1+0.04*4))

	m.Kill(src) // the source goes silent
	if !waitUntil(4*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return !r }) {
		_, v, _ := m.Node(O).Route(src)
		t.Fatalf("O never severed the dead source — vitality stuck at %.3f >= phi (resurrection?)", v)
	}
	_, vit2, _ := m.Node(O).Route(src)
	t.Logf("severed: O's vitality to the dead source decayed to %.3f < phi=0.5", vit2)
}

// TestAsyncGridLiveness exercises a 2-D grid (multiple paths, richer concurrency) for the same
// converge-then-sever cycle, from a corner toward the opposite corner.
func TestAsyncGridLiveness(t *testing.T) {
	m := NewMesh(DefaultConfig())
	ids := BuildGrid(m, 3, 3)
	O, src := ids[0], ids[len(ids)-1] // opposite corners
	m.Start()
	defer m.Stop()

	if !waitUntil(2*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return r }) {
		t.Fatal("corner never became routable to the live opposite corner")
	}
	_, vit, _ := m.Node(O).Route(src)
	t.Logf("grid converged: corner->corner V=%.3f", vit)

	m.Kill(src)
	if !waitUntil(4*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return !r }) {
		t.Fatal("corner never severed the dead opposite corner")
	}
	t.Log("grid severed the dead corner")
}
