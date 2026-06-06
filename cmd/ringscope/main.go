// Command ringscope is an interactive terminal field-scope for the live async RING simulation. It
// builds a topology (grid lattice, single-switch star LAN, leaf-spine Clos fabric, or a multi-AS
// internet region), runs the mesh, and renders its two scalar fields (routing potential f, vitality
// V) as truecolor half-block heatmaps (lattices) and as a universal topology graph you can drive —
// play/pause, switch views and topologies, move a selection cursor, and kill/revive/retarget nodes to
// watch the fields reconverge in real time.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
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
	params  topoParams
	cfg     proto.Config
	m       *proto.Mesh
	topo    Topo
	dst      proto.NodeID
	dead     map[proto.NodeID]bool
	sel      proto.NodeID
	tintV    bool
	cutArm   proto.NodeID          // the cut verb's first endpoint, "" when disarmed
	cut      map[[2]proto.NodeID]bool // edges currently severed (for display)
	stressed map[proto.NodeID]bool // nodes the user has loaded (congestion view)
	f0, phi  float64
	simTick  time.Duration

	paused        bool
	frameInterval time.Duration
	cols, rows    int
	frame         int
	views         []View
	active        int
}

var topoCycle = []string{"grid", "star", "clos", "internet"}

func main() {
	var p topoParams
	flag.StringVar(&p.kind, "topo", "grid", "topology: grid|star|clos|internet")
	flag.IntVar(&p.w, "w", 24, "grid width")
	flag.IntVar(&p.h, "h", 16, "grid height")
	flag.IntVar(&p.hosts, "hosts", 14, "star: host count")
	flag.IntVar(&p.spines, "spines", 4, "clos: spine count")
	flag.IntVar(&p.leaves, "leaves", 6, "clos: leaf count")
	flag.IntVar(&p.hpl, "hpl", 2, "clos: hosts per leaf")
	flag.IntVar(&p.ases, "as", 6, "internet: AS count")
	flag.IntVar(&p.perAS, "ar", 5, "internet: routers per AS")
	tick := flag.Duration("tick", 50*time.Millisecond, "sim emission period (larger = less CPU, calmer evolution)")
	crowd := flag.Float64("crowd", 0, "congestion coupling c (0 = off)")
	flag.Parse()

	cfg := proto.DefaultConfig()
	cfg.Tick = *tick
	cfg.Crowd = *crowd
	a := &App{
		params: p, cfg: cfg, f0: cfg.F0, phi: cfg.Phi, simTick: cfg.Tick,
		frameInterval: 33 * time.Millisecond,
		views:         []View{&fView{}, &vView{}, &splitView{}, &topoView{}, &congView{}},
	}
	a.build()
	defer a.stop()
	if a.topo.Grid == nil {
		a.active = 3 // non-grid topologies default to the universal topology view
	}
	a.Run()
}

// build constructs a fresh mesh for the current params, starts it, and resets selection/destination.
func (a *App) build() {
	m := proto.NewMesh(a.cfg)
	a.topo = buildTopo(m, a.params)
	m.Start()
	a.m = m
	a.dst = a.topo.Dst
	a.dead = map[proto.NodeID]bool{}
	a.cut = map[[2]proto.NodeID]bool{}
	a.stressed = map[proto.NodeID]bool{}
	a.cutArm = ""
	a.sel = a.topo.Dst
}

func (a *App) stop() {
	if a.m != nil {
		a.m.Stop()
	}
}

// cycleTopo tears down the mesh and rebuilds the next topology in the ring — live topology switching.
func (a *App) cycleTopo() {
	i := 0
	for j, k := range topoCycle {
		if k == a.params.kind {
			i = j
		}
	}
	a.params.kind = topoCycle[(i+1)%len(topoCycle)]
	a.stop()
	a.build()
	if a.topo.Grid == nil {
		a.active = 3
	}
}

