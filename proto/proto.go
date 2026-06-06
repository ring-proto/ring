// Package proto is a clean-room, goroutine-per-node implementation of the converged RING
// protocol. Each node is a goroutine that owns its routing state; neighbours communicate ONLY
// through buffered inbox channels. Each node keeps its OWN local clock, with no synchronization
// assumed — there is no global tick, no lock-step, and no protocol timers. A node maintains, per
// destination, two transported scalar fields: a screened routing potential f (forwarded
// up-gradient) and a retarded vitality V (liveness confidence, thresholded by the consumer).
// Vitality is aged in clock-free proper time — one Tick per relay (HopAge), so a node never
// compares timestamps across the unsynchronized clocks. This is the operational reference for
// protocol.md and the honest async test of the theory in ring.md.
package proto

import (
	"math"
	"slices"
	"sync"
	"time"
)

type NodeID string

// Entry is one advertised triple on the wire: a destination, the sender's routing potential
// toward it, and the sender's vitality toward it.
type Entry struct {
	Dest NodeID
	Pot  float64
	Vit  float64
}

// Message is one multicast emission to a node's neighbours. The base (clock-free) protocol needs
// only From, Hears, and Delta. Emit is OPTIONAL: it is consumed only by the clock-synced
// refinement (Config.HopAge=false), where a receiver with a synchronized clock may age by the
// measured transit instead of a fixed Tick. The clock-free default ignores it.
type Message struct {
	From  NodeID
	Hears []NodeID // neighbours From currently hears — the 2-way adjacency echo
	Delta []Entry
	Emit  time.Time // optional: emission timestamp, used only by the clock-synced refinement
}

// Config is the protocol's parameter set. Times are wall-clock; Tick is the proper-time
// quantum (and the nominal emission period), so aging rate Alpha and transit are both counted
// in Ticks.
type Config struct {
	D     float64       // bulk conductance (diffusivity)
	Kappa float64       // screening coefficient; reach lambda = sqrt(D/Kappa)
	F0    float64       // source self-pin
	Floor float64       // reachability floor on the field
	Alpha float64       // vitality aging rate per Tick of proper time
	Phi   float64       // vitality action threshold
	Tick  time.Duration // proper-time quantum and nominal emission period
	Fresh float64       // link-layer freshness window, in Ticks (adjacency liveness only)
	HopAge bool         // retardation mode: false = measured cross-clock transit (needs clock sync);
	//                     true = fixed one-Tick per relay (clock-free, robust to skew)
	Crowd float64 // congestion coupling c: a node's charge target is c·stress (0 = congestion off)
	Glide float64 // glide rate r of the non-overshoot integrator reg() (only relevant when Crowd>0)
}

func DefaultConfig() Config {
	return Config{D: 1, Kappa: 0.05, F0: 1, Floor: 1e-4, Alpha: 0.04, Phi: 0.5,
		Tick: 4 * time.Millisecond, Fresh: 8, HopAge: true, // clock-free by default
		Crowd: 0, Glide: 1} // congestion off by default (Crowd=0 ⇒ charge stays 0)
}

// age applies continuous proper-time vitality decay V(t) = V0 / (1 + Alpha*ticks*V0), the
// exact flow of dV/dt = -Alpha*V^2, whose value at integer k from V0=1 is 1/(1+Alpha*k). It
// accepts a fractional number of ticks, so real wall-clock elapsed maps directly to aging —
// no synchronous unit-transit assumption.
func (c Config) age(v, ticks float64) float64 {
	if v <= 0 || ticks <= 0 {
		return v
	}
	return v / (1 + c.Alpha*ticks*v)
}

// reg is the non-overshoot integrator of §5.8: reg(v,T,r) = T + (v−T)/(1 + r·|v−T|). It shrinks the
// gap to the target T by a factor in (0,1) WITHOUT changing its sign, so the value glides toward T
// monotonically and never overshoots (Lemma 5.8). Because it approaches T algebraically (not
// exponentially), its Jacobian at the fixed point is exactly I — marginal — so the load↔charge loop
// it closes has no linear ringing mode, unlike a proportional update v←v+r(T−v).
func reg(v, target, r float64) float64 {
	return target + (v-target)/(1+r*math.Abs(v-target))
}

type route struct {
	pot, vit float64
	via      NodeID
	aged     time.Time // last wall time vit was aged toward 0
}

