package proto

import (
	"testing"
	"time"
)

// Dead-relay reroute is handled by the foundational link aging (g_eff = age(g, staleness), β=α), with
// NO freshness window. Diamond with redundancy — a preferred 2-hop path src-a-dst and a longer 3-hop
// backup src-b1-b2-dst. Killing the preferred relay `a` (dst stays alive via the backup) must reroute:
// a's silent link ages out (its conductance drains by the same proper-time law as vitality) and the
// current flows through the backup. Vitality toward dst stays high (dst is alive and reachable), so
// ONLY the aged conductance can do this — there is no timer and no Fresh threshold.
func buildDiamond() *Mesh {
	m := NewMesh(DefaultConfig())
	for _, id := range []NodeID{"src", "a", "dst", "b1", "b2"} {
		m.Add(id)
	}
	m.Link("src", "a") // preferred path (2 hops): src-a-dst
	m.Link("a", "dst")
	m.Link("src", "b1") // backup path (3 hops): src-b1-b2-dst
	m.Link("b1", "b2")
	m.Link("b2", "dst")
	return m
}

func TestLinkAgeReroutesAroundDeadRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}
	m := buildDiamond()
	m.Start()
	defer m.Stop()

	time.Sleep(500 * time.Millisecond) // converge
	if via, _, ok := m.Node("src").Route("dst"); !ok || via != "a" {
		t.Fatalf("pre-kill: expected src to prefer the 2-hop relay a, got via=%s ok=%v", via, ok)
	}

	m.Kill("a") // kill the preferred relay; dst still reachable via the backup
	assertEventuallyDeliversVia(t, m, "src", "dst", "b1", dp, 2500*time.Millisecond)
	t.Logf("post-kill: g_eff(dead a)=%.3f  g_eff(live b1)=%.3f",
		m.Node("src").EffConductance("a"), m.Node("src").EffConductance("b1"))
}

func TestLinkAgeReroutesAcrossParallelBridges(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	m := NewMesh(DefaultConfig())
	for _, id := range []NodeID{"src", "a", "b", "dst"} {
		m.Add(id)
	}
	m.LinkW("src", "a", 2) // preferred bridge: equal hop count, higher conductance
	m.LinkW("a", "dst", 2)
	m.Link("src", "b")
	m.Link("b", "dst")
	m.Start()
	defer m.Stop()

	if !waitUntil(2*time.Second, func() bool {
		via, _, ok := m.Node("src").Route("dst")
		return ok && via == "a"
	}) {
		via, _, ok := m.Node("src").Route("dst")
		t.Fatalf("pre-kill: expected src to prefer high-conductance bridge a, got via=%s ok=%v", via, ok)
	}

	m.Kill("a")
	assertEventuallyDeliversVia(t, m, "src", "dst", "b", DataPlane{HopDelay: time.Millisecond, TTL: 64}, 2500*time.Millisecond)
}

func TestLinkAgeKeepsGridReachableAfterInteriorFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	m := NewMesh(DefaultConfig())
	ids := BuildGrid(m, 4, 4)
	dst, src := ids[0], ids[len(ids)-1] // opposite corners
	m.Start()
	defer m.Stop()

	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}
	assertEventuallyDelivers(t, m, src, dst, dp, 2*time.Second)
	m.Kill("g1-1") // interior failure with many surviving routes around it
	assertEventuallyDelivers(t, m, src, dst, dp, 2500*time.Millisecond)
}

func TestLinkAgeReroutesAroundFailedSpine(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	m := NewMesh(DefaultConfig())
	for _, id := range []NodeID{"src", "dst", "spine1", "spine2", "leaf"} {
		m.Add(id)
	}
	m.LinkW("src", "spine1", 2) // preferred spine
	m.LinkW("spine1", "dst", 2)
	m.Link("src", "spine2")
	m.Link("spine2", "dst")
	m.Link("leaf", "spine1") // extra leaf makes this a small leaf-spine fabric, not just a diamond
	m.Link("leaf", "spine2")
	m.Start()
	defer m.Stop()

	if !waitUntil(2*time.Second, func() bool {
		via, _, ok := m.Node("src").Route("dst")
		return ok && via == "spine1"
	}) {
		via, _, ok := m.Node("src").Route("dst")
		t.Fatalf("pre-kill: expected src to prefer spine1, got via=%s ok=%v", via, ok)
	}

	m.Kill("spine1")
	assertEventuallyDeliversVia(t, m, "src", "dst", "spine2", DataPlane{HopDelay: time.Millisecond, TTL: 64}, 2500*time.Millisecond)
}

func TestLinkAgeHandlesFlappingRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	m := buildDiamond()
	m.Start()
	defer m.Stop()
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}

	assertEventuallyDeliversVia(t, m, "src", "dst", "a", dp, 2*time.Second)
	for cycle := 0; cycle < 2; cycle++ {
		m.Kill("a")
		assertEventuallyDeliversVia(t, m, "src", "dst", "b1", dp, 2500*time.Millisecond)
		m.Revive("a")
		assertEventuallyDeliversVia(t, m, "src", "dst", "a", dp, 4*time.Second)
	}
}

