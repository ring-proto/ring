// Package ideal is a direct (Gauss–Seidel) solver of the screened-balance operator (L_g+Q)f=0 over
// an arbitrary weighted graph, for the PEDAGOGICAL field figures. It computes the EXACT discrete
// field the async protocol relaxes toward — but solved centrally, with several pinned sources at
// once and an arbitrary per-node screening charge. This is deliberately NOT what the per-destination
// async protocol computes (it never superposes sources); figures built on it must be captioned as an
// idealized direct solve, distinct from the measured-async figures.
package ideal

import (
	"fmt"
	"math"
	"math/rand"
)

// Node is a graph vertex with a 2-D embedding (for rendering).
type Node struct {
	ID   string
	X, Y float64
}

type edge struct {
	to int
	g  float64
}

// Graph is an undirected weighted graph with uniform edge conductance D and screening floor Kappa —
// the same operator constants as the async protocol.
type Graph struct {
	Nodes []Node
	D     float64
	Kappa float64
	idx   map[string]int
	adj   [][]edge
}

func NewGraph(d, kappa float64) *Graph {
	return &Graph{D: d, Kappa: kappa, idx: map[string]int{}}
}

// Add inserts a node at (x,y); duplicate ids are ignored.
func (g *Graph) Add(id string, x, y float64) {
	if _, ok := g.idx[id]; ok {
		return
	}
	g.idx[id] = len(g.Nodes)
	g.Nodes = append(g.Nodes, Node{ID: id, X: x, Y: y})
	g.adj = append(g.adj, nil)
}

// Link adds an undirected edge of conductance D between a and b.
func (g *Graph) Link(a, b string) {
	i, j := g.idx[a], g.idx[b]
	if i == j {
		return
	}
	g.adj[i] = append(g.adj[i], edge{to: j, g: g.D})
	g.adj[j] = append(g.adj[j], edge{to: i, g: g.D})
}

// Edges returns each undirected edge once, as an (a,b) id pair — for rendering the graph structure.
func (g *Graph) Edges() [][2]string {
	var out [][2]string
	for i := range g.Nodes {
		for _, e := range g.adj[i] {
			if i < e.to {
				out = append(out, [2]string{g.Nodes[i].ID, g.Nodes[e.to].ID})
			}
		}
	}
	return out
}

// Pos returns a node's 2-D position.
func (g *Graph) Pos(id string) (x, y float64) {
	if i, ok := g.idx[id]; ok {
		return g.Nodes[i].X, g.Nodes[i].Y
	}
	return 0, 0
}

// Field is the exact solved potential over the graph, plus the screening charge used.
type Field struct {
	G  *Graph
	F  []float64
	Mu []float64
}

// Solve relaxes the screened balance f_i = (Σ_j g_ij f_j)/(Σ_j g_ij + κ + µ_i) by Gauss–Seidel,
// holding every pinned source at its value, until the max update drops below tol or iters is hit.
// pins maps node id -> pinned potential (the sources); mu maps node id -> extra screening charge
// (absent = 0). The result is the exact discrete field — an M-matrix solve with classical
// convergence.
func (g *Graph) Solve(pins, mu map[string]float64, iters int, tol float64) *Field {
	n := len(g.Nodes)
	f := make([]float64, n)
	muv := make([]float64, n)
	pinned := make([]bool, n)
	for id, v := range pins {
		if i, ok := g.idx[id]; ok {
			f[i], pinned[i] = v, true
		}
	}
	for id, v := range mu {
		if i, ok := g.idx[id]; ok {
			muv[i] = v
		}
	}
	for it := 0; it < iters; it++ {
		var maxDelta float64
		for i := range g.Nodes {
			if pinned[i] {
				continue
			}
			num, den := 0.0, g.Kappa+muv[i]
			for _, e := range g.adj[i] {
				num += e.g * f[e.to]
				den += e.g
			}
			nv := 0.0
			if den > 0 {
				nv = num / den
			}
			if d := math.Abs(nv - f[i]); d > maxDelta {
				maxDelta = d
			}
			f[i] = nv // Gauss–Seidel: use updated values within the sweep
		}
		if maxDelta < tol {
			break
		}
	}
	return &Field{G: g, F: f, Mu: muv}
}

// Pot returns the solved potential at a node.
func (f *Field) Pot(id string) float64 { return f.F[f.G.idx[id]] }

// Flux is the discrete current along edge a→b, J = g·(f_a − f_b): positive when it flows downhill
// from a to b. (Flow toward a source runs up-gradient, so the current INTO a source is positive.)
func (f *Field) Flux(a, b string) float64 {
	i, j := f.G.idx[a], f.G.idx[b]
	return f.G.D * (f.F[i] - f.F[j])
}

// Via returns the steepest-ascent neighbour of id (the up-gradient hop toward a source), or "" if no
// neighbour is higher — i.e. id is a source or a local maximum.
func (f *Field) Via(id string) string {
	i := f.G.idx[id]
	best, bj := f.F[i], -1
	for _, e := range f.G.adj[i] {
		if f.F[e.to] > best {
			best, bj = f.F[e.to], e.to
		}
	}
	if bj < 0 {
		return ""
	}
	return f.G.Nodes[bj].ID
}

// Streamline traces the steepest-ascent walk from id up the gradient, returning the node path until
// it reaches a local maximum (a source) or maxHops. The discrete counterpart of a field line.
func (f *Field) Streamline(id string, maxHops int) []string {
	path := []string{id}
	seen := map[string]bool{id: true}
	for len(path) <= maxHops {
		nxt := f.Via(path[len(path)-1])
		if nxt == "" || seen[nxt] {
			break
		}
		seen[nxt] = true
		path = append(path, nxt)
	}
	return path
}

// RandomGeometric lays n nodes via a seeded RNG in the unit square and links every pair closer than
// radius — a deterministic irregular planar-ish graph for the field figures. Node ids are "v0".."vN".
func RandomGeometric(g *Graph, n int, radius float64, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	xs := make([]float64, n)
	ys := make([]float64, n)
	for i := range n {
		xs[i], ys[i] = rng.Float64(), rng.Float64()
		g.Add(nodeID(i), xs[i], ys[i])
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			dx, dy := xs[i]-xs[j], ys[i]-ys[j]
			if math.Hypot(dx, dy) <= radius {
				g.Link(nodeID(i), nodeID(j))
			}
		}
	}
}

func nodeID(i int) string { return fmt.Sprintf("v%d", i) }
