# physics.md — Field-Theoretic Routing

This document specifies the routing protocol. Each node maintains, for every destination, two scalar
fields obtained by local relaxation of its neighbours' values: a **routing potential** $f$ whose
gradient gives the next hop, and a **vitality** $V\in[0,1]$ that measures confidence the destination is
live. Packets are forwarded up the potential; a route is acted on when its vitality clears a chosen policy
threshold. Stale routing evidence ages by the same reciprocal proper-time law, applied to link resistance.
Both fields are governed by the same screened, monotone, contraction dynamics, and every
safety property below — loop-freedom, black-hole-freedom, convergence, bounded liveness — is a
structural consequence of that operator, proved in place.

---

## 1. The operator

For a destination $d$, the routing potential $f^d:V\to\mathbb{R}_{\ge 0}$ solves a screened-Poisson
equation, pinned at the destination:

$$
\boxed{\;\big(\mathcal{L}_g + Q^d\big)\,f^d = 0 \ \text{ on } V\setminus\{d\},\qquad f^d_d = F_0\;}
$$

where $\mathcal{L}_g$ is the conductance-weighted graph Laplacian and $Q^d=\operatorname{diag}(q^d_i)$ is
the diagonal **screening charge**, $q^d_i \ge \kappa > 0$. Liveness rides as a second field $V^d$,
transported and aged by proper time. The data plane is ascent of $f^d$; actionability is a threshold on
$V^d$. There is no distance table, path vector, or separate failure detector — those are projections of
these two fields.

---

## 2. Model and notation

| symbol | meaning |
|---|---|
| $G=(V,E)$ | the network graph; $N(i)$ = the bidirectionally-confirmed neighbours of $i$ |
| $g_{ij}>0$ | conductance of edge $\{i,j\}$, symmetric ($g_{ij}=g_{ji}$); uniform value $D$ |
| $g^{\mathrm{eff}}_{ij}(k)$ | effective conductance after $k$ stale ticks: $1/g^{\mathrm{eff}}_{ij}=1/g_{ij}+\alpha k$ |
| $G_i=\sum_{j\in N(i)}g_{ij}$ | total conductance incident on $i$ |
| $\kappa>0$ | baseline screening (sets reach) |
| $\mu_i\ge 0$ | local screening from load at $i$; the screening charge is $q_i=\kappa+\mu_i$ |
| $F_0>0$ | the potential a destination pins for its own field |
| $f^d_i\ge 0$ | routing potential at $i$ toward $d$ |
| $V^d_i\in[0,1]$ | vitality (liveness confidence) at $i$ toward $d$ |
| $\alpha>0$ | proper-time stale-evidence aging rate; used for vitality and link aging ($\beta=\alpha$) |
| $\varphi\in(0,1)$ | policy threshold for acting on vitality |
| tick | one relaxation period; proper time is counted in ticks |

**Adjacency by echo.** $j\in N(i)$ only if $i$ hears $j$ *and* $j$'s advertisement names $i$. A one-way
link carries no flow and contributes nothing to $\mathcal{L}_g$.

**The weighted Laplacian.** For $x:V\to\mathbb{R}$,

$$
(\mathcal{L}_g x)_i = \sum_{j\in N(i)} g_{ij}(x_i-x_j) = G_i x_i - \sum_{j\in N(i)} g_{ij} x_j .
$$

$\mathcal{L}_g$ is symmetric positive-semidefinite with the constant vector in its kernel. Adding the
diagonal $Q\succ 0$ (since $q_i\ge\kappa>0$) makes $\mathcal{L}_g+Q$ symmetric, **strictly diagonally
dominant** (row $i$ has diagonal $G_i+q_i$ against off-diagonal mass $G_i$), with non-positive
off-diagonals — a symmetric M-matrix, hence positive-definite and inverse-positive. This is the single
structural fact the proofs below use.

---

## 3. The routing field

### 3.1 The screened balance

Each non-destination node relaxes its potential to the conductance-weighted mean of its neighbours,
attenuated by the screening charge:

$$
\boxed{\;f^d_i \;\longleftarrow\; \frac{\displaystyle\sum_{j\in N(i)} g_{ij}\,f^d_j}{\displaystyle G_i + q_i}\;,\qquad q_i = \kappa+\mu_i\;}
\tag{3.1}
$$

