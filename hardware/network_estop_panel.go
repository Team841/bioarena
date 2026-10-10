package hardware

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// maxSnapshotAge is how stale a panel's sample may be before the main Pi stops
// believing it. The panel samples at 100 Hz and the arena polls at ~100 Hz, so
// anything approaching a quarter second means the panel's sampler has stalled
// even though its HTTP server is still answering.
const maxSnapshotAge = 250 * time.Millisecond

// NetworkEStopPanel implements EStopPanel by polling a remote panel Pi over
// HTTP.
//
// Poll() fails closed. A panel that cannot be reached, answers with garbage,
// or answers with a stale sample reports a wiring fault on every e-stop it
// owns — an unreachable panel is indistinguishable from a field whose buttons
// we cannot see, and that is not a field anyone should be able to start a
// match on.
type NetworkEStopPanel struct {
	url      string
	alliance string
	stations [3]string
	client   *http.Client

	// fieldEStop is whether this panel's field e-stop -- the input that stops all six
	// stations -- is honoured. Off unless the panel is ticked as having the button fitted,
	// because the panel reads its pin regardless, and on a normally-closed input an empty
	// pin reads as pressed: a panel built without the button would hold the whole field.
	fieldEStop bool

	// status is the outcome of the most recent Poll, for the badge on the operating page.
	// Poll already fails closed by synthesising faults, so the field is safe either way;
	// this exists so an operator can see at a glance that a panel is up, rather than
	// inferring it from three stations that have not faulted.
	status atomic.Value // string: one of the PanelStatus* values
}

// Panel status values, sharing the access point's and switch's vocabulary so the badges
// on the operating page render them with the same colours.
const (
	PanelStatusUnknown  = "UNKNOWN"  // never polled
	PanelStatusActive   = "ACTIVE"   // last poll answered with a fresh sample
	PanelStatusError    = "ERROR"    // last poll failed: unreachable, stale, or empty
	PanelStatusDisabled = "DISABLED" // no panel configured for this alliance
)

// PanelStatusReporter is implemented by a panel that can say whether it is reachable, so
// the operating page can show it. Optional: a panel that does not implement it simply has
// no badge.
type PanelStatusReporter interface {
	Alliance() string
	Status() string
}

// Alliance is "red" or "blue": which three stations this panel is responsible for.
func (n *NetworkEStopPanel) Alliance() string {
	return n.alliance
}

// Status is the outcome of the most recent Poll.
func (n *NetworkEStopPanel) Status() string {
	if v, ok := n.status.Load().(string); ok {
		return v
	}
	return PanelStatusUnknown
}

// Compile-time interface assertion.
var _ EStopPanel = (*NetworkEStopPanel)(nil)

// NewNetworkEStopPanel constructs a panel client for the given host.
// host may include a scheme and port, e.g. "http://10.0.100.11:8765",
// or be a bare "host:port" — the constructor normalises it.
//
// alliance ("red" or "blue") names the stations this panel is responsible for,
// so the client can report faults for them when the panel itself has nothing
// to say.
func NewNetworkEStopPanel(host, alliance string) *NetworkEStopPanel {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	stations := [3]string{"B1", "B2", "B3"}
	if alliance == "red" {
		stations = [3]string{"R1", "R2", "R3"}
	}
	panel := &NetworkEStopPanel{
		url:      strings.TrimRight(host, "/") + "/poll",
		alliance: alliance,
		stations: stations,
		client:   &http.Client{Timeout: 200 * time.Millisecond},
	}
	panel.status.Store(PanelStatusUnknown)
	return panel
}

// WithFieldEStop sets whether this panel's field e-stop is honoured, and returns the panel.
// Without it the input is dropped on every poll, whatever the panel reports for it.
func (n *NetworkEStopPanel) WithFieldEStop(enabled bool) *NetworkEStopPanel {
	n.fieldEStop = enabled
	return n
}

// Poll calls GET /poll on the panel Pi and returns the state of every input it
// reports. On any failure it synthesises a fault for each of its stations.
func (n *NetworkEStopPanel) Poll() []InputState {
	resp, err := n.client.Get(n.url)
	if err != nil {
		log.Printf("NetworkEStopPanel: GET %s: %v", n.url, err)
		return n.faulted(FaultUnreachable)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("NetworkEStopPanel: GET %s returned %d", n.url, resp.StatusCode)
		return n.faulted(FaultUnreachable)
	}

	var snapshot PanelSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		log.Printf("NetworkEStopPanel: decode response from %s: %v", n.url, err)
		return n.faulted(FaultUnreachable)
	}
	if age := time.Duration(snapshot.AgeMs) * time.Millisecond; age > maxSnapshotAge {
		log.Printf("NetworkEStopPanel: %s returned a sample %v old (limit %v)", n.url, age, maxSnapshotAge)
		return n.faulted(FaultStale)
	}
	if len(snapshot.Inputs) == 0 {
		// The panel is up but has no pins configured, so it can tell us nothing
		// about its buttons. Treat that the same as not answering at all.
		log.Printf("NetworkEStopPanel: %s reported no configured inputs", n.url)
		return n.faulted(FaultUnreachable)
	}
	n.status.Store(PanelStatusActive)
	if n.fieldEStop {
		return snapshot.Inputs
	}
	// Dropped after the empty check above, not before: a panel with no inputs at all
	// still faults, but one whose only stop is a field e-stop nobody fitted is just quiet.
	inputs := make([]InputState, 0, len(snapshot.Inputs))
	for _, input := range snapshot.Inputs {
		if input.Station != "all" {
			inputs = append(inputs, input)
		}
	}
	return inputs
}

// faulted builds a fault report for each e-stop this panel is responsible for.
// A-stops are omitted: they are single-channel and latch on match phase, so
// synthesising an a-stop press on a network blip would strand the field in a
// state the operator cannot clear.
func (n *NetworkEStopPanel) faulted(kind FaultKind) []InputState {
	n.status.Store(PanelStatusError)
	inputs := make([]InputState, 0, len(n.stations))
	for _, station := range n.stations {
		inputs = append(inputs, InputState{
			Station: station,
			State:   StopFault,
			Fault:   kind,
		})
	}
	return inputs
}