type peer struct {
	inbox chan Message       // the neighbour's inbox (we send here)
	pot   map[NodeID]float64 // its last-advertised potential per destination
	hears bool               // it echoes hearing us — the link is bidirectional
	heard time.Time          // last wall time we received from it
}

// Node is one protocol instance. Its routing state (peers, rib) is mutated only by its own Run
// goroutine, and guarded by mu solely so other goroutines (the harness / data plane) may read a
// consistent snapshot. The inbox channel is the only structure neighbours touch.
type Node struct {
	id    NodeID
	cfg   Config
	inbox chan Message
	skew  time.Duration // this node's clock offset from real time (models unsynchronized clocks)

	mu    sync.Mutex
	peers map[NodeID]*peer
	rib   map[NodeID]*route

	// congestion screening charge (§5.7): stress is the offered load s_i (written by the data plane
	// / harness — the load side of the loop), charge is µ_i, glided toward Crowd·s_i by the
	// non-overshoot integrator reg() once per relax and added to the screened denominator
	// G_i+κ+µ_i. Both are protocol-local scalars under mu; µ_i≥0 keeps q_i≥κ>0, so every routing
	// guarantee holds unchanged at the larger charge (a loaded node simply has shorter reach).
	stress float64
	charge float64
}

// now is this node's LOCAL clock: real time plus its private skew. Durations measured against
// the node's own earlier now() readings are skew-free (the offset cancels); only timestamps
// compared across nodes carry the skew difference.
func (n *Node) now() time.Time { return time.Now().Add(n.skew) }

func NewNode(id NodeID, cfg Config) *Node {
	return &Node{id: id, cfg: cfg, inbox: make(chan Message, 1024),
		peers: map[NodeID]*peer{}, rib: map[NodeID]*route{}}
}

// link wires a directed physical adjacency: this node may send to other's inbox. Called during
// setup, before any goroutine starts.
func (n *Node) link(other *Node) {
	n.peers[other.id] = &peer{inbox: other.inbox, pot: map[NodeID]float64{}}
}

// Run is the node's goroutine: receive a frame, or (on its own cadence) relax-and-emit —
// forever, at its own pace, until stop is closed. No barrier couples it to any other node.
func (n *Node) Run(stop <-chan struct{}) {
	emit := time.NewTimer(n.cfg.Tick)
	defer emit.Stop()
	for {
		select {
		case <-stop:
			return
		case m := <-n.inbox:
			n.ingest(m)
		case <-emit.C:
			n.step()
			emit.Reset(n.cfg.Tick)
		}
	}
}

// ingest records a neighbour's advertisement: the 2-way echo, its advertised potentials, and —
// the retardation — its vitality aged by the message's transit time before the adopt-max.
func (n *Node) ingest(m Message) {
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.peers[m.From]
	if p == nil {
		return // not a wired neighbour
	}
	p.heard = now
	p.hears = slices.Contains(m.Hears, n.id)
	transit := 1.0 // clock-free: one relay = one Tick of proper time
	if !n.cfg.HopAge {
		transit = ticksBetween(m.Emit, now, n.cfg.Tick) // measured cross-clock transit (needs sync)
	}
	for _, e := range m.Delta {
		if e.Dest == n.id {
			continue // our own field is pinned locally, never learned
		}
		p.pot[e.Dest] = e.Pot
		r := n.ribOf(e.Dest, now)
		n.ageRoute(r, now)
		if v := n.cfg.age(e.Vit, transit); v > r.vit { // retarded adopt-max
			r.vit = v
		}
	}
}

// step is one relaxation-and-emit: age vitality, run one screened Jacobi sweep of the routing
// potential per destination, recompute the dominant up-gradient hop, and multicast the self-pin
// plus every routable destination to all neighbours.
func (n *Node) step() {
	now := n.now()
	n.mu.Lock()
	bidir := n.bidirPeers(now)
	gsum := n.cfg.D * float64(len(bidir))
	// glide the congestion charge toward its load target once per relax (§5.8); inert when Crowd=0.
	n.charge = reg(n.charge, n.cfg.Crowd*n.stress, n.cfg.Glide)
	deltas := []Entry{{Dest: n.id, Pot: n.cfg.F0, Vit: 1}} // self-pin: a live source asserts V=1
	for _, d := range n.destSet(now) {
		if d == n.id {
			continue
		}
		r := n.ribOf(d, now)
		n.ageRoute(r, now)
		var fsum float64
		for _, p := range bidir {
			fsum += n.cfg.D * p.pot[d]
		}
		if gsum > 0 {
			r.pot = fsum / (gsum + n.cfg.Kappa + n.charge) // f = (Σ g·f_nbr)/(Σ g + κ + µ), q=κ+µ
		}
		r.via = ""
		var best float64
		for id, p := range bidir { // current split: dominant uphill (max-flux) neighbour
			if w := n.cfg.D * (p.pot[d] - r.pot); w > best {
				best, r.via = w, id
			}
		}
		if r.pot >= n.cfg.Floor && r.via != "" {
			deltas = append(deltas, Entry{Dest: d, Pot: r.pot, Vit: r.vit})
		}
	}
	msg := Message{From: n.id, Hears: n.heardList(now), Delta: deltas, Emit: now}
	targets := make([]chan Message, 0, len(n.peers))
	for _, p := range n.peers {
		targets = append(targets, p.inbox)
	}
	n.mu.Unlock()

	for _, ch := range targets { // non-blocking multicast: a full inbox drops (loss), never blocks
		select {
		case ch <- msg:
		default:
		}
	}
}

