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
)

// fakeReader is a controllable gpioReader for tests.
type fakeReader struct {
	events []hardware.EStopEvent
	closed bool
}

func (f *fakeReader) Read() []hardware.EStopEvent { return f.events }
func (f *fakeReader) Close()                      { f.closed = true }

// setupTest sets the package-level globals for a single test and
// registers a cleanup that restores a no-op reader.
func setupTest(t *testing.T, events []hardware.EStopEvent, c PanelConfig) *fakeReader {
	t.Helper()
	fake := &fakeReader{events: events}
	mu.Lock()
	reader = fake
	cfg = c
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		reader = newNoopReader()
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

// --- GET /health ---

func TestHandleHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- GET /poll ---

func TestHandlePollEmptyReturnsArray(t *testing.T) {
	setupTest(t, nil, PanelConfig{})
	req := httptest.NewRequest(http.MethodGet, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Must be [] not null so arena range loop works cleanly.
	var events []hardware.EStopEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &events))
	assert.NotNil(t, events)
	assert.Empty(t, events)
}

func TestHandlePollWithEvents(t *testing.T) {
	want := []hardware.EStopEvent{
		{Station: "R1", IsAStop: false},
		{Station: "B2", IsAStop: true},
	}
	setupTest(t, want, PanelConfig{})
	req := httptest.NewRequest(http.MethodGet, "/poll", nil)
	w := httptest.NewRecorder()
	handlePoll(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var got []hardware.EStopEvent
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, want, got)
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
		Pins:     PinConfig{Station1EStop: 17, FieldEStop: 27},
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

	update := PanelConfig{Alliance: "blue", HTTPPort: 9000, GpioChip: "gpiochip0"}
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
