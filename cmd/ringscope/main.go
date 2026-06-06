// Command ringscope is an interactive terminal field-scope for the live async RING simulation: it
// runs a mesh and renders its two scalar fields (routing potential f, vitality V) as truecolor
// half-block plots you can drive — play/pause, switch views, and (soon) kill/revive/stress nodes.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/ring-proto/ring/proto"
	"golang.org/x/term"
)

// App holds the whole viewer state; everything runs on the main goroutine except the key reader.
type App struct {
	m             *proto.Mesh
	cfg           proto.Config
	dst           proto.NodeID
	layout        [][]proto.NodeID
	dead          map[proto.NodeID]bool
	f0, phi       float64
	paused        bool
	frameInterval time.Duration
	cols, rows    int
	frame         int
}

func main() {
	w := flag.Int("w", 24, "grid width")
	h := flag.Int("h", 16, "grid height")
	flag.Parse()

	cfg := proto.DefaultConfig()
	m := proto.NewMesh(cfg)
	ids := proto.BuildGrid(m, *w, *h)
	layout := gridLayout(ids, *w, *h)
	m.Start()
	defer m.Stop()

	app := &App{
		m: m, cfg: cfg, dst: layout[*h/2][*w/2], layout: layout,
		dead: map[proto.NodeID]bool{}, f0: cfg.F0, phi: cfg.Phi,
		frameInterval: 33 * time.Millisecond,
	}
	app.Run()
}

// Run is the viewer's lifecycle: enter the alt screen, drive a sample+render loop off a ticker and a
// raw key reader, and — above all — restore the terminal on EVERY exit path (return, signal, panic).
func (a *App) Run() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-TTY (pipe/CI): let the field converge briefly, dump one static frame, exit clean.
		time.Sleep(400 * time.Millisecond)
		fv, _, st := sampleField(a.m, a.dst, a.layout, a.dead, a.f0)
		io.WriteString(os.Stdout, renderField(fv, st, a.colorFn()))
		return
	}

	old, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ringscope: cannot enter raw mode:", err)
		return
	}
	bufw := bufio.NewWriter(os.Stdout)
	io.WriteString(bufw, ansi.SetModeAltScreenSaveCursor)
	io.WriteString(bufw, ansi.HideCursor)
	bufw.Flush()
	// A single idempotent restore, reached from defer, the signal branch, and the panic recovery —
	// a render panic must never leave the terminal in the alt screen with a hidden cursor.
	restore := sync.OnceFunc(func() {
		io.WriteString(bufw, ansi.ShowCursor)
		io.WriteString(bufw, ansi.ResetModeAltScreenSaveCursor)
		bufw.Flush()
		term.Restore(fd, old)
	})
	defer restore()
	defer func() {
		if r := recover(); r != nil {
			restore()
			panic(r)
		}
	}()

	keyCh := make(chan string, 16)
	go func() {
		r := bufio.NewReader(os.Stdin)
		buf := make([]byte, 8)
		for {
			n, err := r.Read(buf)
			if err != nil {
				close(keyCh)
				return
			}
			keyCh <- string(buf[:n])
		}
	}()
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGWINCH)

	a.cols, a.rows, _ = term.GetSize(fd)
	ticker := time.NewTicker(a.frameInterval)
	defer ticker.Stop()
	a.render(bufw)
	for {
		select {
		case k, ok := <-keyCh:
			if !ok || a.handleKey(k) {
				return
			}
			a.render(bufw)
		case s := <-sigCh:
			if s == syscall.SIGWINCH {
				a.cols, a.rows, _ = term.GetSize(fd)
				a.render(bufw)
				continue
			}
			return // SIGINT/SIGTERM: deferred restore() fires
		case <-ticker.C:
			if !a.paused {
				a.render(bufw)
			}
		}
	}
}

// handleKey returns true to quit.
func (a *App) handleKey(k string) bool {
	switch k {
	case "q", "\x03": // q or Ctrl-C (raw mode delivers it as the byte, ISIG off)
		return true
	case " ":
		a.paused = !a.paused
	}
	return false
}

// colorFn is the active field's pixel colorer (F-field for the skeleton).
func (a *App) colorFn() func(float64, PixStatus) ansi.RGBColor {
	return func(v float64, s PixStatus) ansi.RGBColor { return colorF(v, s, a.f0) }
}

func (a *App) render(w *bufio.Writer) {
	a.frame++
	fv, _, st := sampleField(a.m, a.dst, a.layout, a.dead, a.f0)
	state := "▶ playing"
	if a.paused {
		state = "⏸ paused"
	}
	hud := fmt.Sprintf("ringscope   F-field → %s   frame %d   %s    [space] play/pause   [q] quit\r\n",
		a.dst, a.frame, state)
	io.WriteString(w, frame(hud+renderField(fv, st, a.colorFn())))
	w.Flush()
}
