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

	// Five minutes, so a laptop moved between stations corrects itself within a couple of
	// renewals rather than holding a dead address. Deliberately shorter than the switch's
	// "lease 7" (seven days): the switch can force a renewal by bouncing the station's
	// port, and this host cannot -- the laptop's carrier is to the switch, not to us -- so
	// lease length is the only self-correction available here.
	//
	// Safe against reconfiguration churn because stations that do not change are never
	// touched, so their renewals cannot be starved by another station being registered.
	localDhcpLease = "5m"

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

	// applied is the team per station as this host is currently configured, by team
	// number, with 0 for an empty station. Only meaningful once synced is true.
	applied [6]int

	// synced records whether the host's state is known. False at startup -- subinterfaces
	// left by a previous run of the service outlive the process -- and after any failure,
	// either of which makes the next configuration a full reconciliation rather than a
	// difference.
	synced bool

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

// ConfigureTeamEthernet brings the host's VLAN subinterfaces and DHCP scopes into line
// with the given teams.
//
// Only the stations that changed are touched. A station whose team is unchanged keeps its
// subinterface, its address, and its clients' leases -- which matters because registering
// one station must not disturb a robot being driven from another. An unchanged set of
// teams does nothing at all.
func (ln *LocalNetwork) ConfigureTeamEthernet(teams [6]*model.Team) error {
	// Match the switch's guarantee that two configurations never overlap; match load can
	// fire this repeatedly as stations are registered.
	ln.mutex.Lock()
	defer ln.mutex.Unlock()

	desired := teamIds(teams)
	full := !ln.synced

	if !full && desired == ln.applied {
		ln.Status = "ACTIVE"
		return nil
	}

	ln.Status = "CONFIGURING"

	// Routing between the team subnets and the FMS is this host's job now, so it has to
	// forward. Set here rather than once at startup because a sysctl written by hand does
	// not survive a reboot, and this is the cheapest place to be certain of it.
	if _, err := ln.runCommand("sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return ln.fail(fmt.Errorf("enabling IP forwarding: %w", err))
	}

	// changed reports the stations to rebuild. On a full reconciliation that is all of
	// them, because the host's state is unknown: subinterfaces left by a previous run of
	// the service outlive the process, and a station that looks empty may not be.
	changed := func(i int) bool { return full || desired[i] != ln.applied[i] }

	// Every removal before any addition. A team moving between stations must not be
	// addressed on two VLANs at once, and that can happen in either direction, so
	// per-station remove-then-add is not enough.
	for i, vlan := range vlanForStation {
		if !changed(i) {
			continue
		}
		if err := ln.removeStation(vlan); err != nil {
			return ln.fail(err)
		}
	}

	for i, vlan := range vlanForStation {
		if !changed(i) || teams[i] == nil {
			continue
		}
		if err := ln.configureStation(teams[i], vlan); err != nil {
			return ln.fail(err)
		}
	}

	if err := ln.writeFile(ln.config.DnsmasqConfPath, []byte(ln.dnsmasqConfig(teams))); err != nil {
		return ln.fail(fmt.Errorf("writing %s: %w", ln.config.DnsmasqConfPath, err))
	}

	// A restart, not a reload. On SIGHUP dnsmasq re-reads /etc/hosts and its lease file
	// but not its configuration, so a reload would leave the previous match's ranges
	// serving addresses on subnets that no longer exist -- silently, and only for the
	// teams unlucky enough to ask for a lease at the wrong moment.
	//
	// This does not disturb the stations that did not change: DHCP is stateless between
	// requests, the lease file survives the restart, and the restart is far shorter than
	// a client's retry interval.
	if _, err := ln.runCommand("systemctl", "restart", ln.config.DnsmasqService); err != nil {
		return ln.fail(fmt.Errorf("restarting %s: %w", ln.config.DnsmasqService, err))
	}

	ln.applied = desired
	ln.synced = true
	ln.Status = "ACTIVE"
	return nil
}

// fail marks the configuration as failed. The host is left partly configured, so the
// recorded state is no longer trustworthy and the next call reconciles in full.
func (ln *LocalNetwork) fail(err error) error {
	ln.synced = false
	ln.Status = "ERROR"
	return err
}

// teamIds reduces the teams to their numbers, with 0 for an empty station. Comparing
// numbers rather than pointers keeps an unrelated edit to a team record -- a WPA key
// changed from the Teams page, say -- from counting as a network change.
func teamIds(teams [6]*model.Team) [6]int {
	var ids [6]int
	for i, team := range teams {
		if team != nil {
			ids[i] = team.Id
		}
	}
	return ids
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
