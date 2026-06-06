package proto

// This file is the LOAD side of the congestion loop (§5.7–§5.8). The charge side — the screened
// denominator G_i+κ+µ_i and the non-overshoot glide µ_i←reg(µ_i, c·s_i, r) — lives in the operator
// (proto.go). Here we compute the stress s(µ): the load each node carries under the current-split
// flow of the present field, and write it back as node stress to close the exact map µ←G(µ).

// Demand is one fluid traffic demand: Amount units flowing from Src toward Dst along the current
// up-gradient route.
type Demand struct {
	Src, Dst NodeID
	Amount   float64
}

// CarriedLoad routes each demand along the CURRENT current-split flow — the via chain read from the
// public Route() — and returns the total load carried at each node. This is the stress signal
// s(µ) of §5.8: the fluid, deterministic counterpart of Forward (no loss, no clock), counting every
// node a demand traverses. A demand that severs, loops, or exceeds the hop budget contributes only
// up to where it stalls. The result is the load side of the load↔charge loop.
func (m *Mesh) CarriedLoad(demands []Demand, ttl int) map[NodeID]float64 {
	if ttl <= 0 {
		ttl = 256
	}
	load := map[NodeID]float64{}
	for _, d := range demands {
		cur := d.Src
		visited := map[NodeID]bool{}
		for hops := 0; ; hops++ {
			load[cur] += d.Amount
			if cur == d.Dst || hops >= ttl || visited[cur] {
				break
			}
			visited[cur] = true
			n := m.nodes[cur]
			if n == nil {
				break
			}
			via, _, routable := n.Route(d.Dst)
			if !routable {
				break
			}
			cur = via
		}
	}
	return load
}

// ApplyStress writes a load profile onto every node as its offered stress s_i (absent nodes get 0),
// closing the loop: each node's next relax then glides its charge µ_i toward Crowd·s_i. Drive the
// loop by alternating CarriedLoad → ApplyStress with a settle interval for the async field to
// reabsorb the new charge.
func (m *Mesh) ApplyStress(load map[NodeID]float64) {
	for _, id := range m.order {
		if n := m.nodes[id]; n != nil {
			n.SetStress(load[id])
		}
	}
}
