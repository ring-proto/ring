package proto

import (
	"testing"
	"time"
)

// Killing a cut-vertex partitions the far side from the destination (it severs), and Reviving it
// heals the partition (the route reconverges) — node-failure recovery end to end.
func TestReviveHealsNodePartition(t *testing.T) {
	m := NewMesh(DefaultConfig())
	ids := BuildLine(m, 5)
	dst, src := ids[0], ids[4]
	m.Start()
	defer m.Stop()
	routable := func() bool { _, _, r := m.Node(src).Route(dst); return r }

	if !waitUntil(5*time.Second, routable) {
		t.Fatal("did not converge")
	}
	m.Kill(ids[2]) // cut the line at the middle
	if !waitUntil(5*time.Second, func() bool { return !routable() }) {
		t.Fatal("expected the far side to sever after the cut-vertex died")
	}
	m.Revive(ids[2])
	if !waitUntil(8*time.Second, routable) {
		t.Error("expected the route to reconverge after reviving the cut-vertex")
	}
}

// Cutting an edge partitions the segment beyond it (it severs), and Relinking heals it (the route
// reconverges) — edge-failure recovery with both routers staying powered.
func TestCutRelinkHealsEdgePartition(t *testing.T) {
	m := NewMesh(DefaultConfig())
	ids := BuildLine(m, 4)
	dst, src := ids[0], ids[3]
	m.Start()
	defer m.Stop()
	routable := func() bool { _, _, r := m.Node(src).Route(dst); return r }

	if !waitUntil(5*time.Second, routable) {
		t.Fatal("did not converge")
	}
	m.Cut(ids[1], ids[2]) // sever the line between n1 and n2
	if !waitUntil(5*time.Second, func() bool { return !routable() }) {
		t.Fatal("expected severance after cutting the edge")
	}
	m.Relink(ids[1], ids[2])
	if !waitUntil(8*time.Second, routable) {
		t.Error("expected the route to reconverge after relinking the edge")
	}
}
