// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
//
// Interface shared by the per-match team network implementations.

package network

import (
	"fmt"
	"strings"

	"github.com/team841/bioarena/model"
)

// TeamNetwork applies the per-match team subnets: one VLAN and one DHCP scope per
// alliance station, each addressed 10.TE.AM.0/24 from the team number.
//
// The work can live in either of two places. Switch pushes the configuration to a Layer 3
// Cisco over Telnet, which is what a competition field uses. LocalNetwork does the same
// job on the Pi with VLAN subinterfaces and dnsmasq, leaving the switch to carry tagged
// frames and nothing else -- which any managed Layer 2 switch can do, including the
// small TP-Link smart switches and other Layer 2 hardware.
type TeamNetwork interface {
	// ConfigureTeamEthernet applies the given teams' subnets in station order
	// (R1, R2, R3, B1, B2, B3). A nil entry leaves that station without a subnet.
	ConfigureTeamEthernet(teams [6]*model.Team) error

	// GetStationForTeamId reports which alliance station a team is physically wired to,
	// or "" when that cannot be determined.
	GetStationForTeamId(teamId int) (string, error)

	// GetStatus reports the outcome of the last configuration for the status badge:
	// UNKNOWN, DISABLED, CONFIGURING, ACTIVE, or ERROR.
	GetStatus() string
}

var (
	_ TeamNetwork = (*Switch)(nil)
	_ TeamNetwork = (*LocalNetwork)(nil)
)

// vlanForStation lists the VLAN carrying each alliance station, in station order.
var vlanForStation = [6]int{red1Vlan, red2Vlan, red3Vlan, blue1Vlan, blue2Vlan, blue3Vlan}

// describeStations renders the stations a configuration touched, for the log. Success is
// otherwise silent, which leaves the operator unable to tell a working field from one
// where the configuration never ran -- the status badge shows the same red for "never
// configured" as for "failed".
func describeStations(teamIds [6]int, rebuilt [6]bool) string {
	stations := []string{"R1", "R2", "R3", "B1", "B2", "B3"}
	var parts []string
	for i, station := range stations {
		if !rebuilt[i] {
			continue
		}
		if teamIds[i] == 0 {
			parts = append(parts, station+" cleared")
		} else {
			parts = append(parts, fmt.Sprintf("%s %d", station, teamIds[i]))
		}
	}
	if len(parts) == 0 {
		return "no stations"
	}
	return strings.Join(parts, ", ")
}

// teamSubnet returns the first three octets of a team's subnet: Team 841 gets 10.8.41,
// Team 1114 gets 10.11.14. This is the same split switch.go applies inline when it builds
// its DHCP pools; the two must agree, since a field can be run either way.
func teamSubnet(teamId int) string {
	return fmt.Sprintf("10.%d.%d", teamId/100, teamId%100)
}