// Run is the viewer lifecycle: enter the alt screen, drive a sample+render loop off a ticker and a raw
// key reader, and restore the terminal on EVERY exit path (return, signal, panic).
func (a *App) Run() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		time.Sleep(500 * time.Millisecond) // converge briefly, dump one static frame, exit clean
		a.cols, a.rows = 100, 40
		var b strings.Builder
		a.views[a.active].Draw(&b, a)
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
	case "1", "2", "3", "4", "5":
		a.active = int(k[0] - '1')
	case "c":
		a.cutVerb()
	case "\t":
		a.active = (a.active + 1) % len(a.views)
	case "\x1b[Z": // shift-tab
		a.active = (a.active - 1 + len(a.views)) % len(a.views)
	case "t":
		a.cycleTopo()
	case "\x1b[A", "k":
		a.moveSel(0, -1)
	case "\x1b[B", "j":
		a.moveSel(0, 1)
	case "\x1b[C", "l":
		a.moveSel(1, 0)
	case "\x1b[D", "h":
		a.moveSel(-1, 0)
	case "d":
		a.dst = a.sel
	case "x":
		a.killToggle()
	case "r":
		if a.dead[a.sel] {
			a.m.Revive(a.sel)
			delete(a.dead, a.sel)
		}
	case "z":
		a.reset()
	case "+", "=":
		a.retune(0.75)
	case "-", "_":
		a.retune(1 / 0.75)
	case "]":
		a.tuneKappa(1.5) // stiffer: raise the selected node's κ (more load-tolerant)
	case "[":
		a.tuneKappa(1 / 1.5) // softer: lower κ (more load-sensitive)
	}
	return false
}

// tuneKappa scales the selected node's baseline screening κ_i live, clamped to a usable range.
func (a *App) tuneKappa(f float64) {
	if a.sel == "" {
		return
	}
	k := a.m.Node(a.sel).Kappa() * f
	if k < 1e-3 {
		k = 1e-3
	} else if k > 50 {
		k = 50
	}
	a.m.SetKappa(a.sel, k)
}

func (a *App) selID() proto.NodeID { return a.sel }

// moveSel advances the selection to the nearest node in the pressed compass direction — works on any
// topology, reducing to grid steps on the lattice and to spatial nearest-neighbour elsewhere.
func (a *App) moveSel(dx, dy int) {
	cur, ok := a.topo.Pos[a.sel]
	if !ok {
		if len(a.topo.IDs) > 0 {
			a.sel = a.topo.IDs[0]
		}
		return
	}
	best, bestScore := a.sel, math.Inf(1)
	for _, id := range a.topo.IDs {
		if id == a.sel {
			continue
		}
		p := a.topo.Pos[id]
		vx, vy := p[0]-cur[0], p[1]-cur[1]
		if vx*float64(dx)+vy*float64(dy) <= 1e-9 {
			continue // not in the pressed direction
		}
		off := math.Abs(vx*float64(dy) - vy*float64(dx)) // perpendicular drift, penalized
		if score := math.Hypot(vx, vy) + 2*off; score < bestScore {
			bestScore, best = score, id
		}
	}
	a.sel = best
}

func (a *App) killToggle() {
	if a.sel == "" {
		return
	}
	if a.dead[a.sel] {
		a.m.Revive(a.sel)
		delete(a.dead, a.sel)
	} else {
		a.m.Kill(a.sel)
		a.dead[a.sel] = true
	}
}

// cutVerb is the two-step edge cut/relink: first press arms at the selection; a second press on an
// adjacent node toggles that edge (Cut↔Relink); a press on a non-adjacent node re-arms there.
func (a *App) cutVerb() {
	switch {
	case a.cutArm == "":
		a.cutArm = a.sel
	case a.cutArm == a.sel:
		a.cutArm = "" // cancel
	case a.adjacent(a.cutArm, a.sel):
		k := edgeKey(a.cutArm, a.sel)
		if a.cut[k] {
			a.m.Relink(a.cutArm, a.sel)
			delete(a.cut, k)
		} else {
			a.m.Cut(a.cutArm, a.sel)
			a.cut[k] = true
		}
		a.cutArm = ""
	default:
		a.cutArm = a.sel // re-arm
	}
}

func (a *App) adjacent(x, y proto.NodeID) bool {
	for _, nb := range a.topo.Adj[x] {
		if nb == y {
			return true
		}
	}
	return false
}

// stressToggle loads or unloads the selected node — the user-driven load side of the congestion loop.
func (a *App) stressToggle() {
	if a.sel == "" {
		return
	}
	if a.stressed[a.sel] {
		a.m.Node(a.sel).SetStress(0)
		delete(a.stressed, a.sel)
	} else {
		a.m.Node(a.sel).SetStress(30)
		a.stressed[a.sel] = true
	}
}

func (a *App) reset() {
	for id := range a.dead {
		a.m.Revive(id)
	}
	for k := range a.cut {
		a.m.Relink(k[0], k[1])
	}
	for id := range a.stressed {
		a.m.Node(id).SetStress(0)
	}
	a.dead = map[proto.NodeID]bool{}
	a.cut = map[[2]proto.NodeID]bool{}
	a.stressed = map[proto.NodeID]bool{}
	a.cutArm = ""
	a.dst = a.topo.Dst
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
