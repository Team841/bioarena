package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/team841/bioarena/hardware"
	"gopkg.in/yaml.v3"
)

// fakeReader is a controllable gpioReader for tests.
type fakeReader struct {
	inputs []hardware.InputState
	closed bool
}

func (f *fakeReader) Read() []hardware.InputState { return f.inputs }
func (f *fakeReader) Close()                      { f.closed = true }

// setupTest sets the package-level globals for a single test and
// registers a cleanup that restores a no-op sampler. The sampler runs with a
// zero interval so tests drive sampling rather than a ticker.
func setupTest(t *testing.T, inputs []hardware.InputState, c PanelConfig) *fakeReader {
	t.Helper()
	fake := &fakeReader{inputs: inputs}
	mu.Lock()
	smpl = newSampler(fake, 0)
	cfg = c
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		smpl = newSampler(newNoopReader(), 0)
		cfg = PanelConfig{}
		mu.Unlock()
	})
	return fake
}

// --- stationNames ---

func TestStationNamesRed(t *testing.T) {
	assert.Equal(t, [3]string{"R1", "R2", "R3"}, stationNames("red"))
}

func TestStationNamesBlue(t *testing.T) {
	assert.Equal(t, [3]string{"B1", "B2", "B3"}, stationNames("blue"))
}

func TestStationNamesDefaultsToBlue(t *testing.T) {
	assert.Equal(t, [3]string{"B1", "B2", "B3"}, stationNames(""))
}

// --- noopReader ---

func TestNoopReaderReturnsNil(t *testing.T) {
	r := newNoopReader()
	assert.Nil(t, r.Read())
}

func TestNoopReaderCloseNoPanic(t *testing.T) {
	r := newNoopReader()
	assert.NotPanics(t, func() { r.Close() })
}

// --- PinPair decoding ---

func TestPinPairYamlMapping(t *testing.T) {
	var p PinPair
	require.NoError(t, yaml.Unmarshal([]byte("{nc: 17, no: 4}"), &p))
	assert.Equal(t, PinPair{NC: 17, NO: 4}, p)
	assert.True(t, p.wired())
	assert.True(t, p.dual())
}

func TestPinPairYamlBareIntIsSingleChannel(t *testing.T) {
	// Configs written before dual-channel carry a bare pin number.
	var p PinPair
	require.NoError(t, yaml.Unmarshal([]byte("27"), &p))
	assert.Equal(t, PinPair{NC: 0, NO: 27}, p)
	assert.True(t, p.wired())
	assert.False(t, p.dual(), "a bare pin has no second channel to compare against")
}

func TestPinPairYamlZeroIsUnwired(t *testing.T) {
	var p PinPair
	require.NoError(t, yaml.Unmarshal([]byte("0"), &p))
	assert.False(t, p.wired())
}

func TestPinPairJsonMapping(t *testing.T) {
	var p PinPair
	require.NoError(t, json.Unmarshal([]byte(`{"nc":5,"no":6}`), &p))
	assert.Equal(t, PinPair{NC: 5, NO: 6}, p)
}

func TestPinPairJsonBareInt(t *testing.T) {
	var p PinPair
	require.NoError(t, json.Unmarshal([]byte(`22`), &p))
	assert.Equal(t, PinPair{NC: 0, NO: 22}, p)
}

func TestPinConfigYamlRoundTrip(t *testing.T) {
	// The "no" key must survive YAML's boolean-looking scalars.
	in := `
alliance: red
http_port: 8765
gpio_chip: gpiochip0
pins:
  station1_estop: {nc: 17, no: 4}
  station1_astop: 27
  field_estop: {nc: 5, no: 6}
`
	var c PanelConfig
	require.NoError(t, yaml.Unmarshal([]byte(in), &c))
	assert.Equal(t, "red", c.Alliance)
	assert.Equal(t, PinPair{NC: 17, NO: 4}, c.Pins.Station1EStop)
	assert.Equal(t, PinPair{NC: 0, NO: 27}, c.Pins.Station1AStop)
	assert.Equal(t, PinPair{NC: 5, NO: 6}, c.Pins.FieldEStop)
	assert.False(t, c.Pins.Station2EStop.wired())

	// Marshalling and re-reading must not lose the pairs.
	data, err := yaml.Marshal(&c)
	require.NoError(t, err)
	var back PanelConfig
	require.NoError(t, yaml.Unmarshal(data, &back))
	assert.Equal(t, c, back)
}

// --- sampler ---

func TestSamplerTakesInitialSample(t *testing.T) {
	fake := &fakeReader{inputs: []hardware.InputState{{Station: "R1", State: hardware.StopOK}}}
	s := newSampler(fake, 0)
	snapshot := s.snapshot("red")
	assert.Equal(t, "red", snapshot.Alliance)
	assert.Len(t, snapshot.Inputs, 1)
	assert.GreaterOrEqual(t, snapshot.AgeMs, int64(0))
}

func TestSamplerSnapshotNeverNil(t *testing.T) {
	s := newSampler(newNoopReader(), 0)
	assert.NotNil(t, s.snapshot("blue").Inputs)
}

func TestSamplerCloseClosesReader(t *testing.T) {
	fake := &fakeReader{}
	s := newSampler(fake, 0)
	s.Close()
	assert.True(t, fake.closed)
}

