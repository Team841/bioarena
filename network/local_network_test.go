// Copyright 2025 Team 841. All Rights Reserved.

package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/team841/bioarena/model"
)

// newTestLocalNetwork returns a LocalNetwork that records the commands it would run and
// the dnsmasq configuration it would write, without touching the host.
func newTestLocalNetwork(config LocalNetworkConfig) (*LocalNetwork, *[]string, *string) {
	ln := NewLocalNetwork(config)
	var commands []string
	var written string

	ln.runCommand = func(name string, args ...string) (string, error) {
		commands = append(commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		return "", nil
	}
	ln.writeFile = func(_ string, data []byte) error {
		written = string(data)
		return nil
	}
	return ln, &commands, &written
}

func TestLocalNetworkConfigureTeamEthernet(t *testing.T) {
	ln, commands, config := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Equal(t, "UNKNOWN", ln.Status)

	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, {Id: 254}, nil}))
	assert.Equal(t, "ACTIVE", ln.Status)

	// Forwarding first, then all six stations torn down, then only the two present ones
	// built, then dnsmasq restarted.
	assert.Equal(
		t,
		[]string{
			"sysctl -w net.ipv4.ip_forward=1",
			"ip link delete eth0.10",
			"ip link delete eth0.20",
			"ip link delete eth0.30",
			"ip link delete eth0.40",
			"ip link delete eth0.50",
			"ip link delete eth0.60",
			"ip link add link eth0 name eth0.10 type vlan id 10",
			"ip addr add 10.8.41.4/24 dev eth0.10",
			"ip link set dev eth0.10 up",
			"ip link add link eth0 name eth0.50 type vlan id 50",
			"ip addr add 10.2.54.4/24 dev eth0.50",
			"ip link set dev eth0.50 up",
			"systemctl restart dnsmasq",
		},
		*commands,
	)

	// The pool matches the switch's: .20 through .199, seven-day lease, gateway .4.
	assert.Contains(t, *config, "port=0\nbind-dynamic\n")
	assert.Contains(t, *config, "# R1 -- Team 841\ninterface=eth0.10\n")
	assert.Contains(t, *config, "dhcp-range=set:vlan10,10.8.41.20,10.8.41.199,255.255.255.0,5m\n")
	assert.Contains(t, *config, "dhcp-option=tag:vlan10,option:router,10.8.41.4\n")
	assert.Contains(t, *config, "# B2 -- Team 254\ninterface=eth0.50\n")
	assert.Contains(t, *config, "dhcp-range=set:vlan50,10.2.54.20,10.2.54.199,255.255.255.0,5m\n")

	// Stations without a team get no scope at all.
	for _, vlan := range []int{20, 30, 40, 60} {
		assert.NotContains(t, *config, fmt.Sprintf("interface=eth0.%d\n", vlan))
	}
}

// An empty field still tears the previous match's subnets down; leaving them up would
// keep routing traffic for teams that are no longer on the field.
func TestLocalNetworkConfigureNoTeams(t *testing.T) {
	ln, commands, config := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})

	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{}))
	assert.Equal(t, "ACTIVE", ln.Status)

	assert.Equal(t, 8, len(*commands)) // sysctl + six deletes + restart
	assert.NotContains(t, *commands, "ip link add link eth0 name eth0.10 type vlan id 10")
	assert.NotContains(t, *config, "dhcp-range")
}

// Registering one station must not disturb a robot being driven from another: an
// unchanged station keeps its subinterface, its address, and its clients' leases.
func TestLocalNetworkOnlyTouchesChangedStations(t *testing.T) {
	ln, commands, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))

	// Second call adds B1 and leaves R1 alone.
	*commands = nil
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	assert.Equal(
		t,
		[]string{
			"sysctl -w net.ipv4.ip_forward=1",
			"ip link delete eth0.40",
			"ip link add link eth0 name eth0.40 type vlan id 40",
			"ip addr add 10.2.54.4/24 dev eth0.40",
			"ip link set dev eth0.40 up",
			"systemctl restart dnsmasq",
		},
		*commands,
	)
	for _, command := range *commands {
		assert.NotContains(t, command, "eth0.10", "R1 was unchanged and must not be touched")
	}
}

