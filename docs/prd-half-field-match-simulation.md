# PRD: Half-Field Match Simulation (REBUILT 2026)

**Status:** Draft
**Date:** 2026-07-25
**Owner:** Team 841

Enable bioarena to run realistic 1v0 half-field practice matches for the 2026 REBUILT
game, with operator-selectable AUTO outcome, correct FMS Game Data delivery to the
driver station, and a DMX-controlled HUB light that mirrors real-field behaviour.

---

## Contents

1. [Goal](#1-goal)
2. [Background](#2-background)
3. [Non-Goals](#3-non-goals)
4. [Verified Game Reference](#4-verified-game-reference)
5. [Requirements](#5-requirements)
6. [Gap Analysis](#6-gap-analysis)
7. [Implementation Plan](#7-implementation-plan)
8. [Open Questions](#8-open-questions)
9. [Test Plan](#9-test-plan)
10. [Sources](#10-sources)

---

## 1. Goal

A driver on a half field with one robot should be able to practice the REBUILT
teleop shift cycle exactly as it behaves in a real match:

- Run a 1v0 match without hand-bypassing five stations every time.
- Choose whether the practice alliance **won** AUTO, **lost** AUTO, or let the
  system pick **randomly** — because that determines which shifts their HUB is dark.
- Receive the same FMS Game Data string their robot code will see at a real event.
- See a physical HUB light change state at the correct shift boundaries.

The success criterion is **fidelity**: robot code and driver muscle memory developed
on the practice field must transfer to a real field with no surprises.

---

## 2. Background

### Relationship to upstream

bioarena is a fork of [Team254/cheesy-arena](https://github.com/Team254/cheesy-arena),
diverged into a practice-field controller. The upstream `game/` package retains
scoring, fouls, rankings, and HUB scoring logic; bioarena has deliberately stripped
these, keeping only `match_sounds.go`, `match_status.go`, `match_timing.go`, and
`test_helpers.go`.

**We are not merging upstream's `game/` package.** Those files implement scoring and
ranking-point calculation, which a 1v0 practice simulator does not use, and pulling
them in would drag the match-results and rankings data model along with them and
conflict heavily in `field/` and `web/`. What we take from upstream is *behavioural
specification*, not code.

At time of writing upstream has only two open PRs, neither game-related
(#302 field-signal preservation, #297 a dependabot bump). There is nothing to
cherry-pick.

### What already exists in bioarena

The REBUILT shift state machine is **already implemented and tested**:

- `hardware/interfaces.go` — `MatchPhase`, `TeleopSubPhase`, `Alliance`,
  `LightingState`, and the `FieldLights` driver interface.
- `field/arena.go:1155` — `teleopSubPhase()` maps remaining teleop seconds to a shift.
- `field/arena.go:1172` — `shiftWarning()` flags the 3 s pre-boundary window.
- `field/arena.go:569` — fires `FieldLights.SetState()` on every state change.
- `field/arena_hardware_test.go` — boundary tests for all six sub-phases.

Shift boundaries were verified against upstream and the game manual and are
**correct** (see [§4](#4-verified-game-reference)). This PRD builds on that
foundation rather than replacing it.

---

## 3. Non-Goals

- Match scoring, FUEL counting, fouls, ranking points, or match results.
- Merging upstream's `game/` package (`score.go`, `hub.go`, `foul.go`,
  `ranking_fields.go`, `score_summary.go`).
- Full 3v3 event operation — bioarena remains a practice-field controller.
- Referee panels, audience display scoring overlays, or event rankings.

---

## 4. Verified Game Reference

All values below were verified against `Rules/2026GameManual.pdf` (Version TU13) and
cross-checked against upstream `cheesy-arena`. Treat this section as the
specification of record.

### 4.1 Match timing

| Period | Duration | Source |
|---|---|---|
| AUTO | 20 s | `MatchTiming.AutoDurationSec` |
| Pause | 3 s | `MatchTiming.PauseDurationSec` |
| Teleop | 140 s | `MatchTiming.TeleopDurationSec` |

Teleop decomposes as `TransitionShift(10) + 4 × Shift(25) + EndGame(30) = 140`.

### 4.2 Shift boundaries

Expressed as **seconds remaining in teleop**, matching `teleopSubPhase()`:

| Remaining | Sub-phase | Window |
|---|---|---|
| > 130 | `SubPhaseTransition` | T-2:20 → 2:10 |
| > 105 | `SubPhaseShift1` | T-2:10 → 1:45 |
| > 80 | `SubPhaseShift2` | T-1:45 → 1:20 |
| > 55 | `SubPhaseShift3` | T-1:20 → 0:55 |
| > 30 | `SubPhaseShift4` | T-0:55 → 0:30 |
| ≤ 30 | `SubPhaseEndGame` | T-0:30 → 0:00 |

### 4.3 HUB active state (Manual Table 6-3)

The alliance that scores **more FUEL during AUTO** has their HUB set **inactive for
SHIFT 1**. Status alternates each subsequent shift. Both HUBs are active during AUTO,
the TRANSITION SHIFT, and END GAME.

| Timeframe | AUTO winner's HUB | AUTO loser's HUB |
|---|---|---|
| AUTO | Active | Active |
| Transition Shift | Active | Active |
| Shift 1 | **Inactive** | Active |
| Shift 2 | Active | **Inactive** |
| Shift 3 | **Inactive** | Active |
| Shift 4 | Active | **Inactive** |
| End Game | Active | Active |

This matches upstream `hub.go`'s `isShiftActive()`: Shift 1 and 3 are active only
when `!WonAuto`; Shift 2 and 4 are active only when `WonAuto`.

> **Note on practice value:** under this rule a HUB is dark for 50 s of a 140 s
> teleop regardless of the AUTO outcome. Choosing win vs. lose changes *when* the
> dead time lands, not how much there is.

### 4.4 Ties

> If both ALLIANCES score the same number of FUEL during AUTO, the FMS will randomly
> select an ALLIANCE and use its HUB status order.
> — Manual §6, p.44

The FMS resolves ties to a concrete alliance. `hardware.AllianceNone` must therefore
**never** be transmitted as Game Data or reach the lighting driver.

### 4.5 FMS Game Data

> FMS Game Data relays the ALLIANCE who scored more FUEL during AUTO, or the ALLIANCE
> selected by FMS, to all OPERATOR CONSOLES simultaneously at the start of TELEOP.
> — Manual §6, p.44

Per the WPILib 2026 Game Data reference:

- The value is a **single character**: `R` or `B`, identifying the alliance whose HUB
  goes **inactive first**.
- It is transmitted after AUTO FUEL scoring is assessed — **approximately 3 seconds
  after the end of AUTO**, which coincides with bioarena's 3 s pause and therefore
  with the start of teleop.
- **Before that point the Game Data is an empty string.**

Rule reference: a missing or delayed Game Data packet is *not* an ARENA FAULT;
**incorrect** Game Data *is* one (Manual §10, p.119). Failing closed — sending
nothing — is the safe direction.

### 4.6 Driver-station wire format

Two delivery paths, selected by DS protocol version:

| DS generation | Handshake signature | Team ID encoding | Game Data path |
|---|---|---|---|
| New | `packet[1] >= 5 && packet[2] == 30` | **ASCII string** at byte 7, length at byte 6 | UDP control packet, bytes 22+, tag `32` |
| Legacy | `packet[1] == 3 && packet[2] == 24` | Big-endian uint16 at bytes 3–4 | TCP packet type `28` |

The team-ID encoding differs between generations and is easy to miss: the legacy
protocol packs it as a two-byte integer, the new protocol sends it as decimal ASCII
whose length is given by the preceding byte. Upstream reads the handshake into a
1500-byte buffer to accommodate this; bioarena currently reads a fixed `[5]byte`.

New-DS UDP layout, appended to the control packet (upstream `encodeControlPacket`):

```
packet[22] = byte(gameDataLen) + 1
packet[23] = 32                     // game data tag
packet[24+i] = gameData[i]          // gameDataLen bytes, max 8
packetLength += 2 + gameDataLen
```

Shift and sub-phase information is **not** part of the DS protocol. The driver's only
in-match channel for shift state is the physical HUB light — which is why
[R4](#r4-dmx-hub-light) matters for fidelity.

---

## 5. Requirements

### R1 — Half-field 1v0 mode

A named mode that configures the field for a single practice robot, rather than
requiring the operator to manually bypass five stations.

- Operator selects the live station (or at minimum the live alliance).
- All other stations are auto-bypassed so `checkAllianceStationsReady`
  (`field/arena.go:758`) passes.
- The mode is visible in the match-play UI so the operator knows which alliance is
  live.

**Rationale:** 1v0 already works today via per-station `Bypass`, but it is convention,
not a mode. Naming it removes six manual steps per practice session and removes the
ambiguity about which alliance the AUTO-outcome selector refers to.

### R2 — Selectable AUTO outcome

Operator chooses, before the match, one of:

| Mode | Behaviour |
|---|---|
| **Win** | Practice alliance is the AUTO winner → their HUB dark in Shifts 1 and 3 |
| **Lose** | Opposing alliance is the AUTO winner → practice HUB dark in Shifts 2 and 4 |
| **Random** | Resolved to a concrete alliance at match start |

- `assignAutoWinner()` (`field/arena.go:1112`) currently randomises unconditionally.
  It must respect the selected mode.
- Random **must** resolve to `AllianceRed` or `AllianceBlue`, never `AllianceNone`
  (see [§4.4](#44-ties)).
- The decision may be made at AUTO start, but must not be *transmitted* before
  teleop (see R3).

### R3 — FMS Game Data to driver stations

bioarena must transmit Game Data with real-field timing and format.

- Game Data is the **empty string** from match start until the end of the pause
  period.
- At the teleop transition, it becomes `R` or `B` per [§4.5](#45-fms-game-data).
- Delivered via the correct path for the connected DS generation
  ([§4.6](#46-driver-station-wire-format)).
- Resent only on change (upstream tracks this with a `SentGameData` field).

**Decision (Q1, resolved):** bioarena will support **both** DS generations. Current
driver stations are in use, so the tag-30 handshake, ASCII team-ID parsing, and the
UDP game-data path are all in scope for this work — not a follow-up. The legacy
tag-24 path and its TCP type-28 packet are retained for older stations.

**Rationale:** this is the single highest-fidelity item in the PRD. Robot code calling
`DriverStation.getGameSpecificMessage()` is the mechanism teams use to know which
shifts their HUB is live. If bioarena sends it early, or not at all, code that works
on the practice field will behave differently at an event.

### R4 — DMX HUB light

A new `hardware.FieldLights` implementation driving the DMX HUB light.

**Decision (Q2, resolved):** the HUB light is **DMX over a USB interface**, not
Art-Net. This mirrors the existing `hardware/serial_lights.go` pattern and reuses the
`go.bug.st/serial` dependency already in `go.mod`. See [Q2a](#8-open-questions) for the
one remaining hardware detail.

- Implements the existing `SetState(LightingState) error` interface — no interface
  change required.
- Registered as a new `case` in `buildFieldLights()` (`main.go:82`), alongside the
  existing `none` and `serial` drivers.
- Renders HUB active/inactive per [§4.3](#43-hub-active-state-manual-table-6-3),
  deriving the practice alliance's state from `LightingState.AutoWinner`.
- Renders the `ShiftWarning` pre-boundary window.
- At teleop start, indicates which HUB goes inactive in Shift 1 — the manual
  specifies the real HUB shows this alongside the Game Data transmission.
- Must degrade safely: a DMX failure logs and continues; it must never block the
  match loop.

**Note:** the alternation rule currently lives only in comments and in upstream's
`isShiftActive()`. It should be implemented once, in a tested helper, rather than
being reimplemented inside each lighting driver.

### R5 — Timing fidelity

No change to existing period timing — [§4.1](#41-match-timing) and
[§4.2](#42-shift-boundaries) are already correct and covered by
`field/arena_hardware_test.go`. This requirement exists to pin them: any change to
`MatchTiming` or `teleopSubPhase()` must be re-verified against the manual.

---

## 6. Gap Analysis

What exists versus what this PRD requires:

| Component | State | Requirement |
|---|---|---|
| Shift state machine | ✅ Implemented, tested | R5 (pin only) |
| `FieldLights` interface | ✅ Correct shape | R4 (new impl) |
| `LightingState.AutoWinner` | ✅ Plumbed to driver | — |
| 1v0 via `Bypass` | ⚠️ Works, unnamed | R1 |
| `assignAutoWinner()` | ⚠️ Random only | R2 |
| HUB alternation rule | ❌ Comments only, not code | R4 |
| `sendGameDataPacket` | ⚠️ Defined, **never called** | R3 |
| `checkGameData()` | ❌ Absent | R3 |
| `AllianceStation.GameData` | ❌ No field (`arena.go:82`) | R3 |
| `SentGameData` dedupe | ❌ Absent | R3 |
| New-DS handshake (tag 30) | ❌ **Rejected** (`driver_station_connection.go:326`) | R3 |
| ASCII team-ID parsing | ❌ Reads fixed `[5]byte`; new DS needs ~1500 | R3 |
| UDP game-data bytes | ❌ `encodeControlPacket` returns fixed `[22]byte` | R3 |
| DMX driver | ❌ No DMX/Art-Net anywhere in tree | R4 |

### Driver-station compatibility risk

`field/driver_station_connection.go:326` rejects any initial packet that is not the
legacy signature:

```go
if !(packet[0] == 0 && packet[1] == 3 && packet[2] == 24) {
    log.Printf("Invalid initial packet received: %v", packet)
    tcpConn.Close()
    continue
}
```

A new-generation driver station sends `packet[1] >= 5 && packet[2] == 30` and would be
disconnected at handshake. Additionally `encodeControlPacket` returns a fixed
`[22]byte`, leaving no room for the game-data bytes at offset 22+.

**This is now in scope.** Per the Q1 decision, bioarena will accept both handshakes,
parse both team-ID encodings, and widen the control packet. Until that lands,
current-season driver stations cannot connect to bioarena at all — this is the
highest-priority item in the plan, and it blocks any on-field validation of the rest.

---

## 7. Implementation Plan

Ordered by dependency. Phase 1 is a prerequisite for validating everything else on
real hardware.

### Phase 1 — Driver station compatibility

Restores the ability to talk to current-season driver stations.

1. Widen the handshake read in `listenForDriverStations`
   (`field/driver_station_connection.go:301`) from `[5]byte` to a 1500-byte buffer.
2. Accept both signatures; record the result on the connection as `newDs`:
   - legacy — `packet[1] == 3 && packet[2] == 24`, team ID big-endian at bytes 3–4
   - new — `packet[1] >= 5 && packet[2] == 30`, team ID ASCII at byte 7, length at byte 6
3. Reject anything else, as today.

**Exit criteria:** a current-season DS connects, is assigned a station, and its robot
enables and disables under normal match flow. No game data yet.

### Phase 2 — Game Data plumbing

1. Add `GameData` to `Arena` and `SentGameData` to `DriverStationConnection`.

   Upstream stores game data per-station on `AllianceStation`, but the value is
   field-wide by definition — the manual specifies it reaches all operator consoles
   simultaneously — so six copies would be six chances to disagree. `SentGameData`
   does belong per-connection: it tracks what was actually put on the wire for that
   driver station and must reset when one reconnects.
2. Change `encodeControlPacket` to return `([32]byte, int)` — array plus length —
   appending the game-data bytes at offset 22 for new DS only
   ([§4.6](#46-driver-station-wire-format)). Max game data is 8 bytes, so 32 is
   sufficient. `sendControlPacket` writes `packet[:length]`.
3. Add `checkGameData()` for the legacy path, calling the existing but currently
   unused `sendGameDataPacket` (`field/driver_station_connection.go:456`). Send only
   when the value has changed.
4. Set game data to `""` at match start; set it to `R`/`B` on the transition into
   `TeleopPeriod` — **not** at AUTO start.

**Exit criteria:** `getGameSpecificMessage()` on a connected robot returns empty
through AUTO and pause, then the correct character from teleop start, on both DS
generations.

### Phase 3 — AUTO outcome selection

1. Add an `AutoWinnerMode` to arena state, settable pre-match.

   **Named by alliance (`random` / `red` / `blue`), not win/lose.** Arena state has no
   concept of which alliance is practising until [R1](#r1--half-field-1v0-mode) lands,
   so "win" would be undefined here. Phase 6 can relabel the same control in win/lose
   terms once a live alliance exists. The capability is identical either way.
2. `assignAutoWinner()` (`field/arena.go:1112`) honours it; random resolves to a
   concrete alliance, never `AllianceNone`.
3. Expose the selector in the match-play UI.

**Exit criteria:** forcing Win then Lose produces the opposite game-data character and
the opposite HUB dark windows.

### Phase 4 — HUB alternation helper

Implement [Table 6-3](#43-hub-active-state-manual-table-6-3) **once**, as a tested
pure function, rather than inside any individual lighting driver. Both the serial and
DMX drivers consume it.

**Exit criteria:** unit tests reproduce the full table for both AUTO outcomes.

**Done.** Ported from upstream rather than written fresh:

- `game/hub.go` mirrors upstream's `Shift` enum and `isShiftActive` body verbatim.
  Upstream's scoring half — Fuel counting, per-shift tallies, grace periods, and the
  shift-timing lookups serving them — is omitted per [§3](#3-non-goals). The one
  deliberate change is that `IsShiftActive` is exported, since bioarena's lighting
  drivers live outside the package.
- `hardware.HubActive(state, alliance)` is the driver-facing entry point, mapping
  `TeleopSubPhase` onto `game.Shift` and delegating the rule.
- `game/match_timing.go` gains upstream's `TransitionShiftDurationSec`,
  `ShiftDurationSec`, and `EndgameDurationSec`. `teleopSubPhase` now derives its
  boundaries from these instead of the hardcoded 130/105/80/55/30, and a test guards
  the flat `TeleopDurationSec` against drifting from the shift breakdown.

**Known divergence:** `hardware.TeleopSubPhase` and `game.Shift` remain separate enums
bridged by `TeleopSubPhase.Shift()`. Collapsing them onto `game.Shift` would be closer
to upstream still, but `game.Shift` covers the whole match (its zero value is
`ShiftAuto`, where `SubPhaseNone` means "not in teleop"), so the swap changes the
meaning of a zero-valued `LightingState`. Left as a follow-up.

### Phase 5 — DMX HUB light driver

1. New `hardware/dmx_lights.go` implementing `FieldLights`, opened over
   `go.bug.st/serial` following the `serial_lights.go` pattern.
2. Register a `case "dmx":` in `buildFieldLights()` (`main.go:82`), with config for
   port, channel/address, and colour values.
3. Render HUB active/inactive via the Phase 4 helper, plus the `ShiftWarning` window
   and the teleop-start indication of which HUB goes dark in Shift 1.
4. Failures log and continue — never block the match loop.

**Exit criteria:** fixture changes state at every shift boundary; unplugging it
mid-match does not stall the arena.

### Phase 6 — Half-field mode

1. Named 1v0 mode selecting the live station; auto-bypass the rest.
2. Surface the live alliance in the match-play UI.

**Exit criteria:** a 1v0 match starts with no manual bypassing.

### Incidental

- README line 247 documents `field_lights_driver: "gpio"`, which
  `buildFieldLights()` does not handle and would `log.Fatalf` on. Correct it when
  Phase 5 adds the `dmx` case.

---

## 8. Open Questions

**~~Q1 — Which driver station generation?~~ RESOLVED.** Support both. Current-season
driver stations are in use, so tag-30 handshake support and the UDP game-data path are
in scope. See [R3](#r3--fms-game-data-to-driver-stations).

**~~Q2 — DMX transport?~~ RESOLVED.** DMX over USB. See
[R4](#r4--dmx-hub-light).

**Q2a — Which USB DMX interface?**
Still blocks the R4 driver implementation, because the three common options have
materially different wire protocols:

| Interface | Protocol | Effort |
|---|---|---|
| **Enttec DMX USB Pro** | Framed serial messages (`0x7E … 0xE7`) over FTDI VCP | Low — works directly with `go.bug.st/serial`, already a dependency |
| Enttec Open DMX USB | Raw FTDI bit-banging; break/MAB timing generated in software | High — timing-sensitive, unreliable under a non-realtime scheduler |
| uDMX | Raw USB control transfers | Medium — needs libusb, a new dependency; not a serial device |

**Working assumption: Enttec DMX USB Pro** — provisionally adopted so planning can
proceed, *not* confirmed. It is the only one of the three that fits the existing
serial abstraction and avoids new dependencies.

**Blast radius: Phase 5 only.** Phases 1–4 and 6 are independent of the DMX interface
choice. Because the HUB alternation rule lives in the Phase 4 helper rather than in
the driver, Phase 5 is a thin transport shim — if this assumption turns out wrong,
what changes is how bytes reach the fixture, not what state the fixture is asked to
show. Revisit before starting Phase 5; an Open DMX USB would change both the driver
design and its reliability characteristics on a Raspberry Pi.

**Q3 — Is the practice robot always on Red?**
Affects R1 and R2. A fixed alliance makes "win AUTO" a constant mapping; a
selectable one requires an alliance control alongside the outcome selector.

**~~Q4 — Should the HUB light render the AUTO period?~~ RESOLVED by upstream.**
`isShiftActive` treats `ShiftAuto` and `ShiftPostMatch` as always active, so both HUBs
are lit during AUTO and after the match. Ported verbatim; `hardware.HubActive` follows
it. Idle and the auto/teleop pause are not shifts and leave the HUB dark.

---

## 9. Test Plan

### Unit

- `assignAutoWinner()` honours Win / Lose / Random; Random never yields
  `AllianceNone`.
- HUB alternation helper reproduces [Table 6-3](#43-hub-active-state-manual-table-6-3)
  for both AUTO outcomes across all seven timeframes.
- Game Data is empty through AUTO and pause; becomes `R`/`B` at the teleop
  transition and not before.
- Game Data encodes correctly for both DS paths (UDP bytes 22+ tag 32; TCP type 28).
- Handshake parsing accepts tag 24 and tag 30, extracts the correct team ID from each
  encoding, and still rejects malformed packets.
- `encodeControlPacket` returns length 22 with no game data, and 22 + 2 + n with it;
  bytes 0–21 are byte-for-byte identical to the current implementation in both cases.
- Existing `field/arena_hardware_test.go` boundary tests continue to pass unchanged.

### Integration

- A current-season driver station completes the handshake, is assigned a station, and
  enables/disables through a full match cycle.
- A legacy driver station still connects and behaves as it does today — no regression.
- 1v0 match starts with one station live and five bypassed, no manual intervention.
- With a DS connected, `getGameSpecificMessage()` returns empty during AUTO and the
  expected character from teleop start.
- DMX fixture changes state at each shift boundary; warning window fires 3 s early.
- DMX transport failure mid-match logs an error and does not stall the match loop or
  affect DS packets.

### Field acceptance

- Run a full 1v0 match with outcome forced to **Win**, then to **Lose**, and confirm
  the HUB dark windows swap between Shifts 1/3 and 2/4.
- Confirm driver-visible behaviour matches a real field: countdown, HUB state, and
  Game Data all consistent.

---

## 10. Sources

- `Rules/2026GameManual.pdf` — REBUILT game manual, Version TU13. §6 p.44 (HUB status,
  Game Data, ties); §10 p.119 (ARENA FAULT).
- [2026 Game Data Details — WPILib](https://docs.wpilib.org/en/stable/docs/yearly-overview/2026-game-data.html)
  — Game Data format, timing, empty-string-before-teleop behaviour.
- [Team254/cheesy-arena](https://github.com/Team254/cheesy-arena) — `game/hub.go`
  (`isShiftActive`), `game/match_timing.go` (shift durations),
  `field/driver_station_connection.go` (`checkGameData`, `newDs`, packet layout).
- `Rules/fms-whitepaper.rst.txt` — FMS architecture and DS network topology.
- [docs/hardware-wiring.md](hardware-wiring.md) — field hardware wiring, including the
  `FieldLights` serial driver this work extends.