// --- GET /health ---

func TestHandleHealthOK(t *testing.T) {
	setupTest(t, []hardware.InputState{{Station: "R1", State: hardware.StopOK}}, PanelConfig{Alliance: "red"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleHealthReportsFault(t *testing.T) {
	setupTest(t, []hardware.InputState{
		{Station: "R1", State: hardware.StopOK},
		{Station: "R2", State: hardware.StopFault, Fault: hardware.FaultBothOpen},
	}, PanelConfig{Alliance: "red"})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "R2")
}

// --- GET /poll ---

func TestHandlePollEmptyReturnsArray(t *testing.T) {
	setupTest(t, nil, PanelConfig{Alliance: "red"})
	req := httptest.NewRequest(http.MethodGet, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Must be [] not null so the arena range loop works cleanly.
	var snapshot hardware.PanelSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	assert.NotNil(t, snapshot.Inputs)
	assert.Empty(t, snapshot.Inputs)
	assert.Equal(t, "red", snapshot.Alliance)
}

func TestHandlePollReportsEveryInput(t *testing.T) {
	// Released inputs are reported explicitly; the arena must not have to infer
	// health from absence.
	want := []hardware.InputState{
		{Station: "R1", IsAStop: false, State: hardware.StopOK},
		{Station: "R2", IsAStop: false, State: hardware.StopActive},
		{Station: "R3", IsAStop: false, State: hardware.StopFault, Fault: hardware.FaultBothClosed},
		{Station: "R1", IsAStop: true, State: hardware.StopOK},
	}
	setupTest(t, want, PanelConfig{Alliance: "red"})
	req := httptest.NewRequest(http.MethodGet, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var snapshot hardware.PanelSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	assert.Equal(t, want, snapshot.Inputs)
}

func TestHandlePollReportsSampleAge(t *testing.T) {
	setupTest(t, []hardware.InputState{{Station: "R1", State: hardware.StopOK}}, PanelConfig{Alliance: "red"})
	req := httptest.NewRequest(http.MethodGet, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)

	var snapshot hardware.PanelSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snapshot))
	assert.GreaterOrEqual(t, snapshot.AgeMs, int64(0))
	assert.Less(t, snapshot.AgeMs, int64(1000))
}

func TestHandlePollMethodNotAllowed(t *testing.T) {
	setupTest(t, nil, PanelConfig{})
	req := httptest.NewRequest(http.MethodPost, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// --- GET /config ---

func TestHandleConfigGet(t *testing.T) {
	c := PanelConfig{
		Alliance: "red",
		HTTPPort: 8765,
		GpioChip: "gpiochip0",
		Pins: PinConfig{
			Station1EStop: PinPair{NC: 17, NO: 4},
			FieldEStop:    PinPair{NC: 5, NO: 6},
		},
	}
	setupTest(t, nil, c)
	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	w := httptest.NewRecorder()
	handleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var got PanelConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, c, got)
}

// --- POST /config ---

func TestHandleConfigPostUpdatesConfig(t *testing.T) {
	old := setupTest(t, nil, PanelConfig{Alliance: "red"})

	// POST /config needs saveConfig to write estop-panel.yaml; redirect to temp dir.
	orig, err := os.Getwd()
	require.NoError(t, err)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	t.Cleanup(func() { os.Chdir(orig) })

	update := PanelConfig{
		Alliance: "blue",
		HTTPPort: 9000,
		GpioChip: "gpiochip0",
		Pins:     PinConfig{Station1EStop: PinPair{NC: 17, NO: 4}},
	}
	body, err := json.Marshal(update)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	mu.RLock()
	gotCfg := cfg
	mu.RUnlock()
	assert.Equal(t, update, gotCfg)

	// Old reader must have been closed.
	assert.True(t, old.closed, "old reader Close() should have been called")

	// Restore a test sampler for the cleanup path.
	mu.Lock()
	smpl.Close()
	smpl = newSampler(newNoopReader(), 0)
	mu.Unlock()
}

func TestHandleConfigPostAcceptsLegacyBarePins(t *testing.T) {
	setupTest(t, nil, PanelConfig{Alliance: "red"})

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { os.Chdir(orig) })

	body := `{"alliance":"red","http_port":8765,"gpio_chip":"gpiochip0","pins":{"station1_estop":17}}`
	req := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleConfig(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mu.RLock()
	gotCfg := cfg
	mu.RUnlock()
	assert.Equal(t, PinPair{NC: 0, NO: 17}, gotCfg.Pins.Station1EStop)

	mu.Lock()
	smpl.Close()
	smpl = newSampler(newNoopReader(), 0)
	mu.Unlock()
}

func TestHandleConfigPostInvalidJSON(t *testing.T) {
	setupTest(t, nil, PanelConfig{})
	req := httptest.NewRequest(http.MethodPost, "/config", strings.NewReader("not json"))
	w := httptest.NewRecorder()
	handleConfig(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleConfigMethodNotAllowed(t *testing.T) {
	setupTest(t, nil, PanelConfig{})
	req := httptest.NewRequest(http.MethodDelete, "/config", nil)
	w := httptest.NewRecorder()
	handleConfig(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