// A station whose team changes is removed before it is added, so it is never addressed
// for both teams at once.
func TestLocalNetworkReplacesTeamOnSameStation(t *testing.T) {
	ln, commands, config := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))

	*commands = nil
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 254}, nil, nil, nil, nil, nil}))

	assert.Equal(
		t,
		[]string{
			"sysctl -w net.ipv4.ip_forward=1",
			"ip link delete eth0.10",
			"ip link add link eth0 name eth0.10 type vlan id 10",
			"ip addr add 10.2.54.4/24 dev eth0.10",
			"ip link set dev eth0.10 up",
			"systemctl restart dnsmasq",
		},
		*commands,
	)
	assert.NotContains(t, *config, "10.8.41")
}

// Clearing a station removes its subnet and nothing else.
func TestLocalNetworkClearsOneStation(t *testing.T) {
	ln, commands, config := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	*commands = nil
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))

	assert.Equal(
		t,
		[]string{
			"sysctl -w net.ipv4.ip_forward=1",
			"ip link delete eth0.40",
			"systemctl restart dnsmasq",
		},
		*commands,
	)
	assert.Contains(t, *config, "interface=eth0.10\n")
	assert.NotContains(t, *config, "interface=eth0.40\n")
}

// An unchanged team list does nothing at all -- no commands, and no dnsmasq restart that
// would serve no purpose.
func TestLocalNetworkSkipsUnchangedTeams(t *testing.T) {
	ln, commands, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	teams := [6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}
	assert.Nil(t, ln.ConfigureTeamEthernet(teams))

	*commands = nil
	assert.Nil(t, ln.ConfigureTeamEthernet(teams))
	assert.Empty(t, *commands)
	assert.Equal(t, "ACTIVE", ln.Status)

	// Identity is the team number, so a fresh record for the same team is still unchanged.
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841, WpaKey: "changed"}, nil, nil, nil, nil, nil}))
	assert.Empty(t, *commands)
}

// The first configuration after startup cannot trust the recorded state: subinterfaces
// left by a previous run of the service outlive the process.
func TestLocalNetworkFirstConfigurationReconcilesInFull(t *testing.T) {
	ln, commands, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{}))

	for _, vlan := range []int{10, 20, 30, 40, 50, 60} {
		assert.Contains(t, *commands, fmt.Sprintf("ip link delete eth0.%d", vlan))
	}
}

// After a failure the host is partly configured, so the recorded state is worthless and
// the next call must reconcile everything rather than trust its own bookkeeping.
func TestLocalNetworkReconcilesInFullAfterFailure(t *testing.T) {
	ln, commands, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))

	failing := true
	ln.runCommand = func(name string, args ...string) (string, error) {
		*commands = append(*commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
		if failing && name == "ip" && args[0] == "addr" {
			return "RTNETLINK answers: Permission denied", fmt.Errorf("exit status 2")
		}
		return "", nil
	}
	assert.NotNil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 254}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "ERROR", ln.Status)

	failing = false
	*commands = nil
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 254}, nil, nil, nil, nil, nil}))
	for _, vlan := range []int{10, 20, 30, 40, 50, 60} {
		assert.Contains(t, *commands, fmt.Sprintf("ip link delete eth0.%d", vlan))
	}
	assert.Equal(t, "ACTIVE", ln.Status)
}

// Tearing down a station that was never configured is the normal case, not a failure.
func TestLocalNetworkToleratesMissingDevice(t *testing.T) {
	ln, _, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	ln.runCommand = func(name string, args ...string) (string, error) {
		if name == "ip" && args[0] == "link" && args[1] == "delete" {
			return `Cannot find device "eth0.10"`, fmt.Errorf("exit status 1")
		}
		return "", nil
	}

	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{}))
	assert.Equal(t, "ACTIVE", ln.Status)
}

// Any other ip(8) failure is real and must surface rather than leave the field half
// configured with a green badge.
func TestLocalNetworkReportsCommandFailure(t *testing.T) {
	ln, _, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	ln.runCommand = func(name string, args ...string) (string, error) {
		if name == "ip" && args[0] == "addr" {
			return "RTNETLINK answers: Permission denied", fmt.Errorf("exit status 2")
		}
		return "", nil
	}

	err := ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "configuring eth0.10 for Team 841")
	assert.Equal(t, "ERROR", ln.Status)
}

