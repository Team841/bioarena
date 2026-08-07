// Copyright 2025 Team 841. All Rights Reserved.
//
// Applies the per-match team networks on this host instead of on a Layer 3 switch:
// 802.1Q subinterfaces for routing, dnsmasq for DHCP.

package network

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/team841/bioarena/model"
)

const (
	// Pool bounds inside each team subnet, matching the switch implementation: .1-.19 and
	// .200-.254 stay reserved for the gateway, the roboRIO, and static devices.
	localDhcpPoolFirst = 20
	localDhcpPoolLast  = 199

	// The switch issues "lease 7", which IOS reads as seven days.
	localDhcpLease = "7d"

	localDefaultTrunkInterface  = "eth0"
	localDefaultDnsmasqConfPath = "/etc/dnsmasq.d/bioarena.conf"
	localDefaultDnsmasqService  = "dnsmasq"
	localDefaultArpTablePath    = "/proc/net/arp"
)

// LocalNetworkConfig describes where on this host the team networks are applied.
type LocalNetworkConfig struct {
	// TrunkInterface is the NIC facing the switch's trunk port. The VLAN subinterfaces
	// are created on top of it and are named for it, e.g. eth0.10.
	TrunkInterface string

	// DnsmasqConfPath is rewritten on every match load. It must be a file dnsmasq reads
	// -- normally a drop-in under /etc/dnsmasq.d -- and must be writable by the user the
	// service runs as.
	DnsmasqConfPath string

	// DnsmasqService is the systemd unit restarted after the file is rewritten.
	DnsmasqService string

	// DnsServer is handed to teams as their resolver. Blank omits the option, for the
	// same reason the switch omits it: a resolver the team subnet cannot reach makes
	// every lookup wait for a timeout instead of failing fast.
	DnsServer string
}

// LocalNetwork configures team subnets on this host. It is the counterpart to Switch: the
// same six VLANs and DHCP scopes, applied with ip(8) and dnsmasq rather than Telnet, so
// that a Layer 2 switch with no DHCP server and no routing is sufficient.
//
// The host becomes the teams' gateway, holding 10.TE.AM.4 on each VLAN -- the address
// switch.go hands out as default-router -- and forwards between the team subnets and the
// FMS address on the untagged interface.
type LocalNetwork struct {
	config LocalNetworkConfig
	mutex  sync.Mutex
	Status string

	// Seams for testing. runCommand reports combined output alongside the error, since
	// distinguishing "no such device" from a real failure depends on the text.
	runCommand   func(name string, args ...string) (string, error)
	writeFile    func(path string, data []byte) error
	arpTablePath string
}

func NewLocalNetwork(config LocalNetworkConfig) *LocalNetwork {
	if config.TrunkInterface == "" {
		config.TrunkInterface = localDefaultTrunkInterface
	}
	if config.DnsmasqConfPath == "" {
		config.DnsmasqConfPath = localDefaultDnsmasqConfPath
	}
	if config.DnsmasqService == "" {
		config.DnsmasqService = localDefaultDnsmasqService
	}
	return &LocalNetwork{
		config:       config,
		Status:       "UNKNOWN",
		runCommand:   runSystemCommand,
		writeFile:    func(path string, data []byte) error { return os.WriteFile(path, data, 0644) },
		arpTablePath: localDefaultArpTablePath,
	}
}

func (ln *LocalNetwork) GetStatus() string {
	return ln.Status
}

// ConfigureTeamEthernet rebuilds every station's VLAN subinterface and DHCP scope for the
// given teams.
func (ln *LocalNetwork) ConfigureTeamEthernet(teams [6]*model.Team) error {
	// Match the switch's guarantee that two configurations never overlap; match load can
	// fire this repeatedly as stations are registered.
	ln.mutex.Lock()
	defer ln.mutex.Unlock()
	ln.Status = "CONFIGURING"

	// Routing between the team subnets and the FMS is this host's job now, so it has to
	// forward. Set here rather than once at startup because a sysctl written by hand does
	// not survive a reboot, and this is the cheapest place to be certain of it.
	if _, err := ln.runCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		ln.Status = "ERROR"
		return fmt.Errorf("enabling IP forwarding: %w", err)
	}

	// Tear all six down before building any up. A team that has left the field must not
	// keep its subnet, and a team that moved stations must not end up addressed on two
	// VLANs at once -- which would make it reachable by a route the switch no longer
	// carries, and is the sort of thing that only shows up mid-match.
	for _, vlan := range vlanForStation {
		if err := ln.removeStation(vlan); err != nil {
			ln.Status = "ERROR"
			return err
		}
	}

	for i, team := range teams {
		if team == nil {
			continue
		}
		if err := ln.configureStation(team, vlanForStation[i]); err != nil {
			ln.Status = "ERROR"
			return err
		}
	}

	if err := ln.writeFile(ln.config.DnsmasqConfPath, []byte(ln.dnsmasqConfig(teams))); err != nil {
		ln.Status = "ERROR"
		return fmt.Errorf("writing %s: %w", ln.config.DnsmasqConfPath, err)
	}

	// A restart, not a reload. On SIGHUP dnsmasq re-reads /etc/hosts and its lease file
	// but not its configuration, so a reload would leave the previous match's ranges
	// serving addresses on subnets that no longer exist -- silently, and only for the
	// teams unlucky enough to ask for a lease at the wrong moment.
	if _, err := ln.runCommand("systemctl", "restart", ln.config.DnsmasqService); err != nil {
		ln.Status = "ERROR"
		return fmt.Errorf("restarting %s: %w", ln.config.DnsmasqService, err)
	}

	ln.Status = "ACTIVE"
	return nil
}

