package main

import (
	"math"

	"github.com/charmbracelet/x/ansi"
	colorful "github.com/lucasb-eyer/go-colorful"
)

// PixStatus tags a field pixel so the colormap can render out-of-band cells (no node, dead, the
// destination itself, unreachable, error) distinctly from an in-band scalar value.
type PixStatus uint8

const (
	StOK      PixStatus = iota // a normal sampled value
	StUnreach                  // a node whose field is not yet known (below floor / pre-convergence)
	StNoNode                   // no node occupies this layout cell
	StDest                     // the destination itself (pinned)
	StDead                     // a powered-off node
	StNaN                      // sampling error / NaN guard
)

// Fixed out-of-band colors and the panel background (used for the dangling odd half-block row).
var (
	colUnreach = ansi.RGBColor{R: 0x18, G: 0x18, B: 0x1e}
	colNoNode  = ansi.RGBColor{R: 0x0a, G: 0x0a, B: 0x0c}
	colDest    = ansi.RGBColor{R: 0xff, G: 0xff, B: 0xff}
	colDead    = ansi.RGBColor{R: 0xd0, G: 0x20, B: 0x24}
	colNaN     = ansi.RGBColor{R: 0x80, G: 0x00, B: 0x80}
	panelBg    = ansi.RGBColor{R: 0x10, G: 0x10, B: 0x14}
)

// perceptually-uniform-ish anchor stops (hex), blended in Lab into 256-entry LUTs.
var (
	viridisStops = []string{"440154", "472d7b", "3b528b", "2c728e", "21918c", "28ae80", "5ec962", "addc30", "fde725"}
	magmaStops   = []string{"000004", "180f3e", "451077", "721f81", "9f2f7f", "cd4071", "f1605d", "fd9567", "fcfdbf"}
	coolDimStops = []string{"08070d", "121a30", "1b2a4a", "2f4b7a"} // below-threshold ramp for V
)

var (
	fLUT     [256]ansi.RGBColor // f potential (viridis)
	vUpLUT   [256]ansi.RGBColor // V at/above phi (magma)
	vDownLUT [256]ansi.RGBColor // V below phi (cool dim)
)

func init() {
	fLUT = buildLUT(viridisStops)
	vUpLUT = buildLUT(magmaStops)
	vDownLUT = buildLUT(coolDimStops)
}

// buildLUT blends the hex anchor stops in Lab space into a 256-entry truecolor lookup table.
func buildLUT(stops []string) [256]ansi.RGBColor {
	cols := make([]colorful.Color, len(stops))
	for i, h := range stops {
		c, err := colorful.Hex("#" + h)
		if err != nil {
			c = colorful.Color{R: 1, G: 0, B: 1}
		}
		cols[i] = c
	}
	var lut [256]ansi.RGBColor
	for i := range lut {
		t := float64(i) / 255
		seg := t * float64(len(cols)-1)
		k := int(seg)
		if k >= len(cols)-1 {
			k = len(cols) - 2
		}
		c := cols[k].BlendLab(cols[k+1], seg-float64(k)).Clamped()
		r, g, b := c.RGB255()
		lut[i] = ansi.RGBColor{R: r, G: g, B: b}
	}
	return lut
}

func lutAt(lut *[256]ansi.RGBColor, t float64) ansi.RGBColor {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return lut[int(t*255)]
}

// colorF maps the routing potential f to viridis, gamma-expanding the low end so the far field (which
// decays geometrically) stays visible. Out-of-band statuses get their fixed marker colors.
func colorF(v float64, st PixStatus, f0 float64) ansi.RGBColor {
	if c, ok := oob(st); ok {
		return c
	}
	t := v / f0
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return lutAt(&fLUT, math.Pow(t, 0.45))
}

// colorV maps vitality V with a hard color seam at the action threshold phi: magma above (actionable),
// a dim cool ramp below (known but past the horizon).
func colorV(v float64, st PixStatus, phi float64) ansi.RGBColor {
	if c, ok := oob(st); ok {
		return c
	}
	if v >= phi {
		return lutAt(&vUpLUT, (v-phi)/(1-phi))
	}
	return lutAt(&vDownLUT, v/phi)
}

func oob(st PixStatus) (ansi.RGBColor, bool) {
	switch st {
	case StNoNode:
		return colNoNode, true
	case StDead:
		return colDead, true
	case StDest:
		return colDest, true
	case StUnreach:
		return colUnreach, true
	case StNaN:
		return colNaN, true
	}
	return ansi.RGBColor{}, false
}
