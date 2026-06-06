package proto

import (
	"fmt"
	"time"
)

// Mesh owns a set of nodes and their goroutines, for tests and demos. Setup (Add/Link) happens
// before Start; after Start the nodes run concurrently and are touched only through their Route
// reads and Kill.
type Mesh struct {
	cfg   Config
	nodes map[NodeID]*Node
	order []NodeID
	stops map[NodeID]chan struct{}

	// down is PHYSICAL ground truth held by the simulated fabric — which routers are powered off.
	// It is owned by the sim and consumed ONLY by the data plane (Forward), to decide a packet's
	// physical fate: a crashed router forwards nothing. The PROTOCOL never reads this. A node
	// (proto.go) learns a peer is gone solely through the vitality field (V decaying to 0) and the
	// adjacency-freshness window — there is no global death oracle. That separation is the whole
	// point of Vitality: the gap between physical death (here, instant) and protocol detection
	// (V<φ, propagation-delayed) is exactly the black-hole window the data plane measures.
	down map[NodeID]bool
}

func NewMesh(cfg Config) *Mesh {
	return &Mesh{cfg: cfg, nodes: map[NodeID]*Node{}, stops: map[NodeID]chan struct{}{},
		down: map[NodeID]bool{}}
}

func (m *Mesh) Add(id NodeID) *Node {
	n := NewNode(id, m.cfg)
	m.nodes[id] = n
	m.order = append(m.order, id)
	return n
}

// Link wires a bidirectional physical adjacency between a and b.
func (m *Mesh) Link(a, b NodeID) {
	m.nodes[a].link(m.nodes[b])
	m.nodes[b].link(m.nodes[a])
}

func (m *Mesh) Node(id NodeID) *Node { return m.nodes[id] }

// Start launches every node's goroutine. The nodes run freely from here — no barrier, no shared
// tick.
func (m *Mesh) Start() {
	for _, id := range m.order {
		stop := make(chan struct{})
		m.stops[id] = stop
		go m.nodes[id].Run(stop)
	}
}

// Kill silences a node: it stops emitting and relaxing — a dead source / failed node. Its
// neighbours stop hearing it, so their vitality toward it ages to zero with nothing to refresh
// it (and, after the link-freshness window, its stale potential is dropped from their fields).
func (m *Mesh) Kill(id NodeID) {
	if s := m.stops[id]; s != nil {
		close(s)
		delete(m.stops, id)
	}
	m.down[id] = true // physical fabric records the router is powered off (data-plane only; see down)
}

// isDown reports the fabric's physical ground truth that a router is powered off. It exists for the
// data plane (Forward) alone; the protocol must never consult it — death is perceived only via
// Vitality. Unexported on purpose so no control-plane code can reach it.
func (m *Mesh) isDown(id NodeID) bool { return m.down[id] }

// Stop halts every running node.
func (m *Mesh) Stop() {
	for id, s := range m.stops {
		close(s)
		delete(m.stops, id)
	}
}

// BuildLine adds n nodes in a path n0 - n1 - ... - n(N-1).
func BuildLine(m *Mesh, n int) []NodeID {
	ids := make([]NodeID, n)
	for i := range n {
		ids[i] = NodeID(fmt.Sprintf("n%d", i))
		m.Add(ids[i])
	}
	for i := 0; i+1 < n; i++ {
		m.Link(ids[i], ids[i+1])
	}
	return ids
}

// BuildGrid adds a w×h 4-neighbour grid; returns ids in row-major order.
func BuildGrid(m *Mesh, w, h int) []NodeID {
	id := func(x, y int) NodeID { return NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	ids := make([]NodeID, 0, w*h)
	for y := range h {
		for x := range w {
			ids = append(ids, id(x, y))
			m.Add(id(x, y))
		}
	}
	for y := range h {
		for x := range w {
			if x+1 < w {
				m.Link(id(x, y), id(x+1, y))
			}
			if y+1 < h {
				m.Link(id(x, y), id(x, y+1))
			}
		}
	}
	return ids
}

// SetSkew gives a node a private clock offset (must be called before Start) to model
// unsynchronized clocks.
func (m *Mesh) SetSkew(id NodeID, d time.Duration) { m.nodes[id].skew = d }
