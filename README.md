# Practice Field Controller

A Raspberry Pi service for running FRC practice sessions. Controls up to 6 robots across red and blue alliances. Runs timed auto and teleop periods. Manages the field access point and VLAN isolation automatically. Accessible from any browser on the field network.

## Requirements

- Raspberry Pi 4 running 64-bit Raspberry Pi OS (Trixie or Bookworm). 32-bit images work
  too — see [Raspberry Pi OS releases](#raspberry-pi-os-releases) for the one build flag
  that changes
- [Go 1.23+](https://golang.org/dl/) on your build machine
- Vivid-Hosting VH-113 field access point (running OpenWRT)
- Vivid-Hosting VH-109 radio on each robot
- Layer 3 managed switch with the IOS DHCP server (Catalyst 3560-CX or similar; see [Step 2](#step-2--configure-the-managed-switch)) — or any VLAN-capable Layer 2 switch, with the Pi doing the routing and DHCP instead (see [Team networks on the Pi](#team-networks-on-the-pi-layer-2-switches))
- Static IP assigned to Pi (recommend `10.0.100.5`)

## Install

### Preparing a fresh Pi image

Flash with Raspberry Pi Imager and open its customisation panel (the gear icon, or
`Ctrl+Shift+X`) **before writing**. Several things are far easier to set there than after
first boot.

- **Name the login user whatever you like.** Bookworm and later no longer create `pi` by
  default, and nothing here depends on the name: bioarena installs to `/opt/bioarena` and
  runs as a dedicated `bioarena` system account. The `scp` commands below use `<USER>` for
  whichever account you log in with.
- **Enable SSH.** A fresh image has it off, so without this the first boot needs a
  keyboard and monitor.
- **Set the keyboard layout and locale.** A mismatched layout is why keys like `|` end up
  missing when you are working at the Pi directly.
- **Set the WiFi credentials** if the Pi needs to reach the internet before it joins the
  field network. It does — dnsmasq has to be installed while it still has a route out.

After first boot, in this order:

1. **Install the packages, while the Pi still has internet.** This is the step that is
   painful to go back for: once the Pi is on the field network there is no route out, and
   an end-of-life release compounds it by needing its apt sources repointed at
   `archive.debian.org` first.

   ```bash
   sudo apt install chrony
   ```

   Chrony on every field — it makes the Pi the field's time source, without which the
   switch and the controller timestamp their logs years apart. See
   [Step 5](#step-5--serve-time-from-the-pi).

   ```bash
   sudo apt install dnsmasq
   ```

   Dnsmasq only for `team_network_driver: local`, where the Pi serves DHCP itself. On the
   Cisco path the switch does that and dnsmasq is unused — but installing it anyway costs
   nothing and leaves the option open, which is worth more than the disk space when the
   switch fails on a Saturday.

2. Set the static field address, and install the NetworkManager drop-in if using
   `team_network_driver: local` — see [Raspberry Pi OS releases](#raspberry-pi-os-releases)
   and [Step 1](#step-1--assign-a-static-ip-to-the-pi).
3. Install the chrony drop-in and point the switch at the Pi, per
   [Step 5](#step-5--serve-time-from-the-pi).
4. Copy the binary, assets, and service file (below), plus `config.yaml`. Carry `event.db`
   across too if you want the previous field's settings, teams, and admin password;
   without it the first start creates a fresh database with the default password.
5. Confirm the GPIO chip name with `gpiodetect` if you use e-stop panels or GPIO lights.

Two things that will look like faults and are not:

**SSH host key mismatch.** A reimaged Pi generates new host keys, so `ssh` and `scp` refuse
to connect at the same address. Clear the stale entry:

```bash
ssh-keygen -R 10.0.100.5
```

Addresses are tracked separately, so repeat it for whatever address you used before the Pi
moved onto the field network.

**Wrong timestamps in the logs.** The Pi has no real-time clock. On a field with no route
to the internet it restores the last known time from `fake-hwclock` at boot, so journal
entries and match logs can be days behind until it next sees an NTP server. See
[Step 5](#step-5--serve-time-from-the-pi) for making the field's clocks at least agree with
each other.

**Build the Pi binary**

Run this on your development machine (not on the Pi):

```bash
./build-pi.sh
```

This cross-compiles two ARM binaries: `bioarena-pi` for the field controller and
`estop-panel-pi` for the e-stop panel Pis. The `-pi` suffix keeps them from shadowing a
local build. Running the script also prints the full deploy sequence for both.

It targets 64-bit (`aarch64`) by default. On a 32-bit image, build with:

```bash
ARCH=arm GOARM=7 ./build-pi.sh
```

Check which you need with `uname -m` on the Pi: `aarch64` for the default, `armv7l` for
the override. The wrong one does not fail informatively — the Pi reports
`cannot execute binary file: Exec format error`.

**Create the service account (once per Pi)**

Bioarena runs as a dedicated system account and installs to `/opt/bioarena`, so the
deployment does not depend on which login user the SD card was flashed with. Run on the
Pi, replacing `<USER>` with your login name:

```bash
sudo useradd --system --home-dir /opt/bioarena --shell /usr/sbin/nologin bioarena
sudo mkdir -p /opt/bioarena
sudo chown -R bioarena:bioarena /opt/bioarena
sudo chmod 2775 /opt/bioarena
sudo usermod -aG bioarena <USER>
```

Log out and back in for the group to take effect. `2775` is setgid and group-writable, so
your login user can `scp` straight into `/opt/bioarena` and the files land in the
`bioarena` group.

**Copy files to the Pi**

If bioarena is already installed, stop it first — Linux refuses to overwrite a running
executable, and `scp` reports that as a bare `Failure`:

```bash
ssh <USER>@<PI_IP> "sudo systemctl stop bioarena"
```

```bash
scp bioarena-pi <USER>@<PI_IP>:/opt/bioarena/
scp -r static templates <USER>@<PI_IP>:/opt/bioarena/
scp bioarena.service <USER>@<PI_IP>:~/
```

Keep the `bioarena-pi` filename — `bioarena.service` runs
`/opt/bioarena/bioarena-pi`, so renaming it on copy leaves the service unable to
start.

Then make the binary executable:

```bash
chmod +x /opt/bioarena/bioarena-pi
```

**Leave the copied files owned by your login user.** The service only needs to read the
binary and the assets; the files it writes — `event.db`, `logs/`, `db/backups/` — it
creates itself and therefore already owns. Chowning the deploy artifacts to `bioarena`
gains nothing and breaks the next deploy: `scp` overwrites a file by opening it for
writing, so a file owned by the service account with mode 644 refuses the write no matter
what the directory permissions say. The error is `dest open ... Permission denied`, which
reads like a directory problem and is not one.

If a previous deploy did chown them, undo it once:

```bash
sudo chown -R <USER>:bioarena /opt/bioarena
sudo chown bioarena:bioarena /opt/bioarena/event.db
```

`event.db` stays with the service account because that is the one file bioarena must
write. Carrying a database over from another Pi means chowning it the same way.

**Faster deploys**

The steps above are what a deploy does; [deploy-pi.sh](deploy-pi.sh) and
[deploy-pi.ps1](deploy-pi.ps1) do them for you — the same script for Git Bash and for
PowerShell. The common case is a code change with no asset change, so that is the default
and it copies one file:

```bash
./deploy-pi.sh
```

```bash
./deploy-pi.sh --assets              # static/ or templates/ changed too
./deploy-pi.sh --assets --service    # and bioarena.service
./deploy-pi.sh --target 10.2.100.5   # another site
./deploy-pi.sh --panel 10.0.100.11   # an e-stop panel Pi instead
```

In PowerShell the same flags are `-Assets`, `-Service`, `-Target`, `-Panel`:

```powershell
.\deploy-pi.ps1 -Assets -Service
```

It builds, stops the service, copies, makes the binary executable, starts it, and reports
whether the unit came up. Any step that fails stops the run — continuing past a failed copy
would restart the service on whichever binary happened to be there, which looks like a
successful deploy of code that was never pushed.

Assets and the service file are opt-in rather than guessed. Copying `static` and `templates`
every time is most of the wall clock, and installing the service file needs a sudo move and
a `daemon-reload` that are wasted when it has not changed.

**Set up key authentication first**, or every deploy asks for a password three times. On
your development machine:

```powershell
ssh-keygen -t ed25519 -C "bioarena deploy"
```

```powershell
type $env:USERPROFILE\.ssh\id_ed25519.pub | ssh <USER>@10.0.100.5 "mkdir -p ~/.ssh && cat >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"
```

Windows has no `ssh-copy-id`, which is why that is a pipe into `cat` rather than one
command. After it, deploys are silent and take a few seconds.

**Install the systemd service (run on the Pi)**

```bash
sudo mv ~/bioarena.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable bioarena
sudo systemctl start bioarena
```

The service file is **moved**, not copied, so it ends up at
`/etc/systemd/system/bioarena.service` and will not be found in `/opt/bioarena/`. Confirm
it is installed with:

```bash
systemctl cat bioarena
```

The service automatically assigns `10.0.100.5/24` to `eth0` on startup.

**Open the web UI**

```
http://10.0.100.5:8080
```

## Network Setup

This section is the most important part of the physical field setup. Read it carefully before powering anything on.

### Why this network layout matters

FRC Driver Station software is hardcoded to contact its FMS at `10.0.100.5` on ports `1750` (TCP) and `1121`/`1160` (UDP). The Pi must live at that address on the wired field network. Each team lives on its own team-number-derived subnet isolated by a VLAN, with the driver station laptop wired into that VLAN and the robot joining it over WiFi. The access point handles the wireless side; the switch enforces isolation and routes each team subnet to the FMS.

### Topology

This mirrors a competition field: **driver station laptops are wired**, **robots are
wireless** through their own radios.

```
                        ┌─────────────────────────────────────┐
                        │   Cisco L3 Managed Switch            │
                        │                                       │
          ┌─────────────┤ Access ports      6 x access ports   │
          │             │ (Pi, AP)          (one per station)   │
          │             └──────────────────────┬────────────────┘
          │                                    │ (wired)
    ┌─────┴──────┐                      ┌──────┴─────────────┐
    │ Raspberry  │                      │  DS Laptops        │
    │ Pi 4       │                      │  (one per station) │
    │ 10.0.100.5 │                      │  10.TE.AM.5        │
    └─────┬──────┘                      └────────────────────┘
          │
          │ (HTTP to AP, Telnet to switch)
          │
    ┌─────┴────────────┐
    │ Vivid-Hosting    │
    │ VH-113 AP        │
    │ (OpenWRT)        │
    └─────┬────────────┘
          │ (WiFi — one SSID per team, named for the team number)
          │
    ┌─────┴──────────────┐
    │  Robot radios      │
    │  (VH-109)          │
    │  10.TE.AM.xx       │
    └────────────────────┘
```

Each station's laptop and its robot land on the same VLAN and team subnet — the laptop
over its wired port, the robot over WiFi. Sharing a subnet is what lets the driver
station find the roboRIO by mDNS, which does not cross subnet boundaries.

### Step 1 — Assign a static IP to the Pi

The Pi must have `10.0.100.5` on the interface connected to the switch. The systemd service handles this automatically via:

```
ExecStartPre=/sbin/ip addr add 10.0.100.5/24 dev eth0
```

For a permanent static IP that survives reboots without the service, the method depends on
the OS release.

**Trixie and Bookworm** use NetworkManager. Name the connection bound to the field NIC —
`nmcli connection show` lists them, typically `Wired connection 1`:

```bash
sudo nmcli connection modify "Wired connection 1" ipv4.method manual ipv4.addresses 10.0.100.5/24
```

```bash
sudo nmcli connection up "Wired connection 1"
```

Leave the gateway and DNS unset. The field network has no route off itself, and a default
route pointing into it would send the Pi's own internet traffic nowhere.

**Buster and other dhcpcd releases** edit `/etc/dhcpcd.conf` instead:

```
interface eth0
static ip_address=10.0.100.5/24
```

Do not put the Pi on a robot subnet (`10.TE.AM.x`). Use a dedicated management subnet such as `10.0.100.0/24`.

### Step 2 — Configure the managed switch

**Nobody configures this switch by hand.** Bioarena owns its configuration: the VLAN
database, the station and trunk ports, portfast, `ip routing`, and the per-match VLANs and
DHCP pools. What a person does is wire it and run one script.

**Switch requirements.** Bioarena issues six concurrent SVIs with IP addresses and six
DHCP pools, so the switch must be Layer 3 capable with the IOS DHCP server — a
3550/3560/3750-class unit, IP Base rather than LAN Base. A 2960 will not work: it allows
only one active SVI, and the LAN Lite images have no DHCP server. Check with
`show version` before wiring a field around one.

**Wire it like this.** These are the only decisions left to a person, because the code
assumes them:

| Port | Role |
|------|------|
| `GigabitEthernet0/1`–`0/6` | Driver stations, in station order: R1, R2, R3, B1, B2, B3 |
| `GigabitEthernet0/7` | Raspberry Pi |
| `GigabitEthernet0/8` | Vivid-Hosting VH-113 access point |

**Then bootstrap it over the console.** A switch out of the box has no address and no
password, so nothing can reach it over the network — that part is console-only by
definition. [docs/switch-bootstrap.py](docs/switch-bootstrap.py) does it without anyone
composing IOS:

```bash
scp docs/switch-bootstrap.py <USER>@10.0.100.5:~/
```

```bash
sudo python3 ~/switch-bootstrap.py --password <SWITCH_PASSWORD> --hostname RichmondSwitch
```

That sets the management address (`10.0.100.3` by default), the enable and VTY passwords to
the same value, enables Telnet, points the boot loader at the installed IOS image so the
switch stops halting at the `switch:` prompt, and saves. Run it again any time; it changes
nothing the second time.

**Then enter the address and password** under **Arena → Settings**. They are the only
switch settings in the web UI. On its first configuration bioarena pushes the standing
configuration — VLANs named per station, the six access ports with portfast, the two
trunks carrying all six VLANs, `ip routing` — and saves it, so a power cycle brings the
switch back ready to run a field on its own.

That saving happens once, before any team is on the field. Bioarena never writes the
configuration later, and neither should you: doing it mid-session bakes whichever teams
were loaded into the startup config, and their DHCP pools come back after every reboot as
state nobody put there.

[switch_config.txt](switch_config.txt) records what the result looks like, for reference or
for a switch you would rather set up by hand.

Cycling a station's port is what makes its laptop re-request an address on the new subnet
rather than keep the previous match's. Only the stations whose team changed are cycled, so
the others keep their VLAN, their addresses, and their driver station connections — which
is what makes free practice usable while other teams are driving.

The first configuration after a restart rebuilds everything, because the switch outlives
the process and may have been changed by hand in between.

#### Staging networks and self-registration

An unregistered station is not left dead. It gets a **staging subnet** — `172.16.<vlan>.0/24`,
routed to the FMS — so a laptop plugged into it still receives an address and its driver
station still connects. The driver station announces the team number configured in its own
software, and the address it connects from names the port it is in, so bioarena registers
that team to that station and rebuilds it onto the team's real subnet.

In practice: plug a driver station into any station's port and it registers itself there.
Move the cable to another port and the team moves with it, cleared from the station it
left, so a team is never registered in two places.

This is the only identification that survives driver stations being shared between teams.
It comes from the team number set in the driver station software, not from anything about
the laptop — a MAC address would name the hardware, which on a shared field says nothing
about who is driving.

`172.16/12` rather than somewhere in `10/8` because team subnets are derived from team
numbers, and a team numbered under 100 lands on `10.0.NN.0/24` — team 33 would collide with
a staging subnet keyed the obvious way. The driver station does not care what its own
address is, only that it can reach `10.0.100.5`.

Turn it off with **Auto-Configure Teams** under Arena → Settings, which leaves the staging
subnets in place but stops bioarena acting on what connects to them. Registration is also
skipped once a match is running.

Staging is a Cisco-path feature. On `team_network_driver: local` an unregistered station
has no subnet, as before.

**Check the license level** with `show version`. Bioarena needs six concurrent SVIs with
addresses and the IOS DHCP server. IP Base has both; verify before wiring a field on
LAN Base.

**Telnet is likely disabled.** Recent IOS images ship SSH-only, so the VTY lines need
`transport input telnet` set over the console before bioarena can reach the switch at all.

> **Bioarena will not touch the switch or the AP unless network security is enabled.**
> Off means no errors, no switch activity, nothing at all — so if the field appears
> inert, check this first.
>
> It defaults to on. `config.yaml` seeds it on a first run only; from then on the
> checkbox under **Arena → Settings** is authoritative and survives restarts. Turn it off
> there for bench testing, or when a switch fails mid-session and you want to keep
> running matches.

#### First-time switch setup via console cable

A USB-to-RJ45 console cable is required. Connect it to the switch's port labelled
`CONSOLE` — on most units it is on the rear and physically identical to the Ethernet
ports. If the switch also has a USB console socket, unplug anything in it: on many Cisco
models an occupied USB console **disables the RJ45 console** silently.

[docs/console.py](docs/console.py) opens an interactive session using only the Python standard
library, so it needs no packages. That matters on older Raspbian releases, whose apt
repositories have moved to `archive.debian.org` and can no longer install `screen` or
`minicom` without repointing the sources list.

```bash
scp docs/console.py <USER>@10.0.100.5:~/
```

```bash
ssh <USER>@10.0.100.5
python3 ~/console.py            # /dev/ttyUSB0 at 9600 8N1; Ctrl-] to exit
python3 ~/console.py --list     # if unsure which device the cable is
```

Cisco's own USB console ports enumerate as `/dev/ttyACM0` rather than `/dev/ttyUSB0`.

**Run it from the Pi, not from a Windows dev machine.** It drives the terminal through
`termios`, which Windows has no equivalent of, so it exits with instructions rather than
running. The Pi is on the field with the switch anyway. If you would rather stay on
Windows, PuTTY does the same job: connection type Serial, the COM port from Device
Manager, 9600 baud, 8 data bits, 1 stop bit, no parity, no flow control.

Over SSH, run it with `ssh -t` so a terminal is allocated — without that the session
attaches but shows nothing:

```bash
ssh -t <USER>@10.0.100.5 "python3 ~/console.py"
```

Press Enter a few times after connecting — the console prints nothing until it receives
input, which is the most common reason a working cable looks dead. If you get garbage
characters instead of a prompt, the cable is fine and the baud rate is wrong; try
`-b 115200`.

If you see nothing at all through a full power cycle of the switch, the problem is not
the cable. A switch whose fans spin but whose `SYST` LED never lights is not booting, and
there is nothing on the console to talk to.

Assign the switch a static management IP on the field management subnet, mask
`255.255.255.0`. Fields are numbered so that several can be reached over one VPN without
overlapping: site `X` uses `10.X.100.0/24`, giving `10.X.100.3` for the switch alongside
`10.X.100.5` for the Pi and `10.X.100.2` for the access point.

| Site | Management subnet | Switch |
|------|-------------------|--------|
| 1    | 10.0.100.0/24     | 10.0.100.3 |
| 2    | 10.2.100.0/24     | 10.2.100.3 |
| 3    | 10.3.100.0/24     | 10.3.100.3 |

Within a site the last two octets never change, so every field is wired and documented
identically and only the second octet identifies which one you are on. Record the
assignment for each deployment in [docs/sites/](docs/sites).

Give the switch a hostname naming the site — `<Site>Switch` — so a console session makes
it obvious which field you are connected to.

Set an enable password when prompted — this is the password bioarena uses to authenticate over Telnet. Enter it in **Setup > Settings > Switch Password**.

VLAN assignments (fixed, managed automatically):

| Station | VLAN |
|---------|------|
| Red 1   | 10   |
| Red 2   | 20   |
| Red 3   | 30   |
| Blue 1  | 40   |
| Blue 2  | 50   |
| Blue 3  | 60   |

When a match loads, the controller pushes DHCP pool and IP configurations for each team's subnet over Telnet.

#### Team networks on the Pi (Layer 2 switches)

Everything above assumes a Layer 3 switch. If yours cannot route — anything with one SVI
and no DHCP server — the Pi can do that work instead.
Set in `config.yaml`:

```yaml
team_network_driver: local
local_network:
  trunk_interface: eth0
```

On every match load bioarena then rebuilds, on the Pi:

- `eth0.10` … `eth0.60` — one 802.1Q subinterface per station, each holding `10.TE.AM.4`,
  the same gateway address the switch would have handed out
- `/etc/dnsmasq.d/bioarena.conf` — one DHCP scope per station, `.20`–`.199`, matching the
  switch's pool bounds
- `net.ipv4.ip_forward=1` — the Pi routes between the team subnets and the FMS address

Only stations whose team actually changed are touched; an unchanged team list does
nothing at all. Registering one station therefore does not disturb a robot being driven
from another, which is what makes this usable in free practice, where teams come and go
while others are on the field. The first configuration after a restart rebuilds
everything, since subinterfaces left by the previous run outlive the process.

Leases are five minutes, deliberately shorter than the switch's seven days. The switch can
force a laptop to re-request by bouncing its port; the Pi cannot, because the laptop's
carrier is to the switch rather than to us. A short lease is the only self-correction
available, so a laptop moved between stations picks up its new subnet within a couple of
renewals instead of holding a dead address.

Team addressing does not change. Only the switch's job does: carry VLANs 10–60 tagged on
the Pi's port, and put each driver station port in its station's VLAN as an access port.
No routing, no DHCP, no SVIs.

**One-time setup on the Pi**

```bash
sudo apt install dnsmasq
sudo touch /etc/dnsmasq.d/bioarena.conf
sudo chown bioarena /etc/dnsmasq.d/bioarena.conf
```

The service runs `ip`, `sysctl`, and `systemctl restart dnsmasq`. `AmbientCapabilities=CAP_NET_ADMIN`
in `bioarena.service` covers the first two — the kernel grants `CAP_NET_ADMIN` holders the
same access as root under `/proc/sys/net`, so `ip_forward` is writable — but not the
restart, which systemd authorises over D-Bus.

Authorise that one unit for the service account in
`/etc/polkit-1/rules.d/50-bioarena-dnsmasq.rules`:

```javascript
polkit.addRule(function (action, subject) {
  if (action.id == "org.freedesktop.systemd1.manage-units" &&
      action.lookup("unit") == "dnsmasq.service" &&
      subject.user == "bioarena") {
    return polkit.Result.YES;
  }
});
```

The blunter alternative is `User=root` in `bioarena.service`, which needs no rule at all.
The polkit route grants exactly one unit to one account and is worth the extra file.

Polkit, not sudo: bioarena invokes `systemctl` directly, so a `sudoers` entry would never
be consulted. The JavaScript rule format needs polkit 0.106 or newer, which Trixie and
Bookworm have and Buster does not.

**Trade-offs.** Station detection still works — it reads the Pi's own ARP table rather
than the switch's, and reports the same stations. The switch address and password under
**Arena → Settings** go unused. All six teams' traffic now crosses the Pi's single NIC, so
this is a practice-field arrangement, not a competition one.

#### Half field on a Layer 2 switch

A reduced-station field on an eight-port Layer 2 switch — four driver stations
(R1, R2, R3, B1) with `team_network_driver: local` above. The port map below is the shape
any VLAN-capable switch follows; port numbers and the names its firmware gives membership
and PVID will differ.

Both the Pi and the AP are trunks. The VH-113 tags each team's SSID onto that team's
VLAN, so VLANs 10–60 have to reach it — an access port there leaves every robot
associated to WiFi and unable to reach anything.

| Port | Role | Membership | PVID |
|------|------|------------|------|
| 1 | Pi | untagged 1, tagged 10–60 | 1 |
| 2 | VH-113 AP | untagged 1, tagged 10–60 | 1 |
| 3 | R1 driver station | untagged 10 | 10 |
| 4 | R2 driver station | untagged 20 | 20 |
| 5 | R3 driver station | untagged 30 | 30 |
| 6 | B1 driver station | untagged 40 | 40 |
| 7–8 | spare (laptop, future station) | untagged 1 | 1 |

Carry all six VLANs on the two trunks even though 50 and 60 have nowhere to go yet. It
costs nothing, and adding B2 later is then one PVID change on port 7 rather than editing
trunk membership on two ports.

Bioarena still thinks in six stations: B2 and B3 need **BYP** checked in Match Play or the
match will not start. Nothing else needs configuring — a station with no team gets no
subinterface and no DHCP scope, so VLANs 50 and 60 stay dark on their own.

Set the switch's own management address statically to `10.0.100.3/24`, matching the address
reserved for the site switch above, and do that on an isolated link — smart switches
generally ship on a default address that collides with the usual home router range.

**Layer 2 footguns**

- **Membership and PVID are usually two separate settings.** Set membership only and
  untagged frames from the driver station laptops still land in the default VLAN — they get
  no lease, while the field badge stays green and the switch reports no error. If a station
  gets no address, check its PVID before anything else.
- **Keep the management port reachable while you work.** On a unit with no console port,
  moving the switch's own port out of the management VLAN can leave a factory reset as the
  only way back in, taking every VLAN with it.
- **Leave Switch Address blank** under **Arena → Settings**. There is nothing to Telnet.
  The badge reads `DISABLED` (blue) rather than red, which is the honest state — no switch
  configured, as opposed to a switch that cannot be reached.

**Keep a switch backup per site.** Export the configuration once the field works, and
restore it after a factory reset rather than re-entering the VLAN tables by hand. Re-export
whenever the port map changes: a backup that no longer matches the field restores cleanly
and then fails in ways that look like cabling.

Site records live in [docs/sites/](docs/sites) — one file per deployment, with its port
map, addresses, and switch backup. [docs/sites/richmond.md](docs/sites/richmond.md) is a
worked example to copy when standing up another field.

### Step 3 — Configure the field access point

The AP must run the Vivid-Hosting OpenWRT firmware with the REST API enabled. Bioarena communicates over HTTP. Set the AP address and password under **Arena → Settings**.

Specifically, bioarena needs plain HTTP on port 80 serving `POST /configuration` and
`GET /status`, the latter polled once a second and expected to report `ACTIVE`. That is
the **field firmware**, not the team-radio firmware — a radio in team mode serves no such
API and sits at `10.TE.AM.1` for whichever team it was last provisioned for.

**First-time VH-113 setup**

1. Laptop straight into the AP, static `192.168.69.100/24`, browse `http://192.168.69.1` —
   Vivid's default management address.
2. Confirm or flash the field-mode image, following Vivid Hosting's own instructions.
3. Set the AP's static address to `10.0.100.2` on the management subnet.
4. Set the radio channel. It must match **AP Channel** under Arena → Settings, which is
   what gets pushed on every match load.

**The AP password is normally blank.** The practice firmware exposes no API token, and
that is a supported configuration rather than a workaround: bioarena adds the
`Authorization: Bearer` header only when the password field is non-empty, so a blank field
means unauthenticated calls, which is what this firmware expects. Confirm with:

```bash
curl -s http://10.0.100.2/status
```

JSON back means leave **AP Password** empty. A `401` means this build does want a token,
and on Vivid's field images that is usually the web UI's admin password.

**Enter the address without a scheme.** `10.0.100.2`, not `http://10.0.100.2` — the code
prepends `http://`, so a typed prefix produces `http://http://10.0.100.2` and every poll
fails.

When a match loads, the controller pushes one SSID + WPA2 key per team (six total). The
SSID is the team number and the key is that team's WPA key from its record under
**Teams** — so each robot's VH-109 radio, provisioned for its team as it would be for a
competition, joins the correct VLAN with no field-side changes.

A team with no WPA key set is provisioned with an empty one. Set it on the team record
before the robot will associate.

### Step 4 — Verify Pi reachability

The Pi must be able to reach:

| Destination          | Protocol | Port |
|----------------------|----------|------|
| Field AP             | HTTP     | 80   |
| Cisco switch         | Telnet   | 23   |
| Each robot subnet    | UDP      | 1160 |

Test from the Pi:

```bash
ping 10.0.100.5        # self
curl http://<AP_IP>/status
telnet <SWITCH_IP> 23
```

### Step 5 — Serve time from the Pi

**Nothing on a practice field has a battery-backed clock.** The Pi restores the last known
time from `fake-hwclock` at boot; a Catalyst comes up believing it is 2004. With no route
to the internet neither corrects itself, so timestamps across the field disagree by years —
and the moment that costs you is the one where a match went wrong and you are trying to
line up the switch's log against bioarena's.

The field controller is the only sensible source, so it serves time to everything else.

Part of the first deploy, not an optional extra — it needs internet, so it has to happen
before the Pi joins the field network.

**On the Pi.** Raspberry Pi OS ships `systemd-timesyncd`, which is a client only and cannot
serve; there is no setting that changes this. Chrony can, and Debian's package replaces
timesyncd rather than running alongside it, since two daemons steering one clock is worse
than either alone:

```bash
sudo apt install chrony
```

```bash
scp docs/chrony-bioarena.conf <USER>@10.0.100.5:~/
```

```bash
sudo mv ~/chrony-bioarena.conf /etc/chrony/conf.d/bioarena.conf
sudo systemctl restart chrony
```

The drop-in carries `local stratum 10`, which is the part that matters: chrony otherwise
refuses to answer while it is unsynchronised, which on an isolated field is always. Stratum
10 is deliberately poor, so a real upstream source wins if the Pi ever reaches one.

**On the switch.**

```
configure terminal
ntp server 10.0.100.5
end
```

Then `write memory`. Confirm with `show ntp associations` after a few minutes — the switch
polls slowly, so it is not instant.

**This makes the field's clocks consistent, not correct.** Correct requires the Pi reaching
a real NTP server at some point, or being set by hand:

```bash
sudo timedatectl set-time "2026-08-08 14:30:00"
```

Worth doing before a session you might need to review afterwards. Consistent-but-wrong is
still enough to correlate two logs; disagreeing by years is not.

### Team subnet addressing

Each team's subnet is derived from the team number. Team 4834 uses `10.48.34.x`:

```
10. [first two digits] . [last two digits] . x
     48                   34
```

| Device         | Address          |
|----------------|------------------|
| Switch gateway | 10.TE.AM.4       |
| Robot (RoboRIO)| 10.TE.AM.2       |
| DS laptop      | 10.TE.AM.5 (DHCP)|

The DHCP pool reserves `.1`–`.19` and `.200`–`.254`. Addresses `.20`–`.199` are available for laptops and other devices.

## Usage

### Starting and stopping the service

```bash
sudo systemctl start bioarena
sudo systemctl stop bioarena
sudo systemctl restart bioarena
sudo systemctl status bioarena
```

### Viewing logs

Service output:

```bash
journalctl -u bioarena -f
```

Per-team driver-station packet logs are written to `logs/` on the Pi, one CSV per team
per match. Browse and download them from any device on the field network:

```
http://10.0.100.5:8080/logs/
```

The listing has no authentication, matching how `/static/` is served — anyone on the
field network can read them. They sit outside `static/` only so the deploy step does not
copy them to the Pi.

Or pull them off over SSH:

```bash
scp <USER>@10.0.100.5:/opt/bioarena/logs/\*.csv ./
```

The directory grows with every match. Clear it periodically:

```bash
ssh <USER>@10.0.100.5 "sudo rm -f /opt/bioarena/logs/*.csv"
```

### Running a practice match

Match Play does not record scores or results — it is a pure practice tool. Each match is a standalone timed run.

1. Open `http://10.0.100.5:8080` in a browser on any device on the field network.
2. Go to **Setup > Teams** and enter the team numbers for each station.
3. Go to **Match Play**.
4. Type team numbers into the station fields and click **Register** to assign them, or check **BYP** to bypass empty stations.
5. Wait for assigned stations to show a DS connection (or bypass them), then click **Start Match**.
6. After the match ends, click **Clear Match** to reset and run another round.

Match timing defaults (2026 REBUILT):

| Period  | Duration |
|---------|----------|
| Auto    | 20 s     |
| Pause   | 3 s      |
| Teleop  | 140 s    |

### Running free practice

Free practice has no timers: registered robots are drivable continuously until the
operator stops them. Register each station first — the team's SSID and its VLAN subnet are
created on registration, so a driver station plugged in beforehand has nothing to get an
address from.

Three controls, and the difference between the last two matters:

| Control | Effect |
|---------|--------|
| **ENABLE FIELD** | Starts free practice, or resumes robots after a halt |
| **DISABLE FIELD** | Halts all robot operation. Teams stay registered, SSIDs stay up, team subnets stay configured, driver stations stay connected. **ENABLE FIELD** resumes immediately |
| **Reset Field** | Clears every slot, drops all SSIDs and team subnets, disconnects every driver station, and returns to setup |

Reach for **DISABLE FIELD** between runs. **Reset Field** is for ending the session or
starting over — after it, every station has to be registered again, and laptops re-request
an address, which takes up to one DHCP lease unless the port is unplugged and replugged.

Per-station E-stops remain functional throughout and are independent of both.

### Ports used by the service

| Port | Protocol | Purpose                          |
|------|----------|----------------------------------|
| 8080 | TCP/HTTP | Web UI and WebSocket updates     |
| 1750 | TCP      | Driver Station connection        |
| 1121 | UDP      | Enable/disable packets to DS     |
| 1160 | UDP      | Status packets from DS           |

## Configuration

Match timing and hardware drivers are configured in Settings inside the web UI. No config file is required for basic operation.

To change match timing, go to **Setup > Settings** and adjust the duration fields. Defaults:

| Setting                 | Default |
|-------------------------|---------|
| Auto duration           | 20 s    |
| Pause duration          | 3 s     |
| Teleop duration         | 140 s   |
| HTTP port               | 8080    |

Network credentials (AP address, AP password, switch address, switch password) are also set in the Settings page and stored in the local database.

## Field hardware

**Hub LEDs (DMX over Ethernet)**

The 2026 Hub lighting runs E1.31 sACN, ported from upstream cheesy-arena. It is
configured from **Arena → LEDs → DMX Hub LEDs** in the web UI and stored in the
database, so it survives a restart. A blank address disables output.

Practice fields with cheaper fixtures can override the layout — one fixture per alliance
Hub instead of eight — and select a fixture capability so per-pixel sequences degrade to
a solid colour. See
[docs/prd-half-field-match-simulation.md](docs/prd-half-field-match-simulation.md) for
the addressing rules.

**Field lights (serial)**

A separate, simpler interface for an Arduino-driven light or sound cue, independent of
the Hub LEDs:

```go
type FieldLights interface {
    SetState(state LightingState) error
}
```

Configured in `config.yaml`. Supported `field_lights_driver` values are `none` (the
default) and `serial`:

```yaml
field_lights_driver: "serial"
field_lights_port: "/dev/ttyUSB0"
field_lights_baud: 9600
field_lights_command: "START\n"
```

**E-stop panel**

> For full wiring diagrams, component list, and step-by-step assembly, see **[docs/hardware-wiring.md](docs/hardware-wiring.md)**.

Each alliance can have a dedicated Raspberry Pi wired to 7 inputs:

| Pin role           | Station              | Channels |
|--------------------|----------------------|----------|
| station1_estop     | R1 or B1 (e-stop)    | NC + NO  |
| station1_astop     | R1 or B1 (a-stop)    | NO       |
| station2_estop     | R2 or B2 (e-stop)    | NC + NO  |
| station2_astop     | R2 or B2 (a-stop)    | NO       |
| station3_estop     | R3 or B3 (e-stop)    | NC + NO  |
| station3_astop     | R3 or B3 (a-stop)    | NO       |
| field_estop        | all stations (e-stop) | NC + NO |

Wiring: e-stop buttons are **dual-channel**. Three conductors run to each button — common GND, the NC contact, and the NO contact — with the internal pull-up on both GPIO lines. Released, NC reads LOW and NO reads HIGH; pressed, they swap. Any other combination is a wiring fault, which stops the field and blocks match start until the wiring is repaired. A-stops stay single-channel NO.

Use latching mushroom-head buttons for e-stops and momentary pushbuttons for a-stops. The panel samples its pins locally at 100 Hz and reports the state of every configured input on each poll.

Recommended static IPs: `10.0.100.11` (red panel), `10.0.100.12` (blue panel).

Create `estop-panel.yaml` in the panel Pi's working directory:

```yaml
alliance: "red"       # "red" or "blue"
http_port: 8765
gpio_chip: "gpiochip0"
pins:
  # BCM GPIO numbers. {nc: 0, no: N} or a bare N is single-channel (no fault
  # detection); no: 0 means not wired and is skipped.
  station1_estop: {nc: 17, no: 4}
  station1_astop: 27
  station2_estop: {nc: 22, no: 12}
  station2_astop: 23
  station3_estop: {nc: 24, no: 13}
  station3_astop: 25
  field_estop: {nc: 5, no: 6}
```

Build and deploy:

```bash
./build-pi.sh          # produces estop-panel-pi alongside bioarena-pi
```

On the panel Pi, once: create the same service account as the field controller but with
`/opt/estop-panel`, and add it to the `gpio` group so it can read the pins.

```bash
sudo useradd --system --home-dir /opt/estop-panel --shell /usr/sbin/nologin bioarena
sudo usermod -aG gpio bioarena
sudo mkdir -p /opt/estop-panel
sudo chown -R bioarena:bioarena /opt/estop-panel
sudo chmod 2775 /opt/estop-panel
sudo usermod -aG bioarena <USER>
```

Then deploy:

```bash
scp estop-panel-pi <USER>@10.0.100.11:/opt/estop-panel/estop-panel
scp estop-panel.yaml <USER>@10.0.100.11:/opt/estop-panel/
# Edit ExecStartPre IP in estop-panel.service, then:
scp cmd/estop-panel/estop-panel.service <USER>@10.0.100.11:~/
# On the panel Pi:
chmod +x /opt/estop-panel/estop-panel
sudo mv ~/estop-panel.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now estop-panel
```

As on the field controller, leave the copied files owned by your login user — the panel
only reads them, and chowning them to the service account makes the next `scp` fail.

The `gpio` group membership is the one that bites: without it the panel starts, logs that
it cannot open the GPIO chip, and reports no stops — a field that looks fine and has no
working e-stops.

Wire the main bioarena to the panel by adding to `config.yaml` and restarting:

```yaml
red_estop_panel:
  host: "http://10.0.100.11:8765"
blue_estop_panel:
  host: "http://10.0.100.12:8765"
```

Panel addresses can also be changed live via **Setup > Settings** without a restart.

The field runs normally without panel Pis; missing panels log a warning and return no stops.

## Development

Go 1.23+ — see `go.mod`.

**Repository layout**

| Path | Contents |
|------|----------|
| `main.go` | Entry point: loads `config.yaml`, builds the arena, starts the web server |
| `field/` | Arena state machine, driver station connections, match flow, free practice |
| `game/` | Scoring and match timing rules |
| `network/` | Access point, switch, and local team network drivers |
| `hardware/`, `plc/`, `led/` | Field hardware: e-stop panels, lights, sACN output |
| `web/`, `templates/`, `static/` | HTTP handlers, HTML templates, client-side assets |
| `model/` | BoltDB datastore and record types |
| `websocket/` | WebSocket plumbing for live UI updates |
| `cmd/estop-panel/` | Separate binary for the alliance e-stop panel Pis |
| `docs/` | Wiring, upstream divergences, serial console, site configurations |

The runtime database is BoltDB in `event.db`, created on first start and not tracked.

**Style**

Standard Go: tabs, `CamelCase` for exported names, `camelCase` otherwise, and short
domain-named packages matching the directories above. Run `gofmt` before submitting.

**First-time setup**

Node LTS is required for the JavaScript tests, which the pre-push hook runs:

```bash
winget install OpenJS.NodeJS.LTS
```

Then, in a new terminal so the updated `PATH` is picked up:

```bash
npm install
```

```bash
git config core.hooksPath .githooks
```

That last line enables [.githooks/pre-push](.githooks/pre-push), which runs both test
suites and refuses the push if either fails. Git stores `core.hooksPath` per clone, so
every clone needs it once.

**Run Go tests**

```bash
go test ./...
```

Tests are `*_test.go` files co-located with the package they cover. Target one with
`go test ./field -run TestName`. Prefer table-driven tests where a behaviour has several
cases.

**Run JavaScript tests**

Client-side behaviour (DOM manipulation, WebSocket message handlers, UI state) is tested with [Jest](https://jestjs.io/) using a jsdom environment.

```bash
npm install        # first time only
npm run test:js
```

Tests live in `static/js/__tests__/`. Each JS source file that contains non-trivial logic should have a corresponding `*.test.js` file. To make a file importable by Jest, add a `module.exports` guard at the bottom:

```javascript
if (typeof module !== "undefined") {
  module.exports = { myFunction };
}
```

Client-side behaviour cannot be caught by the Go tests, so any change to a `.js` file
needs Jest coverage of its state transitions — what a handler does when the server reports
an empty slot versus an occupied one, whether a user-typed value survives a status push,
and so on. Both suites must pass before committing; the pre-push hook enforces it.

**Run locally (no robots)**

```bash
go build -o bioarena
./bioarena
```

Open `http://localhost:8080`. No network hardware is required for testing.

**Build for Pi**

```bash
./build-pi.sh
```

Output: `bioarena-pi` and `estop-panel-pi` (ARM, statically linked, ready to copy to the
Pi). Add `ARCH=arm GOARM=7` for a 32-bit image.

### Raspberry Pi OS releases

Bioarena needs systemd, `ip(8)`, and — for `team_network_driver: local` — dnsmasq. All
three are present on every current release, so the OS choice comes down to what else it
changes.

| | Trixie / Bookworm | Buster and earlier |
|---|---|---|
| Build flag | `./build-pi.sh` (arm64) | `ARCH=arm GOARM=7 ./build-pi.sh` |
| Static IP | `nmcli` (NetworkManager) | `/etc/dhcpcd.conf` |
| VLAN subinterfaces | Need the NetworkManager drop-in below | Nothing extra |
| apt | Works | End of life; repositories moved to `archive.debian.org` |

**The NetworkManager drop-in is required for `team_network_driver: local`.**
NetworkManager claims every interface that appears, including the VLAN subinterfaces
bioarena creates on each match load. Left managed, it starts a DHCP client on each one and
can strip the gateway address bioarena just assigned — so a station loses its subnet
seconds after being registered, which reads as a flaky field rather than a configuration
problem.

```bash
scp docs/99-bioarena-unmanaged.conf <USER>@10.0.100.5:~/
```

On the Pi:

```bash
sudo mv ~/99-bioarena-unmanaged.conf /etc/NetworkManager/conf.d/
```

```bash
sudo systemctl reload NetworkManager
```

Edit the interface pattern inside if the field NIC is not `eth0`.

**Verify the GPIO chip name** if you use e-stop panels or GPIO field lights. Both default
to `gpiochip0`, which is correct for a Pi 4 today, but chip numbering has moved between
kernel versions and hardware generations. Confirm on the Pi with `gpiodetect`, and set
`gpio_chip` in `estop-panel.yaml` if it differs.

## Documentation

- [docs/hardware-wiring.md](docs/hardware-wiring.md) — field hardware wiring, opto-isolation, e-stop panel assembly.
- [docs/prd-half-field-match-simulation.md](docs/prd-half-field-match-simulation.md) — requirements for 1v0 half-field REBUILT 2026 simulation: AUTO outcome selection, FMS Game Data, DMX HUB light.
- [docs/upstream-divergences.md](docs/upstream-divergences.md) — where this fork differs from cheesy-arena, which differences are candidates to send upstream, and which files are kept byte-identical.
- [docs/console.py](docs/console.py) — serial console for the field switch, standard library only; Linux and macOS.
- [docs/99-bioarena-unmanaged.conf](docs/99-bioarena-unmanaged.conf) — NetworkManager drop-in keeping it away from the VLAN subinterfaces, required on Trixie and Bookworm with `team_network_driver: local`.
- [docs/chrony-bioarena.conf](docs/chrony-bioarena.conf) — chrony drop-in making the Pi the field's time source, so switch and controller logs can be correlated.
- [docs/switch-bootstrap.py](docs/switch-bootstrap.py) — brings a factory or reset Catalyst to the point bioarena can configure it over Telnet: address, passwords, boot image. Console cable, one command.
- [docs/sites/](docs/sites) — one record per deployed field: addresses, switch port map, and switch backup. [richmond.md](docs/sites/richmond.md) is the example to copy.

## Contributing

- Open a [GitHub issue](https://github.com/Team254/cheesy-arena/issues) for bugs or feature requests.
- Send a pull request with a clear summary.
- Include test notes: the exact commands run, for example `go test ./...` and
  `npm run test:js`.
- Include screenshots for any change to `web/`, `templates/`, or `static/`.

Commit messages use short imperative sentences, e.g. `Fix driver station TCP reads`, and
often carry an issue or PR number in parentheses: `Fix driver station TCP reads (#258)`.

## License

Teams may use this software freely for practice, scrimmages, and off-season events. See [LICENSE](LICENSE) for details.
