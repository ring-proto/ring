// Package baseline holds comparison protocols RING is measured against — deliberately separate from
// the RING protocol itself. It is a foil, not part of the substrate.
//
// DV is a textbook SYNCHRONOUS distance-vector router set: Bellman-Ford on a hop metric, with NO
// split-horizon and NO poison reverse — the canonical count-to-infinity demonstrator. When a
// destination dies, the stale metric circulates upward between routers that each believe the other
// has a path, incrementing slowly toward a cap ("infinity") instead of failing cleanly. RING obeys
// the same min-plus recurrence in U = 1/V, but reads the bounded V, so the identical divergence is a
// graceful fade; this baseline makes that contrast visible on shared axes.
package baseline

// Inf is the unreachable sentinel; Cap is the classic count-to-infinity ceiling (RIP uses 16).
const defaultCap = 16

// DV is a set of synchronous distance-vector routers over a fixed undirected graph.
type DV struct {
	nodes []string
	adj   map[string][]string
	dist  map[string]map[string]int // dist[from][to] = current advertised hop metric (Cap = unreachable)
	dead  map[string]bool
	Cap   int
}

// NewDV builds the router set over an undirected adjacency list. Each node starts knowing only
// itself (0) and everything else as unreachable (Cap); call Converge to settle the true metrics.
func NewDV(adj map[string][]string, cap int) *DV {
	if cap <= 0 {
		cap = defaultCap
	}
	g := &DV{adj: map[string][]string{}, dist: map[string]map[string]int{}, dead: map[string]bool{}, Cap: cap}
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			g.nodes = append(g.nodes, n)
		}
	}
	for n, nbrs := range adj {
		add(n)
		g.adj[n] = append(g.adj[n], nbrs...)
		for _, m := range nbrs {
			add(m)
		}
	}
	for _, i := range g.nodes {
		row := map[string]int{}
		for _, d := range g.nodes {
			if i == d {
				row[d] = 0
			} else {
				row[d] = g.Cap
			}
		}
		g.dist[i] = row
	}
	return g
}

// Round runs one synchronous Bellman-Ford update across every router (all read the previous round's
// vectors, then all commit) toward every destination. No split-horizon: a router will adopt a metric
// offered by a neighbour even if that neighbour's path runs back through itself — the move that lets
// a dead destination's metric count to infinity.
func (g *DV) Round() {
	next := make(map[string]map[string]int, len(g.nodes))
	for _, i := range g.nodes {
		row := make(map[string]int, len(g.nodes))
		for _, d := range g.nodes {
			switch {
			case g.dead[i]:
				row[d] = g.Cap // a powered-off router advertises nothing
			case i == d:
				row[d] = 0 // a live router is distance 0 to itself
			default:
				best := g.Cap
				for _, j := range g.adj[i] {
					if g.dead[j] {
						continue
					}
					if dj := g.dist[j][d]; dj < g.Cap && dj+1 < best {
						best = dj + 1
					}
				}
				row[d] = best
			}
		}
		next[i] = row
	}
	g.dist = next
}

// Converge runs rounds until the vectors stop changing or maxRounds is hit; returns the rounds used.
func (g *DV) Converge(maxRounds int) int {
	for r := 1; r <= maxRounds; r++ {
		before := g.snapshot()
		g.Round()
		if g.equal(before) {
			return r
		}
	}
	return maxRounds
}

// Kill powers a router off: it stops advertising, so destinations behind it (or it itself) lose
// their source and the surviving routers count up.
func (g *DV) Kill(n string) { g.dead[n] = true }

// Metric reports router from's current advertised hop distance to dst (Cap = unreachable).
func (g *DV) Metric(from, dst string) int { return g.dist[from][dst] }

// Nodes returns the router ids in insertion order.
func (g *DV) Nodes() []string { return g.nodes }

func (g *DV) snapshot() map[string]map[string]int {
	cp := make(map[string]map[string]int, len(g.dist))
	for i, row := range g.dist {
		r := make(map[string]int, len(row))
		for d, v := range row {
			r[d] = v
		}
		cp[i] = r
	}
	return cp
}

func (g *DV) equal(other map[string]map[string]int) bool {
	for i, row := range g.dist {
		for d, v := range row {
			if other[i][d] != v {
				return false
			}
		}
	}
	return true
}
