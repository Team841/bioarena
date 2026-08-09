// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Methods for configuring a Cisco Catalyst 3560-CX switch for team VLANs.

package network

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/team841/bioarena/model"
	"net"
	"sync"
	"time"
)

const (
	switchConfigBackoffDurationSec = 5
	switchConfigPauseDurationSec   = 2
	switchTeamGatewayAddress       = 4
	switchTelnetPort               = 23
)

const (
	red1Vlan  = 10
	red2Vlan  = 20
	red3Vlan  = 30
	blue1Vlan = 40
	blue2Vlan = 50
	blue3Vlan = 60
)

// dsPortInterfaces is the driver station port for each alliance station, in station order.
// A Catalyst 3560-CX with the stations on its first six ports is the assumed field, so
// wire R1 to Gi0/1 and so on; the trunks to the Pi and the access point go on the ports
// above these.
//
// These are shut and reopened around a VLAN change, which is what makes a laptop
// re-request an address on its new subnet rather than keeping the previous match's. Only
// the stations whose team changed are cycled.
var dsPortInterfaces = [6]string{
	"GigabitEthernet0/1",
	"GigabitEthernet0/2",
	"GigabitEthernet0/3",
	"GigabitEthernet0/4",
	"GigabitEthernet0/5",
	"GigabitEthernet0/6",
}

type Switch struct {
	address               string
	port                  int
	password              string
	dnsServer             string
	mutex                 sync.Mutex
	configBackoffDuration time.Duration
	configPauseDuration   time.Duration
	Status                string

	// applied is the team per station as the switch is currently configured, by team
	// number, with 0 for an empty station. Only meaningful once synced is true.
	applied [6]int

	// synced records whether the switch's configuration is known. False at startup, since
	// the switch outlives the process and may have been changed by hand, and after any
	// failure, which leaves it half configured. Either makes the next call a full
	// reconciliation rather than a difference.
	synced bool
}

var ServerIpAddress = "10.0.100.5" // The DS will try to connect to this address only.

// SwitchConfig collects the switch settings. A struct rather than positional arguments
// because they are all strings, and a transposed pair would misconfigure a field without
// failing.
type SwitchConfig struct {
	Address   string
	Password  string
	DnsServer string
}

func NewSwitch(config SwitchConfig) *Switch {
	return &Switch{
		address:               config.Address,
		port:                  switchTelnetPort,
		password:              config.Password,
		dnsServer:             config.DnsServer,
		configBackoffDuration: switchConfigBackoffDurationSec * time.Second,
		configPauseDuration:   switchConfigPauseDurationSec * time.Second,
		Status:                "UNKNOWN",
	}
}

func (sw *Switch) GetStatus() string {
	return sw.Status
}

// Sets up wired networks for the given set of teams.
func (sw *Switch) ConfigureTeamEthernet(teams [6]*model.Team) error {
	// Make sure multiple configurations aren't being set at the same time.
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	// With no address there is nothing to configure. Without this the Telnet dial fails
	// on every match load and pins the badge red, which reads as a broken switch rather
	// than an absent one. GetStationForTeamId already guards the same way.
	if sw.address == "" {
		sw.Status = "DISABLED"
		return nil
	}

	desired := teamIds(teams)

	// A full reconciliation when the switch's state is unknown: it outlives the process
	// and may have been changed by hand in between.
	full := !sw.synced
	if !full && desired == sw.applied {
		sw.Status = "ACTIVE"
		return nil
	}

	sw.Status = "CONFIGURING"

	rebuild := [6]bool{}
	for i := range rebuild {
		rebuild[i] = full || desired[i] != sw.applied[i]
	}

	// Shut down DS ethernet ports to prevent conflicts during VLAN reconfiguration. Only
	// the stations being rebuilt: cycling a port disconnects the driver station behind it,
	// and in free practice the others are mid-drive.
	if portsDown := portCommands(rebuild, "shutdown"); portsDown != "" {
		if _, err := sw.runConfigCommand(portsDown); err != nil {
			return sw.fail(err)
		}
	}

	// Remove the old team VLANs to reset the switch state.
	removeTeamVlansCommand := ""
	for i, vlan := range vlanForStation {
		if !rebuild[i] {
			continue
		}
		removeTeamVlansCommand += fmt.Sprintf(
			"interface Vlan%d\nno ip address\nno ip dhcp pool dhcp%d\n", vlan, vlan,
		)
	}
	_, err := sw.runConfigCommand(removeTeamVlansCommand)
	if err != nil {
		return sw.fail(err)
	}
	time.Sleep(sw.configPauseDuration)

	// Create the new team VLANs.
	addTeamVlansCommand := ""
	addTeamVlan := func(team *model.Team, vlan int) {
		if team == nil {
			return
		}
		teamPartialIp := fmt.Sprintf("%d.%d", team.Id/100, team.Id%100)

		// Omitted entirely when unconfigured. Handing out a resolver the team subnet
		// cannot reach makes every lookup wait for a timeout instead of failing fast,
		// which is worse than having no DNS at all.
		dnsServerCommand := ""
		if sw.dnsServer != "" {
			dnsServerCommand = fmt.Sprintf("dns-server %s\n", sw.dnsServer)
		}

		addTeamVlansCommand += fmt.Sprintf(
			"ip dhcp excluded-address 10.%s.1 10.%s.19\n"+
				"ip dhcp excluded-address 10.%s.200 10.%s.254\n"+
				"ip dhcp pool dhcp%d\n"+
				"network 10.%s.0 255.255.255.0\n"+
				"default-router 10.%s.%d\n"+
				"%s"+
				"lease 7\n"+
				"interface Vlan%d\nip address 10.%s.%d 255.255.255.0\n",
			teamPartialIp,
			teamPartialIp,
			teamPartialIp,
			teamPartialIp,
			vlan,
			teamPartialIp,
			teamPartialIp,
			switchTeamGatewayAddress,
			dnsServerCommand,
			vlan,
			teamPartialIp,
			switchTeamGatewayAddress,
		)
	}
	for i, vlan := range vlanForStation {
		if rebuild[i] {
			addTeamVlan(teams[i], vlan)
		}
	}
	if len(addTeamVlansCommand) > 0 {
		_, err = sw.runConfigCommand(addTeamVlansCommand)
		if err != nil {
			return sw.fail(err)
		}
	}

	// Give some time for the configuration to take before another one can be attempted.
	time.Sleep(sw.configBackoffDuration)

	// Bring back up exactly the ports that were shut.
	if portsUp := portCommands(rebuild, "no shutdown"); portsUp != "" {
		if _, err := sw.runConfigCommand(portsUp); err != nil {
			return sw.fail(err)
		}
	}

	sw.applied = desired
	sw.synced = true
	sw.Status = "ACTIVE"
	return nil
}

