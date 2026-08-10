# Richmond lab

Site record for one deployment of this project. Everything here is specific to this
field — the README describes the product, this describes how one instance of it is wired.

Copy this file when standing up another site and change the values.

## Field

| | |
|---|---|
| Stations | 4 — R1, R2, R3, B1 |
| Site number | 1 (team subnets on `10.0.100.0/24` management) |
| Team networks | `team_network_driver: switch` — the switch routes and serves DHCP |

B2 and B3 need **BYP** checked in Match Play, since bioarena always thinks in six
stations.

## Addresses

| Device | Address |
|---|---|
| Field controller Pi | `10.0.100.5` |
| Switch (management) | `10.0.100.3` — set as **Switch Address** under Arena → Settings, with its Telnet password |
| Field access point | `192.168.69.1` (fixed by the VH-113; the Pi and switch carry addresses in that subnet) |
| Red e-stop panel | `10.0.100.11` (not installed) |
| Blue e-stop panel | `10.0.100.12` (not installed) |

## Hardware

**Switch — Catalyst 3560-CX**, IP Base, hostname `ChezySwitch`, management `10.0.100.3`.
Boot image lives in `flash:c3560cx-universalk9-mz.152-7.E/`, with `boot system` set so it
does not stop at the boot loader.

The AP is the only trunk. The VH-113 tags each team's SSID onto that team's VLAN, so
VLANs 10–60 have to reach it — an access port there leaves every robot associated to WiFi
and unable to reach anything. The Pi needs only VLAN 100; with the `switch` driver it does
no routing, so it sits on an access port like any other field device.

| Port | Role | Mode |
|------|------|------|
| Gi0/1 | R1 driver station | access vlan 10 |
| Gi0/2 | R2 driver station | access vlan 20 |
| Gi0/3 | R3 driver station | access vlan 30 |
| Gi0/4 | B1 driver station | access vlan 40 |
| Gi0/5 | B2 driver station | access vlan 50 |
| Gi0/6 | B3 driver station | access vlan 60 |
| Gi0/7 | VH-113 AP | trunk, native vlan 100 |
| Gi0/8 | Field controller Pi | access vlan 100 |
| Gi0/9 | Art-Net node | access vlan 100 |
| Gi0/10–12 | spare, for troubleshooting | access vlan 100 |

Gi0/1–6 are not a site choice. [`dsPortInterfaces`](../../network/switch.go) hardcodes them
in station order and shuts and reopens each one around a VLAN change, which is what makes a
laptop re-request an address on its new subnet. Wire a station elsewhere and it keeps the
previous match's address.

B2 and B3 have ports and VLANs but no driver stations yet; a station with no team gets no
subnet, so VLANs 50 and 60 stay dark until one is registered.

Baseline to load before bioarena connects: [switch_config.txt](../../switch_config.txt),
which is written for exactly this port map. Re-export the running config whenever the map
changes — a backup that no longer matches the field restores cleanly and then fails in
ways that look like cabling.

**Access point — Vivid-Hosting VH-113.** No API password; the practice firmware exposes no
token, so **AP Password** under Arena → Settings is blank.

**Art-Net node — PKNight CR011R**, static `10.0.100.100`, on Gi0/9. The switch's
`dhcppool` covers `10.0.100.0/24` but excludes `.1`–`.125`, so a static in that range can
never collide with a lease. Its address goes in **sACN Receiver Address** under
Arena → Settings → LEDs.

## Notes

Driver station laptops are statically addressed at present. `10.TE.AM.5` is the address to
use — station detection looks for exactly that, and both DHCP implementations reserve
`.1`–`.19` so it never collides with a pool.
