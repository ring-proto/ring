package main

import (
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/ring-proto/ring/proto"
)

// topoView is the universal graph lens: nodes at their layout positions, edges as dim Bresenham
// lines, and each node drawn as an arrow pointing at its current up-gradient hop (via) — so the whole
// routing flow toward the destination reads at a glance. Works for every topology. 'g' toggles
// whether nodes are tinted by f (potential) or V (vitality).
type topoView struct{}

func (topoView) Name() string { return "topology" }

func (topoView) Key(k string, a *App) bool {
	if k == "g" {
		a.tintV = !a.tintV
		return true
	}
	return false
}

func (topoView) Draw(b *strings.Builder, a *App) {
	tint := "f"
	if a.tintV {
		tint = "V"
	}
	drawGraph(b, a, "topology · "+a.topo.Name+" · tint "+tint, a.nodeRender)
}

// drawGraph plots the topology into a cell canvas: edges first (severed edges marked), then each node
// via nodeFn, with the selection brightened and the cut-verb's armed endpoint in amber.
func drawGraph(b *strings.Builder, a *App, title string, nodeFn func(proto.NodeID) (rune, ansi.RGBColor)) {
	b.WriteString(hudLine(a, title))
	b.WriteString("\r\n")
	pw := a.cols
	if pw <= 0 {
		pw = 100
	}
	ph := a.rows - 4
	if ph < 4 {
		ph = 24
	}
	cv := newCanvas(pw, ph)
	cellOf := func(id proto.NodeID) (int, int) {
		p := a.topo.Pos[id]
		return int(p[0]*float64(pw-1) + 0.5), int(p[1]*float64(ph-1) + 0.5)
	}

	severed := ansi.RGBColor{R: 0x66, G: 0x1a, B: 0x1a}
	maxW := 1.0
	for _, w := range a.topo.Wt {
		if w > maxW {
			maxW = w
		}
	}
	seen := map[[2]proto.NodeID]bool{}
	for id, nbrs := range a.topo.Adj {
		x0, y0 := cellOf(id)
		for _, nb := range nbrs {
			k := edgeKey(id, nb)
			if seen[k] {
				continue
			}
			seen[k] = true
			x1, y1 := cellOf(nb)
			if a.cut[k] {
				cv.line(x0, y0, x1, y1, '╳', severed)
				continue
			}
			w := a.topo.Wt[k]
			if w == 0 {
				w = 1
			}
			cv.line(x0, y0, x1, y1, edgeGlyph(w, maxW), edgeColor(w, maxW)) // brightness ∝ bandwidth
		}
	}
	for _, id := range a.topo.IDs {
		x, y := cellOf(id)
		r, col := nodeFn(id)
		switch id {
		case a.cutArm:
			col = ansi.RGBColor{R: 0xff, G: 0xd0, B: 0x33} // armed cut endpoint = amber
		case a.sel:
			col = ansi.RGBColor{R: 0xff, G: 0xff, B: 0xff} // selection = bright white
		}
		cv.put(x, y, r, col)
	}
	b.WriteString(cv.render())
	b.WriteString(inspectorLine(a))
	b.WriteString("\r\n")
	b.WriteString(footerLine(a))
}

// nodeRender picks a node's glyph and colour: a flow-arrow toward its via (tinted by f or V), ◎ for
// the destination, ✕ for a dead node, a dim dot for an unreachable one.
func (a *App) nodeRender(id proto.NodeID) (rune, ansi.RGBColor) {
	if a.dead[id] {
		return '✕', colDead
	}
	if id == a.dst {
		return '◎', colDest
	}
	pot, vit, via, known := a.m.Node(id).Field(a.dst)
	if !known || via == "" {
		return '·', colUnreach
	}
	col := colorF(pot, StOK, a.f0)
	if a.tintV {
		col = colorV(vit, StOK, a.phi)
	}
	p, q := a.topo.Pos[id], a.topo.Pos[via]
	return arrowGlyph(q[0]-p[0], q[1]-p[1]), col
}

// arrowGlyph returns the 8-way arrow nearest the (dx,dy) direction in screen coordinates (+y down).
func arrowGlyph(dx, dy float64) rune {
	if dx == 0 && dy == 0 {
		return '●'
	}
	o := int(math.Round(math.Atan2(dy, dx)/(math.Pi/4))) & 7
	return [8]rune{'→', '↘', '↓', '↙', '←', '↖', '↑', '↗'}[o]
}

func edgeKey(a, b proto.NodeID) [2]proto.NodeID {
	if a < b {
		return [2]proto.NodeID{a, b}
	}
	return [2]proto.NodeID{b, a}
}

// edgeColor brightens an edge by its conductance: thin links dim grey, fat links bright blue.
func edgeColor(w, maxW float64) ansi.RGBColor {
	t := 0.0
	if maxW > 0 {
		t = w / maxW
	}
	lerp := func(lo, hi float64) uint8 { return uint8(lo + t*(hi-lo)) }
	return ansi.RGBColor{R: lerp(0x44, 0x6a), G: lerp(0x44, 0x9a), B: lerp(0x52, 0xe0)}
}

// edgeGlyph thickens the fattest links a touch.
func edgeGlyph(w, maxW float64) rune {
	if maxW > 0 && w/maxW >= 0.66 {
		return '•'
	}
	return '·'
}
