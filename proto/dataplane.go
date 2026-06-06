package proto

import (
	"math/rand"
	"time"
)

// Outcome classifies how one forwarded packet ended. The classification is defined ONCE, here, and
// reused by every data-plane measurement, so "black-hole" and "correct severance" never blur — the
// distinction is the whole point of the vitality field:
//
//   - Delivered:        reached the destination.
//   - Severed:          a router on the path correctly DECLINED to forward — the destination's
//                       vitality is below the action threshold (V < φ), or its field is unknown.
//                       This is the protocol working as intended after a failure, NOT a black hole.
//   - BlackholeDeadHop: a router forwarded the packet (it still considered the route live) INTO a
//                       powered-off router. This is the gap between physical death and protocol
//                       detection — the window vitality exists to shrink. The headline black hole.
//   - BlackholeLoop:    the current field momentarily routed the packet through a via-cycle (a
//                       transient eddy during reconvergence; Theorem 5.7 says these dissipate).
//   - BlackholeTTL:     the packet exceeded its hop budget without delivery or any case above — a
//                       catch-all for wandering.
//   - Lost:             a physical per-hop drop (channel/link loss), independent of routing.
type Outcome int

const (
	Delivered Outcome = iota
	Severed
	BlackholeDeadHop
	BlackholeLoop
	BlackholeTTL
	Lost
)

func (o Outcome) String() string {
	switch o {
	case Delivered:
		return "delivered"
	case Severed:
		return "severed"
	case BlackholeDeadHop:
		return "blackhole:dead-hop"
	case BlackholeLoop:
		return "blackhole:loop"
	case BlackholeTTL:
		return "blackhole:ttl"
	case Lost:
		return "lost"
	default:
		return "unknown"
	}
}

// Blackholed reports the outcomes that mean "routing ACCEPTED the packet but failed to deliver it"
// — the measure of stale/cyclic routing state. It is deliberately distinct from Severed (the
// protocol correctly declined) and Lost (a physical drop), so a black-hole count never silently
// includes correct severances.
func (o Outcome) Blackholed() bool {
	return o == BlackholeDeadHop || o == BlackholeLoop || o == BlackholeTTL
}

// Delivery is the result of forwarding one packet.
type Delivery struct {
	Outcome Outcome
	Hops    int           // hops traversed (0 if dropped at the source)
	Latency time.Duration // simulated end-to-end latency = Hops * DataPlane.HopDelay
	Path    []NodeID      // the routers visited, source first
}

// DataPlane carries the PHYSICAL-fabric parameters for forwarding — link delay and loss, a hop
// budget. It holds no protocol state; forwarding reads only the public Route() and the fabric's
// physical down status.
type DataPlane struct {
	HopDelay time.Duration // simulated transit per hop; end-to-end latency = Hops * HopDelay
	LossProb float64       // per-hop physical loss probability in [0,1] (0 = lossless)
	TTL      int           // hop budget before BlackholeTTL (<=0 defaults to 256)
}

// Forward injects one packet at src toward dst and walks it hop-by-hop along the CURRENT routing
// decision (Route) of each live router it visits — a control-plane-accurate data plane: the packet
// experiences whatever the async fields currently say, including a stale route into a just-failed
// node. It consumes ONLY the public Route() and the fabric's physical down status; it never touches
// protocol internals, and it never feeds liveness back to the protocol (death is perceived solely
// through Vitality).
//
// The walk is instantaneous against a routing snapshot — the data-plane timescale (a packet across
// a few hops) is far below control-plane reconvergence, so a single packet effectively sees a frozen
// field; the delivery-ratio dip after a failure is recovered by injecting a STREAM of packets over
// the reconvergence window, each snapshot-walking the then-current state. Accumulated Latency is
// simulated, never slept. rng (caller-owned, for reproducibility and safe concurrency) drives loss;
// it may be nil when LossProb == 0.
func (m *Mesh) Forward(src, dst NodeID, dp DataPlane, rng *rand.Rand) Delivery {
	ttl := dp.TTL
	if ttl <= 0 {
		ttl = 256
	}
	d := Delivery{Path: []NodeID{src}}
	visited := map[NodeID]bool{src: true}
	cur := src
	for {
		// Physical fate FIRST: a packet at a powered-off router is lost — including one that reached
		// a just-killed destination (a corpse), which is a black hole, not a delivery.
		if m.isDown(cur) {
			d.Outcome = BlackholeDeadHop
			return d
		}
		if cur == dst {
			d.Outcome = Delivered
			return d
		}
		if d.Hops >= ttl {
			d.Outcome = BlackholeTTL
			return d
		}
		n := m.nodes[cur]
		if n == nil { // off the fabric entirely — treat as a dead hop
			d.Outcome = BlackholeDeadHop
			return d
		}
		via, _, routable := n.Route(dst)
		if !routable { // V < φ or field unknown: the protocol correctly declines — correct severance
			d.Outcome = Severed
			return d
		}
		if dp.LossProb > 0 && rng != nil && rng.Float64() < dp.LossProb {
			d.Outcome = Lost
			return d
		}
		if visited[via] { // the current field routed us into a cycle (transient during reconvergence)
			d.Outcome = BlackholeLoop
			return d
		}
		visited[via] = true
		d.Path = append(d.Path, via)
		d.Hops++
		d.Latency += dp.HopDelay
		cur = via
	}
}
