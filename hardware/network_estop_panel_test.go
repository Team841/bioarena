package hardware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// snapshotServer serves a fixed PanelSnapshot from /poll.
func snapshotServer(t *testing.T, snapshot PanelSnapshot) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/poll", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshot)
	}))
}

// assertAllFaulted checks that every listed station reported the given fault,
// which is what a panel client owes the arena when it has nothing better.
func assertAllFaulted(t *testing.T, got []InputState, kind FaultKind, stations ...string) {
	t.Helper()
	assert.Len(t, got, len(stations))
	seen := map[string]bool{}
	for _, input := range got {
		assert.Equal(t, StopFault, input.State)
		assert.Equal(t, kind, input.Fault)
		assert.True(t, input.Stopped())
		seen[input.Station] = true
	}
	for _, station := range stations {
		assert.True(t, seen[station], "expected a fault for station %s", station)
	}
}

func TestNetworkEStopPanelSuccessfulPoll(t *testing.T) {
	want := []InputState{
		{Station: "R1", IsAStop: false, State: StopActive},
		{Station: "R2", IsAStop: true, State: StopOK},
		{Station: "R3", IsAStop: false, State: StopOK},
	}
	srv := snapshotServer(t, PanelSnapshot{Alliance: "red", AgeMs: 5, Inputs: want})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	assert.Equal(t, want, panel.Poll())
}

func TestNetworkEStopPanelConnectionFailureFaults(t *testing.T) {
	panel := NewNetworkEStopPanel("http://127.0.0.1:1", "red") // nothing listening
	assertAllFaulted(t, panel.Poll(), FaultUnreachable, "R1", "R2", "R3")
}

func TestNetworkEStopPanelTimeoutFaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // longer than the 200 ms client timeout
		w.Write([]byte("{}"))
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "blue")
	start := time.Now()
	got := panel.Poll()
	assert.Less(t, time.Since(start), 400*time.Millisecond)
	assertAllFaulted(t, got, FaultUnreachable, "B1", "B2", "B3")
}

func TestNetworkEStopPanelNon200Faults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	assertAllFaulted(t, panel.Poll(), FaultUnreachable, "R1", "R2", "R3")
}

func TestNetworkEStopPanelBadJSONFaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	assertAllFaulted(t, panel.Poll(), FaultUnreachable, "R1", "R2", "R3")
}

func TestNetworkEStopPanelStaleSampleFaults(t *testing.T) {
	// The panel is answering, but its sampler has stalled: the HTTP server being
	// alive says nothing about whether anyone is still reading the pins.
	srv := snapshotServer(t, PanelSnapshot{
		Alliance: "red",
		AgeMs:    (maxSnapshotAge + 100*time.Millisecond).Milliseconds(),
		Inputs:   []InputState{{Station: "R1", State: StopOK}},
	})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	assertAllFaulted(t, panel.Poll(), FaultStale, "R1", "R2", "R3")
}

func TestNetworkEStopPanelFreshSampleAccepted(t *testing.T) {
	srv := snapshotServer(t, PanelSnapshot{
		Alliance: "red",
		AgeMs:    (maxSnapshotAge - 50*time.Millisecond).Milliseconds(),
		Inputs:   []InputState{{Station: "R1", State: StopOK}},
	})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	got := panel.Poll()
	assert.Len(t, got, 1)
	assert.Equal(t, StopOK, got[0].State)
}

func TestNetworkEStopPanelNoConfiguredInputsFaults(t *testing.T) {
	// A panel with no pins wired can report nothing about its buttons, which is
	// not the same as reporting that they are fine.
	srv := snapshotServer(t, PanelSnapshot{Alliance: "blue", AgeMs: 5, Inputs: []InputState{}})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "blue")
	assertAllFaulted(t, panel.Poll(), FaultUnreachable, "B1", "B2", "B3")
}

// The field e-stop is honoured only on a panel ticked as having the button. Off, it is
// dropped whatever the panel reports -- on an NC input with no button fitted, that is a
// permanent press -- and the station inputs come through untouched.
func TestNetworkEStopPanelFieldEStopOnlyWhenEnabled(t *testing.T) {
	inputs := []InputState{
		{Station: "B1", State: StopOK},
		{Station: "all", State: StopActive},
	}
	srv := snapshotServer(t, PanelSnapshot{Alliance: "blue", AgeMs: 5, Inputs: inputs})
	defer srv.Close()

	off := NewNetworkEStopPanel(srv.URL, "blue")
	assert.Equal(t, []InputState{{Station: "B1", State: StopOK}}, off.Poll(), "off by default")
	assert.Equal(t, PanelStatusActive, off.Status(), "a panel whose field e-stop is ignored is still up")

	on := NewNetworkEStopPanel(srv.URL, "blue").WithFieldEStop(true)
	assert.Equal(t, inputs, on.Poll())
}

// A panel reporting nothing but an ignored field e-stop answered, so it is not faulted as
// one reporting nothing at all.
func TestNetworkEStopPanelOnlyIgnoredFieldEStopIsQuiet(t *testing.T) {
	srv := snapshotServer(t, PanelSnapshot{Alliance: "red", AgeMs: 5,
		Inputs: []InputState{{Station: "all", State: StopActive}}})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	assert.Empty(t, panel.Poll())
	assert.Equal(t, PanelStatusActive, panel.Status())
}

func TestNetworkEStopPanelBareHostNormalised(t *testing.T) {
	want := []InputState{{Station: "R1", State: StopOK}}
	srv := snapshotServer(t, PanelSnapshot{Alliance: "red", AgeMs: 1, Inputs: want})
	defer srv.Close()

	// Strip the "http://" scheme to test bare-host normalisation.
	bare := strings.TrimPrefix(srv.URL, "http://")
	panel := NewNetworkEStopPanel(bare, "red")
	assert.Equal(t, want, panel.Poll())
}

func TestNetworkEStopPanelUnknownAllianceDefaultsToBlue(t *testing.T) {
	panel := NewNetworkEStopPanel("http://127.0.0.1:1", "")
	assertAllFaulted(t, panel.Poll(), FaultUnreachable, "B1", "B2", "B3")
}

// --- status, for the operating page's badge ---

// The badge is meant to answer "is the panel on?" at a glance, so it has to track every
// outcome Poll can have, and start from a state that is neither.
func TestNetworkEStopPanelStatusFollowsPolls(t *testing.T) {
	inputs := []InputState{{Station: "B1", State: StopOK}}
	srv := snapshotServer(t, PanelSnapshot{Alliance: "blue", AgeMs: 5, Inputs: inputs})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "blue")
	assert.Equal(t, "blue", panel.Alliance())
	assert.Equal(t, PanelStatusUnknown, panel.Status(), "nothing has been polled yet")

	panel.Poll()
	assert.Equal(t, PanelStatusActive, panel.Status())

	// Every failure path funnels through faulted(), so one is enough to prove the wiring.
	srv.Close()
	panel.Poll()
	assert.Equal(t, PanelStatusError, panel.Status())
}

// A stale sample is a failure too: the panel's HTTP server is up but its sampler has
// stalled, and a badge that stayed green there would be lying about the buttons.
func TestNetworkEStopPanelStaleSampleIsError(t *testing.T) {
	srv := snapshotServer(t, PanelSnapshot{
		Alliance: "red", AgeMs: 5000, Inputs: []InputState{{Station: "R1", State: StopOK}},
	})
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL, "red")
	panel.Poll()
	assert.Equal(t, PanelStatusError, panel.Status())
}
