package proto

import (
	"testing"
	"time"
)

// reg glides strictly toward its target without ever overshooting or reversing (Lemma 5.8),
// approaching algebraically from either side.
func TestRegNonOvershoot(t *testing.T) {
	const target, r = 10.0, 1.0
	// from below: strictly increasing, never past target
	v, prev := 0.0, 0.0
	for i := range 200 {
		v = reg(v, target, r)
		if v <= prev-1e-12 || v > target+1e-12 {
			t.Fatalf("from below, step %d: not monotone/non-overshoot (v=%.9f prev=%.9f T=%.1f)", i, v, prev, target)
		}
		prev = v
	}
	if target-v > 0.05 {
		t.Errorf("did not approach target from below: v=%.4f T=%.1f", v, target)
	}
	// from above: strictly decreasing, never below target
	v, prev = 25.0, 25.0
	for i := range 200 {
		v = reg(v, target, r)
		if v >= prev+1e-12 || v < target-1e-12 {
			t.Fatalf("from above, step %d: not monotone/non-overshoot (v=%.9f prev=%.9f T=%.1f)", i, v, prev, target)
		}
		prev = v
	}
	if v-target > 0.05 {
		t.Errorf("did not approach target from above: v=%.4f T=%.1f", v, target)
	}
}

// A node under sustained stress glides its charge µ_i monotonically toward Crowd·s_i and settles
// there — never overshooting, never reversing.
func TestChargeGlidesTowardStress(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Crowd = 1.0
	cfg.Tick = 2 * time.Millisecond
	m := NewMesh(cfg)
	id := NodeID("solo")
	m.Add(id)
	m.Start()
	defer m.Stop()

	const target = 3.0 // Crowd(1) * stress(3)
	m.Node(id).SetStress(target)
	var prev float64
	conv := waitUntil(2*time.Second, func() bool {
		c := m.Node(id).Charge()
		if c < prev-1e-9 {
			t.Fatalf("charge decreased (%.6f -> %.6f)", prev, c)
		}
		if c > target+1e-9 {
			t.Fatalf("charge overshot target (%.6f > %.1f)", c, target)
		}
		prev = c
		return target-c < 0.05
	})
	if !conv {
		t.Errorf("charge did not converge to target (last=%.4f target=%.1f)", prev, target)
	}
}

// The screening effect (§5.7): a congested node's potential flattens, so the current split steers
// flow around it. With a short path S-A-D and a long path S-B-C-D, S prefers A when uncongested and
// diverts to B once A carries enough charge.
func TestCongestionDivertsRoute(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Crowd = 1.0
	m := NewMesh(cfg)
	S, A, B, C, D := NodeID("S"), NodeID("A"), NodeID("B"), NodeID("C"), NodeID("D")
	for _, id := range []NodeID{S, A, B, C, D} {
		m.Add(id)
	}
	m.Link(S, A)
	m.Link(A, D) // short path S-A-D (2 hops)
	m.Link(S, B)
	m.Link(B, C)
	m.Link(C, D) // long path S-B-C-D (3 hops)
	m.Start()
	defer m.Stop()

	if !waitUntil(5*time.Second, func() bool { via, _, r := m.Node(S).Route(D); return r && via == A }) {
		via, _, _ := m.Node(S).Route(D)
		t.Fatalf("uncongested: expected S to route via A (short path), got via=%q", via)
	}
	m.Node(A).SetStress(50) // heavy load on the short-path hop
	if !waitUntil(5*time.Second, func() bool { via, _, r := m.Node(S).Route(D); return r && via == B }) {
		via, _, _ := m.Node(S).Route(D)
		t.Fatalf("congested: expected S to divert via B, got via=%q (A.charge=%.2f)", via, m.Node(A).Charge())
	}
}

// The load side closes the loop: CarriedLoad concentrates flow on the nodes nearest the destination
// (every demand funnels through them), and ApplyStress turns that load into charge there.
func TestCarriedLoadClosesLoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Crowd = 1.0
	m := NewMesh(cfg)
	ids := BuildLine(m, 5)
	dst := ids[0]
	m.Start()
	defer m.Stop()
	if !waitUntil(5*time.Second, func() bool { _, _, r := m.Node(ids[4]).Route(dst); return r }) {
		t.Fatal("did not converge")
	}
	// one unit of demand from every node toward dst — the hop adjacent to dst carries them all
	var demands []Demand
	for _, s := range ids[1:] {
		demands = append(demands, Demand{Src: s, Dst: dst, Amount: 1})
	}
	load := m.CarriedLoad(demands, 64)
	if load[ids[1]] <= load[ids[4]] {
		t.Errorf("expected load to concentrate near dst: n1=%.1f n4=%.1f", load[ids[1]], load[ids[4]])
	}
	m.ApplyStress(load)
	if !waitUntil(2*time.Second, func() bool { return m.Node(ids[1]).Charge() > 1.5 }) {
		t.Errorf("most-loaded node's charge did not rise (n1.charge=%.2f)", m.Node(ids[1]).Charge())
	}
}

// Endogenous congestion: with no external stress at all, a bottleneck node self-measures the current
// flowing through it and develops a larger charge than an interior node. A barbell — two triangles
// joined by a single bridge edge — forces all cross-cluster current through the bridge.
func TestEndogenousCongestionAtBottleneck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Crowd = 0.5
	m := NewMesh(cfg)
	for _, id := range []NodeID{"a0", "a1", "a2", "b0", "b1", "b2"} {
		m.Add(id)
	}
	m.Link("a0", "a1")
	m.Link("a1", "a2")
	m.Link("a2", "a0") // cluster A triangle
	m.Link("b0", "b1")
	m.Link("b1", "b2")
	m.Link("b2", "b0") // cluster B triangle
	m.Link("a0", "b0") // the single bridge — all A↔B current crosses here
	m.Start()
	defer m.Stop()
	time.Sleep(1500 * time.Millisecond) // let the self-driven load↔charge loop settle

	bridge, interior := m.Node("a0").Charge(), m.Node("a1").Charge()
	if bridge <= interior {
		t.Errorf("bridge should self-congest more than an interior node (no injected load): a0=%.4f a1=%.4f",
			bridge, interior)
	}
	if m.Node("a0").Load() <= 0 {
		t.Errorf("bridge should measure positive carried current; got %.4f", m.Node("a0").Load())
	}
}

// Per-node κ is the response knob: raising a node's baseline screening sinks its potential (and
// shortens its reach), with congestion off so the effect is isolated.
func TestPerNodeKappaSinksField(t *testing.T) {
	cfg := DefaultConfig() // Crowd = 0: isolate κ from the congestion loop
	m := NewMesh(cfg)
	ids := BuildLine(m, 7)
	src, mid := ids[0], ids[3]
	m.Start()
	defer m.Stop()
	if !waitUntil(5*time.Second, func() bool { _, _, r := m.Node(ids[6]).Route(src); return r }) {
		t.Fatal("did not converge")
	}
	time.Sleep(500 * time.Millisecond)
	before, _, _, _ := m.Node(mid).Field(src)

	m.SetKappa(mid, cfg.Kappa*20) // crank the middle node's baseline screening
	time.Sleep(1 * time.Second)
	after, _, _, _ := m.Node(mid).Field(src)

	if after >= before {
		t.Errorf("higher κ should sink the node's potential: before=%.5f after=%.5f", before, after)
	}
}
