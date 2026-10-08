// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package model

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEventSettingsReadWrite(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	eventSettings, err := db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(
		t,
		EventSettings{
			Id:                          1,
			Name:                        "Untitled Event",
			PlayoffType:                 DoubleEliminationPlayoff,
			NumPlayoffAlliances:         8,
			SelectionRound2Order:        "L",
			SelectionRound3Order:        "",
			SelectionShowUnpickedTeams:  true,
			TbaDownloadEnabled:          false,
			ApChannel:                   36,
			WarmupDurationSec:           0,
			AutoDurationSec:             20,
			PauseDurationSec:            3,
			TeleopDurationSec:           140,
			WarningRemainingDurationSec: 30,
			CompanionAddress:            "",
			CompanionPort:               0,

			// Hub lights default to DMX, with no address: nothing sent until a field is set
			// up. The relay pins are filled in regardless, so choosing relays later starts
			// from working pins rather than zeroes.
			HubRelayRedPin:  22,
			HubRelayBluePin: 23,

			// The FUEL counter's port is filled in for the same reason, and its address is
			// blank, which is what actually leaves the counter off.
			HubCounterPort: 8411,

			// Random until a field says otherwise: most practice fields have no camera,
			// and a default of Counted would block match start on hardware nobody has.
			AutoWinnerMode: "random",
		},
		*eventSettings,
	)

	eventSettings.Name = "Chezy Champs"
	eventSettings.NumPlayoffAlliances = 6
	eventSettings.SelectionRound2Order = "F"
	eventSettings.SelectionRound3Order = "L"
	err = db.UpdateEventSettings(eventSettings)
	assert.Nil(t, err)
	eventSettings2, err := db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(t, eventSettings, eventSettings2)
}

// A database written before the relay settings existed has no pins stored. Loading it
// fills in the defaults, so the Settings page never offers zeroes.
func TestEventSettingsRelayPinDefaultsForOlderDatabase(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	eventSettings, err := db.GetEventSettings()
	assert.Nil(t, err)
	eventSettings.HubRelayRedPin = 0
	eventSettings.HubRelayBluePin = 0
	assert.Nil(t, db.UpdateEventSettings(eventSettings))

	eventSettings, err = db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(t, 22, eventSettings.HubRelayRedPin)
	assert.Equal(t, 23, eventSettings.HubRelayBluePin)

	// A configured pin survives the reload untouched.
	eventSettings.HubRelayRedPin = 17
	assert.Nil(t, db.UpdateEventSettings(eventSettings))
	eventSettings, err = db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(t, 17, eventSettings.HubRelayRedPin)
}
