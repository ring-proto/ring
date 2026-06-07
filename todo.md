# RING — link-aging unification: roadmap

The routing field should forget stale evidence the same way vitality does. This file tracks the
discovery (confirmed), the remaining sim/test prep, and the math + doc/paper revision that follows.

---

## 1. Confirmed (the discovery)

**The freshness window was load-bearing _and_ redundant.**
- A dead **relay** (vs a dead destination) in a **redundant** topology black-holes forever without the
  freshness window: vitality toward the destination stays high (it's alive via the backup), so the
  consumer never severs, and the direction field keeps pointing at the corpse. Measured: delivery
  **1.00** (Fresh=8) vs **0.00** (Fresh=∞), `via` stuck on the dead relay.
- The dead-relay signal already existed, unused: `vit[neighbour] → 0.10` (the relay self-pins while
  alive, so we age its vitality). Forwarding gates on `vit[dst]`, never `vit[via]`.

**Why freezing the stored potential is wrong (the physics).**
- `(L_g+Q)f=0` is Kirchhoff's current law: conductances `g_ij` on edges, leak `q=κ+μ` to ground,
  destination = voltage source `F0`, `f_i` = node voltage. The routing field **is** a resistor network.
- A stored `pot_j` is a snapshot of a node that keeps diffusing **and leaking** between emissions.
  Freezing it asserts the leak switched off. The source-free field decays at rate `λ_min(L_g+Q) ≥ κ`,
  the same κ that sets the reach `λ=√(D/κ)`.
- A frozen `pot_j` is a **spurious pin** — a fixed source term κ (a denominator) cannot drain.

**The right operator is `reg`, not exponential.** (provenance: parallel-resistance / reciprocal
Bellman-Ford; rational reciprocal `1/x`, not the textbook `e^{-x}` Cole–Hopf.)
- Staleness = **series resistance**. Age the link conductance:
  `g_eff = age(g, k) = g/(1 + α·k·g)`, i.e. `1/g_eff = 1/g + α·k` (k = ticks since last heard).
- This is the **same `age()` call** as vitality, at the **same rate** → `β = α`. One operator, two
  fields, dual coordinates: vitality ages path-distance (`U = 1/V`, `U += α/hop`); the routing field
  ages link-resistance (`R = 1/g`, `R += α/stale-tick`). Both reciprocal-Bellman, both graceful, both
  tombstone-free.

**Prototype → foundation (implemented in `proto`, full suite green).**
- `g_eff = age(g, staleness)` folded into the sweep (`step`), always on.
- `Config.Fresh` and the prototype `LinkAge` **removed** — no foundational backstop.
- `bidir`: bidirectional echo only (`p.hears`); staleness handled by the aged conductance.
- `heardList`: gated on `age(g, staleness) > Floor` (infinitely-stale / never-heard peers age to ~0).
- `EffConductance(nbr)` accessor for instrumentation.
- Measured: dead-relay reroute delivers (drain follows `1/g_eff = 1/g + α·k` to the digit; live-link
  sawtooth bounded ~`β·τ_emit`; `via` flips when the corroding link drains below the backup).
- **Decision:** no backstop in the math/theory/protocol; a fast-failover cutoff is a **policy** read on
  `g_eff`, exactly parallel to φ on `V`.

---

## 2. Next — finish prepping the sim + testing

- [x] **Breadth tests across topologies** (dead-relay reroute + delivery stays high under single failure):
  - [x] parallel bridges (kill one bridge hub → reroute via the other)
  - [x] grid / mesh robustness (kill an interior node)
  - [x] leaf-spine / Clos (kill a spine)
  - [x] flapping: repeated kill/revive (drain on death, re-acquire on return, no stuck state, no
        resurrection)
  - [x] multi-failure (several relays down, reroute through survivors)
- [x] **Field stability under always-on aging** — explicit live-line sawtooth test keeps route/delivery
      / unit stretch stable; observed live `g_eff` sawtooth bounded at roughly 0.96–1.00 in the sample.
- [x] **Reroute latency characterization** — added timing characterization for the dead-relay diamond.
      Observed async spread includes fast flips (~10–20ms) and drain-limited flips (~630ms in a 10-run
      sample); document as topology/scheduler/phase-sensitive rather than a single fixed latency. It
      remains tunable via α (which also sets the vitality horizon — one knob).
- [x] **β=α alignment** — decision: keep `β = α` in the field foundation. Reason: both transported
      stale-evidence coordinates are reciprocal Bellman variables aged in proper time: vitality uses
      `U = 1/V` with `U ← U + α·Δt`; link evidence uses resistance `R = 1/g` with
      `R ← R + β·Δt`. Setting `β=α` is the one-operator statement. κ is not the link-forget rate; it is
      the routing field's spatial leak/screening term, setting `λ ≈ √(D/κ)`. Tying `β` directly to κ
      would mix the evidence-aging clock with the elliptic screening length and gives the wrong scaling
      for horizon alignment. The α/κ relationship is a **tuning relation**, not a second operator:
      choose `α` so `R_V = (1/φ - 1)/α ≍ λ = √(D/κ)` when space-time locality should align, i.e.
      `α ≍ (1/φ - 1)·√(κ/D)`. With `φ=0.5`, this reduces to `α ≍ √(κ/D)`. Decision for the sim/paper
      proving ground: set `DefaultConfig` to κ-scaled aging with a policy action tail,
      `D=1, κ=0.05, α≈√0.05≈0.224, φ=0.2`, so `R_V≈4λ`. This is not exact `R_V=λ` alignment; exact
      alignment at `φ=0.2` would require `α≈0.894` and intentionally makes existing long-path tests
      non-actionable. Paper/docs now frame this as foundation (`β=α`) plus policy (`φ`) plus tuning
      (`α` relative to `κ`).
- [ ] **Ablation flag?** — decide whether to keep a `NoLinkAge` toggle (parallel to `NoRetard`) for the
      "link-aging is necessary" demonstration/figure.
- [~] **Tidy** — promoted scratch test to `link_aging_test.go`; cleaned stale in-code freshness-window
      comments; `EffConductance` stays for instrumentation through breadth/latency characterization.
      Keep language sharp: the field foundation has no load-bearing thresholds; thresholds are policy
      reads/pruning choices above the continuous fields.
- [ ] **Checkpoint commit** the sim change once breadth-confirmed.

---

## 3. Then — math review + doc/paper revision

**Review the math, then revise `ring.tex` + `docs/ring.md` + `docs/protocol.md` + `docs/physics.md`.**

- [x] **§5.3 (convergence):** κ + Chazan–Miranker handle **bounded-delay** staleness (slow neighbours).
      A dead neighbour is **unbounded** delay — handled by the aged conductance, not κ. State this
      cleanly so the two are not conflated.
- [x] **§7.1 / protocol.md:** delete the freshness-window apology ("the one place a staleness threshold
      appears", "may remove", "nothing to expire"). The field forgets via `age()` like vitality; the
      bidirectional echo is gated by the aged conductance. The only thresholds left are **policy** (φ,
      Floor, and any deployment failover cutoff).
- [x] **The unification (new core result):** staleness = series resistance; `g_eff = age(g, staleness)`;
      both fields run one `reg`/`age` operator at α. The parallel-resistance / reciprocal-Bellman
      semiring is the Kirchhoff↔Bellman-Ford bridge — credit the provenance; contrast the rational
      reciprocal `1/x` against the textbook `e^{-x}`. Strengthens the space-time-horizon thesis (both
      fields forget stale evidence in proper time at aligned rates).
- [x] **New theorem/lemma:** the routing field forgets stale evidence at α; a dead relay's frozen
      advertisement is a spurious pin the leak cannot drain; the aged conductance reroutes (the
      dead-relay analog of retardation / Thm 6.2). Possibly: convergence preserved (the sawtooth is
      bounded and source-refreshed).
- [ ] **Figures:** "link-aging is necessary" (freeze pins onto corpses vs graceful reroute — parallel to
      the retardation figure); the drain law matching `1/(1+α·k)`; the live-link sawtooth bound.
- [x] **Notation / model:** add `g_eff`, staleness, the resistor-network/Kirchhoff framing; note β=α.
- [ ] **Related work / provenance:** position vs Age-of-Information (`U += ατ` unbounded), AntNet
      (exponential heuristic), and the 1980s integer band-aids (split-horizon, poison-reverse, sequence
      numbers, DUAL/EIGRP); the novelty is the rational-reciprocal mapping to the parallel-resistance
      semiring.
- [ ] **Keep the twins in lockstep** (`ring.tex` ↔ `docs/ring.md`) and re-run the consistency audit after.
- [ ] Rebuild `ring.pdf`.
