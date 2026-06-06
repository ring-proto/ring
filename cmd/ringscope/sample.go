package main

import "github.com/ring-proto/ring/proto"

// sampleField samples both transported fields toward dst over the layout grid in a single pass: the
// routing potential f and the vitality V per cell, each tagged with a PixStatus for out-of-band cells
// (no node here / the destination itself / a dead node / not-yet-reachable). It reads only the public
// Node.Field, which takes the node's own mutex, so it is safe to call every frame against the live
// async sim.
func sampleField(m *proto.Mesh, dst proto.NodeID, layout [][]proto.NodeID, dead map[proto.NodeID]bool, f0 float64) (fv, vv [][]float64, st [][]PixStatus) {
	h := len(layout)
	fv = make([][]float64, h)
	vv = make([][]float64, h)
	st = make([][]PixStatus, h)
	for y := range layout {
		w := len(layout[y])
		fv[y] = make([]float64, w)
		vv[y] = make([]float64, w)
		st[y] = make([]PixStatus, w)
		for x, id := range layout[y] {
			switch {
			case id == "":
				st[y][x] = StNoNode
			case id == dst:
				fv[y][x], vv[y][x], st[y][x] = f0, 1, StDest
			case dead[id]:
				st[y][x] = StDead
			default:
				pot, vit, _, known := m.Node(id).Field(dst)
				fv[y][x], vv[y][x] = pot, vit
				if known {
					st[y][x] = StOK
				} else {
					st[y][x] = StUnreach
				}
			}
		}
	}
	return
}
