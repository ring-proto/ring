package proto

import (
	"math/rand"
	"testing"
	"time"
)

// converged builds an N-node line toward dst = n0, starts it, and waits until src = n(N-1) carries
// a route. Returns the mesh and the (src, dst) pair; caller defers m.Stop().
func convergedLine(t *testing.T, n int) (*Mesh, NodeID, NodeID) {
	t.Helper()
	m := NewMesh(DefaultConfig())
	ids := BuildLine(m, n)
	dst, src := ids[0], ids[n-1]
	m.Start()
	if !waitUntil(5*time.Second, func() bool { _, _, r := m.Node(src).Route(dst); return r }) {
		m.Stop()
		t.Fatalf("line of %d did not converge", n)
	}
	return m, src, dst
}

// On a converged line the packet follows the unique up-gradient path and is delivered with stretch
// 1 (hop count == graph distance) and latency == hops * HopDelay.
func TestDataPlaneDeliversOnConvergedLine(t *testing.T) {
	m, src, dst := convergedLine(t, 5)
	defer m.Stop()
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}
	d := m.Forward(src, dst, dp, nil)
	if d.Outcome != Delivered {
		t.Fatalf("want Delivered, got %v (path %v)", d.Outcome, d.Path)
	}
	if d.Hops != 4 {
		t.Errorf("want 4 hops (unit stretch), got %d (path %v)", d.Hops, d.Path)
	}
	if d.Latency != 4*time.Millisecond {
		t.Errorf("want 4ms latency, got %v", d.Latency)
	}
}

// Killing an interior router partitions the source side from the destination. During the detection
// window the source still routes toward the destination and forwards INTO the dead hop (a black
// hole); once vitality severs, forwarding correctly declines (Severed) — never a permanent black
// hole. This is the gap the vitality field exists to close, observed end to end.
func TestDataPlaneBlackholeThenSeverAcrossPartition(t *testing.T) {
	m := NewMesh(DefaultConfig())
	ids := BuildLine(m, 5)
	dst, src := ids[0], ids[4]
	m.Start()
	defer m.Stop()
	if !waitUntil(5*time.Second, func() bool { _, _, r := m.Node(src).Route(dst); return r }) {
		t.Fatal("did not converge")
	}
	m.Kill(ids[2]) // cut the line in half

	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}
	var sawBlackhole, severed bool
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		switch o := m.Forward(src, dst, dp, nil).Outcome; {
		case o.Blackholed():
			sawBlackhole = true
		case o == Severed && sawBlackhole:
			severed = true
		}
		if severed {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !sawBlackhole {
		t.Error("expected a black hole during the post-failure detection window")
	}
	if !severed {
		t.Error("expected eventual correct severance after the partition (no permanent black hole)")
	}
}

// A destination in a disconnected component is never learned, so forwarding toward it is correctly
// declined (Severed), not black-holed.
func TestDataPlaneSeveredWhenUnreachable(t *testing.T) {
	m := NewMesh(DefaultConfig())
	a, b, c := NodeID("a"), NodeID("b"), NodeID("c")
	m.Add(a)
	m.Add(b)
	m.Add(c) // c is isolated — no links
	m.Link(a, b)
	m.Start()
	defer m.Stop()
	time.Sleep(250 * time.Millisecond) // let a/b converge what they can
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}
	if d := m.Forward(a, c, dp, nil); d.Outcome != Severed {
		t.Errorf("want Severed toward unreachable dst, got %v (path %v)", d.Outcome, d.Path)
	}
}

// A physical per-hop loss is distinct from a routing failure: with LossProb 1 the first hop drops
// (Lost), and with no loss the same path delivers.
func TestDataPlaneLossIsNotSeverance(t *testing.T) {
	m, src, dst := convergedLine(t, 5)
	defer m.Stop()
	rng := rand.New(rand.NewSource(1))
	if d := m.Forward(src, dst, DataPlane{HopDelay: time.Millisecond, LossProb: 1, TTL: 64}, rng); d.Outcome != Lost {
		t.Errorf("want Lost with LossProb=1, got %v", d.Outcome)
	}
	if d := m.Forward(src, dst, DataPlane{HopDelay: time.Millisecond, TTL: 64}, rng); d.Outcome != Delivered {
		t.Errorf("want Delivered with no loss, got %v", d.Outcome)
	}
}