The destination pins $f^d_d=F_0$ and does not relax its own field. Clearing the denominator gives the
operator form of §1, $(\mathcal{L}_g f^d)_i + q_i f^d_i = 0$. Each destination's field is independent and
linear; a node maintains one scalar relaxation per known destination over a shared neighbour table.

**Non-negativity.** The pin is $F_0>0$ and (3.1) is a non-negative combination, so if the field starts
non-negative it stays non-negative. The operator $\mathcal{L}_g+Q$ is inverse-positive (§2), so the fixed
point satisfies $f^{d\star}_i>0$ for every node in $d$'s connected component; operational reach is where
that positive field remains above the floor, not a hard hop count.

### 3.2 The screening charge

$q_i=\kappa+\mu_i$ is one quantity with two contributions. $\kappa$ is the uniform floor that sets the
global reach (§3.3); $\mu_i\ge 0$ is the extra local screening a node carries under load (§3.8). Every
result in §3 is proved for an arbitrary charge field $q_i\ge\kappa>0$, so it holds identically whether a
node is idle ($\mu_i=0$) or congested ($\mu_i>0$): congestion is the same operator evaluated at a larger
diagonal, never a special case.

### 3.3 Reach

In the homogeneous interior ($q_i=\kappa$, uniform $g_{ij}=D$) on a chain, a geometric mode
$f_i=\rho^{\,i}$ satisfies the fixed point $f_i(2D+\kappa)=D(f_{i-1}+f_{i+1})$, i.e.

$$
\rho+\rho^{-1} = 2 + \frac{\kappa}{D}.
$$

The decaying root $\rho\in(0,1)$ gives per-hop attenuation; the **reach** is $\lambda=-1/\ln\rho$. For
$\kappa/D\ll 1$, $\rho\approx 1-\sqrt{\kappa/D}$, so

$$
\boxed{\;\lambda \approx \sqrt{D/\kappa}\;}
$$

the screening length of the continuum operator $D\nabla^2 f - \kappa f = 0$. Reach is a length set by
$\kappa$, not a hop count. A node carrying $\mu_i>0$ has a locally shorter reach $\sqrt{D/(\kappa+\mu_i)}$.

### 3.4 Convergence

Write (3.1) as $f\leftarrow Mf$ with $M_{ij}=g_{ij}/(G_i+q_i)$ for $j\in N(i)$ and $M_{ii}=0$ (the pin
enters as a fixed boundary term).

**Lemma 3.1 (contraction).** $\;\lVert M\rVert_\infty \le \gamma < 1$, where
$\displaystyle \gamma = \max_i \frac{G_i}{G_i+q_i} \le \max_i \frac{G_i}{G_i+\kappa} < 1.$
*Proof.* Each row is non-negative with sum $\sum_j M_{ij} = G_i/(G_i+q_i) = 1 - q_i/(G_i+q_i)$. Since
$q_i\ge\kappa>0$ this is $<1$; the maximum over rows is $\gamma<1$. $\;\blacksquare$

**Theorem 3.2 (fixed point).** The field equation has a unique solution $f^{d\star}$, and synchronous
relaxation converges geometrically: $\lVert f^{(k)}-f^{d\star}\rVert_\infty \le \gamma^k\,\lVert
f^{(0)}-f^{d\star}\rVert_\infty$.
*Proof.* $M$ is an $\lVert\cdot\rVert_\infty$-contraction (Lemma 3.1); apply Banach. $\;\blacksquare$

**Theorem 3.3 (asynchronous convergence).** For a fixed charge field $q$, under any schedule in which
every node relaxes infinitely often using possibly-stale neighbour values (bounded delay), the iteration
converges to $f^{d\star}$.
*Proof.* $M\ge 0$ entrywise, so $\rho(M)\le\lVert M\rVert_\infty=\gamma<1$; the Chazan–Miranker theorem
on chaotic relaxation gives convergence of $x\leftarrow Mx$ whenever $\rho(M)<1$. $\;\blacksquare$

