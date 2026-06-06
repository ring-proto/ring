package proto

import (
	"testing"
	"time"
)

// TestUnsyncedClocks drops the shared-clock assumption: each node gets a large private clock
// skew (offsets of many Ticks, as if NTP were absent). It runs the same converge-then-sever
// cycle in both retardation modes — measured cross-clock transit (needs sync) and clock-free
// hop-age — to show which survives unsynchronized clocks.
func TestUnsyncedClocks(t *testing.T) {
	// a fixed, adversarial-ish skew pattern in Ticks (both signs, several Ticks each), so the
	// measured transit (recv_clock − emit_clock) is corrupted by skew differences that dwarf the
	// true sub-Tick transit.
	skewTicks := []int{-15, 11, -9, 14, -12}

	run := func(hopAge bool) (convergedLive bool, vLive float64, severed bool, vDead float64) {
		cfg := DefaultConfig()
		cfg.HopAge = hopAge
		m := NewMesh(cfg)
		ids := BuildLine(m, 5)
		O, src := ids[0], ids[4]
		for i, id := range ids {
			m.SetSkew(id, time.Duration(skewTicks[i])*cfg.Tick)
		}
		m.Start()
		defer m.Stop()
		convergedLive = waitUntil(2*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return r })
		_, vLive, _ = m.Node(O).Route(src)
		m.Kill(src)
		severed = waitUntil(4*time.Second, func() bool { _, _, r := m.Node(O).Route(src); return !r })
		_, vDead, _ = m.Node(O).Route(src)
		return
	}

	cl, vl, sv, vd := run(false)
	t.Logf("MEASURED-TRANSIT (needs clock sync) under skew: live-routable=%-5v (V=%.3f)  dead-severed=%-5v (V=%.3f)", cl, vl, sv, vd)
	cl2, vl2, sv2, vd2 := run(true)
	t.Logf("CLOCK-FREE HOP-AGE              under skew: live-routable=%-5v (V=%.3f)  dead-severed=%-5v (V=%.3f)", cl2, vl2, sv2, vd2)

	if !cl2 || !sv2 {
		t.Errorf("clock-free hop-age must converge AND sever under unsynchronized clocks; got live=%v dead-severed=%v", cl2, sv2)
	}
	if cl && sv {
		t.Logf("(measured-transit happened to survive this pattern — try larger skews)")
	} else {
		t.Logf("(measured-transit malfunctions under skew, as predicted: %s)", brokeHow(cl, sv))
	}
}

func brokeHow(convergedLive, severed bool) string {
	switch {
	case !convergedLive:
		return "live source never became routable (over-aged transit → V driven below φ)"
	case !severed:
		return "dead source never severed (under-aged/negative transit → resurrection)"
	default:
		return "ok"
	}
}
