// Command ringscope is an interactive terminal field-scope for the live async RING simulation: it
// runs a mesh and renders its two scalar fields (routing potential f, vitality V) as truecolor
// half-block plots you can drive — play/pause, switch views, move a selection cursor, and
// kill/revive/retarget nodes to watch the fields reconverge in real time.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/ring-proto/ring/proto"
	"golang.org/x/term"
)

// App is the whole viewer state; everything runs on the main goroutine except the key reader.
type App struct {
	m       *proto.Mesh
	cfg     proto.Config
	layout  [][]proto.NodeID
	W, H    int
	dst     proto.NodeID
	dead    map[proto.NodeID]bool
	f0, phi float64
	simTick time.Duration

	paused        bool
	frameInterval time.Duration
	cols, rows    int
	frame         int

	views      []View
	active     int
	selX, selY int
}

func main() {
	w := flag.Int("w", 24, "grid width")
	h := flag.Int("h", 16, "grid height")
	tick := flag.Duration("tick", 50*time.Millisecond, "sim emission period (larger = less CPU, calmer evolution)")
	crowd := flag.Float64("crowd", 0, "congestion coupling c (0 = off)")
	flag.Parse()

	cfg := proto.DefaultConfig()
	cfg.Tick = *tick
	cfg.Crowd = *crowd
	m := proto.NewMesh(cfg)
	ids := proto.BuildGrid(m, *w, *h)
	m.Start()
	defer m.Stop()

	a := &App{
		m: m, cfg: cfg, layout: gridLayout(ids, *w, *h), W: *w, H: *h,
		dead: map[proto.NodeID]bool{}, f0: cfg.F0, phi: cfg.Phi, simTick: cfg.Tick,
		frameInterval: 33 * time.Millisecond, selX: *w / 2, selY: *h / 2,
		views: []View{&fView{}, &vView{}, &splitView{}},
	}
	a.dst = a.layout[*h/2][*w/2]
	a.Run()
}

// Run is the viewer lifecycle: enter the alt screen, drive a sample+render loop off a ticker and a raw
// key reader, and restore the terminal on EVERY exit path (return, signal, panic).
func (a *App) Run() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		time.Sleep(400 * time.Millisecond) // converge briefly, dump one static frame, exit clean
		var b strings.Builder
		a.views[0].Draw(&b, a)
		io.WriteString(os.Stdout, b.String())
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
			return
		case <-ticker.C:
			if !a.paused {
				a.render(bufw)
			}
		}
	}
}

// handleKey returns true to quit. View-local keys get first refusal, then the global verbs.
func (a *App) handleKey(k string) bool {
	if a.views[a.active].Key(k, a) {
		return false
	}
	switch k {
	case "q", "\x03":
		return true
	case " ":
		a.paused = !a.paused
	case "1", "2", "3":
		a.active = int(k[0] - '1')
	case "\t":
		a.active = (a.active + 1) % len(a.views)
	case "\x1b[Z": // shift-tab
		a.active = (a.active - 1 + len(a.views)) % len(a.views)
	case "\x1b[A", "k":
		a.moveSel(0, -1)
	case "\x1b[B", "j":
		a.moveSel(0, 1)
	case "\x1b[C", "l":
		a.moveSel(1, 0)
	case "\x1b[D", "h":
		a.moveSel(-1, 0)
	case "d":
		if id := a.selID(); id != "" {
			a.dst = id
		}
	case "x":
		a.killToggle()
	case "r":
		if id := a.selID(); a.dead[id] {
			a.m.Revive(id)
			delete(a.dead, id)
		}
	case "z":
		a.reset()
	case "+", "=":
		a.retune(0.75)
	case "-", "_":
		a.retune(1 / 0.75)
	}
	return false
}

func (a *App) selID() proto.NodeID { return a.layout[a.selY][a.selX] }

func (a *App) moveSel(dx, dy int) {
	a.selX = clampi(a.selX+dx, 0, a.W-1)
	a.selY = clampi(a.selY+dy, 0, a.H-1)
}

func (a *App) killToggle() {
	id := a.selID()
	if id == "" {
		return
	}
	if a.dead[id] {
		a.m.Revive(id)
		delete(a.dead, id)
	} else {
		a.m.Kill(id)
		a.dead[id] = true
	}
}

func (a *App) reset() {
	for id := range a.dead {
		a.m.Revive(id)
	}
	a.dead = map[proto.NodeID]bool{}
	a.dst = a.layout[a.H/2][a.W/2]
}

// retune scales the sim emission period (and proper-time quantum) live, clamped to a usable range.
func (a *App) retune(f float64) {
	d := time.Duration(float64(a.simTick) * f)
	if d < 5*time.Millisecond {
		d = 5 * time.Millisecond
	} else if d > 2*time.Second {
		d = 2 * time.Second
	}
	a.simTick = d
	a.m.SetTick(d)
}

func (a *App) render(w *bufio.Writer) {
	a.frame++
	var b strings.Builder
	a.views[a.active].Draw(&b, a)
	io.WriteString(w, frame(b.String()))
	w.Flush()
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
