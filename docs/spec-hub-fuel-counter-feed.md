# Spec: Hub FUEL Counter Feed

**Status:** Draft
**Date:** 2026-09-15
**Owner:** Team 841
**Audience:** the counter's author (external), bioarena maintainers

Define how a webcam-based FUEL counter feeds scores for both HUBs into bioarena, and
what bioarena does with them, so that the score arrives in time to drive the field
actions that depend on it and is shown to drivers with no perceptible lag.

This document is the contract between the two sides. Sections 4 and 5 are the parts the
counter's author must implement against; the rest describes bioarena's side so both
parties can see why the contract is shaped the way it is.

---

## Bottom line up front

**What you're building:** a program that watches the field through a webcam, counts
every FUEL ball that goes into the red HUB and the blue HUB, and tells bioarena the
two running totals over the network. That's it. bioarena does the scoring, the
shift logic, and the screens.

**How you tell it:** send one small text message, about ten times a second, to the
field computer at `10.0.100.5` on port `8411`. The message looks like this:

```json
{"v":1,"session":"c1f3a9d2","seq":4821,"red":57,"blue":0,"age_ms":38}
```

`red` and `blue` are "how many balls so far since my program started" — they only ever
go up. You never reset them; bioarena figures out where each match starts and ends.
`session` is a random string you pick once when your program starts, `seq` goes up by
one every message, and `age_ms` is how many milliseconds ago your camera saw the ball
you just counted. The moment a ball goes in, send a message right away — don't wait
for the next scheduled one. bioarena sends a short reply to every message so you can
show a "connected" light and see the match clock if you want to.

