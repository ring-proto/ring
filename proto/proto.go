// Package proto is a clean-room, goroutine-per-node implementation of the converged RING protocol.
// Each node is a goroutine that owns its routing state; neighbours communicate ONLY through buffered
// inbox channels. Each node keeps its OWN local clock, with no synchronization assumed — there is no
// global tick, no lock-step, and no protocol timers. A node maintains, per destination, two
// transported scalar fields: a screened routing potential f (forwarded up-gradient) and a retarded
// vitality V (liveness confidence, thresholded by the consumer). Vitality is aged in clock-free
// proper time — one Tick per relay (HopAge), so a node never compares timestamps across the
// unsynchronized clocks.
//
// State is stored as DENSE slices indexed by a shared destination index (the node set), not maps: a
// node's per-destination field is a contiguous []float64, so each relaxation is a tight,
// cache-friendly vector sweep with no per-tick hashing or allocation. The destination universe is the
// fixed node set, assigned an index at mesh build; pot=0 means "not yet learned" exactly as a missing
// map entry did. This is the operational reference for the protocol spec and the honest async test of
// the theory.
package proto

import (
	"math"
	"slices"
	"sync"
	"time"
)

type NodeID string

// Entry is one advertised triple on the wire: a destination (by shared index), the sender's routing
// potential toward it, and the sender's vitality toward it.
type Entry struct {
	Dest int
	Pot  float64
	Vit  float64
}

// Message is one multicast emission to a node's neighbours. The base (clock-free) protocol needs only
// From, Hears, and Delta. Emit is OPTIONAL: it is consumed only by the clock-synced refinement
// (Config.HopAge=false), where a receiver with a synchronized clock may age by the measured transit
// instead of a fixed Tick. The clock-free default ignores it.
type Message struct {
	From  int   // sender's node index
	Hears []int // neighbour indices From currently hears — the 2-way adjacency echo
	Delta []Entry
	Emit  time.Time
}

// Config is the protocol's parameter set. Times are wall-clock; Tick is the proper-time quantum (and
// the nominal emission period), so aging rate Alpha and transit are both counted in Ticks.
type Config struct {
	D      float64       // bulk conductance (diffusivity)
	Kappa  float64       // screening coefficient; reach lambda = sqrt(D/Kappa)
	F0     float64       // source self-pin
	Floor  float64       // reachability floor on the field
	Alpha  float64       // vitality aging rate per Tick of proper time
	Phi    float64       // vitality action threshold
	Tick   time.Duration // proper-time quantum and nominal emission period
	Fresh  float64       // link-layer freshness window, in Ticks (adjacency liveness only)
	HopAge bool          // retardation mode: false = measured cross-clock transit (needs clock sync);
	//                      true = fixed one-Tick per relay (clock-free, robust to skew)
	Crowd float64 // congestion coupling c: a node's charge target is c·stress (0 = congestion off)
	Glide float64 // glide rate r of the non-overshoot integrator reg() (only relevant when Crowd>0)
}

func DefaultConfig() Config {
	return Config{D: 1, Kappa: 0.05, F0: 1, Floor: 1e-4, Alpha: 0.04, Phi: 0.5,
		Tick: 4 * time.Millisecond, Fresh: 8, HopAge: true, // clock-free by default
		Crowd: 0, Glide: 1} // congestion off by default (Crowd=0 ⇒ charge stays 0)
}

// age applies continuous proper-time vitality decay V(t) = V0 / (1 + Alpha*ticks*V0), the exact flow
// of dV/dt = -Alpha*V^2, whose value at integer k from V0=1 is 1/(1+Alpha*k). It accepts a fractional
// number of ticks, so real wall-clock elapsed maps directly to aging — no synchronous unit-transit
// assumption. It is ADDITIVE in ticks: age(age(v,a),b)=age(v,a+b), which is why a single shared aging
// clock per node is exact.
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

// peer is a neighbour's directed state, keyed in the node's peer map by the neighbour's index. Its
// advertised potentials are a DENSE vector over the shared destination index — the array indexing
// that replaced the old per-destination map lookups.
type peer struct {
	idx   int
	inbox chan Message
	g     float64   // link conductance (bandwidth) toward this neighbour; weights it in the balance
	pot   []float64 // neighbour's last-advertised potential per destination index
	hears bool      // it echoes hearing us — the link is bidirectional
	heard time.Time // last wall time we received from it
}

