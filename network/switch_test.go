// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package network

import (
	"bytes"
	"fmt"
	"github.com/team841/bioarena/model"
	"github.com/stretchr/testify/assert"
	"net"
	"sync"
	"testing"
	"time"
)

func TestConfigureSwitch(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	assert.Equal(t, "UNKNOWN", sw.Status)
	sw.port = 9050
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond
	var command1, command2 string
	expectedResetCommand := "password\nenable\npassword\nterminal length 0\nconfig terminal\n" +
		"interface Vlan10\nno ip address\nno ip dhcp pool dhcp10\n" +
		"interface Vlan20\nno ip address\nno ip dhcp pool dhcp20\n" +
		"interface Vlan30\nno ip address\nno ip dhcp pool dhcp30\n" +
		"interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n" +
		"interface Vlan50\nno ip address\nno ip dhcp pool dhcp50\n" +
		"interface Vlan60\nno ip address\nno ip dhcp pool dhcp60\n" +
		"end\nexit\n"

	// Should remove all previous VLANs and do nothing else if current configuration is blank.
	mockTelnet(t, sw.port, &command1, &command2)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, nil, nil}))
	assert.Equal(t, expectedResetCommand, command1)
	assert.Equal(t, "", command2)
	assert.Equal(t, "ACTIVE", sw.Status)

	// Should configure one team if only one is present.
	sw.port += 1
	mockTelnet(t, sw.port, &command1, &command2)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, {Id: 254}, nil}))
	assert.Equal(t, expectedResetCommand, command1)
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"ip dhcp excluded-address 10.2.54.1 10.2.54.19\nip dhcp excluded-address 10.2.54.200 10.2.54.254\nip dhcp pool dhcp50\n"+
			"network 10.2.54.0 255.255.255.0\ndefault-router 10.2.54.4\nlease 7\n"+
			"interface Vlan50\nip address 10.2.54.4 255.255.255.0\n"+
			"end\nexit\n",
		command2,
	)

	// Should configure all teams if all are present.
	sw.port += 1
	mockTelnet(t, sw.port, &command1, &command2)
	assert.Nil(
		t,
		sw.ConfigureTeamEthernet([6]*model.Team{{Id: 1114}, {Id: 254}, {Id: 296}, {Id: 1503}, {Id: 1678}, {Id: 1538}}),
	)
	assert.Equal(t, expectedResetCommand, command1)
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"ip dhcp excluded-address 10.11.14.1 10.11.14.19\nip dhcp excluded-address 10.11.14.200 10.11.14.254\nip dhcp pool dhcp10\n"+
			"network 10.11.14.0 255.255.255.0\ndefault-router 10.11.14.4\nlease 7\n"+
			"interface Vlan10\nip address 10.11.14.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.2.54.1 10.2.54.19\nip dhcp excluded-address 10.2.54.200 10.2.54.254\nip dhcp pool dhcp20\n"+
			"network 10.2.54.0 255.255.255.0\ndefault-router 10.2.54.4\nlease 7\n"+
			"interface Vlan20\nip address 10.2.54.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.2.96.1 10.2.96.19\nip dhcp excluded-address 10.2.96.200 10.2.96.254\nip dhcp pool dhcp30\n"+
			"network 10.2.96.0 255.255.255.0\ndefault-router 10.2.96.4\nlease 7\n"+
			"interface Vlan30\nip address 10.2.96.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.15.3.1 10.15.3.19\nip dhcp excluded-address 10.15.3.200 10.15.3.254\nip dhcp pool dhcp40\n"+
			"network 10.15.3.0 255.255.255.0\ndefault-router 10.15.3.4\nlease 7\n"+
			"interface Vlan40\nip address 10.15.3.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.16.78.1 10.16.78.19\nip dhcp excluded-address 10.16.78.200 10.16.78.254\nip dhcp pool dhcp50\n"+
			"network 10.16.78.0 255.255.255.0\ndefault-router 10.16.78.4\nlease 7\n"+
			"interface Vlan50\nip address 10.16.78.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.15.38.1 10.15.38.19\nip dhcp excluded-address 10.15.38.200 10.15.38.254\nip dhcp pool dhcp60\n"+
			"network 10.15.38.0 255.255.255.0\ndefault-router 10.15.38.4\nlease 7\n"+
			"interface Vlan60\nip address 10.15.38.4 255.255.255.0\n"+
			"end\nexit\n",
		command2,
	)
}