// removeStation deletes a station's subinterface, tolerating its absence.
func (ln *LocalNetwork) removeStation(vlan int) error {
	device := ln.deviceName(vlan)
	output, err := ln.runCommand("ip", "link", "delete", device)
	if err == nil || isMissingDeviceError(output) {
		return nil
	}
	return fmt.Errorf("removing %s: %w", device, err)
}

// configureStation creates one station's VLAN subinterface and gives this host the
// gateway address inside that team's subnet.
func (ln *LocalNetwork) configureStation(team *model.Team, vlan int) error {
	device := ln.deviceName(vlan)
	gateway := fmt.Sprintf("%s.%d/24", teamSubnet(team.Id), switchTeamGatewayAddress)

	steps := [][]string{
		{
			"ip", "link", "add", "link", ln.config.TrunkInterface, "name", device,
			"type", "vlan", "id", strconv.Itoa(vlan),
		},
		{"ip", "addr", "add", gateway, "dev", device},
		{"ip", "link", "set", "dev", device, "up"},
	}
	for _, step := range steps {
		if _, err := ln.runCommand(step[0], step[1:]...); err != nil {
			return fmt.Errorf("configuring %s for Team %d: %w", device, team.Id, err)
		}
	}
	return nil
}

// dnsmasqConfig renders the DHCP scopes for the given teams.
func (ln *LocalNetwork) dnsmasqConfig(teams [6]*model.Team) string {
	var b strings.Builder

	b.WriteString("# Generated by bioarena on every match load. Edits here are lost.\n")
	b.WriteString("#\n")
	b.WriteString("# DHCP only: port=0 disables the DNS listener, so this cannot collide with a\n")
	b.WriteString("# resolver already running on the Pi.\n")
	b.WriteString("port=0\n")
	// bind-dynamic rather than bind-interfaces: the subinterfaces below are created
	// seconds earlier and are deleted between matches, and bind-interfaces refuses to
	// start when a named interface is missing.
	b.WriteString("bind-dynamic\n")

	for i, team := range teams {
		if team == nil {
			continue
		}
		vlan := vlanForStation[i]
		device := ln.deviceName(vlan)
		subnet := teamSubnet(team.Id)
		tag := fmt.Sprintf("vlan%d", vlan)

		fmt.Fprintf(&b, "\n# %s -- Team %d\n", vlanToAllianceStation[vlan], team.Id)
		fmt.Fprintf(&b, "interface=%s\n", device)
		fmt.Fprintf(
			&b,
			"dhcp-range=set:%s,%s.%d,%s.%d,255.255.255.0,%s\n",
			tag, subnet, localDhcpPoolFirst, subnet, localDhcpPoolLast, localDhcpLease,
		)
		fmt.Fprintf(
			&b, "dhcp-option=tag:%s,option:router,%s.%d\n", tag, subnet, switchTeamGatewayAddress,
		)
		if ln.config.DnsServer != "" {
			fmt.Fprintf(&b, "dhcp-option=tag:%s,option:dns-server,%s\n", tag, ln.config.DnsServer)
		}
	}

	return b.String()
}

// GetStationForTeamId reads the host's ARP table to determine which alliance station a
// team's driver station laptop is wired to. It is the local counterpart to the switch's
// "show ip arp": each station's laptop reaches this host over its own VLAN
// subinterface, so the interface an entry arrived on names the station.
func (ln *LocalNetwork) GetStationForTeamId(teamId int) (string, error) {
	teamIp := fmt.Sprintf("%s.5", teamSubnet(teamId))

	table, err := os.ReadFile(ln.arpTablePath)
	if os.IsNotExist(err) {
		// No ARP table to read -- a non-Linux development machine. Detection failing is
		// an expected outcome the caller already handles by falling back.
		return "", nil
	}
	if err != nil {
		return "", err
	}

	// IP address       HW type  Flags  HW address         Mask  Device
	// 10.8.41.5        0x1      0x2    b8:27:eb:00:00:01  *     eth0.10
	for _, line := range strings.Split(string(table), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != teamIp {
			continue
		}
		// Flags 0x0 marks an incomplete entry: the address was asked after, never
		// answered, so it says nothing about where the laptop is.
		if fields[2] == "0x0" {
			continue
		}
		device := fields[5]
		_, suffix, found := strings.Cut(device, ".")
		if !found {
			continue
		}
		vlan, err := strconv.Atoi(suffix)
		if err != nil {
			continue
		}
		return vlanToAllianceStation[vlan], nil
	}

	return "", nil
}

func (ln *LocalNetwork) deviceName(vlan int) string {
	return fmt.Sprintf("%s.%d", ln.config.TrunkInterface, vlan)
}

// isMissingDeviceError reports whether ip(8) failed only because the device was already
// gone, which is the normal case when tearing down a station that was not configured.
func isMissingDeviceError(output string) bool {
	return strings.Contains(output, "Cannot find device")
}

func runSystemCommand(name string, args ...string) (string, error) {
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf(
			"%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)),
		)
	}
	return string(output), nil
}
