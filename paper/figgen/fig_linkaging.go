package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ring-proto/ring/proto"
)

// emitLinkAging measures the §5.4 result: a dead RELAY in a redundant topology is handled by aging its
// link conductance (g_eff = age(g, staleness)), not by a freshness timer. Diamond: a preferred 2-hop
// path src-a-dst and a longer 3-hop backup src-b1-b2-dst; kill `a`. The destination stays alive via the
// backup, so vitality toward it never severs — only the aged conductance can reroute. Two products at
// an illustrative aging rate (α=0.04, legible; the κ-scaled default is α≈0.224, §8):
//   - linkaging_delivery.dat (t freeze aged): delivery fraction over time after the kill, under the
//     NoLinkAge ablation (freeze — a's frozen potential pins src forever, delivery stays ~0) vs the
//     foundation (aged — a's link drains, the field reroutes, delivery recovers to 1). Necessity panel.
//   - linkaging_drain.dat (t g_dead g_live g_ideal): the effective conductance of the dead link a (the
//     reciprocal drain 1/g_eff = 1/g + αk) and a live link b1 (bounded sawtooth near g), with the ideal
//     1/(1+αk) overlay — the mechanism panel. Measured on the aged run.
func emitLinkAging(dir string) {
	const (
		alpha   = 0.04                   // illustrative aging rate (legible); default is κ-scaled α≈0.224
		tick    = 8 * time.Millisecond   // calmer cadence so the async run is cheap and the drain is smooth
		samples = 72                     //
		step    = 50 * time.Millisecond  // 72*50ms = 3.6 s window
		warm    = 800 * time.Millisecond // converge before the kill
	)
	dp := proto.DataPlane{HopDelay: time.Millisecond, TTL: 64}

	diamond := func(noAge bool) *proto.Mesh {
		cfg := figCfg()
		cfg.Alpha, cfg.Tick, cfg.NoLinkAge = alpha, tick, noAge
		m := proto.NewMesh(cfg)
		for _, id := range []proto.NodeID{"src", "a", "dst", "b1", "b2"} {
			m.Add(id)
		}
		m.Link("src", "a") // preferred (2 hops)
		m.Link("a", "dst")
		m.Link("src", "b1") // backup (3 hops)
		m.Link("b1", "b2")
		m.Link("b2", "dst")
		return m
	}
	deliv := func(m *proto.Mesh) float64 {
		const N = 60
		d := 0
		for i := 0; i < N; i++ {
			if m.Forward("src", "dst", dp, nil).Outcome == proto.Delivered {
				d++
			}
		}
		return float64(d) / float64(N)
	}

	type row struct{ t, deliv, gDead, gLive float64 }
	run := func(noAge bool) []row {
		m := diamond(noAge)
		m.Start()
		defer m.Stop()
		time.Sleep(warm)
		m.Kill("a")
		t0 := time.Now()
		out := make([]row, samples)
		for i := 0; i < samples; i++ {
			out[i] = row{
				t:     float64(i) * step.Seconds() * 1000,
				deliv: deliv(m),
				gDead: m.Node("src").EffConductance("a"),
				gLive: m.Node("src").EffConductance("b1"),
			}
			if next := t0.Add(time.Duration(i+1) * step); time.Now().Before(next) {
				time.Sleep(time.Until(next))
			}
		}
		return out
	}

	freeze := run(true)
	aged := run(false)

	var del strings.Builder
	del.WriteString("t freeze aged\n")
	for i := 0; i < samples; i++ {
		fmt.Fprintf(&del, "%.1f %.4f %.4f\n", freeze[i].t, freeze[i].deliv, aged[i].deliv)
	}
	write(filepath.Join(dir, "linkaging_delivery.dat"), del.String())

	tickMs := float64(tick.Milliseconds())
	var dr strings.Builder
	dr.WriteString("t g_dead g_live g_ideal\n")
	for i := 0; i < samples; i++ {
		k := aged[i].t / tickMs // staleness in ticks since the kill
		ideal := 1.0 / (1.0 + alpha*k)
		fmt.Fprintf(&dr, "%.1f %.4f %.4f %.4f\n", aged[i].t, aged[i].gDead, aged[i].gLive, ideal)
	}
	write(filepath.Join(dir, "linkaging_drain.dat"), dr.String())

	fmt.Fprintf(os.Stderr, "linkaging: freeze final delivery=%.2f  aged final delivery=%.2f  g_dead final=%.3f\n",
		freeze[samples-1].deliv, aged[samples-1].deliv, aged[samples-1].gDead)
}
