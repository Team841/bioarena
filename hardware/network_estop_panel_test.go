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

func TestNetworkEStopPanelBareHostNormalised(t *testing.T) {
	want := []InputState{{Station: "all", State: StopOK}}
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
