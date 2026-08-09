# Richmond lab

Site record for one deployment of this project. Everything here is specific to this
field — the README describes the product, this describes how one instance of it is wired.

Copy this file when standing up another site and change the values.

## Field

| | |
|---|---|
| Stations | 4 — R1, R2, R3, B1 |
| Site number | 1 (team subnets on `10.0.100.0/24` management) |
| Team networks | `team_network_driver: local` — the Pi routes and serves DHCP |

B2 and B3 need **BYP** checked in Match Play, since bioarena always thinks in six
stations.

## Addresses

| Device | Address |
|---|---|
| Field controller Pi | `10.0.100.5` |
| Switch (management) | `10.0.100.3` |
| Field access point | `10.0.100.2` |
| Red e-stop panel | `10.0.100.11` (not installed) |
| Blue e-stop panel | `10.0.100.12` (not installed) |

## Hardware

**Switch — TP-Link TL-SG108E**, hostname `RichmondSwitch`, VLANs named `Red1`, `Red2`,
`Red3`, `Blue1`. Layer 2 only, which is why the Pi does the routing and DHCP.

| Port | Role | Membership | PVID |
|------|------|------------|------|
| 1 | Pi | untagged 1, tagged 10–60 | 1 |
| 2 | VH-113 AP | untagged 1, tagged 10–60 | 1 |
| 3 | R1 driver station | untagged 10 | 10 |
| 4 | R2 driver station | untagged 20 | 20 |
| 5 | R3 driver station | untagged 30 | 30 |
| 6 | B1 driver station | untagged 40 | 40 |
| 7–8 | spare | untagged 1 | 1 |

Backup: [richmond-switch.cfg](richmond-switch.cfg). Restore from *System Tools → Backup and
Restore* after a factory reset, and re-export whenever the port map changes — a backup
that no longer matches the field restores cleanly and then fails in ways that look like
cabling.

**Access point — Vivid-Hosting VH-113.** No API password; the practice firmware exposes no
token, so **AP Password** under Arena → Settings is blank.

**Catalyst 3560-CX**, IP Base, on hand as the Layer 3 alternative. Boot image lives in
`flash:c3560cx-universalk9-mz.152-7.E/`, with `boot system` set so it does not stop at the
boot loader.

## Notes

Driver station laptops are statically addressed at present. `10.TE.AM.5` is the address to
use — station detection looks for exactly that, and both DHCP implementations reserve
`.1`–`.19` so it never collides with a pool.
