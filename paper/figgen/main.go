// Command figgen runs the RING reference implementation on a few topologies and writes the
// MEASURED scalar fields to whitespace-separated .dat tables under paper/figures/. The paper's
// figures are rendered from these tables by PGFPlots/TikZ, so every plotted number is a value the
// real async protocol produced — the generator is the single source of truth, the renderers are
// dumb. Re-run it and every figure updates from fresh measurements.
//
//	go run ./paper/figgen            # writes to paper/figures/
//	go run ./paper/figgen -out DIR   # writes elsewhere
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

func main() {
	out := flag.String("out", "paper/figures", "output directory for .dat tables")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		die(err)
	}
	emitVDroop(filepath.Join(*out, "vdroop.dat"))
	emitTimeseries(*out)
	emitGridField(filepath.Join(*out, "gridfield.dat"))
	emitMultisource(*out)
	emitReciprocal(*out)
	emitMaxPrinciple(*out)
	fmt.Println("figgen: wrote tables to", *out)
}

// waitUntil polls pred until it holds or the deadline passes; returns whether it held.
func waitUntil(d time.Duration, pred func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return pred()
}

// emitVDroop measures both transported fields along a line as a function of hop distance from the
// source: the screened routing potential f (geometric decay, reach lambda = sqrt(D/kappa)) and the
// retarded vitality V (1/(1+alpha*delta) by hops). It overlays the closed-form vitality so the
// figure shows measurement tracking theory. Columns: delta f V Videal.
func emitVDroop(path string) {
	cfg := proto.DefaultConfig()
	const N = 9
	m := proto.NewMesh(cfg)
	ids := proto.BuildLine(m, N)
	src := ids[0]
	m.Start()
	defer m.Stop()
	// converge: wait until the far end carries a route, then let the field settle.
	waitUntil(5*time.Second, func() bool { _, _, r := m.Node(ids[N-1]).Route(src); return r })
	time.Sleep(time.Second)

	var b strings.Builder
	b.WriteString("delta f V Videal Teff\n")
	for i, id := range ids {
		if i == 0 { // the source pins its own field; it is never "learned", so report the pin.
			fmt.Fprintf(&b, "%d %.6f %.6f %.6f %.6f\n", 0, cfg.F0, 1.0, 1.0, 0.0)
			continue
		}
		f, v, _, _ := m.Node(id).Field(src)
		ideal := 1.0 / (1.0 + cfg.Alpha*float64(i))
		teff := (1.0/v - 1.0) / cfg.Alpha // effective transit the measured V implies (≥ δ: the relay dwell)
		fmt.Fprintf(&b, "%d %.6f %.6f %.6f %.6f\n", i, f, v, ideal, teff)
	}
	write(path, b.String())
}

// emitTimeseries records the vitality lifecycle at a far observer: it climbs to a steady droop as
// the field converges, the source is then killed, and V ages through the action threshold phi —
// graceful severance, no flap. Writes timeseries.dat (t_ms V phi) and timeseries_kill.dat (the
// single kill time) so the figure can draw the event marker from data, not a hard-coded constant.
func emitTimeseries(dir string) {
	cfg := proto.DefaultConfig()
	const N = 7
	m := proto.NewMesh(cfg)
	ids := proto.BuildLine(m, N)
	src, obs := ids[0], ids[N-1]
	m.Start()
	defer m.Stop()

	start := time.Now()
	var b strings.Builder
	b.WriteString("t V phi\n")
	sample := func() {
		_, v, _ := m.Node(obs).Route(src)
		fmt.Fprintf(&b, "%.1f %.6f %.3f\n", time.Since(start).Seconds()*1000, v, cfg.Phi)
	}
	// converge phase
	for time.Since(start) < 1500*time.Millisecond {
		sample()
		time.Sleep(15 * time.Millisecond)
	}
	killAt := time.Since(start)
	m.Kill(src)
	// decay phase — sample until V has fallen well below phi.
	for time.Since(start) < 4500*time.Millisecond {
		sample()
		time.Sleep(15 * time.Millisecond)
	}
	write(filepath.Join(dir, "timeseries.dat"), b.String())
	// the kill instant as a 2-point vertical segment, so the figure plots the event marker
	// directly with \addplot table — no auxiliary table-reading package.
	km := killAt.Seconds() * 1000
	write(filepath.Join(dir, "timeseries_kill.dat"),
		fmt.Sprintf("t V\n%.1f 0\n%.1f 1.05\n", km, km))
}

