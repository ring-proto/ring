package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// halfBlock renders one terminal cell as two stacked, ~square pixels: the upper half block '▀' with
// foreground = top pixel and background = bottom pixel. Reference primitive; the hot path coalesces
// SGR across runs instead of calling this per cell.
func halfBlock(top, bottom ansi.Color) string {
	return ansi.NewStyle().ForegroundColor(top).BackgroundColor(bottom).Styled("▀")
}

// renderHalfBlock draws a W×H field as a half-block raster: text row r carries pixel rows 2r (fg) and
// 2r+1 (bg). pix maps a pixel's (f, V, status) to its truecolor, so a view colours by f or by V from
// the same sample. SGR is coalesced across horizontal runs of identical (fg,bg) and reset once per
// row, so a dense grid costs a few hundred bytes per row rather than ~40 per cell. A dangling odd
// final pixel row uses the panel background. Rows are CR/LF terminated for raw mode.
func renderHalfBlock(fv, vv [][]float64, st [][]PixStatus, pix func(f, v float64, s PixStatus) ansi.RGBColor) string {
	h := len(fv)
	if h == 0 {
		return ""
	}
	w := len(fv[0])
	var b strings.Builder
	var lastFg, lastBg ansi.RGBColor
	rows := (h + 1) / 2
	for cr := 0; cr < rows; cr++ {
		top := 2 * cr
		bot := top + 1
		have := false
		for x := 0; x < w; x++ {
			fg := pix(fv[top][x], vv[top][x], st[top][x])
			bg := panelBg
			if bot < h {
				bg = pix(fv[bot][x], vv[bot][x], st[bot][x])
			}
			if !have || fg != lastFg || bg != lastBg {
				b.WriteString(ansi.NewStyle().ForegroundColor(fg).BackgroundColor(bg).String())
				lastFg, lastBg, have = fg, bg, true
			}
			b.WriteString("▀")
		}
		b.WriteString(ansi.ResetStyle)
		b.WriteString("\r\n")
	}
	return b.String()
}

// frame wraps a fully-built body in one atomic screen update: synchronized-output begin, home, erase
// below, the body, synchronized-output end. The caller does a single buffered write + flush.
func frame(body string) string {
	var b strings.Builder
	b.WriteString(ansi.SetModeSynchronizedOutput)
	b.WriteString(ansi.CursorHomePosition)
	b.WriteString(ansi.EraseScreenBelow)
	b.WriteString(body)
	b.WriteString(ansi.ResetModeSynchronizedOutput)
	return b.String()
}

// at positions the cursor at (col,row) — 1-based — and appends s.
func at(col, row int, s string) string { return ansi.CursorPosition(col, row) + s }