// A dnsmasq that cannot be restarted is still serving the previous match's ranges, so the
// badge must not claim the field is configured.
func TestLocalNetworkReportsRestartFailure(t *testing.T) {
	ln, _, _ := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	ln.runCommand = func(name string, _ ...string) (string, error) {
		if name == "systemctl" {
			return "Failed to restart dnsmasq.service: Access denied", fmt.Errorf("exit status 1")
		}
		return "", nil
	}

	err := ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "restarting dnsmasq")
	assert.Equal(t, "ERROR", ln.Status)
}

// Blank omits the option entirely, for the same reason the switch omits it: a resolver
// the team subnet cannot reach makes lookups time out rather than fail fast.
func TestLocalNetworkDnsServer(t *testing.T) {
	ln, _, config := newTestLocalNetwork(
		LocalNetworkConfig{TrunkInterface: "eth0", DnsServer: "10.0.100.5"},
	)
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}}))
	assert.Contains(t, *config, "dhcp-option=tag:vlan10,option:dns-server,10.0.100.5\n")

	lnNoDns, _, configNoDns := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	assert.Nil(t, lnNoDns.ConfigureTeamEthernet([6]*model.Team{{Id: 841}}))
	assert.NotContains(t, *configNoDns, "dns-server")
	assert.Contains(t, *configNoDns, "dhcp-option=tag:vlan10,option:router,10.8.41.4\n")
}

// The trunk interface names the subinterfaces, so a field wired to a different NIC gets
// its VLANs there.
func TestLocalNetworkHonoursTrunkInterface(t *testing.T) {
	ln, commands, config := newTestLocalNetwork(LocalNetworkConfig{TrunkInterface: "enp1s0"})
	assert.Nil(t, ln.ConfigureTeamEthernet([6]*model.Team{{Id: 841}}))
	assert.Contains(t, *commands, "ip link add link enp1s0 name enp1s0.10 type vlan id 10")
	assert.Contains(t, *config, "interface=enp1s0.10\n")
}

func TestLocalNetworkGetStationForTeamId(t *testing.T) {
	arpTable := "IP address       HW type     Flags       HW address            Mask     Device\n" +
		"10.8.41.5        0x1         0x2         b8:27:eb:00:00:01     *        eth0.10\n" +
		"10.2.54.5        0x1         0x2         b8:27:eb:00:00:02     *        eth0.50\n" +
		"10.16.78.5       0x1         0x0         00:00:00:00:00:00     *        eth0.30\n" +
		"10.15.3.5        0x1         0x2         b8:27:eb:00:00:04     *        eth0.99\n"

	path := filepath.Join(t.TempDir(), "arp")
	assert.Nil(t, os.WriteFile(path, []byte(arpTable), 0644))

	ln := NewLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	ln.arpTablePath = path

	station, err := ln.GetStationForTeamId(841)
	assert.Nil(t, err)
	assert.Equal(t, "R1", station)

	station, err = ln.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "B2", station)

	// Incomplete entry (flags 0x0): asked after, never answered, so it locates nothing.
	station, err = ln.GetStationForTeamId(1678)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// A VLAN outside the six alliance stations.
	station, err = ln.GetStationForTeamId(1503)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// A team with no entry at all.
	station, err = ln.GetStationForTeamId(1114)
	assert.Nil(t, err)
	assert.Equal(t, "", station)
}

// On a machine with no ARP table -- any non-Linux development box -- detection is simply
// unavailable, which the caller already handles by falling back to sequential assignment.
func TestLocalNetworkGetStationWithoutArpTable(t *testing.T) {
	ln := NewLocalNetwork(LocalNetworkConfig{TrunkInterface: "eth0"})
	ln.arpTablePath = filepath.Join(t.TempDir(), "absent")

	station, err := ln.GetStationForTeamId(841)
	assert.Nil(t, err)
	assert.Equal(t, "", station)
}

func TestLocalNetworkDefaults(t *testing.T) {
	ln := NewLocalNetwork(LocalNetworkConfig{})
	assert.Equal(t, "eth0", ln.config.TrunkInterface)
	assert.Equal(t, "/etc/dnsmasq.d/bioarena.conf", ln.config.DnsmasqConfPath)
	assert.Equal(t, "dnsmasq", ln.config.DnsmasqService)
}
