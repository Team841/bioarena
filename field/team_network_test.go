// Copyright 2025 Team 841. All Rights Reserved.
//
// Tests that the wired team networks are configured everywhere the wireless ones are.

package field

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/team841/bioarena/model"
)

// fakeTeamNetwork records what would have been applied to the wired network.
type fakeTeamNetwork struct {
	mutex       sync.Mutex
	applied     [][6]*model.Team
	entered     chan struct{} // signalled on entry, to observe a call reaching the hardware
	block       chan struct{} // when non-nil, held until closed, to keep a call in flight
	station     string
	stationErr  error
	statusValue string
}

func (f *fakeTeamNetwork) ConfigureTeamEthernet(teams [6]*model.Team) error {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.applied = append(f.applied, teams)
	return nil
}

func (f *fakeTeamNetwork) GetStationForTeamId(_ int) (string, error) {
	return f.station, f.stationErr
}

func (f *fakeTeamNetwork) GetStatus() string {
	return f.statusValue
}

// lastApplied reports the most recent configuration, waiting briefly for the background
// goroutine to run.
func (f *fakeTeamNetwork) lastApplied(t *testing.T) [6]*model.Team {
	t.Helper()
	var last [6]*model.Team
	assert.Eventually(
		t,
		func() bool {
			f.mutex.Lock()
			defer f.mutex.Unlock()
			if len(f.applied) == 0 {
				return false
			}
			last = f.applied[len(f.applied)-1]
			return true
		},
		2*time.Second,
		5*time.Millisecond,
		"expected the wired network to be configured",
	)
	return last
}

func (f *fakeTeamNetwork) applyCount() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.applied)
}

// setupTeamNetworkTestArena returns an arena with the wired network faked and network
// security on. The access point is left as built by LoadSettings, which has security off
// and so returns without attempting any HTTP.
func setupTeamNetworkTestArena(t *testing.T) (*Arena, *fakeTeamNetwork) {
	arena := setupTestArena(t)
	fake := &fakeTeamNetwork{}
	arena.teamNetwork = fake
	arena.EventSettings.NetworkSecurityEnabled = true
	return arena, fake
}

// A free practice slot used to get an SSID but no VLAN subinterface and no DHCP scope, so
// a driver station wired to that station's port never received an address.
func TestSetFreePracticeSlotConfiguresTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("B1", 841, "key"))

	teams := fake.lastApplied(t)
	assert.NotNil(t, teams[3], "B1 should have been configured")
	assert.Equal(t, 841, teams[3].Id)
	assert.Nil(t, teams[0], "R1 has no team and should not be configured")
}

func TestClearFreePracticeSlotConfiguresTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("B1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[3].Id)

	assert.NoError(t, arena.ClearFreePracticeSlot("B1"))
	assert.Eventually(
		t,
		func() bool { return fake.lastApplied(t)[3] == nil },
		2*time.Second,
		5*time.Millisecond,
		"clearing a slot should remove its subnet",
	)
}

// Leaving free practice must take the subnets down with it; a station left routable to a
// team that has gone home is a subnet handing out leases nobody owns.
func TestExitFreePracticeTearsDownTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	assert.NoError(t, arena.ExitFreePractice())
	assert.Eventually(
		t,
		func() bool {
			teams := fake.lastApplied(t)
			return teams == [6]*model.Team{}
		},
		2*time.Second,
		5*time.Millisecond,
		"exiting free practice should tear every subnet down",
	)
}

// With network security off nothing is touched, matching how the wireless side behaves.
func TestFreePracticeSkipsTeamEthernetWhenSecurityDisabled(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	arena.EventSettings.NetworkSecurityEnabled = false

	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fake.applyCount())
}

// DISABLE FIELD halts robots and nothing else: teams stay registered, the AP keeps its
// SSIDs, and the team subnets stay configured, so ENABLE FIELD resumes without anyone
// re-registering or re-connecting.
func TestDisableFieldLeavesNetworkingIntact(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	appliedBefore := fake.applyCount()
	arena.DisableField()

	assert.True(t, arena.IsFieldDisabled())
	assert.Equal(t, FreePractice, arena.MatchState, "the field stays in free practice")
	assert.NotNil(t, arena.AllianceStations["R1"].Team, "the team stays registered")
	assert.Equal(t, appliedBefore, fake.applyCount(), "the wired network is not touched")

	arena.EnableField()
	assert.False(t, arena.IsFieldDisabled())
	assert.Equal(t, appliedBefore, fake.applyCount(), "resuming does not reconfigure either")
	assert.Equal(t, 841, arena.AllianceStations["R1"].Team.Id)
}

// Reset Field is the heavy option, and remains so.
func TestResetFieldTearsEverythingDown(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	assert.NoError(t, arena.ExitFreePractice())
	assert.Equal(t, PreMatch, arena.MatchState)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
	assert.Eventually(
		t,
		func() bool { return fake.lastApplied(t) == [6]*model.Team{} },
		2*time.Second,
		5*time.Millisecond,
		"reset should tear the subnets down",
	)
}

// A halt must not survive into the next session, in either direction.
func TestFieldDisableClearedOnEnterAndExit(t *testing.T) {
	arena, _ := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	arena.DisableField()
	assert.NoError(t, arena.ExitFreePractice())
	assert.False(t, arena.IsFieldDisabled(), "reset should clear the halt")

	assert.NoError(t, arena.EnterFreePractice())
	arena.DisableField()
	assert.NoError(t, arena.ExitFreePractice())
	assert.NoError(t, arena.EnterFreePractice())
	assert.False(t, arena.IsFieldDisabled(), "entering free practice should start live")
}

// Registering teams one at a time queues a configuration per registration. Whichever
// goroutine reached the hardware last used to decide the field's state, which need not be
// the most recent team list; superseded requests must drop out instead.
func TestConfigureTeamEthernetAppliesOnlyTheLatestRequest(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	fake.entered = make(chan struct{}, 8)
	fake.block = make(chan struct{})

	// First request: hold it inside the hardware call so the rest queue behind it.
	var teams [6]*model.Team
	teams[0] = &model.Team{Id: 100}
	arena.configureTeamEthernet(teams)
	<-fake.entered

	// Three more arrive while the first is still in flight.
	for i := 1; i <= 3; i++ {
		teams[i] = &model.Team{Id: 100 + i}
		arena.configureTeamEthernet(teams)
	}
	close(fake.block)

	// The one in flight completes and the newest queued request applies. The two
	// superseded in between never reach the hardware.
	assert.Eventually(
		t,
		func() bool { return fake.applyCount() == 2 },
		2*time.Second,
		5*time.Millisecond,
		fmt.Sprintf("expected exactly two applications, got %d", fake.applyCount()),
	)

	last := fake.lastApplied(t)
	for i := 0; i <= 3; i++ {
		assert.NotNil(t, last[i], fmt.Sprintf("station %d missing from the final configuration", i))
	}
}