Bounded-delay staleness is not node death. A silent relay is an unbounded delay: its last potential may
remain stored while the physical node no longer diffuses or leaks. RING therefore ages the conductance of
stale routing evidence as added series resistance,
$$
\boxed{\;\frac{1}{g^{\mathrm{eff}}_{ij}(k)}=\frac{1}{g_{ij}}+\alpha k\;},\qquad
g^{\mathrm{eff}}_{ij}(k)=\frac{g_{ij}}{1+\alpha k g_{ij}}.
\tag{3.3a}
$$
This is the same reciprocal law as vitality aging: $U=1/V$ gains $\alpha k$, and link resistance $R=1/g$
gains $\alpha k$. If one writes a separate link-aging rate $\beta$, the foundation sets $\beta=\alpha$.
For finite $k$, $g^{\mathrm{eff}}\ge0$ leaves the screened operator an M-matrix with $q_i\ge\kappa$; as
$k\to\infty$, $g^{\mathrm{eff}}\to0$, so a dead relay's frozen potential loses all coefficient and cannot
remain a permanent pin. Pruning negligible $g^{\mathrm{eff}}$ is an implementation policy, not a load-bearing
freshness threshold.

When the charge field drifts (a node's $\mu_i$ changing under load, §3.8) the field tracks a moving
target rather than a fixed one, and the relevant property is stability, not convergence to a point. Every
$M_t$ satisfies $\lVert M_t\rVert_\infty\le\bar\gamma<1$ with $\bar\gamma=\max_i G_i/(G_i+\kappa)$ — a
uniform contraction in one fixed norm, independent of the drift. Two consequences follow. The homogeneous
error contracts at rate $\bar\gamma$ under every schedule (the asynchronous-iteration theorem for maps
that are uniform contractions in a common norm), so the field cannot diverge while $\mu$ moves; and if
$\mu$ settles to any $\mu_\infty$ the iteration converges to the corresponding fixed point $f^\star_\infty$
by Theorem 3.2/3.3. Drift in $\mu$ thus bounds, never destabilises, the field.

### 3.5 The maximum principle

**Theorem 3.4 (no interior maximum).** At the fixed point, every non-destination node $i$ with
$f^{d\star}_i>0$ satisfies $f^{d\star}_i < \max_{j\in N(i)} f^{d\star}_j$.
*Proof.* By (3.1), $f^{d\star}_i = \gamma_i\,\langle f^{d\star}\rangle_{N(i)}$ where
$\gamma_i=G_i/(G_i+q_i)<1$ and $\langle f\rangle_{N(i)}=\big(\sum_j g_{ij}f_j\big)/G_i$ is the
conductance-weighted neighbour mean. A mean is at most its maximum, so $f^{d\star}_i\le\gamma_i\max_j
f^{d\star}_j$. With $f^{d\star}_i>0$ we have $\max_j f^{d\star}_j>0$, hence $\gamma_i\max_j f^{d\star}_j <
\max_j f^{d\star}_j$. $\;\blacksquare$

The field's only maxima are the pinned destinations. Equivalently: the gradient has no interior sink.

### 3.6 Forwarding — the current split

A node forwards along the discrete current $J_{ij}=g_{ij}(f^d_j-f^d_i)$. The active next hops are the
strictly-uphill neighbours, with the flow split in proportion to the current:

$$
\boxed{\;N^{\uparrow}(i)=\{\,j\in N(i):f^d_j>f^d_i\,\},\qquad w_{ij}=g_{ij}\,(f^d_j-f^d_i),\qquad \text{via}=\arg\max_j w_{ij}\;}
\tag{3.6}
$$

Forwarding to all uphill neighbours is a current-proportional spread (a generalisation of equal-cost
multipath); the dominant edge is the maximum-current hop. Conductance biases the split toward
high-quality links without affecting which neighbours qualify, since the gate (3.6) is on the potential
alone.

### 3.7 Loop-freedom and dissipativity

**Theorem 3.5 (loop-freedom).** For any single field configuration $f$, the active-edge relation
$\{i\to j: f_j>f_i\}$ is acyclic.
*Proof.* A directed walk along active edges strictly increases $f$; a cycle would force
$f_{i_0}<f_{i_1}<\cdots<f_{i_0}$, a contradiction. The active edges are the strict-ascent relation of a
real-valued labeling, hence a DAG — for the equilibrium field and for every relaxation iterate alike.
$\;\blacksquare$

**Theorem 3.6 (black-hole-freedom).** At equilibrium, ascent from any node with $f^{d\star}_i>0$ reaches
$d$ in finitely many hops.
*Proof.* By Theorem 3.4 every non-destination node with positive potential has a strictly-higher
neighbour, so $N^{\uparrow}(i)\neq\varnothing$; each hop strictly increases a quantity bounded above by
$F_0$, and on a finite graph a strictly-increasing walk terminates — necessarily at the maximum, $d$.
$\;\blacksquare$

A node decides (3.6) by comparing a neighbour's advertised potential against its own. When every
comparison reads one field configuration — the common case, in which each node re-advertises every tick —
the active edges are that configuration's strict-ascent relation and are acyclic at every round by
Theorem 3.5. Under bounded-delay asynchrony a comparison may instead mix values from different relaxation
steps, and a transient cycle can momentarily appear; it cannot persist:

**Theorem 3.7 (eventual non-trapping).** Fix the graph and charge field after the last disturbance. Under
bounded-delay fair asynchrony there is a finite time after which the dominant forwarding hop is acyclic
and no packet is trapped.
*Proof.* By Theorem 3.3 every node's advertised potential converges to $f^{d\star}$, so the bounded-delay
samples any node compares are all eventually within any $\varepsilon>0$ of $f^{d\star}$. Let $g_{\min}$
be the smallest *nonzero* equilibrium gap, $\min\{|f^{d\star}_i-f^{d\star}_j|:f^{d\star}_i\neq
f^{d\star}_j\}$. Once every sample is within $g_{\min}/2$ of equilibrium, every edge with
$f^{d\star}_i\neq f^{d\star}_j$ is compared with the correct sign, so the active relation restricted to
such edges is the equilibrium strict-ascent DAG (Theorems 3.5, 3.4). The current weight on a *level-set*
edge ($f^{d\star}_i=f^{d\star}_j$) is $O(\varepsilon)\to 0$, while by Theorem 3.4 every non-destination
node — including one on a level set — has a strictly-higher *exterior* neighbour, so its dominant
(maximum-weight) current leaves the level set and climbs the equilibrium gradient. The via-map therefore
follows the equilibrium ascent and is acyclic; the only circulation not excluded is the vanishing
$O(\varepsilon)$ flow on a flat level set, which carries no potential progress and decays to zero. $\;\blacksquare$

Loop-freedom is thus structural for any consistent field (Theorem 3.5) and, once the operator settles,
non-trapping under asynchrony: packets follow the dominant gradient current, which leaves every
non-destination node, so a transient eddy carries vanishing flow and cannot trap them. Under *continuing*
forcing — a load that keeps $\mu$ in motion — the guarantee is the corresponding bounded one: circulation
stays confined to near-flat level sets and carries vanishing current, a bounded non-equilibrium steady
state rather than exact acyclicity.

### 3.8 Congestion

A node under load raises its screening charge through $\mu_i$, which by (3.1) flattens its potential
toward zero — a shorter local reach (§3.3). The current split (3.6) then steers flow around it: the
loaded node advertises a depressed potential, so fewer neighbours find it uphill. Because $\mu_i\ge 0$
keeps $q_i\ge\kappa>0$, every guarantee of §3.4–§3.7 holds unchanged; congestion reshapes the field
without weakening any invariant.

The load-to-charge map is closed through a **non-overshoot integrator**. With stress $s_i$ (queue depth /
carried demand) and a coupling $c$, the charge glides

$$
\mu_i \;\longleftarrow\; \operatorname{reg}(\mu_i,\;c\,s_i,\;r),\qquad
\operatorname{reg}(v,T,r)=T+\frac{v-T}{1+r\,\lvert v-T\rvert}.
\tag{3.8}
$$

**Lemma 3.8 (non-overshoot).** $\operatorname{reg}(v,T,r)$ lies strictly between $v$ and $T$ for $r>0$,
$v\neq T$, reaching $T$ only in the limit.
*Proof.* The step is $T+(v-T)/(1+r\lvert v-T\rvert)$; the factor $1/(1+r\lvert v-T\rvert)\in(0,1)$ shrinks
the gap $v-T$ without changing its sign. $\;\blacksquare$

Because $\mu_i$ approaches its instantaneous target monotonically and never crosses it, the charge update
is itself non-overshooting and bounded — it removes the explicit-Euler overshoot mechanism that a
proportional update would introduce, and damps the load$\leftrightarrow$field feedback. (The target
$c\,s_i$ itself moves as routing shifts load, so closed-loop freedom from ringing under sustained forcing
is an empirical property, not a consequence of the lemma alone.)

---

## 4. The vitality field

Reach screens the routing field in space. Vitality screens liveness in time. For a destination $d$ each
node carries $V^d_i\in[0,1]$, its confidence that $d$ is live, transported from the source and aged by the
proper time the evidence has spent in flight.

### 4.1 Proper time and the light cone

A relayed value is evidence about its source as of the moment it left. Information advances at most one
hop per tick, so a node at hop-distance $\delta$ from $d$ can hold evidence no fresher than $\delta$ ticks
old: liveness lies on a light cone. A relay therefore ages the value it forwards by the proper time it
carried it — one tick per hop traversed.

### 4.2 The aging law

Vitality decays toward $0$ (the unknown/dead state) by the integrator of (3.8) with target $0$ and rate
$\alpha$. One tick of aging is $A(v)=\operatorname{reg}(v,0,\alpha)=v/(1+\alpha v)$.

**Lemma 4.1 (Lorentzian decay).** $\;A^{k}(1)=\dfrac{1}{1+\alpha k}.$
*Proof.* $1/A(v)=(1+\alpha v)/v = 1/v+\alpha$, so $1/A^{k}(1)=1/1+k\alpha$. $\;\blacksquare$

Vitality is an exact reciprocal-linear function of the age of its evidence — the harmonic continuation of
the source's last assertion of life.

### 4.3 Retardation

A value arriving after $\tau\ge 1$ ticks in flight is evidence from $\tau$ ticks ago and is aged by $\tau$
before use, $\tilde v=A^{\tau}(v)$. Aging the carried evidence by its transit, rather than reading it as
current, is required for the field to decay at all.

**Theorem 4.2 (retardation is necessary).** Adopting a neighbour's vitality without aging it by transit
admits a spurious fixed point: on any edge whose endpoints exchange vitality, $V\equiv v$ is fixed for any
$v$, even after the source dies.
*Proof.* With the source gone, suppose each endpoint ages its own value one tick and then takes the max
with the neighbour's last value: $V_a\leftarrow\max\!\big(A(V_a),\,V_b\big)$. If $V_a=V_b=v$ then
$A(v)=v/(1+\alpha v)<v$, so $\max(A(v),v)=v$ — fixed at any $v$, in particular $1$: the un-aged exchange
refreshes each endpoint with a value fresher than its own decay, and the decay never takes hold.
Retardation replaces the neighbour's value with $A^{\tau}(V_b)$. At $V_a=V_b=v$ and the minimal transit
$\tau=1$, $A^{\tau}(v)=A(v)$, equal to the self-aged value, so $\max\big(A(V_a),A^{\tau}(V_b)\big)=A(v)<v$;
for $\tau\ge 2$ the neighbour value is strictly smaller still. In all cases the max no longer exceeds the
self-aging, the spurious fixed point is gone, and the only fixed point is the source-pinned one. Since a
relayed value spends at least one tick in flight, $\tau\ge 1$ always holds. $\;\blacksquare$

### 4.4 Transport and the steady state

The destination pins $V^d_d=1$. Each node ages its held value and adopts the retarded value of any port
that offers a higher one — any sufficiently-fresh path keeps the field alive:

$$
\boxed{\;V^d_i \;\longleftarrow\; \max\!\Big(A(V^d_i),\;\; \max_{j\in N(i)} A^{\tau_{ij}}\big(V^d_{j}\big)\Big)\;}
\tag{4.4}
$$

**Theorem 4.3 (steady state).** For a continuously-live source, the converged vitality is
$$
\boxed{\;V^d_i=\frac{1}{1+\alpha\,T^d_i}\;}
$$
where $T^d_i$ is the least accumulated transit time from $d$ to $i$; with unit per-hop transit
$T^d_i=\delta$ (hop-distance) and $V^d(\delta)=1/(1+\alpha\delta)$.
*Proof.* Substitute the **reciprocal vitality** $U^d_i = 1/V^d_i$. Since $1/A^{\tau}(v)=1/v+\alpha\tau$
(Lemma 4.1), a $\max$ over $V$ is a $\min$ over $U$, and the update (4.4) becomes, away from the source,
$$
U^d_i \;\longleftarrow\; \min\!\Big(\,U^d_i+\alpha,\;\; \min_{j\in N(i)}\big(U^d_j+\alpha\,\tau_{ij}\big)\Big),\qquad U^d_d=1.
$$
This is the Bellman equation for least-cost paths with non-negative edge weights $\alpha\tau_{ij}$ and
source value $1$. Its unique fixed point is $U^d_i=1+\alpha T^d_i$ (least accumulated transit from $d$),
and the $\min$-iteration converges to it from any initialisation under synchronous or bounded-delay
asynchronous updates (the asynchronous shortest-path theorem). Inverting, $V^d_i=1/(1+\alpha T^d_i)$.
$\;\blacksquare$

The reciprocal $U^d_i = 1+\alpha T^d_i$ is the light cone made exact: liveness confidence is the
reciprocal of one plus the least proper time separating $i$ from the source.

A node whose freshest evidence is $T^d_i$ proper-time old therefore reports exactly the confidence the
decay law assigns to evidence of that age — no more. When the source stops asserting life, every held
value ages toward $0$ by Lemma 4.1 and, by Theorem 4.2, nothing can lift it back; vitality decays to $0$
monotonically.

### 4.5 The horizon

Acting only above a threshold $\varphi$ bounds the distance at which liveness can be asserted:

$$
V^d(\delta)\ge\varphi \iff \delta \le R_V=\frac{1/\varphi-1}{\alpha}.
$$

$R_V$ is the radius within which the source's life is known to tolerance $\varphi$; it is set by the
aging rate $\alpha$, the rate at which stale evidence is discounted.

---

## 5. Direction and confidence

A node's routing state for a destination $d$ is the pair it can offer a packet:

- a **direction** — the dominant uphill hop of $f^d$ (3.6), defined whenever the field is above the
  reachability floor and an uphill neighbour exists;
- a **confidence** — the vitality $V^d_i$ (§4).

A consumer acts on the direction when the confidence clears its threshold:

$$
\boxed{\;\text{route}(d,\varphi)=\big(\text{via},\;V^d_i\ge\varphi\big),\qquad \text{known}\equiv\big(f^d_i\ge \text{floor}\ \wedge\ \text{via defined}\big)\;}
$$

The one hard precondition is **known**: a destination is routable only where its field has been received
above the floor. Confidence below $\varphi$ does not erase the direction — it reports that the source's
life is no longer vouched for — so the threshold is the consumer's to choose, per packet.

---

## 6. Guarantees

| property | mechanism | where |
|---|---|---|
| Unique field, geometric convergence | $\lVert M\rVert_\infty=\gamma<1$ contraction | Lem 3.1, Thm 3.2 |
| Convergence under asynchrony | $\rho(M)<1$, Chazan–Miranker; uniform $\bar\gamma$ under load drift | Thm 3.3 |
| No interior potential maximum | M-matrix maximum principle | Thm 3.4 |
| Loop-freedom (any single consistent configuration) | strict ascent of a real labeling is acyclic | Thm 3.5 |
| Black-hole-freedom (at equilibrium, positive in-field nodes) | every such non-source has an uphill neighbour | Thm 3.6 |
| Eventual non-trapping under asynchrony | convergence + max-principle exit; bounded circulation under continuing load | Thm 3.7 |
| Non-overshoot congestion charge | $\mu$ in the screening charge via a non-overshoot integrator | §3.8, Lem 3.8 |
| Distance-graded liveness | retarded vitality $V=1/(1+\alpha T)$ (Bellman in proper time) | Lem 4.1, Thm 4.3 |
| Vitality cannot be revived | retardation removes the circulating fixed point | Thm 4.2 |

The routing guarantees follow from the screened M-matrix operator $\mathcal{L}_g+Q$ with charge
$q_i\ge\kappa>0$ (diagonal dominance, reach, congestion, conductance); the vitality guarantees follow from
the retarded monotone aging law (the Bellman shortest-proper-time equation). Both are local monotone
contractions. The data plane is ascent of $f$; liveness is a threshold on $V$.

---

## 7. Parameters

| symbol | role | reference |
|---|---|---|
| $D$ | bulk conductance | $1$ |
| $\kappa$ | baseline screening; reach $\lambda=\sqrt{D/\kappa}$ | $0.01\Rightarrow\lambda=10$ |
| $F_0$ | destination pin height | $1$ |
| floor | reachability cutoff on $f$ | $10^{-4}$ |
| $\alpha$ | stale-evidence aging rate; horizon $R_V=(1/\varphi-1)/\alpha$ | $\approx\sqrt{\kappa/D}$ (`0.2236` when `κ=0.05,D=1`) |
| $\varphi$ | policy threshold for action | `0.2` in the reference sim (`R_V\approx4\lambda`) |
| $c,\,r$ | load$\to\mu$ coupling and glide rate | per deployment |

*Reach is space, vitality is time, congestion is local screening — one charge on one operator, one field
per destination, ascent on the gradient.*