func TestLinkAgeReroutesAfterMultipleRelayFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async reroute")
	}
	m := NewMesh(DefaultConfig())
	for _, id := range []NodeID{"src", "a", "b", "c", "dst"} {
		m.Add(id)
	}
	m.LinkW("src", "a", 3)
	m.LinkW("a", "dst", 3)
	m.LinkW("src", "b", 2)
	m.LinkW("b", "dst", 2)
	m.Link("src", "c")
	m.Link("c", "dst")
	m.Start()
	defer m.Stop()
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}

	assertEventuallyDeliversVia(t, m, "src", "dst", "a", dp, 2*time.Second)
	m.Kill("a")
	m.Kill("b")
	assertEventuallyDeliversVia(t, m, "src", "dst", "c", dp, 2500*time.Millisecond)
}

func TestLinkAgeLiveLinksRemainStableUnderSawtooth(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async stability sample")
	}
	m, src, dst := convergedLine(t, 5)
	defer m.Stop()
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}

	minG, maxG := 1.0, 0.0
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		via, _, ok := m.Node(src).Route(dst)
		if !ok || via != "n3" {
			t.Fatalf("live line route perturbed under sawtooth: via=%s routable=%v", via, ok)
		}
		if d := m.Forward(src, dst, dp, nil); d.Outcome != Delivered || d.Hops != 4 {
			t.Fatalf("live line delivery/stretch perturbed under sawtooth: outcome=%v hops=%d path=%v", d.Outcome, d.Hops, d.Path)
		}
		g := m.Node(src).EffConductance("n3")
		if g < minG {
			minG = g
		}
		if g > maxG {
			maxG = g
		}
		time.Sleep(2 * time.Millisecond)
	}
	// With the κ-scaled default α≈0.224, a one-tick live-link sawtooth bottoms near
	// 1/(1+α)≈0.817; scheduler phase/jitter can sample slightly below that without
	// perturbing route choice or delivery. This test guards against unbounded drain,
	// not against ordinary live-link phase ripple.
	if minG < 0.75 {
		t.Fatalf("live-link sawtooth drained too far: min g_eff=%.3f max=%.3f", minG, maxG)
	}
	t.Logf("live-link sawtooth bounded: g_eff(n4→n3) min=%.3f max=%.3f", minG, maxG)
}

func TestLinkAgeRerouteLatencyCharacterization(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-based async latency characterization")
	}
	m := buildDiamond()
	m.Start()
	defer m.Stop()
	dp := DataPlane{HopDelay: time.Millisecond, TTL: 64}

	assertEventuallyDeliversVia(t, m, "src", "dst", "a", dp, 2*time.Second)
	start := time.Now()
	m.Kill("a")
	if !waitUntil(2500*time.Millisecond, func() bool {
		via, _, ok := m.Node("src").Route("dst")
		return ok && via == "b1"
	}) {
		via, vit, ok := m.Node("src").Route("dst")
		t.Fatalf("did not reroute within characterization bound: via=%s routable=%v vit=%.2f", via, ok, vit)
	}
	latency := time.Since(start)
	if frac := deliveryFraction(m, "src", "dst", dp, 100); frac < 0.95 {
		t.Fatalf("post-reroute delivery too low: %.2f", frac)
	}
	t.Logf("β=α=%.3f reroute latency after dead relay: %s; g_eff(dead a)=%.3f g_eff(live b1)=%.3f",
		DefaultConfig().Alpha, latency, m.Node("src").EffConductance("a"), m.Node("src").EffConductance("b1"))
}

func assertEventuallyDeliversVia(t *testing.T, m *Mesh, src, dst, wantVia NodeID, dp DataPlane, timeout time.Duration) {
	t.Helper()
	var via NodeID
	var vit float64
	var ok bool
	if !waitUntil(timeout, func() bool {
		via, vit, ok = m.Node(src).Route(dst)
		if !ok || via != wantVia {
			return false
		}
		return deliveryFraction(m, src, dst, dp, 50) >= 0.95
	}) {
		t.Fatalf("expected reroute from %s to %s via %s, got via=%s routable=%v vit=%.2f delivery=%.2f",
			src, dst, wantVia, via, ok, vit, deliveryFraction(m, src, dst, dp, 50))
	}
}

func assertEventuallyDelivers(t *testing.T, m *Mesh, src, dst NodeID, dp DataPlane, timeout time.Duration) {
	t.Helper()
	if !waitUntil(timeout, func() bool {
		return deliveryFraction(m, src, dst, dp, 50) >= 0.95
	}) {
		via, vit, ok := m.Node(src).Route(dst)
		t.Fatalf("expected delivery from %s to %s, got via=%s routable=%v vit=%.2f delivery=%.2f",
			src, dst, via, ok, vit, deliveryFraction(m, src, dst, dp, 50))
	}
}

func deliveryFraction(m *Mesh, src, dst NodeID, dp DataPlane, total int) float64 {
	delivered := 0
	for i := 0; i < total; i++ {
		if m.Forward(src, dst, dp, nil).Outcome == Delivered {
			delivered++
		}
	}
	return float64(delivered) / float64(total)
}
