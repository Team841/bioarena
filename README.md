# Practice Field Controller

A Raspberry Pi service for running FRC practice sessions. Controls up to 6 robots across red and blue alliances. Runs timed auto and teleop periods. Manages the field access point and VLAN isolation automatically. Accessible from any browser on the field network.

## Requirements

- Raspberry Pi 4 (armv7 / 32-bit Raspberry Pi OS recommended)
- [Go 1.23+](https://golang.org/dl/) on your build machine
- Vivid-Hosting VH-113 field access point (running OpenWRT)
- Layer 3 managed switch with the IOS DHCP server (Catalyst 3560-CX or similar; see [Step 2](#step-2--configure-the-managed-switch))
- Static IP assigned to Pi (recommend `10.0.100.5`)

## Install

**Build the Pi binary**

Run this on your development machine (not on the Pi):

```bash
./build-pi.sh
```

This cross-compiles two ARM binaries: `bioarena-pi` for the field controller and
`estop-panel-pi` for the e-stop panel Pis. The `-pi` suffix keeps them from shadowing a
local build. Running the script also prints the full deploy sequence for both.

**Copy files to the Pi**

If bioarena is already installed, stop it first — Linux refuses to overwrite a running
executable, and `scp` reports that as a bare `Failure`:

```bash
ssh pi@<PI_IP> "sudo systemctl stop bioarena"
```

```bash
scp bioarena-pi pi@<PI_IP>:~/bioarena/
scp -r static templates pi@<PI_IP>:~/bioarena/
scp bioarena.service pi@<PI_IP>:~/
```

Keep the `bioarena-pi` filename — `bioarena.service` runs
`/home/pi/bioarena/bioarena-pi`, so renaming it on copy leaves the service unable to
start.

Then make it executable on the Pi:

```bash
chmod +x ~/bioarena/bioarena-pi
```

**Install the systemd service (run on the Pi)**

```bash
sudo mv ~/bioarena.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable bioarena
sudo systemctl start bioarena
```

The service file is **moved**, not copied, so it ends up at
`/etc/systemd/system/bioarena.service` and will not be found in `~/bioarena/`. Confirm
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

FRC Driver Station software is hardcoded to contact its FMS at `10.0.100.5` on ports `1750` (TCP) and `1121`/`1160` (UDP). The Pi must live at that address on the wired field network. Each robot lives on its own team-number-derived subnet isolated by a VLAN. The access point handles wireless; the switch enforces isolation.

### Topology

```
                        ┌─────────────────────────────────────┐
                        │   Cisco L3 Managed Switch            │
                        │                                       │
          ┌─────────────┤ Trunk port        6 x access ports   │
          │             │                   (one per station)   │
          │             └──────────────────────┬────────────────┘
          │                                    │ (wired robot connections)
    ┌─────┴──────┐                      ┌──────┴──────┐
    │ Raspberry  │                      │  Robots      │
    │ Pi 4       │                      │  (RoboRIO)   │
    │ 10.0.100.5 │                      │  10.TE.AM.xx │
    └─────┬──────┘                      └─────────────┘
          │
          │ (HTTP to AP, Telnet to switch)
          │
    ┌─────┴────────────┐
    │ Vivid-Hosting    │
    │ VH-113 AP        │
    │ (OpenWRT)        │
    └─────┬────────────┘
          │ (WiFi — one SSID per team)
          │
    ┌─────┴──────────────┐
    │  DS Laptops        │
    │  (one per station) │
    │  10.TE.AM.5        │
    └────────────────────┘
```

### Step 1 — Assign a static IP to the Pi

The Pi must have `10.0.100.5` on the interface connected to the switch. The systemd service handles this automatically via:

```
ExecStartPre=/sbin/ip addr add 10.0.100.5/24 dev eth0
```

If you need a permanent static IP (survives reboots without the service), edit `/etc/dhcpcd.conf` on the Pi:

```
interface eth0
static ip_address=10.0.100.5/24
```

Do not put the Pi on a robot subnet (`10.TE.AM.x`). Use a dedicated management subnet such as `10.0.100.0/24`.

### Step 2 — Configure the managed switch

Bioarena reconfigures the switch over Telnet at every match load. You do the one-time
setup by hand; bioarena does the per-match VLAN and DHCP work.

**Switch requirements.** Bioarena issues six concurrent SVIs with IP addresses and six
DHCP pools, so the switch must be Layer 3 capable with the IOS DHCP server — a
3550/3560/3750-class unit. A 2960 will not work: it allows only one active SVI, and the
LAN Lite images have no DHCP server.

One-time setup:

1. Enable Telnet on the VTY lines with a password (`transport input telnet`). Recent IOS
   images ship SSH-only or with the lines unconfigured.
2. **Set the enable password to the same value as the VTY password.** Bioarena has a
   single password field and sends it for both. Different passwords fail silently: it
   authenticates to the VTY line, never reaches privileged mode, and every configuration
   command is discarded with no error.
3. Create VLANs 10, 20, 30, 40, 50, 60 (one per alliance station).
4. Set the Pi's port as a trunk carrying all VLANs.
5. Set each robot's port as an access port in the correct VLAN.
6. Enable `ip routing`.

The switch address and password are set in the web UI under **Arena → Settings**.

> **Bioarena will not touch the switch or the AP unless network security is enabled**,
> and that flag is read from `config.yaml` on every start — setting it in the web UI is
> overwritten on the next restart. Set `network_security_enabled: true` in
> `config.yaml` on the Pi and restart the service. Symptom if you miss it: no errors, no
> switch activity, nothing at all.

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
scp docs/console.py pi@10.0.100.5:~/
```

```bash
ssh pi@10.0.100.5
python3 ~/console.py            # /dev/ttyUSB0 at 9600 8N1; Ctrl-] to exit
python3 ~/console.py --list     # if unsure which device the cable is
```

Cisco's own USB console ports enumerate as `/dev/ttyACM0` rather than `/dev/ttyUSB0`.

Press Enter a few times after connecting — the console prints nothing until it receives
input, which is the most common reason a working cable looks dead. If you get garbage
characters instead of a prompt, the cable is fine and the baud rate is wrong; try
`-b 115200`.

If you see nothing at all through a full power cycle of the switch, the problem is not
the cable. A switch whose fans spin but whose `SYST` LED never lights is not booting, and
there is nothing on the console to talk to.

When prompted by the setup wizard, assign the switch a static management IP on the field management subnet. Each site uses `10.X.100.3/24` where `X` is the site number:

| Site         | Switch IP     |
|--------------|---------------|
| Richmond lab | 10.0.100.3    |
| Site 2       | 10.2.100.3    |
| Site 3       | 10.3.100.3    |
| Site 4       | 10.4.100.3    |
| Site 5       | 10.5.100.3    |

Subnet mask: `255.255.255.0`

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

### Step 3 — Configure the field access point

The AP must run the Vivid-Hosting OpenWRT firmware with the REST API enabled. Bioarena communicates over HTTP. Set the AP address and password in Settings > Network.

When a match loads, the controller pushes one SSID + WPA2 key per team (six total). Driver Station laptops connect to their team's SSID and land on the correct VLAN.

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
scp pi@10.0.100.5:~/bioarena/logs/\*.csv ./
```

The directory grows with every match. Clear it periodically:

```bash
ssh pi@10.0.100.5 "rm -f ~/bioarena/logs/*.csv"
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

Each alliance can have a dedicated Raspberry Pi wired to 7 GPIO inputs:

| Pin role           | Station              |
|--------------------|----------------------|
| station1_estop     | R1 or B1 (e-stop)    |
| station1_astop     | R1 or B1 (a-stop)    |
| station2_estop     | R2 or B2 (e-stop)    |
| station2_astop     | R2 or B2 (a-stop)    |
| station3_estop     | R3 or B3 (e-stop)    |
| station3_astop     | R3 or B3 (a-stop)    |
| field_estop        | all stations (e-stop) |

Wiring: NO (normally-open) contacts; one side to a GPIO pin, the other to GND. Internal pull-up enabled; pin reads LOW (0) when the button is pressed (active-low). Use latching mushroom-head buttons for e-stops and momentary pushbuttons for a-stops. The panel reports current pin state on every poll.

Recommended static IPs: `10.0.100.11` (red panel), `10.0.100.12` (blue panel).

Create `estop-panel.yaml` in the panel Pi's working directory:

```yaml
alliance: "red"       # "red" or "blue"
http_port: 8765
gpio_chip: "gpiochip0"
pins:
  station1_estop: 17  # BCM GPIO; 0 = not wired, skipped
  station1_astop: 27
  station2_estop: 22
  station2_astop: 23
  station3_estop: 24
  station3_astop: 25
  field_estop: 5
```

Build and deploy:

```bash
./build-pi.sh          # produces estop-panel binary alongside bioarena
scp estop-panel pi@10.0.100.11:~/estop-panel/
scp estop-panel.yaml   pi@10.0.100.11:~/estop-panel/
# Edit ExecStartPre IP in estop-panel.service, then:
scp cmd/estop-panel/estop-panel.service pi@10.0.100.11:~/
# On the panel Pi:
sudo mv ~/estop-panel.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now estop-panel
```

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

**Run Go tests**

```bash
go test ./...
```

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

Output: `bioarena` (ARM, statically linked, ready to copy to the Pi).

## Documentation

- [docs/hardware-wiring.md](docs/hardware-wiring.md) — field hardware wiring, opto-isolation, e-stop panel assembly.
- [docs/prd-half-field-match-simulation.md](docs/prd-half-field-match-simulation.md) — requirements for 1v0 half-field REBUILT 2026 simulation: AUTO outcome selection, FMS Game Data, DMX HUB light.

## Contributing

- Open a [GitHub issue](https://github.com/Team254/cheesy-arena/issues) for bugs or feature requests.
- Send a pull request with a clear summary and `go test ./...` results.
- Include screenshots for any UI changes.

Commit messages use short imperative sentences, e.g. `Fix driver station TCP reads`.

## License

Teams may use this software freely for practice, scrimmages, and off-season events. See [LICENSE](LICENSE) for details.
