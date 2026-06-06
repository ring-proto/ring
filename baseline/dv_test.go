package baseline

import "testing"

// an undirected line A—B—C—D, D the destination
var lineAdj = map[string][]string{
	"A": {"B"},
	"B": {"A", "C"},
	"C": {"B", "D"},
	"D": {"C"},
}

func TestDVConvergesToHopDistance(t *testing.T) {
	g := NewDV(lineAdj, 16)
	g.Converge(50)
	for from, want := range map[string]int{"A": 3, "B": 2, "C": 1, "D": 0} {
		if got := g.Metric(from, "D"); got != want {
			t.Errorf("metric %s->D = %d, want %d", from, got, want)
		}
	}
}

// After the destination dies, the stale metric circulates upward and counts to the cap — the classic
// count-to-infinity, monotonically increasing, never recovering on its own.
func TestDVCountsToInfinity(t *testing.T) {
	g := NewDV(lineAdj, 16)
	g.Converge(50)
	if got := g.Metric("A", "D"); got != 3 {
		t.Fatalf("precondition: A->D should be 3, got %d", got)
	}
	g.Kill("D")
	prev := g.Metric("A", "D")
	reachedCap := false
	for r := range 100 {
		g.Round()
		m := g.Metric("A", "D")
		if m < prev {
			t.Fatalf("round %d: metric decreased %d -> %d (count-to-infinity only climbs)", r, prev, m)
		}
		prev = m
		if m >= g.Cap {
			reachedCap = true
			break
		}
	}
	if !reachedCap {
		t.Errorf("metric did not count to infinity; stuck at %d (cap %d)", prev, g.Cap)
	}
}