// portCommands builds an interface block applying the given verb to each selected
// station's driver station port.
func portCommands(stations [6]bool, verb string) string {
	command := ""
	for i, selected := range stations {
		if selected {
			command += fmt.Sprintf("interface %s\n%s\n", dsPortInterfaces[i], verb)
		}
	}
	return command
}

// fail marks the configuration as failed. The switch is left half configured, so the
// recorded state is no longer trustworthy and the next call reconciles in full.
func (sw *Switch) fail(err error) error {
	sw.synced = false
	sw.Status = "ERROR"
	return err
}

// Logs into the switch via Telnet and runs the given command in user exec mode. Reads the output and
// returns it as a string.
func (sw *Switch) runCommand(command string) (string, error) {
	// Open a Telnet connection to the switch.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", sw.address, sw.port), 10*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// Set a deadline so the read doesn't block forever if the switch doesn't close the connection.
	if err = conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return "", err
	}

	// Login to the AP, send the command, and log out all at once.
	writer := bufio.NewWriter(conn)
	_, err = writer.WriteString(
		fmt.Sprintf(
			"%s\nenable\n%s\nterminal length 0\n%sexit\n", sw.password, sw.password,
			command,
		),
	)
	if err != nil {
		return "", err
	}
	err = writer.Flush()
	if err != nil {
		return "", err
	}

	// Read the response. The switch may not close the connection after exit, so we read
	// until the deadline fires (indicated by a timeout error, which we treat as success).
	var reader bytes.Buffer
	_, err = reader.ReadFrom(conn)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout just means the switch kept the connection open — the commands were sent.
			return reader.String(), nil
		}
		return "", err
	}
	return reader.String(), nil
}

// Logs into the switch via Telnet and runs the given command in global configuration mode. Reads the output
// and returns it as a string.
func (sw *Switch) runConfigCommand(command string) (string, error) {
	// Terminate the caller's last line. Without this "end" is appended to it -- the
	// stock driver-station port commands end in "no shutdown", which became
	// "no shutdownend" and was rejected by IOS. The failure is invisible: a Telnet read
	// timeout is treated as success, so the port cycling silently did nothing.
	//
	// Upstream cannot hit this: it has no driver-station port commands, and both of its
	// callers build strings ending in a newline. The guard is still worth sending back,
	// since the precondition is unstated and the failure mode is silent. Tracked in
	// docs/upstream-divergences.md.
	if command != "" && !strings.HasSuffix(command, "\n") {
		command += "\n"
	}
	return sw.runCommand(fmt.Sprintf("config terminal\n%send\n", command))
}

var vlanToAllianceStation = map[int]string{
	10: "R1", 20: "R2", 30: "R3",
	40: "B1", 50: "B2", 60: "B3",
}

// GetStationForTeamId queries the switch ARP table to determine which alliance station
// a team is physically connected to. Returns "" if the switch is unconfigured or the
// team IP has no ARP entry.
func (sw *Switch) GetStationForTeamId(teamId int) (string, error) {
	if sw.address == "" {
		return "", nil
	}
	teamIp := fmt.Sprintf("10.%d.%d.5", teamId/100, teamId%100)
	output, err := sw.runCommand(fmt.Sprintf("show ip arp %s\n", teamIp))
	if err != nil {
		return "", err
	}
	// Cisco IOS output example:
	//   Protocol  Address     Age(min)  Hardware Addr   Type   Interface
	//   Internet  10.2.54.5       2     0050.b6ff.ee5   ARPA   Vlan20
	re := regexp.MustCompile(`Vlan(\d+)`)
	matches := re.FindStringSubmatch(output)
	if matches == nil {
		return "", nil
	}
	vlan, _ := strconv.Atoi(matches[1])
	return vlanToAllianceStation[vlan], nil
}
