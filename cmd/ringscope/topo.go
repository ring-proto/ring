package main

import (
	"fmt"
	"math"

	"github.com/ring-proto/ring/proto"
)

// Topo is a built topology: the node ids, their normalized 2-D positions for rendering, the
// adjacency (for drawing edges), an optional rectangular grid (non-nil only for the lattice, which
// enables the half-block heatmap views), and a sensible default destination.
type Topo struct {
	Name string
	Kind string
	IDs  []proto.NodeID
	Pos  map[proto.NodeID][2]float64        // normalized [0,1]^2
	Adj  map[proto.NodeID][]proto.NodeID
	Wt   map[[2]proto.NodeID]float64        // per-edge conductance (bandwidth), for rendering
	Grid [][]proto.NodeID                   // non-nil only for "grid"
	Dst  proto.NodeID
}

// topoParams carries the CLI sizing for every topology kind.
type topoParams struct {
	kind                          string
	w, h                          int // grid
	hosts                         int // star
	spines, leaves, hpl           int // clos
	ases, perAS                   int // internet
}

func newTopo(name, kind string) Topo {
	return Topo{Name: name, Kind: kind, Pos: map[proto.NodeID][2]float64{},
		Adj: map[proto.NodeID][]proto.NodeID{}, Wt: map[[2]proto.NodeID]float64{}}
}

func (t *Topo) add(m *proto.Mesh, id proto.NodeID, x, y float64) {
	m.Add(id)
	t.IDs = append(t.IDs, id)
	t.Pos[id] = [2]float64{x, y}
}

func (t *Topo) link(m *proto.Mesh, a, b proto.NodeID) { t.linkW(m, a, b, 1) }

// linkW wires an edge with conductance g (bandwidth) and records it for rendering.
func (t *Topo) linkW(m *proto.Mesh, a, b proto.NodeID, g float64) {
	if g == 1 {
		m.Link(a, b)
	} else {
		m.LinkW(a, b, g)
	}
	t.Adj[a] = append(t.Adj[a], b)
	t.Adj[b] = append(t.Adj[b], a)
	t.Wt[edgeKey(a, b)] = g
}

// buildTopo dispatches to the requested topology builder, adding nodes and links to m.
func buildTopo(m *proto.Mesh, p topoParams) Topo {
	switch p.kind {
	case "star":
		return buildStar(m, p.hosts)
	case "clos":
		return buildClos(m, p.spines, p.leaves, p.hpl)
	case "internet":
		return buildInternet(m, p.ases, p.perAS)
	default:
		return buildGrid(m, p.w, p.h)
	}
}

// buildGrid is the w×h 4-neighbour lattice — the only topology with a heatmap-compatible grid.
func buildGrid(m *proto.Mesh, w, h int) Topo {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	t := newTopo(fmt.Sprintf("grid %d×%d", w, h), "grid")
	t.Grid = make([][]proto.NodeID, h)
	id := func(x, y int) proto.NodeID { return proto.NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	for y := 0; y < h; y++ {
		t.Grid[y] = make([]proto.NodeID, w)
		for x := 0; x < w; x++ {
			px, py := 0.0, 0.0
			if w > 1 {
				px = float64(x) / float64(w-1)
			}
			if h > 1 {
				py = float64(y) / float64(h-1)
			}
			t.add(m, id(x, y), px, py)
			t.Grid[y][x] = id(x, y)
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x+1 < w {
				t.link(m, id(x, y), id(x+1, y))
			}
			if y+1 < h {
				t.link(m, id(x, y), id(x, y+1))
			}
		}
	}
	t.Dst = id(w/2, h/2)
	return t
}

// buildStar is a single switch with hosts hanging off it — a one-switch LAN. Every host routes to
// every other host in exactly two hops, through the hub.
func buildStar(m *proto.Mesh, hosts int) Topo {
	if hosts < 2 {
		hosts = 2
	}
	t := newTopo(fmt.Sprintf("star · 1 switch + %d hosts", hosts), "star")
	sw := proto.NodeID("sw")
	t.add(m, sw, 0.5, 0.5)
	for i := 0; i < hosts; i++ {
		th := 2 * math.Pi * float64(i) / float64(hosts)
		id := proto.NodeID(fmt.Sprintf("h%d", i))
		t.add(m, id, 0.5+0.42*math.Cos(th), 0.5+0.42*math.Sin(th))
		t.link(m, sw, id)
	}
	t.Dst = "h0"
	return t
}

// buildClos is a leaf-spine fabric: every leaf connects to every spine (a folded Clos), with hosts
// under each leaf. Cross-fabric traffic goes host→leaf→spine→leaf→host — equal-cost through any spine.
func buildClos(m *proto.Mesh, spines, leaves, hpl int) Topo {
	if spines < 1 {
		spines = 1
	}
	if leaves < 2 {
		leaves = 2
	}
	if hpl < 1 {
		hpl = 1
	}
	t := newTopo(fmt.Sprintf("leaf-spine · %d spine · %d leaf · %d host/leaf", spines, leaves, hpl), "clos")
	sp := make([]proto.NodeID, spines)
	for i := 0; i < spines; i++ {
		sp[i] = proto.NodeID(fmt.Sprintf("sp%d", i))
		t.add(m, sp[i], float64(i+1)/float64(spines+1), 0.12)
	}
	for j := 0; j < leaves; j++ {
		lf := proto.NodeID(fmt.Sprintf("lf%d", j))
		lx := float64(j+1) / float64(leaves+1)
		t.add(m, lf, lx, 0.5)
		for i := 0; i < spines; i++ {
			t.linkW(m, lf, sp[i], 4) // fat spine↔leaf fabric trunks
		}
		for k := 0; k < hpl; k++ {
			hid := proto.NodeID(fmt.Sprintf("h%d-%d", j, k))
			hx := lx + (float64(k)-float64(hpl-1)/2)*0.035
			t.add(m, hid, hx, 0.88)
			t.link(m, lf, hid)
		}
	}
	t.Dst = proto.NodeID(fmt.Sprintf("h%d-0", leaves-1)) // a far-corner host
	return t
}

// buildInternet is a region of autonomous systems: each AS is a ring of routers, and one border
// router per AS peers with the next AS's border in a ring — multi-AS inter-domain routing.
func buildInternet(m *proto.Mesh, ases, perAS int) Topo {
	if ases < 2 {
		ases = 2
	}
	if perAS < 3 {
		perAS = 3
	}
	t := newTopo(fmt.Sprintf("internet · %d AS × %d routers", ases, perAS), "internet")
	border := make([]proto.NodeID, ases)
	for a := 0; a < ases; a++ {
		phi := 2 * math.Pi * float64(a) / float64(ases)
		cx, cy := 0.5+0.34*math.Cos(phi), 0.5+0.34*math.Sin(phi)
		rs := make([]proto.NodeID, perAS)
		for i := 0; i < perAS; i++ {
			th := 2 * math.Pi * float64(i) / float64(perAS)
			rs[i] = proto.NodeID(fmt.Sprintf("a%dr%d", a, i))
			t.add(m, rs[i], cx+0.10*math.Cos(th), cy+0.10*math.Sin(th))
		}
		for i := 0; i < perAS; i++ { // intra-AS ring (fast local)
			t.linkW(m, rs[i], rs[(i+1)%perAS], 3)
		}
		border[a] = rs[0]
	}
	for a := 0; a < ases; a++ { // inter-AS peering (the WAN bottleneck)
		t.link(m, border[a], border[(a+1)%ases])
	}
	t.Dst = border[ases/2]
	return t
}
