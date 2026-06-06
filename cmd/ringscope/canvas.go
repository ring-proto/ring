package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// canvas is a full-cell raster for the topology graph view: each cell is one rune in one truecolor.
// Nodes are plotted as glyphs and edges as Bresenham lines (which never overwrite a node), then the
// whole thing is emitted as coalesced-SGR rows.
type canvas struct {
	w, h  int
	rune_ []rune
	fg    []ansi.RGBColor
	set   []bool
}

func newCanvas(w, h int) *canvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &canvas{w: w, h: h, rune_: make([]rune, w*h), fg: make([]ansi.RGBColor, w*h), set: make([]bool, w*h)}
}

func (c *canvas) put(x, y int, r rune, fg ansi.RGBColor) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	i := y*c.w + x
	c.rune_[i], c.fg[i], c.set[i] = r, fg, true
}

// putUnder draws only into an empty cell — used for edges so they never paint over a node glyph.
func (c *canvas) putUnder(x, y int, r rune, fg ansi.RGBColor) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || c.set[y*c.w+x] {
		return
	}
	i := y*c.w + x
	c.rune_[i], c.fg[i], c.set[i] = r, fg, true
}

// line draws a Bresenham segment with r, leaving any nodes it crosses intact.
func (c *canvas) line(x0, y0, x1, y1 int, r rune, fg ansi.RGBColor) {
	dx, dy := iabs(x1-x0), -iabs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		c.putUnder(x0, y0, r, fg)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// render emits the canvas as CR/LF rows, coalescing SGR across runs of equal colour. Empty cells are
// spaces (the frame's erase already cleared the screen, but interior gaps still need spacing).
func (c *canvas) render() string {
	var b strings.Builder
	var last ansi.RGBColor
	for y := 0; y < c.h; y++ {
		have := false
		for x := 0; x < c.w; x++ {
			i := y*c.w + x
			if !c.set[i] {
				if have {
					b.WriteString(ansi.ResetStyle)
					have = false
				}
				b.WriteByte(' ')
				continue
			}
			if !have || c.fg[i] != last {
				b.WriteString(ansi.NewStyle().ForegroundColor(c.fg[i]).String())
				last, have = c.fg[i], true
			}
			b.WriteRune(c.rune_[i])
		}
		if have {
			b.WriteString(ansi.ResetStyle)
		}
		b.WriteString("\r\n")
	}
	return b.String()
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