// An unset switch address means no switch, not a broken one. Dialing it on every match
// load fails and pins the badge red, which reads as a fault rather than an absence.
func TestConfigureSwitchWithoutAddress(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "DISABLED", sw.Status)
}

func TestGetStationForTeamId(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = 9060

	ciscoArpOutput := "password\nenable\npassword\nterminal length 0\n" +
		"Protocol  Address     Age(min)  Hardware Addr   Type   Interface\n" +
		"Internet  10.2.54.5       2     0050.b6ff.ee5   ARPA   Vlan20\n" +
		"exit\n"

	// Returns correct station when switch ARP table has an entry.
	var command string
	mockTelnetSingleWithResponse(t, sw.port, ciscoArpOutput, &command)
	station, err := sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "R2", station)

	// Returns "" when ARP table has no Vlan entry.
	sw.port++
	noArpOutput := "password\nenable\npassword\nterminal length 0\n% IP ARP table is empty\nexit\n"
	mockTelnetSingleWithResponse(t, sw.port, noArpOutput, &command)
	station, err = sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// Returns "" when VLAN is not in the known map.
	sw.port++
	unknownVlanOutput := "password\nenable\npassword\nterminal length 0\nInternet  10.2.54.5  2  0050.b6ff.ee5  ARPA  Vlan99\nexit\n"
	mockTelnetSingleWithResponse(t, sw.port, unknownVlanOutput, &command)
	station, err = sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// Returns "" when switch address is empty.
	emptySw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	station, err = emptySw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)
}

// gigabitPorts is a 3560-CX driver station port map: R1, R2, R3, B1, B2, B3.
const gigabitPorts = "GigabitEthernet0/1,GigabitEthernet0/2,GigabitEthernet0/3," +
	"GigabitEthernet0/4,GigabitEthernet0/5,GigabitEthernet0/6"

func newIncrementalTestSwitch(port int) *Switch {
	sw := NewSwitch(
		SwitchConfig{
			Address:            "127.0.0.1",
			Password:           "password",
			DSPortUpCommands:   "interface range GigabitEthernet0/1-6\nno shutdown",
			DSPortDownCommands: "interface range GigabitEthernet0/1-6\nshutdown",
			DSPortInterfaces:   gigabitPorts,
		},
	)
	sw.port = port
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond
	return sw
}

// Registering one team must not disturb a robot being driven from another station. The
// full rebuild shuts every driver station port, which disconnects all six for as long as
// the reconfiguration takes -- tolerable between matches, not during free practice.
func TestConfigureSwitchOnlyTouchesChangedStations(t *testing.T) {
	sw := newIncrementalTestSwitch(9100)

	// First call reconciles in full: the switch's state is unknown at startup.
	commands := mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "ACTIVE", sw.Status)
	assert.Contains(t, commands.at(0), "interface range GigabitEthernet0/1-6\nshutdown")
	assert.Contains(t, commands.at(1), "interface Vlan60\nno ip address")

	// Second call adds B1 only.
	sw.port++
	commands = mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	// Only B1's port is cycled, and only its VLAN rebuilt.
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/4\nshutdown\n")
	assert.NotContains(t, commands.at(0), "GigabitEthernet0/1")
	assert.NotContains(t, commands.at(0), "range")
	assert.Contains(t, commands.at(1), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(1), "Vlan10")
	assert.Contains(t, commands.at(2), "interface Vlan40\nip address 10.2.54.4 255.255.255.0\n")
	assert.NotContains(t, commands.at(2), "10.8.41")
	assert.Contains(t, commands.at(3), "interface GigabitEthernet0/4\nno shutdown\n")
	assert.NotContains(t, commands.at(3), "GigabitEthernet0/1")
}