// Node is one protocol instance. Its routing state is mutated only by its own Run goroutine and
// guarded by mu solely so other goroutines (the harness / data plane) may read a consistent snapshot.
// The per-destination fields f/vit/via are dense slices over the shared index; ids/index translate
// to/from NodeID only on the read path (Route/Field), never in the hot loop.
type Node struct {
	id    NodeID
	idx   int // this node's destination index
	cfg   Config
	inbox chan Message
	skew  time.Duration // this node's clock offset from real time (models unsynchronized clocks)

	ids   []NodeID       // shared index→id (immutable after setup)
	index map[NodeID]int // shared id→index (immutable after setup)
	n     int            // number of destinations (= node count)

	mu       sync.Mutex
	peers    map[int]*peer // by neighbour index — deg-sized, NOT the O(N²) hot path
	f        []float64     // routing potential per destination index
	vit      []float64     // vitality per destination index, as of lastAged
	via      []int         // dominant up-gradient hop (neighbour index, or -1) per destination
	lastAged time.Time     // wall time the whole vit vector was last aged

	// congestion screening charge (§5.7): stress is the offered load s_i (written by the data plane /
	// harness — the load side of the loop), charge is µ_i, glided toward Crowd·s_i by the non-overshoot
	// integrator reg() once per relax and added to the screened denominator G_i+κ+µ_i. µ_i≥0 keeps
	// q_i≥κ>0, so every routing guarantee holds unchanged at the larger charge.
	stress float64
	charge float64

	fbuf []float64 // reused neighbour-sum scratch (len n)
	bbuf []*peer   // reused bidirectional-neighbour scratch (deg)
	tbuf []chan Message
}

// now is this node's LOCAL clock: real time plus its private skew.
func (n *Node) now() time.Time { return time.Now().Add(n.skew) }

func NewNode(id NodeID, cfg Config) *Node {
	return &Node{id: id, cfg: cfg, inbox: make(chan Message, 1024), peers: map[int]*peer{}}
}

// setup binds the node to the shared destination index and allocates its dense state. Called by the
// mesh once, after all nodes exist and before any goroutine starts.
func (n *Node) setup(idx, count int, ids []NodeID, index map[NodeID]int) {
	n.idx, n.n, n.ids, n.index = idx, count, ids, index
	n.f = make([]float64, count)
	n.vit = make([]float64, count)
	n.via = make([]int, count)
	for d := range n.via {
		n.via[d] = -1
	}
	n.fbuf = make([]float64, count)
	n.lastAged = n.now()
	for _, p := range n.peers {
		if p.pot == nil {
			p.pot = make([]float64, count)
		}
	}
}

// link wires a directed physical adjacency by neighbour index, at the default link conductance.
// Called during setup, before any goroutine starts; the dense pot vector is sized later in setup.
func (n *Node) link(other *Node) { n.linkW(other, n.cfg.D) }

// linkW wires a directed adjacency with an explicit link conductance (bandwidth).
func (n *Node) linkW(other *Node, g float64) {
	n.peers[other.idx] = &peer{idx: other.idx, inbox: other.inbox, g: g}
}

// cutPeer and addPeer are RUNTIME adjacency edits (after Start), used by the sim fabric to sever and
// heal a physical edge. They take n.mu because the node's Run goroutine reads peers under it; a healed
// edge starts fresh (zeroed advertised potentials, not-yet-heard), so the link re-acquires like new.
func (n *Node) cutPeer(other *Node) {
	n.mu.Lock()
	delete(n.peers, other.idx)
	n.mu.Unlock()
}

func (n *Node) addPeer(other *Node, g float64) {
	n.mu.Lock()
	n.peers[other.idx] = &peer{idx: other.idx, inbox: other.inbox, g: g, pot: make([]float64, n.n)}
	n.mu.Unlock()
}

