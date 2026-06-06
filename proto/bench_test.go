package proto

import (
	"testing"
	"time"
)

// BenchmarkStep measures one relaxation-and-emit on a node in a converged 20×20 grid (400
// destinations) at steady state — the hot path whose per-tick cost and allocations drove CPU. It is
// the apples-to-apples measure for the map→dense-slice vectorization.
func BenchmarkStep(b *testing.B) {
	cfg := DefaultConfig()
	m := NewMesh(cfg)
	ids := BuildGrid(m, 20, 20)
	m.Start()
	src, far := ids[0], ids[len(ids)-1]
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, r := m.Node(far).Route(src); r {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.Stop() // freeze converged state; step() still runs as a plain method
	n := m.Node(ids[len(ids)/2])
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		n.step()
	}
}