// Clearing a station removes its VLAN and cycles its port, and nothing else.
func TestConfigureSwitchClearsOneStation(t *testing.T) {
	sw := newIncrementalTestSwitch(9110)
	mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	sw.port++
	commands := mockTelnetMulti(t, sw.port, 3) // no VLANs to add, so no third config command
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/4\nshutdown\n")
	assert.Contains(t, commands.at(1), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(1), "Vlan10")
	assert.Contains(t, commands.at(2), "interface GigabitEthernet0/4\nno shutdown\n")
}

// An unchanged team list touches the switch not at all -- no Telnet session, so nothing to
// mock. Anything else would cycle ports for no reason.
func TestConfigureSwitchSkipsUnchangedTeams(t *testing.T) {
	sw := newIncrementalTestSwitch(9120)
	mockTelnetMulti(t, sw.port, 4)
	teams := [6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}
	assert.Nil(t, sw.ConfigureTeamEthernet(teams))

	sw.port++ // nothing listening here; a connection attempt would fail the call
	assert.Nil(t, sw.ConfigureTeamEthernet(teams))
	assert.Equal(t, "ACTIVE", sw.Status)

	// Identity is the team number, so a fresh record for the same team is unchanged too.
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841, WpaKey: "x"}, nil, nil, nil, nil, nil}))
}

// Without a per-station port map there is nothing to be gained by rebuilding only some
// VLANs: the range commands cycle every port regardless, so every station is disturbed.
func TestConfigureSwitchWithoutPortMapRebuildsEverything(t *testing.T) {
	sw := NewSwitch(
		SwitchConfig{
			Address:            "127.0.0.1",
			Password:           "password",
			DSPortDownCommands: "interface range GigabitEthernet0/1-6\nshutdown",
		},
	)
	sw.port = 9130
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	mockTelnetMulti(t, sw.port, 3)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))

	sw.port++
	commands := mockTelnetMulti(t, sw.port, 3)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))
	assert.Contains(t, commands.at(1), "Vlan10")
	assert.Contains(t, commands.at(1), "Vlan60")
}

// A partial or malformed port map is ignored rather than half applied: a short list would
// leave some stations never cycled, surfacing much later as one team unable to get an
// address.
func TestSwitchPortMapValidation(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		interfaces  string
		incremental bool
	}{
		{"six names", gigabitPorts, true},
		{"blank", "", false},
		{"too few", "GigabitEthernet0/1,GigabitEthernet0/2", false},
		{"too many", gigabitPorts + ",GigabitEthernet0/7", false},
		{"empty entry", "GigabitEthernet0/1,,Gi0/3,Gi0/4,Gi0/5,Gi0/6", false},
		{"whitespace tolerated", "Gi0/1, Gi0/2, Gi0/3, Gi0/4, Gi0/5, Gi0/6", true},
	} {
		sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", DSPortInterfaces: testCase.interfaces})
		assert.Equal(t, testCase.incremental, sw.canConfigureIncrementally(), testCase.name)
	}
}

// commandLog collects the commands received across several Telnet sessions.
type commandLog struct {
	mutex    sync.Mutex
	commands []string
}

func (log *commandLog) append(command string) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.commands = append(log.commands, command)
}

func (log *commandLog) at(i int) string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if i >= len(log.commands) {
		return ""
	}
	return log.commands[i]
}

// mockTelnetMulti accepts the given number of connections, recording each in order.
func mockTelnetMulti(t *testing.T, port int, connections int) *commandLog {
	log := &commandLog{}
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		assert.Nil(t, err)
		defer ln.Close()

		for i := 0; i < connections; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			var reader bytes.Buffer
			reader.ReadFrom(conn)
			log.append(reader.String())
			conn.Close()
		}
	}()
	time.Sleep(100 * time.Millisecond) // Give it some time to open the socket.
	return log
}

