package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// View is one renderable mode. Draw writes the entire frame body (HUD + plot + chrome) into b; Key
// gives the view first refusal on a keypress (returns true if it consumed it). Name labels it in the
// HUD and the view switcher.
type View interface {
	Name() string
	Key(k string, a *App) bool
	Draw(b *strings.Builder, a *App)
}

// --- the three v1 views ---

type fView struct{}

func (fView) Name() string             { return "F potential" }
func (fView) Key(string, *App) bool    { return false }
func (v fView) Draw(b *strings.Builder, a *App) {
	drawFieldView(b, a, v.Name(), func(f, _ float64, st PixStatus) ansi.RGBColor { return colorF(f, st, a.f0) })
}

type vView struct{}

func (vView) Name() string          { return "V vitality" }
func (vView) Key(string, *App) bool { return false }
func (v vView) Draw(b *strings.Builder, a *App) {
	drawFieldView(b, a, v.Name(), func(_, vv float64, st PixStatus) ansi.RGBColor { return colorV(vv, st, a.phi) })
}

type splitView struct{}

func (splitView) Name() string          { return "F | V" }
func (splitView) Key(string, *App) bool { return false }
func (splitView) Draw(b *strings.Builder, a *App) {
	fv, vv, st := sampleField(a.m, a.dst, a.layout, a.dead, a.f0)
	b.WriteString(hudLine(a, "F potential + V vitality"))
	b.WriteString("\r\n")
	b.WriteString(dim("  f · routing potential (viridis)"))
	b.WriteString("\r\n")
	b.WriteString(renderHalfBlock(fv, vv, st, func(f, _ float64, s PixStatus) ansi.RGBColor { return colorF(f, s, a.f0) }))
	b.WriteString(dim("  V · vitality (magma above φ, dim below — the horizon)"))
	b.WriteString("\r\n")
	b.WriteString(renderHalfBlock(fv, vv, st, func(_, w float64, s PixStatus) ansi.RGBColor { return colorV(w, s, a.phi) }))
	b.WriteString(inspectorLine(a))
	b.WriteString("\r\n")
	b.WriteString(footerLine(a))
}

// drawFieldView renders a single half-block field panel with HUD, inspector, footer, and the
// selection marker overlaid at the selected node's cell.
func drawFieldView(b *strings.Builder, a *App, name string, pix func(f, v float64, st PixStatus) ansi.RGBColor) {
	fv, vv, st := sampleField(a.m, a.dst, a.layout, a.dead, a.f0)
	b.WriteString(hudLine(a, name))
	b.WriteString("\r\n")
	b.WriteString(renderHalfBlock(fv, vv, st, pix)) // field begins at screen row 2
	b.WriteString(inspectorLine(a))
	b.WriteString("\r\n")
	b.WriteString(footerLine(a))
	b.WriteString(selOverlay(a))
}

// --- chrome ---

func hudLine(a *App, name string) string {
	state := "▶ play"
	if a.paused {
		state = "⏸ pause"
	}
	s := fmt.Sprintf("ringscope · %s · dst %s · %s · %dms/tick · sel(%d,%d) · frame %d",
		name, a.dst, state, a.simTick.Milliseconds(), a.selX, a.selY, a.frame)
	return ansi.NewStyle().Bold().Styled(clip(s, a.cols))
}

func footerLine(a *App) string {
	return dim(clip("[1]F [2]V [3]split  [hjkl/↑↓←→]move  [x]kill·revive [d]dst [z]reset  [space]pause [+/-]speed  [q]quit", a.cols))
}

// inspectorLine reads the selected node's routing state toward the current destination.
func inspectorLine(a *App) string {
	id := a.selID()
	switch {
	case id == "":
		return dim("  (no node selected)")
	case id == a.dst:
		return dim(fmt.Sprintf("  sel %s   ◎ DESTINATION", id))
	case a.dead[id]:
		return dim(fmt.Sprintf("  sel %s   ✕ DEAD  ([x] or [r] to revive)", id))
	}
	pot, vit, via, known := a.m.Node(id).Field(a.dst)
	_, _, routable := a.m.Node(id).Route(a.dst)
	v := string(via)
	if v == "" {
		v = "—"
	}
	return dim(clip(fmt.Sprintf("  sel %s   f=%.4f  V=%.3f  via=%s  known=%v  routable=%v",
		id, pot, vit, v, known, routable), a.cols))
}

// selOverlay marks the selected node's cell. The field begins at screen row 2 (HUD is row 1); each
// half-block cell row covers two grid rows, so the node at (selX,selY) is at column selX+1, row
// 2+selY/2 (1-based). It rides on top of the field in the same atomic frame.
func selOverlay(a *App) string {
	col := 1 + a.selX
	row := 2 + a.selY/2
	mark := ansi.NewStyle().ForegroundColor(ansi.RGBColor{R: 0xff, G: 0xff, B: 0xff}).Styled("◎")
	return at(col, row, mark)
}

func dim(s string) string {
	return ansi.NewStyle().Faint().Styled(s)
}

func clip(s string, cols int) string {
	if cols <= 0 {
		return s
	}
	return ansi.Truncate(s, cols, "")
}