// Run is the node's goroutine: receive a frame, or (on its own cadence) relax-and-emit — forever, at
// its own pace, until stop is closed. No barrier couples it to any other node.
func (n *Node) Run(stop <-chan struct{}) {
	emit := time.NewTimer(n.tick())
	defer emit.Stop()
	for {
		select {
		case <-stop:
			return
		case m := <-n.inbox:
			n.ingest(m)
		case <-emit.C:
			n.step()
			emit.Reset(n.tick()) // re-read each cycle so SetTick can retune the cadence live
		}
	}
}

// ingest records a neighbour's advertisement: the 2-way echo, its advertised potentials (dense
// writes), and — the retardation — its vitality aged by the message's transit before the adopt-max.
func (n *Node) ingest(m Message) {
	now := n.now()
	n.mu.Lock()
	defer n.mu.Unlock()
	p := n.peers[m.From]
	if p == nil {
		return // not a wired neighbour
	}
	n.ageAll(now)
	p.heard = now
	p.hears = slices.Contains(m.Hears, n.idx)
	transit := 1.0 // clock-free: one relay = one Tick of proper time
	if !n.cfg.HopAge {
		transit = ticksBetween(m.Emit, now, n.cfg.Tick) // measured cross-clock transit (needs sync)
	}
	for _, e := range m.Delta {
		if e.Dest == n.idx || e.Dest < 0 || e.Dest >= n.n {
			continue // our own field is pinned locally; ignore out-of-range
		}
		p.pot[e.Dest] = e.Pot
		if v := n.cfg.age(e.Vit, transit); v > n.vit[e.Dest] { // retarded adopt-max
			n.vit[e.Dest] = v
		}
	}
}

// step is one relaxation-and-emit: age vitality, glide the congestion charge, run one screened Jacobi
// sweep of the routing potential over ALL destinations as a dense vector op, recompute the dominant
// up-gradient hop, and multicast the self-pin plus every routable destination to all neighbours.
func (n *Node) step() {
	now := n.now()
	n.mu.Lock()
	n.ageAll(now)
	n.charge = reg(n.charge, n.cfg.Crowd*n.stress, n.cfg.Glide) // §5.8 load→charge; inert when Crowd=0
	bidir := n.bidir(now)
	gsum := 0.0
	for _, p := range bidir {
		gsum += p.g // total link conductance (bandwidth) into this node
	}

	deltas := make([]Entry, 0, n.n)                                // the message payload (escapes)
	deltas = append(deltas, Entry{Dest: n.idx, Pot: n.cfg.F0, Vit: 1}) // self-pin: a live source asserts V=1
	if gsum > 0 {
		inv := 1.0 / (gsum + n.cfg.Kappa + n.charge)
		fb := n.fbuf
		for d := range fb {
			fb[d] = 0
		}
		for _, p := range bidir { // neighbour-sum: fb = Σ g_ij·pot_nbr  (bandwidth-weighted)
			g, pp := p.g, p.pot
			for d := 0; d < n.n; d++ {
				fb[d] += g * pp[d]
			}
		}
		for d := 0; d < n.n; d++ { // screened balance + current split per destination
			if d == n.idx {
				n.f[d], n.via[d] = 0, -1
				continue
			}
			fd := fb[d] * inv
			n.f[d] = fd
			best, bvia := 0.0, -1
			for _, p := range bidir { // current split: dominant max-flux (bandwidth × gradient) hop
				if w := p.g * (p.pot[d] - fd); w > best {
					best, bvia = w, p.idx
				}
			}
			n.via[d] = bvia
			if fd >= n.cfg.Floor && bvia >= 0 {
				deltas = append(deltas, Entry{Dest: d, Pot: fd, Vit: n.vit[d]})
			}
		}
	} else { // no bidirectional neighbours: the field collapses, nothing routable
		for d := 0; d < n.n; d++ {
			n.f[d], n.via[d] = 0, -1
		}
	}

	msg := Message{From: n.idx, Hears: n.heardList(now), Delta: deltas, Emit: now}
	n.tbuf = n.tbuf[:0]
	for _, p := range n.peers {
		n.tbuf = append(n.tbuf, p.inbox)
	}
	targets := n.tbuf
	n.mu.Unlock()

	for _, ch := range targets { // non-blocking multicast: a full inbox drops (loss), never blocks
		select {
		case ch <- msg:
		default:
		}
	}
}