func mockTelnetSingleWithResponse(t *testing.T, port int, response string, command *string) {
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		assert.Nil(t, err)
		defer ln.Close()
		*command = ""

		conn, err := ln.Accept()
		assert.Nil(t, err)
		conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		var reader bytes.Buffer
		reader.ReadFrom(conn)
		*command = reader.String()
		conn.Write([]byte(response))
		conn.Close()
	}()
	time.Sleep(100 * time.Millisecond)
}

func mockTelnet(t *testing.T, port int, command1 *string, command2 *string) {
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		assert.Nil(t, err)
		defer ln.Close()
		*command1 = ""
		*command2 = ""

		// Fake the first connection.
		conn1, err := ln.Accept()
		assert.Nil(t, err)
		conn1.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		var reader bytes.Buffer
		reader.ReadFrom(conn1)
		*command1 = reader.String()
		conn1.Close()

		// Fake the second connection.
		conn2, err := ln.Accept()
		assert.Nil(t, err)
		conn2.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		reader.Reset()
		reader.ReadFrom(conn2)
		*command2 = reader.String()
		conn2.Close()
	}()
	time.Sleep(100 * time.Millisecond) // Give it some time to open the socket.
}

// The stock driver-station port commands do not end in a newline, so "end" was appended
// to the last line and IOS rejected "no shutdownend". A Telnet read timeout counts as
// success, so the port cycling failed silently on every match load.
func TestConfigCommandTerminatesLastLine(t *testing.T) {
	sw := NewSwitch(
		SwitchConfig{
			Address:            "127.0.0.1",
			Password:           "password",
			DSPortUpCommands:   "interface range FastEthernet0/1-6\nno shutdown",
			DSPortDownCommands: "interface range FastEthernet0/1-6\nshutdown",
		},
	)
	sw.port = 9080
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	var command1, command2 string
	mockTelnetSingleWithResponse(t, sw.port, "", &command1)
	_, err := sw.runConfigCommand(sw.dsPortDownCommands)
	assert.Nil(t, err)
	assert.Contains(t, command1, "shutdown\nend\n")
	assert.NotContains(t, command1, "shutdownend")

	sw.port++
	mockTelnetSingleWithResponse(t, sw.port, "", &command2)
	_, err = sw.runConfigCommand(sw.dsPortUpCommands)
	assert.Nil(t, err)
	assert.Contains(t, command2, "no shutdown\nend\n")
	assert.NotContains(t, command2, "no shutdownend")
}

// A command already ending in a newline must not gain a second one.
func TestConfigCommandDoesNotDoubleTerminate(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = 9082

	var command string
	mockTelnetSingleWithResponse(t, sw.port, "", &command)
	_, err := sw.runConfigCommand("interface Vlan10\nno ip address\n")
	assert.Nil(t, err)
	assert.Contains(t, command, "no ip address\nend\n")
	assert.NotContains(t, command, "no ip address\n\nend")
}

// The DHCP pools carry a DNS server only when one is configured. An unreachable
// resolver makes every lookup wait for a timeout, so blank must omit the option rather
// than emit an empty or placeholder value.
func TestConfigureSwitchDnsServer(t *testing.T) {
	// Configured: the option appears in the pool, after default-router.
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password", DnsServer: "10.0.100.5"})
	sw.port = 9090
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	var command1, command2 string
	mockTelnet(t, sw.port, &command1, &command2)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, command2, "default-router 10.8.41.4\ndns-server 10.0.100.5\nlease 7\n")

	// Blank: no dns-server line at all, and the surrounding pool is unchanged.
	swNoDns := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	swNoDns.port = 9092
	swNoDns.configBackoffDuration = time.Millisecond
	swNoDns.configPauseDuration = time.Millisecond

	mockTelnet(t, swNoDns.port, &command1, &command2)
	assert.Nil(t, swNoDns.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.NotContains(t, command2, "dns-server")
	assert.Contains(t, command2, "default-router 10.8.41.4\nlease 7\n")
}