// emitGridField snapshots both fields over a 2-D grid toward a corner source after convergence,
// together with the up-gradient hop as a unit flow vector AND the current it carries on that hop.
// This one table drives the f heatmap, the V heatmap, and the flux-weighted flow quiver. Columns:
// x y f V dx dy cur known.
func emitGridField(path string) {
	cfg := proto.DefaultConfig()
	const W, H = 11, 11
	m := proto.NewMesh(cfg)
	proto.BuildGrid(m, W, H)
	id := func(x, y int) proto.NodeID { return proto.NodeID(fmt.Sprintf("g%d-%d", x, y)) }
	src := id(0, 0)
	m.Start()
	defer m.Stop()
	waitUntil(8*time.Second, func() bool { _, _, r := m.Node(id(W-1, H-1)).Route(src); return r })
	time.Sleep(1500 * time.Millisecond)

	// first pass: sample the field into grids
	var f, v [H][W]float64
	var dx, dy, known [H][W]int
	for y := range H {
		for x := range W {
			if x == 0 && y == 0 {
				f[y][x], v[y][x], known[y][x] = cfg.F0, 1, 1
				continue
			}
			fv, vv, via, k := m.Node(id(x, y)).Field(src)
			f[y][x], v[y][x] = fv, vv
			if k {
				known[y][x] = 1
			}
			if via != "" {
				var vx, vy int
				fmt.Sscanf(string(via), "g%d-%d", &vx, &vy)
				dx[y][x], dy[y][x] = vx-x, vy-y
			}
		}
	}
	// second pass: current = D·(f_via − f_here), the up-gradient flux this node forwards toward src
	var b strings.Builder
	b.WriteString("x y f V dx dy cur known\n")
	for y := range H {
		for x := range W {
			cur := 0.0
			if dx[y][x] != 0 || dy[y][x] != 0 {
				if vx, vy := x+dx[y][x], y+dy[y][x]; vx >= 0 && vx < W && vy >= 0 && vy < H {
					cur = cfg.D * (f[vy][vx] - f[y][x])
				}
			}
			fmt.Fprintf(&b, "%d %d %.6f %.6f %d %d %.6f %d\n",
				x, y, f[y][x], v[y][x], dx[y][x], dy[y][x], cur, known[y][x])
		}
	}
	write(path, b.String())

	// marginal CDF of V over the grid — drives the gridV figure's distribution panel; 1−CDF(φ) reads
	// off the fraction of the fabric still above the action threshold (actionable vs past the horizon).
	var vals []float64
	for y := range H {
		for x := range W {
			if known[y][x] == 1 {
				vals = append(vals, v[y][x])
			}
		}
	}
	sort.Float64s(vals)
	var c strings.Builder
	c.WriteString("V cdf\n")
	for i, val := range vals {
		fmt.Fprintf(&c, "%.6f %.6f\n", val, float64(i+1)/float64(len(vals)))
	}
	write(filepath.Join(filepath.Dir(path), "vcdf.dat"), c.String())
}

// emitReciprocal is the EXACT reciprocal-eikonal: hop-distance T (here Manhattan to a centre source,
// which is BFS shortest path on the 4-grid — the protocol's exact fixed point), U = 1+αT (the min-plus
// Bellman metric that "counts to infinity"), and V = 1/U (its bounded reciprocal). The grid drives the
// V light-cone heatmap; the slice drives the twin-axis U-vs-V reciprocal panel. [ideal] = exact.
func emitReciprocal(dir string) {
	cfg := proto.DefaultConfig()
	const W, H = 41, 41
	cx, cy := W/2, H/2
	abs := func(a int) float64 {
		if a < 0 {
			return float64(-a)
		}
		return float64(a)
	}
	var grid strings.Builder
	grid.WriteString("x y T U V\n")
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			t := abs(x-cx) + abs(y-cy)
			u := 1 + cfg.Alpha*t
			fmt.Fprintf(&grid, "%d %d %.1f %.4f %.5f\n", x, y, t, u, 1/u)
		}
	}
	write(filepath.Join(dir, "recip_grid.dat"), grid.String())

	var slice strings.Builder
	slice.WriteString("delta U V phi\n")
	for d := 0; d <= cx+cy; d++ {
		u := 1 + cfg.Alpha*float64(d)
		fmt.Fprintf(&slice, "%d %.4f %.5f %.3f\n", d, u, 1/u, cfg.Phi)
	}
	write(filepath.Join(dir, "recip_slice.dat"), slice.String())
}

func write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		die(err)
	}
	fmt.Printf("  %s (%d bytes)\n", path, len(content))
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "figgen:", err)
	os.Exit(1)
}
