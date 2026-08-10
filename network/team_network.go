// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
//
// Shared helpers for the per-match team networks.

package network

import (
	"fmt"
	"strings"

	"github.com/team841/bioarena/model"
)

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
// Team 1114 gets 10.11.14.
func teamSubnet(teamId int) string {
	return fmt.Sprintf("10.%d.%d", teamId/100, teamId%100)
}
