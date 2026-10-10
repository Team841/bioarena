# Divergences from upstream cheesy-arena

bioarena is a fork of [Team254/cheesy-arena](https://github.com/Team254/cheesy-arena).
This is the register of where the two differ deliberately, and which differences are
worth sending back upstream.

Keeping this current matters because some of the fork's value depends on staying close
to upstream — in particular `led/` is byte-identical, so upstream lighting changes can be
taken with a checkout rather than a merge.

To add the upstream remote if it is not configured:

```bash
git remote add upstream https://github.com/Team254/cheesy-arena.git
git fetch upstream main
```

To check a file or directory:

```bash
git diff upstream/main -- led/
```

---

## Upstream candidates

Changes worth offering back. None are blocking for upstream; they are hardening or
generalisation rather than fixes for bugs upstream can currently hit.

### `network/switch.go` — terminate the command before appending `end`

**Status:** ready to propose. Not yet submitted.

`runConfigCommand` wraps the caller's text as `config terminal\n<command>end\n`. If the
caller's last line is not newline-terminated, `end` is concatenated onto it — IOS
receives something like `no shutdownend`, rejects it, and the whole configuration block
silently fails to close. The failure is invisible because a Telnet read timeout is
treated as success further up.

**Upstream cannot currently trigger it.** Both upstream callers build strings ending in
`\n`, and upstream does not cycle driver station ports at all. bioarena hit it by adding a
third caller whose text did not honour the unstated precondition.

**The pitch is therefore hardening, not a bug report.** A helper that silently corrupts
its input when a precondition is unmet is worth guarding regardless of whether any
current caller violates it — particularly one whose failure mode is a silently discarded
configuration block on live field hardware.

The change is three lines and is a no-op for newline-terminated input, so upstream's
existing callers and their tests are unaffected:

```go
if command != "" && !strings.HasSuffix(command, "\n") {
    command += "\n"
}
```

Covered by `TestConfigCommandTerminatesLastLine` and
`TestConfigCommandDoesNotDoubleTerminate` in `network/switch_test.go`.

**Submit it as an isolated patch, not a file copy.** `network/switch.go` carries several
unrelated fork changes — the standing baseline, incremental configuration, the status badge,
`ServerIpAddress` as a var rather than a const, `DevMode` removed, the module path. A PR should touch only `runConfigCommand` and add the two tests. Check
what else has drifted before branching:

```bash
git diff upstream/main -- network/switch.go
```

Note also that upstream's tests construct the switch as `NewSwitch(address, password)`,
where bioarena's takes a `SwitchConfig`. The upstream-facing tests must use upstream's
two-argument signature.

---

## Fork-local

Deliberate, specific to a practice field, and not appropriate to send upstream.

| Area | Divergence | Why it stays local |
|---|---|---|
| `game/hub_bioarena.go` | New file: `IsShiftActive`, a one-line wrapper exporting upstream's unexported `isShiftActive`. | bioarena's lighting drivers live outside `game/`, where upstream's callers are all inside it. A separate file so `hub.go` stays byte-identical; it was previously a fork-local edit with the scoring half omitted, until the FUEL counter gave bioarena a reason to score and the whole file could be taken back. |
| `plc/override.go` | New file: `HubCountSource` and `WithHubCounts`, which return a `Plc` whose `GetHubCounts` is answered by a webcam FUEL counter instead of Modbus registers, and whose `ResetMatch` reaches both. Wired in `field/arena_hub_counter.go` from the counter address under Arena → Settings → LEDs. | Upstream fields have a PLC counting FUEL at the HUB chutes. A practice field has a webcam. Additive, on the `led/override.go` pattern, so `plc.go` keeps upstream's shape and the arena's scoring lines stay upstream's own — see [docs/spec-hub-fuel-counter-feed.md](spec-hub-fuel-counter-feed.md). |
| `hardware/hub_counter.go` | New file: `HubCounterReceiver`, the UDP listener for the counter feed — acceptance rules, per-match baseline, liveness, and the status reply. | Practice-field hardware with no upstream counterpart. It implements `plc.HubCountSource` structurally, so `hardware/` need not know about Modbus nor `plc/` about cameras. |
| `field/arena_hub_counter.go`, `field/arena.go` | `updateHubCounts` runs upstream's two scoring lines — `Plc.GetHubCounts` into each `game.Hub.UpdateState` — from `Update` rather than from `handlePlcInputOutput`, and the Hubs are zeroed at `StartMatch` as well as at `LoadMatch`. | `handlePlcInputOutput` returns early when the PLC is not enabled, and on a practice field it never is. The extra zeroing is because a driver shoots in PreMatch for ten minutes before a practice round, where upstream loads a match and immediately runs it. `field/arena.go` is forked throughout. |
| `game/match_timing.go` | Adds `WarmupDurationSec` and keeps a flat `TeleopDurationSec` alongside upstream's shift constants. `ScoringGracePeriodSec` is upstream's own; `MotorsOnExtraPeriodSec` is omitted. | Warmup is a practice-field feature with no upstream counterpart, threaded through the match state machine. The omitted constant times upstream's HUB exhaust motors, which no practice field has. |
| `led/override.go`, `hardware/gpio_hub_relays.go`, `field/arena_leds.go` | Hub lights have two outputs, picked under Arena → Settings → LEDs: DMX to upstream's full-field layout (sACN, or Art-Net via `led/artnet.go`), or one GPIO relay per HUB switching a red or blue lamp. The relays take the same `led.Mode` sequence as the DMX controller and turn each into on, off or a blink with `led.Lit`, the only thing left in `override.go`: the warning pulse and the transition-shift advantage sweep blink, at periods read from upstream's `pulseHalfPeriod` and `advantageStepCycle`, since both carry game information a steady lamp would lose. Relay pins and polarity are stored in the database, defaulting to BCM 22 and 23. The output is rebuilt on every settings save, releasing the previous one first. | Upstream fields have the full 16-fixture hardware; a practice field may have two lamps. Driving the relays from the mode sequence rather than a parallel computation means they cannot disagree with DMX about which HUB is active. The practice-field layout override and capability fallback that used to live in `override.go` were removed, leaving these two outputs as the only choice. |
| `led/artnet.go` | New file: `ArtNetController`, an Art-Net alternative to the E1.31 sACN in `controller.go`, chosen by a checkbox under Arena → Settings → LEDs. Embeds `Controller` and replaces only `SetAddress` and `Update`, reusing the zone rendering and fixture layout. | Upstream fields have sACN nodes. A practice field buys whatever DMX gateway is cheap, and plenty of them speak only Art-Net. Additive for the same reason as `override.go`: a protocol switch inside `controller.go` would end the byte-identical property. |
| `web/match_play.go`, `templates/match_play.html`, `static/js/match_play.js`, `field/arena.go` | Match sounds play on the Match Play page. The websocket subscribes to `PlaySoundNotifier`, the template carries the `<audio>` elements, and `MuteMatchSounds` defaults to true. | Upstream plays cues on the audience display. This fork has no `audience_display` template — only `static/js/audience_display.js` survives, orphaned — so the notifier fired into nothing and the field was silent regardless of settings. The operator's page is the only one there is. Muted by default because a practice field is usually a room with people in it. |
| `network/switch.go`, `field/arena.go`, `field/driver_station_connection.go` | **Removed, was fork-local.** Staging subnets (`172.16.<vlan>.0/24`) for unregistered stations, self-registration from them, ARP-table station detection (`GetStationForTeamId`), driver station port cycling (`CycleStationPort`, `recoverMissingDriverStations`) and the `AutoConfigureTeams` setting are all gone. Teams are registered by typing numbers into Match Play, as upstream does. | The auto-registration was slower than it replaced: every driver station arrival cost a switch round trip and a port cycle, and a wrong guess cost more to undo than typing six numbers. What is kept is the read-only port link poll, which is the only thing that sees a cable in an unregistered station — it runs on the periodic task, not the match-load path. Upstream's own wrong-station check covers a laptop in the wrong port. |
| `game/match_sounds.go`, `field/arena.go` | `shift_change` is not scheduled in `MatchSounds`. It is sounded on the tick `currentShift` changes in `Arena.Update`, gated by `shiftChangeSounds` to the four HUB handovers. | Upstream schedules the cue at four timestamps computed from the shift constants. Bioarena's match clock runs from a flat `TeleopDurationSec` and finds shift boundaries through `teleopShift`, so the cue follows those. The Hub lights read the shift constants through `game.Hub`, as upstream's do; the two agree for as long as `TestTeleopDurationMatchesShiftBreakdown` passes. |
| `templates/match_play.html`, `static/js/match_play.js`, `web/match_play.go`, `web/setup_teams.go`, `field/arena.go` | Match Play carries a WPA key field per station alongside the team number, stacked in the width the number alone used to have. Entering a team number looks it up: a known team fills in its key, an unknown one offers to add it to the database. Register stores any changed keys before substituting, so the access point push already triggered carries them; a blank key leaves the team's existing one, and a key the operator typed is never overwritten by a later lookup. `quick-add` gives a new team the zero-padded team number as its key. | Upstream sets keys from the team list, which assumes an event where teams are entered ahead of time. A practice field registers whoever turns up, and leaving Match Play for Setup → Teams to set a key was friction at exactly the wrong moment. The default key matters because a team created without one has no working WiFi, and nothing says so until a robot fails to associate. The lookup and its modal came from the Free Practice page, and outlived it. |
| `field/`, `web/`, `templates/`, `static/js/` | **Removed, was fork-local.** The Free Practice page and the `FreePractice` arena state, along with `EnterFreePractice`, `ExitFreePractice`, `SetFreePracticeSlot`, `ClearFreePracticeSlot`, and the `DisableField`/`EnableField` halt that only it used. Match Play is the only operating page. | The mode enabled robots continuously with no timer, which nothing else does — that capability is gone, deliberately. Open driving is now a long test match, aborted when finished. Its team-lookup modal was ported to Match Play first, so registering an unknown team did not regress with it. |
| `network/switch.go`, `field/arena.go` | Clearing a match shuts and reopens the driver station port of every station with a team registered, batched into one shut and one reopen. | Driver station software releases its address at the end of a match and Windows then sits unaddressed; the link event is what prompts a fresh DHCP request. Upstream does not do this and does not need to, running matches back to back with a crew. Done only at the clear, where the match is already over — an earlier version cycled on every VLAN change and from a background rescue poll, which cost seconds on every match load. |
| `field/team_match_log.go` | Logs written to `logs/` rather than `static/logs/`. | Upstream's placement makes them downloadable for free, but it also means the deploy step's `scp -r static` copies every accumulated log to the field Pi. bioarena serves them from `/logs/` instead. |
| `web/web.go` | Serves `/logs/` with directory listing and no authentication. | Restores the browse-and-download behaviour lost by moving the directory, deliberately matching how `/static/` is served. |
| `web/audience_display.go`, `templates/audience_display.html`, `static/{css,js}/audience_display.*` | An audience display of two screens -- the match, and a logo between matches -- swapped by the match state rather than by an operator, filling the screen. Served unauthenticated at `/audience_display` from an Audience tab in the nav. Informed by upstream's rather than ported from it; the stale copies of upstream's JS and CSS that the fork had been carrying are replaced. | Upstream's is a chroma-key strip across the bottom of a webcast, with ten screens on an operator's radio buttons. A practice field puts a television beside the field and has nobody spare to drive it. Six of those ten -- final score, bracket, alliance selection, sponsor reel, lower thirds, timeout -- need models this fork deleted, and upstream's per-display configuration needs the display registry it also deleted. What is kept is what upstream's overlay shows during a match, which is what bioarena now has the data for. |
| `field/arena.go` | The a-stop bit goes to the driver station during AUTO only (`auto && AStop`), the same rule the enable decision already follows; upstream sends it whenever the station's a-stop reads pressed. And a match will not start while any station's a-stop still reads pressed. | Built for latching a-stop buttons. Upstream's a-stop is latched in software, which suits a momentary button; a latching one pressed in AUTO is still in at teleop, and the driver station holds the robot disabled for as long as it sees the bit — the whole of teleop. The start check is needed because upstream's `aStopReset` only clears when a team is assigned on a field with a PLC, so on this field it never trips, and a button left in would cost that robot its next AUTO. |
| `field/`, `hardware/`, `cmd/estop-panel` | GPIO and network e-stop panels, half-field conveniences. | Practice-field hardware and workflow with no upstream counterpart. |
| `hardware/`, `field/arena.go`, `main.go` | **Removed, was fork-local.** The `FieldLights` interface, its serial Arduino driver (`field_lights_*` in `config.yaml`, now retired keys that are ignored with a warning), `LightingState`, `MatchPhase`, `HubActive` and `shiftWarning`. | Hub lighting is the two outputs above and nothing else. The serial driver sent one command at match start to an Arduino that ran its own clock; `LightingState` existed to feed it, and the shift cue was its last reader. |
| `network/switch.go` | Bioarena pushes the switch's standing configuration itself: on the first configuration of a run it applies the VLAN database, the station and trunk ports, the DMX gateway port, portfast, `ip routing` and `ntp server`, and saves it — so setting up a field is wiring plus a console bootstrap script rather than hand-written IOS. Per-match configuration is incremental: the last applied teams are cached and only the stations whose team changed are rebuilt, with a full rebuild whenever the switch's state is unknown, at startup and after a failure. Driver station ports are fixed in code as `GigabitEthernet0/1`–`0/6` in station order. `NewSwitch` takes a `SwitchConfig` struct. | Upstream assumes the switch was configured by hand from `switch_config.txt`; the baseline is what makes a new field wiring plus a script, and it costs no match time because it runs once per process start. Incremental rebuild keeps a registration change from disturbing the stations that did not change: a full rebuild drops every team's subnet and with it every driver station connection. |

---

## Kept byte-identical

Files taken from upstream unchanged, so an upstream change to them can be taken with a
checkout rather than a merge. Fork-local behaviour goes in a neighbouring file instead —
`led/override.go`, `led/artnet.go`, `plc/override.go`, `game/hub_bioarena.go`.

| Files | Local behaviour lives in |
|---|---|
| `led/color.go`, `led/controller.go`, `led/controller_test.go`, `led/fixture.go`, `led/mode.go`, `led/mode_test.go`, `led/zone.go` | `led/override.go`, `led/artnet.go` |
| `game/hub.go`, `game/hub_test.go` | `game/hub_bioarena.go`, `game/hub_bioarena_test.go` |
| `plc/register_string.go` | generated from the register enum in `plc/plc.go`, which is upstream's |

```bash
git fetch upstream main
git diff upstream/main -- \
  led/color.go led/controller.go led/controller_test.go led/fixture.go \
  led/mode.go led/mode_test.go led/zone.go \
  game/hub.go game/hub_test.go plc/register_string.go
```

Empty output means the property still holds. **Fetch first** — a stale `upstream/main`
reports differences that are only the fork being behind, which reads exactly like the
property having been broken.

If the output is ever genuinely non-empty, either move the edit into the neighbouring
file or update this document to say the property was given up and why.

`game/hub.go` is the worked example of taking one back. It was fork-local for a while,
with the scoring half deleted on the grounds that a practice field does not score; when
the FUEL counter gave it a reason to, the whole file came back from upstream unchanged and
upstream's `hub_test.go` passed against bioarena's timings without an edit. The one thing
bioarena needs that upstream does not — an exported `IsShiftActive` — is three lines in
`game/hub_bioarena.go`.