**Why speed matters:** in this year's game, whichever alliance scores more FUEL in
the 20-second AUTO period gets an advantage for the rest of the match, and the field
tells both robots who won exactly 3 seconds after AUTO ends. bioarena makes that call
at that instant, using whatever counts it has received *by then*. If your count of a
ball that clearly went in during AUTO is still working its way through your program
when the 3 seconds are up, that ball doesn't affect the decision. So the target is:
**from the ball crossing into the HUB to your message leaving your computer, under
a tenth of a second, and never more than a quarter of a second.** Most of that time
is your camera and your detection code, so that's where to spend your effort. Two
tips that matter more than anything else: count the ball on the first frame you're
sure it went in (don't wait to see where it lands), and measure `age_ms` from when the
camera *captured* that frame, not when your code finished thinking about it.

**What can go wrong, and what bioarena does about it:** if a message gets lost, the
next one replaces it — no harm done. If your program crashes and restarts mid-match,
bioarena notices the new `session` and keeps the score it already had. If it hears
nothing from you for a full second it shows "OFFLINE" on the operator's screen but
does not stop the match. The only time your counter can stop a match from starting
is when the operator has chosen to let the real count decide the AUTO winner and
your counter isn't talking.

**What you'll get back:** a live scoreboard on the field computer that updates the
instant a ball goes in, a count of balls shot into a HUB while it was dark (which
don't score — the most useful number a driver can see), and a per-match CSV of both
for review afterwards.

---

## Contents

1. [Goal](#1-goal)
2. [What the score drives](#2-what-the-score-drives)
3. [Design decisions](#3-design-decisions)
4. [Wire protocol](#4-wire-protocol)
5. [Timing budget](#5-timing-budget)
6. [bioarena behaviour](#6-bioarena-behaviour)
7. [Configuration](#7-configuration)
8. [Implementation plan](#8-implementation-plan)
9. [Test plan](#9-test-plan)
10. [Open questions](#10-open-questions)
11. [Sources](#11-sources)

---

## 1. Goal

One webcam watching the practice field reports every FUEL scored into either HUB.
bioarena credits each FUEL to the correct scoring shift, decides the AUTO winner from
real scoring rather than an operator's guess, and shows drivers a live tally — all with
the same timing a real field would give them.

Fidelity is the success criterion, as in the
[half-field PRD](prd-half-field-match-simulation.md): what a driver learns on the
practice field must transfer. A score that arrives late enough to change which alliance
gets the AUTO advantage, or a tally that lags the shot by half a second, is a fidelity
error.

This supersedes the "FUEL counting" bullet in the PRD's
[non-goals](prd-half-field-match-simulation.md#3-non-goals). Fouls, ranking points,
match results and rankings remain out of scope.

---

## 2. What the score drives

There is exactly one score-driven **field action** in REBUILT, and it has a hard
deadline. Everything else is display and analytics.

| # | Action | Where it is decided | Deadline | Consequence of lateness |
|---|---|---|---|---|
| 1 | **AUTO winner** → FMS Game Data (`R`/`B`) to driver stations, and which HUB is dark in shifts 1 & 3 vs 2 & 4 | `PausePeriod → TeleopPeriod` transition, **T+23.000 s** on the match clock | Hard. Count in hand at that instant is what is compared. | A FUEL not yet received is credited to the TRANSITION shift instead — still credited, but it did not influence the winner. |
| 2 | Live FUEL tally on the match panel | Every count change | Soft, ≤ 250 ms shot-to-screen | Drivers see a tally that lags their shooting. |
| 3 | Per-shift counted / credited tallies in the match logs | Match end | None | — |

Action 1 is why this feed exists as a real-time channel rather than a post-match CSV.
On a real field the FMS decides the winner about 3 s after AUTO ends, from what the HUB's
sensors have counted by then, and sends the Game Data at the start of teleop
([PRD §4.5](prd-half-field-match-simulation.md#45-fms-game-data)). bioarena mirrors that
exactly: the decision point is the same instant as the teleop enable, and it cannot be
delayed without delaying teleop.

Upstream cheesy-arena does not make the HUB lights or any sound react to individual
FUEL, and neither will bioarena. Per-ball feedback would be a practice-field invention
drivers will not have at an event.

---

## 3. Design decisions

**One source, both hubs, one datagram.** This is upstream's shape, not a departure from
it. Upstream's arena reads scoring from a single PLC in a single call —
`Plc.GetHubCounts() (int, int)`, red and blue together — and then hands each number to
its own `game.Hub`. A webcam that sees both HUBs and reports both counts in one message
is the same source-and-split, with a camera and a UDP socket where upstream has Modbus
registers. Nothing per-hub in upstream is disturbed by it.

**Upstream-shaped, by override.** The substitution of a webcam for the PLC's counters
lives in one new file, `plc/override.go`, on the pattern of `led/override.go`: the
upstream-derived files stay as they are and the practice-field behaviour is additive.
The arena's scoring lines are then upstream's own — `arena.Plc.GetHubCounts()` and the
two `UpdateState` calls — and `game/hub.go` can be taken byte-identical. See
[§6.2](#62-the-override).

**Cumulative counts, not events.** The counter reports "FUEL scored into this hub since
I started", never "one FUEL just scored". A cumulative count is idempotent: a lost
datagram costs nothing because the next one supersedes it, a duplicate changes nothing,
and the receiver needs no event log to recover. It is also exactly what
`Hub.UpdateState(count, ...)` takes.

**Push, not poll.** The counter sends; bioarena listens. The arena loop runs every 10 ms
under a mutex and must never wait on the network. bioarena's e-stop panel client polls
over HTTP from inside that loop with a 200 ms timeout — a pattern that works only because
a panel that fails to answer is *supposed* to stop the field. A scoring feed that could
stall the loop for 200 ms is not acceptable, so it is push into a goroutine that never
holds the arena lock.

**UDP, one JSON object per datagram.** Lowest latency, no connection state to rebuild
after a restart on either side, trivially produced from any language. JSON rather than
a binary layout because the datagram is ~130 bytes at 20 Hz — bandwidth is irrelevant
and readability at commissioning is not.

**Attribution at arrival, on bioarena's clock.** A FUEL is credited to whichever shift
is current when its count increment *reaches* the arena, exactly as upstream credits the
PLC count. The counter's own timestamps are used to *measure* feed latency, never to
re-attribute a FUEL to an earlier shift. This keeps the scoring logic untouched and keeps
the AUTO decision honest: a decision already sent to the driver stations cannot be
revised by a late datagram, so nothing should pretend it can.

**No clock synchronisation required.** The only time the counter reports is a relative
age in milliseconds on its own clock, in the same way the e-stop panel reports
`age_ms`. The Pi does serve NTP on the field ([chrony](chrony-bioarena.conf)) and the
counter is welcome to use it for its own logs, but the protocol does not depend on it.

**Match boundaries are bioarena's problem.** The counter does not know when a match
starts and never needs resetting; it runs continuously across matches. Upstream's PLC
zeroes its counters when the arena calls `ResetMatch`, and the override does the same
thing with the webcam's running totals ([§6.2](#62-the-override)).

---

## 4. Wire protocol

### 4.1 Addressing

| | Value |
|---|---|
| Destination | `10.0.100.5`, the FMS address driver stations are hardcoded to |
| Transport | UDP/IPv4 |
| Port | **8411** (configurable, [§7](#7-configuration)) |
| Counter's network position | **Wired** into the field switch on the management VLAN, static address on `10.0.100.0/24`. Suggested `10.0.100.21`, alongside the e-stop panels at `.11` / `.12`. |

The counter must not use the field access point: its SSIDs are per-team and carry robot
traffic, and each lands on a team VLAN that bioarena will not accept counter datagrams
from ([§6.1](#61-receiver)).

### 4.2 Count datagram (counter → bioarena)

One JSON object, UTF-8, no framing, at most 512 bytes, carrying both hubs.

```json
{"v":1,"session":"c1f3a9d2","seq":4821,"red":57,"blue":0,"age_ms":38}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `v` | integer | yes | Protocol version. Must be `1`. Anything else is dropped. |
| `session` | string, 1–32 chars of `[A-Za-z0-9_-]` | yes | Identifies one run of the counter process. **New on every start** — random hex, or boot time. Lets bioarena tell a restart from a lost packet. |
| `seq` | unsigned integer | yes | Increments by one per datagram within a session. Starts anywhere. Used to drop reordered and duplicate datagrams. |
| `red` | unsigned integer | yes | **Cumulative** FUEL scored into the red HUB since the session began. Never decreases within a session. |
| `blue` | unsigned integer | yes | The same for the blue HUB. On a half field with one HUB, the absent one is always `0`. |
| `age_ms` | unsigned integer | no | Milliseconds between the camera observing the most recent increment to either count and this datagram being sent, on the counter's clock. Read only from datagrams where a count rose; used for the latency figure on the match panel. |
| `info` | string, ≤ 64 chars | no | Free text for commissioning, shown as a tooltip on the match panel. E.g. `"cam 29.8fps conf 0.91"`. |

Unknown fields are ignored, so the counter may add its own for logging.

"FUEL scored" means what the Game Manual means by it. bioarena does not second-guess
the count; deciding whether a ball has scored, and into which HUB, is entirely the
counter's responsibility.

### 4.3 Sending policy

- **On change:** send immediately when either count increments. Do not batch or wait
  for the next heartbeat — this is the datagram that carries the latency budget.
- **Heartbeat:** when unchanged, send every **100 ms**. This is how bioarena knows the
  counter is alive; it has no other liveness signal.
- Send from one socket kept open for the process lifetime, so replies
  ([§4.4](#44-status-datagram-bioarena--counter)) reach it.

### 4.4 Status datagram (bioarena → counter)

bioarena replies to the source address and port of **every datagram it accepts** with
one JSON object:

```json
{"v":1,"seq":4821,"match_state":"TELEOP_PERIOD","match_time_s":47.31,"shift":"SHIFT2",
 "hub_active":{"red":true,"blue":false},
 "match_count":{"red":60,"blue":0},"credited":{"red":57,"blue":0},"auto_count":{"red":12,"blue":0}}
```

| Field | Meaning |
|---|---|
| `seq` | Echo of the accepted datagram's `seq`, so the counter can measure round-trip time. |
| `match_state` | `PRE_MATCH`, `START_MATCH`, `WARMUP_PERIOD`, `AUTO_PERIOD`, `PAUSE_PERIOD`, `TELEOP_PERIOD`, `POST_MATCH`. |
| `match_time_s` | Seconds since the match clock started, or `0` outside a match. |
| `shift` | `AUTO`, `TRANSITION`, `SHIFT1`–`SHIFT4`, `ENDGAME`, `POST_MATCH`, or `NONE`. |
| `hub_active` | Per hub, whether it is currently active — FUEL scored now will be credited. |
| `match_count` | Per hub, FUEL bioarena has counted this match, active or not. |
| `credited` | Per hub, FUEL scored while that hub was active — the score. |
| `auto_count` | Per hub, FUEL credited to the AUTO shift. |

The reply exists so the counter can show its operator an "FMS link" indicator and so a
video overlay or per-match log can be built without touching bioarena's authenticated
web interface. **The counter must not depend on it**: bioarena may be down, restarting,
or configured for a different port, and the counter keeps sending regardless.

### 4.5 Sessions and restarts

A datagram is **accepted** when all of the following hold; otherwise it is dropped and
the reason is logged (rate-limited to once a minute per source):

1. Source address is the configured counter address ([§7](#7-configuration)).
2. `v == 1` and the object parses with all required fields.
3. Either `session` differs from the last accepted one (a restart), or `seq` is greater
   than the last accepted `seq` (not a duplicate or reordering).
4. Within a session, neither `red` nor `blue` is lower than its last accepted value.

On a **new session** bioarena logs `hub counter restarted`, carries forward what the
previous session had contributed to the current match on each hub, and treats the new
session's counts as starting from zero ([§6.2](#62-the-override)). The counter may
therefore be restarted mid-match and lose nothing but the FUEL scored while it was down.

### 4.6 Reference sender

The complete protocol from the counter's side. `fuel_seen()` stands for the vision
pipeline; it returns `"red"`, `"blue"`, or `None` per iteration.

```python
import json, random, socket, time

FMS = ("10.0.100.5", 8411)
session = f"{random.getrandbits(32):08x}"
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.setblocking(False)
seq, counts, last_sent, last_increment = 0, {"red": 0, "blue": 0}, 0.0, None

def send():
    global seq, last_sent
    seq += 1
    msg = {"v": 1, "session": session, "seq": seq, **counts}
    if last_increment is not None:
        msg["age_ms"] = int((time.monotonic() - last_increment) * 1000)
    sock.sendto(json.dumps(msg, separators=(",", ":")).encode(), FMS)
    last_sent = time.monotonic()

while True:
    hub = fuel_seen()                     # your vision pipeline here
    if hub:
        counts[hub] += 1
        last_increment = capture_time_of_confirming_frame()   # see section 5.2
        send()                            # on change: immediately
    elif time.monotonic() - last_sent >= 0.1:
        send()                            # heartbeat: 10 Hz
    try:
        reply, _ = sock.recvfrom(1024)    # optional: show "FMS link OK"
    except BlockingIOError:
        pass
```

`docs/hub_counter_sim.py` ([§9](#9-test-plan)) is this loop with a keyboard in place of
the camera — `r` and `b` score a FUEL — plus knobs for injected latency and packet loss.

---

## 5. Timing budget

### 5.1 Derivation

The match clock starts at T=0 when AUTO begins. AUTO ends at T+20.000 s; the 3 s pause
follows; teleop — and the AUTO winner decision — happen in the first arena loop
iteration at or after **T+23.000 s**. Upstream credits FUEL to the AUTO shift for a
3 s grace period after AUTO ends (`ScoringGracePeriodSec`), which is the whole pause:
anything received before the decision counts for AUTO.

So the feed's end-to-end latency *L* — ball through the scoring plane to datagram at the
Pi — shortens the effective AUTO grace window from 3000 ms to 3000 − *L* ms. A real
field has the same structure with a hardwired PLC counter, where *L* is a few tens of
milliseconds. The fidelity error is the difference.

### 5.2 Budget

| Stage | Owner | Typical | Worst (p99) |
|---|---|---|---|
| Camera observes FUEL → count increments | counter | ≤ 80 ms | ≤ 200 ms |
| Serialise and send (on-change path, no batching) | counter | ≤ 5 ms | ≤ 10 ms |
| Wired management VLAN, one switch hop | field | < 1 ms | < 1 ms |
| Receive, validate, publish to the arena | bioarena | < 1 ms | < 1 ms |
| Arena loop picks it up | bioarena | ≤ 10 ms | ≤ 10 ms |
| **Camera → credited** | | **≤ 100 ms** | **≤ 250 ms** |
| Credited → match panel (websocket, browser paint on LAN) | bioarena | + ≤ 50 ms | + ≤ 100 ms |
| **Camera → screen** | | **≤ 150 ms** | **≤ 350 ms** |

bioarena's contribution is bounded at 11 ms plus the display path; the budget is
almost entirely the counter's, and the counter is a camera, so the first row is where
the work is. At 30 fps it is roughly half a frame period of waiting, capture, and
inference — which fits inside 80 ms on a Jetson or a Pi 5 with a light model, and does
not fit if frames are queued. Two things follow for the vision pipeline:

- **Count at the plane, not at the end of a track.** A FUEL should increment the count
  on the first frame that confirms it crossed the scoring plane, not once its track is
  closed some frames later. A tracker that needs *N* frames of confirmation adds *N* frame
  periods to *L*.
- **Measure `age_ms` from the camera timestamp.** The capture time of the frame that
  confirmed the FUEL, not the moment inference finished, is when the ball was observed.
  Reporting from capture makes the latency figure on the match panel honest about
  pipeline depth, which is the number that needs tuning.

A 60 fps camera halves the waiting term for free and is worth having if the inference
budget allows it. One camera covering both HUBs shares its frame budget between them;
that is fine as long as it is one detector pass per frame, not one per hub.

At p99 the driver loses 8 % of the grace window relative to a real field. At the typical
figure, 3 %. Both are below what a human can perceive at the shot; the number that
matters is that a FUEL clearly scored before the AUTO buzzer is always credited to AUTO,
which a 250 ms bound guarantees with 2.75 s to spare.

### 5.3 Liveness

| Condition | Threshold | bioarena's response |
|---|---|---|
| No datagram accepted | **1000 ms** (ten missed heartbeats) | Counter marked **offline**. Match panel badge; log line. Counts already received stand. |
| Datagram accepted after offline | — | Counter marked online; log line. |
| `age_ms` on an on-change datagram | > 250 ms | Latency figure on the match panel turns amber. Informational. |

Offline is a scoring condition, not a safety one. It blocks a match from *starting* in
Counted mode ([§6.4](#64-auto-winner-mode-counted)) and never aborts one.

---

## 6. bioarena behaviour

### 6.1 Receiver

`hardware/hub_counter.go` — a `HubCounterReceiver` listening on the configured UDP port
in its own goroutine, following `listenForDsUdpPackets` in
`field/driver_station_connection.go`. For each datagram it applies the acceptance rules
of [§4.5](#45-sessions-and-restarts), updates one record held behind an
`atomic.Pointer`, and sends the status reply. It never takes the arena lock; the reply's
match context comes from a snapshot the arena loop publishes each tick.

The record is the last accepted datagram plus receipt time:

```go
type hubCounterRecord struct {
    Session    string
    Seq        uint64
    Red, Blue  int       // cumulative within Session
    ReceivedAt time.Time
    AgeMs      int       // from the last on-change datagram
    Info       string
}
```

Datagrams from any source other than the configured address are dropped before
parsing. In practice this excludes every team VLAN, so a robot cannot post scores; the
motive is a stray second sender rather than an adversary, but it costs nothing.

### 6.2 The override

Upstream's arena gets its scoring in two lines of `handlePlcInputOutput`:

```go
redHubCount, blueHubCount := arena.Plc.GetHubCounts()
arena.RedRealtimeScore.CurrentScore.Hub.UpdateState(redHubCount, matchStartTime, currentTime)
arena.BlueRealtimeScore.CurrentScore.Hub.UpdateState(blueHubCount, matchStartTime, currentTime)
```

bioarena has no PLC — `arena.Plc` is a `FakePlc` on every practice field — but it keeps
the shape. `plc/override.go`, a new file alongside the upstream-derived `plc.go`:

```go
// HubCountSource is anything that can stand in for the PLC's Hub counters.
type HubCountSource interface {
    GetHubCounts() (red, blue int) // match-relative, like the PLC's registers after ResetMatch
    ResetMatch()                   // zero the match-relative counts
}

// WithHubCounts returns a Plc that behaves as base in every respect except that
// GetHubCounts is answered by src, and ResetMatch resets both.
func WithHubCounts(base Plc, src HubCountSource) Plc
```

The receiver in [§6.1](#61-receiver) implements `HubCountSource`. The arena constructs
`plc.WithHubCounts(new(plc.FakePlc), receiver)` when a counter is configured and its
scoring lines read exactly as upstream's, with `arena.RedHub` and `arena.BlueHub` in
place of the realtime-score fields bioarena does not have.

Two consequences of routing through `ResetMatch`, which the arena already calls at
`LoadMatch` and again at `StartMatch`:

- **Baseline** is taken by the receiver, not the arena. `ResetMatch` snapshots the
  current session and both counts; `GetHubCounts` returns `carried + (current −
  baseline)` per hub, where `carried` accumulates on a session change mid-match. The
  arena never sees a session, only match-relative totals — the same thing upstream's PLC
  gives it.
- **No `Plc` interface change beyond upstream's own.** `GetHubCounts() (int, int)` is
  restored to `plc.Plc` and `ModbusPlc` from upstream, along with the
  `redHubTotal`/`blueHubTotal` registers `ModbusPlc` reads; bioarena's own `FakePlc`
  answers `0, 0`. That is a step toward upstream, not away from it; the only thing
  bioarena adds is the override file. Upstream's `ResetMatch` pulses the PLC's
  `matchReset` coil and zeroes its register mirror, so "match-relative after
  `ResetMatch`" is what the PLC's counts already are.

The one place the arena diverges: `handlePlcInputOutput` returns early when the PLC is
not enabled, and on a practice field it never is. The two scoring lines therefore run
from `Update()` directly, in a small `updateHubCounts()`, rather than inside
`handlePlcInputOutput`. `field/arena.go` is forked throughout; this is one more line in
`docs/upstream-divergences.md`.

### 6.3 Shift attribution

`game/hub.go` is restored to upstream **byte-identical**, bringing back `ShiftCounts`,
`UpdateState`, `GetShiftCount`, `GetTeleopActiveFuelCount`, `getCurrentShift` and the
grace-period logic. bioarena's exported `IsShiftActive` moves to a one-line wrapper in a
new `game/hub_bioarena.go`; `ScoringGracePeriodSec = 3` is added to
`game/match_timing.go`. The port note at the top of the current file, explaining what
was omitted and why, is deleted along with the omission.

The arena holds a persistent `game.Hub` per alliance for the duration of a match instead
of constructing them on the fly in `hubs()`, zeroed at `LoadMatch` where upstream zeroes
its realtime scores.

Two numbers fall out per hub for free, because upstream records FUEL under every shift
and filters at read time:

- **counted** — every FUEL the camera saw enter this hub this match (`activeOnly = false`)
- **credited** — those scored while the hub was active (`activeOnly = true`); the score

The difference is FUEL shot into a dark hub. That is the single most useful number a
practice field can put in front of a driver, and it costs nothing.

### 6.4 AUTO winner mode "Counted"

A fourth `AutoWinnerMode`, wire name `counted`, after `random`, `red`, `blue`.

The mode moves from arena state into `EventSettings` so that it survives a restart: a
field that runs Counted runs it every day, and having it silently fall back to Random on
a power cycle would be a fidelity error nobody would notice until the wrong HUB went
dark. The select on the match panel stays where it is — it is the control the operator
reaches for — and writes through to the setting.

The decision is made at the transition into teleop **for every mode** — bioarena's
`handleAutoWinner`, mirroring upstream's — and `AutoWinner` is `AllianceNone` until
then. This already holds for the forced and random modes (it was moved from the start of
AUTO in September 2026; nothing observes the winner earlier, since both HUBs are active
throughout AUTO and the pause), so Counted mode is one more case in `assignAutoWinner`
rather than a second code path.

In Counted mode the rule is upstream's `handleAutoWinner`, verbatim:

```
redAuto  = RedHub.GetShiftCount(ShiftAuto, true)
blueAuto = BlueHub.GetShiftCount(ShiftAuto, true)
redWon   = redAuto == blueAuto ? coin flip : redAuto > blueAuto
```

On a half field with one HUB the counter reports the other as `0` throughout, so any
FUEL in AUTO wins the advantage and none at all is a coin flip — the real field's
tie-break ([PRD §4.4](prd-half-field-match-simulation.md#44-ties)) applied to an
opponent who scored nothing. There is deliberately no invented opponent score to beat:
the practice field reports what happened, and a coach who wants the driver to lose AUTO
on purpose has the `red`/`blue` modes for that.

A counter that is offline at the decision contributes its last received counts, frozen.
This matches what upstream would do with a dead PLC register and is the only answer that
does not invent FUEL.

**Readiness.** `checkCanStartMatch` gains, in Counted mode only:

- a counter configured, else `no Hub FUEL counter configured`;
- the counter online, else `Hub FUEL counter offline`.

These block the start button exactly as an unlinked driver station does. They are not
checked in the other three modes, where the counter is display-only.

`SetAutoWinnerMode` continues to refuse changes mid-match.

### 6.5 Failure behaviour

| Failure | Effect on scoring | Effect on the match | Shown as |
|---|---|---|---|
| Counter never sends | Both hubs score `0` | Blocks start in Counted mode only | `NOT CONFIGURED` / `OFFLINE` badge |
| Counter stops mid-match | Counts freeze | None | `OFFLINE` badge, log |
| Counter restarts mid-match | FUEL during the outage lost; nothing else | None | log `hub counter restarted` |
| Lost datagrams | None (next one supersedes) | None | — |
| Reordered / duplicate | Dropped | None | — |
| A count decreases within a session | Dropped, logged | None | Badge tooltip `count went backwards` |
| FUEL credited to an alliance with no team registered | Counted and credited as reported | None | Warning on that hub's strip: `FUEL scored for RED but no red team is registered` — the driver station is plugged into the wrong side, or the camera has the hubs swapped |
| Datagram from unknown source | Dropped before parsing, logged once/min | None | — |
| Malformed JSON or `v ≠ 1` | Dropped, logged once/min | None | — |
| bioarena restarts mid-match | Match is lost anyway; counter keeps sending and is picked up on the next match | — | — |

### 6.6 Operator UI

**Match panel** (`templates/match_play.html`): one counter status line, and one strip
per alliance beneath its station cards.

Status line:

- dot and text: `ONLINE`, `OFFLINE`, `NOT CONFIGURED`; hover shows `info`, session,
  and the last drop reason;
- feed latency, from `age_ms`, amber above 250 ms.

Per-alliance strip:

- the credited count, large;
- `counted N · M into dark hub` beneath it, small;
- the wrong-side warning from [§6.5](#65-failure-behaviour) when it applies.

The AUTO winner select gains **Counted** as its last option, and the selection persists
([§6.4](#64-auto-winner-mode-counted)).

**Settings** (`templates/setup_settings.html`), a **Hub FUEL Counter** section under
Field hardware: UDP port and counter address. Blank address means no counter.

### 6.7 Notifiers

A new `HubScoreNotifier` (`hubScore`), fired from `Update()` whenever a counted,
credited, online or latency value changes since the last tick. Scores must not ride on
`arenaStatus`: that broadcasts every 500 ms and would put up to half a second between a
shot and the tally, blowing the [§5.2](#52-budget) display budget on its own. Firing on
change costs nothing between shots and at most a few messages a second during them.

Message shape:

```json
{"Counter": {"Configured":true,"Online":true,"LatencyMs":38,"Info":"cam 29.8fps"},
 "Red":  {"Counted":60,"Credited":57,"Auto":12,"ByShift":[12,8,0,15,0,14,8,0],"NoTeam":false},
 "Blue": {"Counted":0,"Credited":0,"Auto":0,"ByShift":[0,0,0,0,0,0,0,0],"NoTeam":false}}
```

`arenaStatus` gains only the counter's `Configured`/`Online` flags, which
`CanStartMatch` already reflects.

### 6.8 Match logs

At match close, `logs/<timestamp>_<match>_fuel.csv` with one row per hub per shift:
`hub, shift, counted, credited`, plus a `TOTAL` row and the AUTO decision (`red 12 vs
blue 0 → R`). The file sits beside the team match logs and is served from `/logs/` like
them.

---

## 7. Configuration

`model.EventSettings`, persisted in the database and edited from the Settings page, in
the style of the Hub LED fields:

| Field | Default | Meaning |
|---|---|---|
| `HubCounterPort` | `8411` | UDP listen port. `0` disables the receiver entirely. |
| `HubCounterAddress` | `""` | IPv4 address of the counter. Blank = no counter; the arena keeps its plain `FakePlc` and both hubs read `0`. |
| `AutoWinnerMode` | `random` | `random`, `red`, `blue`, or `counted`. Moves here from arena state so it survives a restart; the match panel select writes through to it ([§6.4](#64-auto-winner-mode-counted)). |

Nothing goes in `config.yaml`: these are field-hardware settings that must survive a
power cycle and be changeable from a browser, which is what the database is for.

---

## 8. Implementation plan

Each phase is independently mergeable and leaves the field working.

### Phase 1 — Protocol and simulator

- `docs/hub_counter_sim.py`: the reference sender with a keyboard for a camera.
  `--target`, `--port`, `--rate`, `--latency-ms`, `--loss`, and an auto-scoring mode
  for soak tests. Prints replies. Stdlib only, runs on the collaborator's laptop with no
  bioarena checkout.
- Hand this document and the simulator to the collaborator. They can build against the
  simulator's behaviour while Phase 2 lands.

### Phase 2 — Receiver, override and display

- `plc/plc.go`: `GetHubCounts` and the Hub registers restored from upstream verbatim;
  `plc/fake_plc.go` (fork-local) answers `0, 0`.
- `plc/override.go`, `plc/override_test.go`: `HubCountSource`, `WithHubCounts`.
- `hardware/hub_counter.go`, `hardware/hub_counter_test.go`: receiver, acceptance
  rules, session accounting behind `ResetMatch`, status reply.
- `game/hub.go` restored byte-identical; `game/hub_bioarena.go`; `ScoringGracePeriodSec`.
  Take upstream's `game/hub_test.go` too.
- `field/arena.go`: persistent hubs, `updateHubCounts` in `Update`, `HubScoreNotifier`,
  the override wired in from `LoadSettings` when an address is configured.
- Settings fields and section; match panel status line and strips; `hubScore` handling
  in `static/js/match_play.js`.
- `docs/upstream-divergences.md`: `game/hub.go` moves to the byte-identical list;
  `plc/override.go` and `updateHubCounts` get rows in the fork-local table.

At the end of Phase 2 the tally is live and the AUTO winner is still operator-chosen.

### Phase 3 — Counted mode

- `AutoWinnerMode` moved into `EventSettings`, the match panel select writing through.
- `AutoWinnerCounted` as a case in `assignAutoWinner`; readiness checks; UI.

### Phase 4 — Logs

- `_fuel.csv` at match close.

### Phase 5 — Commissioning

- [§9 field acceptance](#field-acceptance) with the real camera on the real field.

---

## 9. Test plan

### Unit

- Receiver: each acceptance rule in [§4.5](#45-sessions-and-restarts) with a
  hand-built datagram — wrong source, wrong `v`, missing field, oversize, seq regress,
  either count regressing, new session. Reply content and addressing.
- Override: `WithHubCounts` delegates everything but `GetHubCounts` to the base;
  `ResetMatch` reaches both; a nil source reads `0, 0`.
- Session accounting: baseline at `ResetMatch`; increments on each hub independently;
  restart mid-match carries the earlier contribution per hub; restart before
  `ResetMatch` does not.
- Attribution: a count increment at each shift boundary ±1 ms lands in the right shift,
  including the 3 s grace on active shifts and its absence on dark ones — upstream's
  `hub_test.go` covers this once the file is byte-identical.
- Counted mode: red > blue, blue > red, tie → both outcomes reachable, one hub
  scoring nothing throughout, offline counter frozen at last counts. Game Data matches
  the decision. `AutoWinner` is `AllianceNone` until the transition.
- Persistence: the mode survives `LoadSettings`; `counted` round-trips through the
  settings form; a database predating the field reads back as `random`.
- Readiness: both Counted-mode checks block start, neither applies in the other modes.
- Wrong-side warning: FUEL credited to an alliance with no team registered sets
  `NoTeam` on that hub and nowhere else.
- Notifier: fires on a count change, on online/offline, not on an unchanged heartbeat.
- The existing `TestConcurrentOperatorsDoNotDeadlockOrPanic` extended with a goroutine
  hammering the receiver's atomic record.

### Integration (simulator against a running bioarena)

- Scoreboard latency: `--latency-ms 0`; film the terminal and the match panel together
  at 60 fps; expect ≤ 9 frames from keypress to tally.
- Injected latency: `--latency-ms 300`; match panel reports ~300 ms and turns amber.
- Loss: `--loss 0.3`; both tallies still correct, no badge flicker.
- Heartbeat loss: kill the simulator; `OFFLINE` within ~1 s; restart; `ONLINE`,
  `restarted` logged, both tallies continue.
- AUTO decision: score in the last 200 ms of the pause; confirm which side of T+23.000
  it fell and that the Game Data the driver station received agrees.
- Source filtering: a second simulator from another host is ignored and logged once.

### Field acceptance

- Real camera on the real field, real robot shooting. Twenty FUEL by hand-count against
  each tally; zero disagreement is the bar, since the counter's own accuracy — including
  which HUB it attributes a ball to — is what is being accepted.
- A full match in Counted mode: driver scores in AUTO and gets their alliance's letter
  and the advantage; repeat scoring nothing in AUTO, and over several matches both
  letters turn up.
- Driver station deliberately on the wrong side: the warning appears on the first FUEL.
- `_fuel.csv` after the match matches the match panel's final figures.
- One full day of practice with the counter left running across matches, restarts of
  bioarena, and a deliberate counter restart mid-match.

---

## 10. Open questions

1. **Does the counter want match context beyond the reply?** If it needs to know a
   match started *before* the first FUEL — to arm itself, start recording — bioarena
   could send an unsolicited status datagram on every state change to the configured
   address. Not in the plan until asked for.

### Decided

Recorded so the collaborator can see what was considered and where it landed.

- **One webcam, both hubs, one datagram.** An earlier draft had one sender per hub with
  a `hub` field. Replaced: a single camera sees the whole field, and one datagram
  carrying `red` and `blue` is what upstream's `GetHubCounts` already returns. The
  per-hub `MISCONFIGURED` state went with it; the camera's attribution of a ball to a
  hub is its own responsibility, and the wrong-side warning in [§6.5](#65-failure-behaviour)
  catches the field-side mistake it was guarding against.
- **Byte-identity is kept by override, not by contortion.** `game/hub.go` is taken
  verbatim, `plc.Plc` regains upstream's `GetHubCounts`, and the webcam substitution is
  one additive file, `plc/override.go` — the same arrangement that keeps `led/`
  identical.
- **The sensor is a camera.** The 80 ms sensing budget is therefore the hard part of
  [§5.2](#52-budget), and the vision-specific guidance there is what commissioning will
  be tuning against. The protocol is unchanged.
- **Counted mode persists.** `AutoWinnerMode` moves into `EventSettings` as the fourth
  option after `random`, `red`, `blue` ([§6.4](#64-auto-winner-mode-counted), [§7](#7-configuration)).
- **No invented opponent score.** An earlier draft had a configurable "phantom" AUTO
  count for a hub with no counter. Dropped: a HUB the camera never sees a ball enter
  scores nothing, and the real tie-break does the rest. Deliberately losing AUTO is what
  the `red`/`blue` modes are for.
- **No free-practice tally.** Free Practice was removed from bioarena in September 2026;
  Match Play is the only operating page, and open driving is a long test match.

---

## 11. Sources

- Upstream cheesy-arena `game/hub.go`, `field/arena.go` (`handleAutoWinner`,
  `handlePlcInputOutput`), `plc/plc.go` (`GetHubCounts`, `ResetMatch`) — the
  semantics being mirrored.
- `led/override.go` — the additive-override arrangement `plc/override.go` copies.
- [PRD: Half-Field Match Simulation](prd-half-field-match-simulation.md) §4.4, §4.5 —
  tie-break and Game Data timing, verified against the Game Manual.
- `hardware/network_estop_panel.go`, `hardware/interfaces.go` — the existing external
  input conventions (`age_ms`, fail-closed readiness) this follows, and the polling
  pattern it deliberately does not.
- `field/driver_station_connection.go` `listenForDsUdpPackets` — the UDP listener
  pattern.
- [docs/chrony-bioarena.conf](chrony-bioarena.conf) — field time source, available to
  the counter but not required.
