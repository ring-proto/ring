package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/ring-proto/ring/proto"
)

// congView shows the congestion screening charge µ across the graph: each node tinted by its charge
// on a magma heat ramp, ◆ for nodes the user has loaded. Press [S] to load/unload the selected node
// and watch its charge build and the routing flow (topology view) bend around it. Requires -crowd>0
// for the load→charge loop to be live.
type congView struct{}

func (congView) Name() string { return "congestion" }

func (congView) Key(k string, a *App) bool {
	if k == "S" {
		a.stressToggle()
		return true
	}
	return false
}

func (congView) Draw(b *strings.Builder, a *App) {
	if a.cfg.Crowd <= 0 {
		b.WriteString(hudLine(a, "congestion · charge µ"))
		b.WriteString("\r\n\r\n")
		b.WriteString(dim("  Congestion is off. Restart with  -crowd 1  to enable the load→charge loop,"))
		b.WriteString("\r\n")
		b.WriteString(dim("  then press [S] on a node to load it and watch the field bend around it."))
		b.WriteString("\r\n\r\n")
		b.WriteString(footerLine(a))
		return
	}
	mx := a.maxCharge() // normalize once per frame, not per node
	title := fmt.Sprintf("congestion · charge µ (max %.2f) · [S] load node", mx)
	drawGraph(b, a, title, func(id proto.NodeID) (rune, ansi.RGBColor) {
		if a.dead[id] {
			return '✕', colDead
		}
		t := 0.0
		if mx > 1e-9 {
			t = a.m.Node(id).Charge() / mx
		}
		g := '●'
		if id == a.dst {
			g = '◎'
		}
		if a.stressed[id] {
			g = '◆'
		}
		return g, lutAt(&vUpLUT, t)
	})
}

// maxCharge is the largest congestion charge over the graph, for auto-scaling the heat ramp.
func (a *App) maxCharge() float64 {
	mx := 0.0
	for _, id := range a.topo.IDs {
		if c := a.m.Node(id).Charge(); c > mx {
			mx = c
		}
	}
	return mx
}