// Route is the data-plane / inspection read: the dominant next hop toward dst and the action
// decision (known AND vitality above threshold). The vitality is aged to now on the fly,
// without storing, so the read does not mutate.
func (n *Node) Route(dst NodeID) (via NodeID, vit float64, routable bool) {
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	r := n.rib[dst]
	if r == nil {
		return "", 0, false
	}
	v := n.cfg.age(r.vit, ticksBetween(r.aged, now, n.cfg.Tick))
	known := r.pot >= n.cfg.Floor && r.via != ""
	return r.via, v, known && v >= n.cfg.Phi
}

// Field is the instrumentation read: a non-mutating snapshot of both transported scalars toward
// dst — the screened routing potential pot (f) and the retarded vitality vit (V, aged to now) —
// plus the dominant up-gradient hop. known reports whether the field carries a usable route
// (above the reachability floor with a next hop). It is the measurement surface the figure
// generator samples to render the F/V fields and their gradients; unlike Route it does not apply
// the φ action threshold, so it exposes the raw field value at every node, routable or not.
func (n *Node) Field(dst NodeID) (pot, vit float64, via NodeID, known bool) {
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	r := n.rib[dst]
	if r == nil {
		return 0, 0, "", false
	}
	v := n.cfg.age(r.vit, ticksBetween(r.aged, now, n.cfg.Tick))
	return r.pot, v, r.via, r.pot >= n.cfg.Floor && r.via != ""
}

// SetStress sets the node's offered load s_i — the load side of the congestion loop. The data plane
// or harness writes it; the node's next relax glides its charge µ_i toward Crowd·s_i. Protocol code
// never calls this: load is an external physical signal, not something the routing reads back.
func (n *Node) SetStress(s float64) {
	n.mu.Lock()
	n.stress = s
	n.mu.Unlock()
}

// Charge reports the node's current congestion screening charge µ_i (instrumentation for the
// congestion figures).
func (n *Node) Charge() float64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.charge
}

// --- helpers (all called under n.mu) ---

func (n *Node) ribOf(d NodeID, now time.Time) *route {
	r := n.rib[d]
	if r == nil {
		r = &route{aged: now}
		n.rib[d] = r
	}
	return r
}

func (n *Node) ageRoute(r *route, now time.Time) {
	r.vit = n.cfg.age(r.vit, ticksBetween(r.aged, now, n.cfg.Tick))
	r.aged = now
}

func (n *Node) bidirPeers(now time.Time) map[NodeID]*peer {
	out := map[NodeID]*peer{}
	for id, p := range n.peers {
		if p.hears && ticksBetween(p.heard, now, n.cfg.Tick) < n.cfg.Fresh {
			out[id] = p
		}
	}
	return out
}

func (n *Node) destSet(now time.Time) []NodeID {
	set := map[NodeID]bool{}
	for _, p := range n.bidirPeers(now) {
		for d := range p.pot {
			set[d] = true
		}
	}
	for d := range n.rib {
		set[d] = true
	}
	out := make([]NodeID, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	slices.Sort(out)
	return out
}

func (n *Node) heardList(now time.Time) []NodeID {
	var out []NodeID
	for id, p := range n.peers {
		if ticksBetween(p.heard, now, n.cfg.Tick) < n.cfg.Fresh {
			out = append(out, id)
		}
	}
	return out
}

func ticksBetween(earlier, now time.Time, tick time.Duration) float64 {
	d := now.Sub(earlier)
	if d <= 0 {
		return 0
	}
	return float64(d) / float64(tick)
}
