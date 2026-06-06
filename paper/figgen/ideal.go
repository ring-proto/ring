package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ring-proto/ring/ideal"
)

// emitMultisource solves the screened operator on an IRREGULAR graph with TWO pinned sources and a
// high-µ congested sink, then writes the data for the keystone field figure: node potentials, the
// edge set, steepest-ascent streamlines that climb to a source and bend around the sink, and the
// source/sink markers. This is an [ideal] direct solve of (L_g+Q)f=0 — what the operator computes,
// not what the per-destination async protocol runs — and the figure must be captioned as such.
func emitMultisource(dir string) {
	g := ideal.NewGraph(1, 0.04)
	ideal.RandomGeometric(g, 60, 0.215, 3)
	s1 := nearest(g, 0.2, 0.5)
	s2 := nearest(g, 0.8, 0.5)
	// a congested sink: nodes near the centre carry an elevated screening charge µ
	sinkCx, sinkCy, sinkR := 0.5, 0.55, 0.15
	mu := map[string]float64{}
	for _, nd := range g.Nodes {
		if math.Hypot(nd.X-sinkCx, nd.Y-sinkCy) < sinkR {
			mu[nd.ID] = 4
		}
	}
	f := g.Solve(map[string]float64{s1: 1, s2: 1}, mu, 40000, 1e-12)
	mn, mx := 1.0, 0.0
	for _, nd := range g.Nodes {
		if p := f.Pot(nd.ID); p < mn {
			mn = p
		} else if p > mx {
			mx = p
		}
	}
	fmt.Fprintf(os.Stderr, "multisource: f(s1)=%.3f f(s2)=%.3f  range[%.3f,%.3f]  %d nodes %d edges\n",
		f.Pot(s1), f.Pot(s2), mn, mx, len(g.Nodes), len(g.Edges()))

	connected := map[string]bool{} // drop the occasional isolated node (no edges, f=0)
	for _, e := range g.Edges() {
		connected[e[0]], connected[e[1]] = true, true
	}
	var nodes strings.Builder // potential per node (colours the scatter)
	nodes.WriteString("x y f\n")
	for _, nd := range g.Nodes {
		if connected[nd.ID] {
			fmt.Fprintf(&nodes, "%.4f %.4f %.5f\n", nd.X, nd.Y, f.Pot(nd.ID))
		}
	}
	write(filepath.Join(dir, "ms_nodes.dat"), nodes.String())

	var edges strings.Builder // blank-line-separated 2-point segments → one \addplot
	for _, e := range g.Edges() {
		ax, ay := g.Pos(e[0])
		bx, by := g.Pos(e[1])
		fmt.Fprintf(&edges, "%.4f %.4f\n%.4f %.4f\n\n", ax, ay, bx, by)
	}
	write(filepath.Join(dir, "ms_edges.dat"), edges.String())

	var stream strings.Builder // steepest-ascent walks from a spread of start nodes
	for i, nd := range g.Nodes {
		if i%3 != 0 {
			continue // a subset, to avoid clutter
		}
		path := f.Streamline(nd.ID, 64)
		if len(path) < 2 {
			continue
		}
		for _, id := range path {
			x, y := g.Pos(id)
			fmt.Fprintf(&stream, "%.4f %.4f\n", x, y)
		}
		stream.WriteString("\n")
	}
	write(filepath.Join(dir, "ms_stream.dat"), stream.String())

	var src strings.Builder
	src.WriteString("x y\n")
	for _, s := range []string{s1, s2} {
		x, y := g.Pos(s)
		fmt.Fprintf(&src, "%.4f %.4f\n", x, y)
	}
	write(filepath.Join(dir, "ms_src.dat"), src.String())

	var sink strings.Builder
	sink.WriteString("x y\n")
	for _, nd := range g.Nodes {
		if mu[nd.ID] > 0 {
			fmt.Fprintf(&sink, "%.4f %.4f\n", nd.X, nd.Y)
		}
	}
	write(filepath.Join(dir, "ms_sink.dat"), sink.String())
}

func nearest(g *ideal.Graph, x, y float64) string {
	best, bd := "", math.Inf(1)
	for _, nd := range g.Nodes {
		if d := math.Hypot(nd.X-x, nd.Y-y); d < bd {
			bd, best = d, nd.ID
		}
	}
	return best
}
