# Watchtower remote control

Lets a Watchtower scoring system on the network run this field, so a match is loaded, started and
ended once instead of separately on two systems. It is off until it is turned on here.

The field still runs every match on its own clock, and keeps running if the link drops. E-stop panels
and the PLC do not go through this link. The stop commands below travel over the network and do not
replace them.

## Turning it on

Arena > Settings > Field > **Watchtower Remote Control**:

| Setting | Meaning |
|---|---|
| Watchtower address | The IP of the computer running Watchtower. Blank turns remote control off. Connections from any other address are refused, as with the FUEL counter. It changes per network, so re-check it at each venue. |
| Key | Shared with Watchtower. Leave blank and save to have one made. At least 8 characters. |
| Allow Watchtower to start matches | Starting enables robots, so this is separate and off by default. Off, Watchtower can do everything else and still follows a match started from this field. |

Then in Watchtower, `/control` > Setup > **Field control**: this field's address, web port and key.

The **WATCHTOWER** badge on the Match page shows the state: `WATCHTOWER CAN START`, `WATCHTOWER`
(connected, starts only from here) or `WATCHTOWER OFFLINE`. Blanking the address drops a connection
that is already open within a second.

## Protocol

`GET /remote_control/websocket` with the header `X-Remote-Key: <key>`, from the configured address.
Anything else gets HTTP 403. The key is sent in the clear, as the admin login is on this network.

Messages are `{"type", "data"}`, like the Match page's websocket. The field sends the same
`matchTiming`, `arenaStatus`, `matchLoad` and `matchTime` messages. `arenaStatus` also carries
`RemoteControlStatus`, `RemoteStartAllowed` and `StartProblem`, the reason a start would be refused.

Each command's `data` has an `Id`, and is answered with `{"type": "result", "data": {"Id", "Error"}}`,
where `Error` is empty on success. An `arenaStatus` is sent just before each result.

| Command | Arguments | Notes |
|---|---|---|
| `loadLineup` | `Name`, `Red1`..`Blue3` (0 = empty) | Between matches only. Adds teams it has not seen. Clears a bypass left on a station that now has a team. |
| `startMatch` | `Red1`..`Blue3` | Needs "Allow Watchtower to start matches" and the same lineup the field has. The field's normal start checks apply. |
| `abortMatch` | | |
| `setEStop` | `Station`, `On` | Sets the value, unlike the Match page's toggle, so a repeated command cannot release a stop. `On: false` is refused during a match. |
| `setBypass` | `Station`, `On` | `On: false` is refused during a match, since it would enable a robot. |
| `bypassEmptyStations` | | |
| `clearFieldEStop` | | Refused during a match. |
| `setAutoWinnerMode` | `Mode` | `random`, `red`, `blue` or `counted`. Refused during a match. |