// Route is the data-plane / inspection read: the dominant next hop toward dst and the action decision
// (known AND vitality above threshold). Vitality is aged to now on the fly, without storing.
func (n *Node) Route(dst NodeID) (via NodeID, vit float64, routable bool) {
	now := n.now()
	d, ok := n.index[dst]
	n.mu.Lock()
	defer n.mu.Unlock()
	if !ok || n.f == nil || d >= n.n {
		return "", 0, false
	}
	v := n.cfg.age(n.vit[d], ticksBetween(n.lastAged, now, n.cfg.Tick))
	known := n.f[d] >= n.cfg.Floor && n.via[d] >= 0
	if n.via[d] >= 0 {
		via = n.ids[n.via[d]]
	}
	return via, v, known && v >= n.cfg.Phi
}

// Field is the instrumentation read: a non-mutating snapshot of both transported scalars toward dst —
// the routing potential pot (f) and the retarded vitality vit (V, aged to now) — plus the dominant
// up-gradient hop. known reports a usable route (above the floor with a next hop). Unlike Route it does
// not apply the φ threshold, so it exposes the raw field at every node.
func (n *Node) Field(dst NodeID) (pot, vit float64, via NodeID, known bool) {
	now := n.now()
	d, ok := n.index[dst]
	n.mu.Lock()
	defer n.mu.Unlock()
	if !ok || n.f == nil || d >= n.n {
		return 0, 0, "", false
	}
	v := n.cfg.age(n.vit[d], ticksBetween(n.lastAged, now, n.cfg.Tick))
	if n.via[d] >= 0 {
		via = n.ids[n.via[d]]
	}
	return n.f[d], v, via, n.f[d] >= n.cfg.Floor && n.via[d] >= 0
}

// SetStress sets the node's offered load s_i — the load side of the congestion loop. The data plane or
// harness writes it; the node's next relax glides its charge µ_i toward Crowd·s_i. Protocol code never
// calls this: load is an external physical signal, not something the routing reads back.
func (n *Node) SetStress(s float64) {
	n.mu.Lock()
	n.stress = s
	n.mu.Unlock()
}

// Charge reports the node's current congestion screening charge µ_i (instrumentation).
func (n *Node) Charge() float64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.charge
}

// tick returns the emission period / proper-time quantum under the lock, so the Run loop reads it
// race-free while SetTick retunes it.
func (n *Node) tick() time.Duration {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.cfg.Tick
}

// SetTick retunes this node's wall-clock cadence — emission period AND proper-time quantum together —
// live. Slowing it cuts the node's CPU proportionally WITHOUT distorting per-hop behaviour, since
// aging is counted in these same ticks. Takes effect on the next emit.
func (n *Node) SetTick(d time.Duration) {
	if d <= 0 {
		return
	}
	n.mu.Lock()
	n.cfg.Tick = d
	n.mu.Unlock()
}

// --- helpers (all called under n.mu) ---

// ageAll ages the whole vitality vector by the ticks elapsed since the last aging, then advances the
// shared aging clock. Exact because age is additive in ticks (see Config.age).
func (n *Node) ageAll(now time.Time) {
	ticks := ticksBetween(n.lastAged, now, n.cfg.Tick)
	if ticks <= 0 {
		return
	}
	for d := 0; d < n.n; d++ {
		n.vit[d] = n.cfg.age(n.vit[d], ticks)
	}
	n.lastAged = now
}

// bidir collects the bidirectional, fresh neighbours into the reused scratch buffer.
func (n *Node) bidir(now time.Time) []*peer {
	out := n.bbuf[:0]
	for _, p := range n.peers {
		if p.hears && ticksBetween(p.heard, now, n.cfg.Tick) < n.cfg.Fresh {
			out = append(out, p)
		}
	}
	n.bbuf = out
	return out
}

// heardList is the freshly-allocated list of neighbour indices we currently hear (it escapes into the
// emitted message, so it cannot reuse a buffer).
func (n *Node) heardList(now time.Time) []int {
	var out []int
	for idx, p := range n.peers {
		if ticksBetween(p.heard, now, n.cfg.Tick) < n.cfg.Fresh {
			out = append(out, idx)
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
