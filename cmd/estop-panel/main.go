// Binary estop-panel runs on a Raspberry Pi attached to hardware e-stop
// buttons. It samples GPIO pins on a local timer and serves the decoded state
// of every configured input over HTTP so the main bioarena can poll it.
//
// E-stops are dual-channel: an NC and an NO contact on separate conductors,
// sharing a common ground. Disagreement between the two channels is a wiring
// fault, which the panel reports as its own state rather than silently
// treating as "not pressed". A-stops stay single-channel.
//
// Sampling runs on a local ticker rather than inside the HTTP handler because
// the discrepancy window that distinguishes a fault from a button in travel
// needs a sample rate the network cannot influence.
//
// Configuration is read from estop-panel.yaml in the working directory.
// POST /config replaces the full config, re-opens GPIO, and persists to disk.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/team841/bioarena/hardware"
	"gopkg.in/yaml.v3"
)

// sampleInterval is the local GPIO sample period (100 Hz). The main Pi's poll
// rate is independent of it; hardware.maxSnapshotAge is what ties the two
// together.
const sampleInterval = 10 * time.Millisecond

// PinPair is the GPIO pin pair for one input, as BCM numbers.
//
// NO alone is a single-channel input with no fault detection — correct for
// A-stops, and what every pre-dual-channel config decodes to. NC = 0 means
// single-channel; both zero means the input is not wired and is skipped.
type PinPair struct {
	NC int `yaml:"nc" json:"nc"`
	NO int `yaml:"no" json:"no"`
}

// wired reports whether this input is configured at all.
func (p PinPair) wired() bool { return p.NO != 0 }

// dual reports whether this input has fault detection.
func (p PinPair) dual() bool { return p.NC != 0 && p.NO != 0 }

// UnmarshalYAML accepts either a mapping ({nc: 17, no: 4}) or a bare int,
// which is read as NO-only. The bare form is what configs written before
// dual-channel contain, and it keeps working unchanged.
func (p *PinPair) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var pin int
		if err := value.Decode(&pin); err != nil {
			return err
		}
		p.NC, p.NO = 0, pin
		return nil
	}
	type raw PinPair // shed the custom unmarshaler to avoid recursing
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*p = PinPair(r)
	return nil
}

// UnmarshalJSON mirrors UnmarshalYAML so POST /config accepts both forms too.
func (p *PinPair) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] != '{' {
		var pin int
		if err := json.Unmarshal(trimmed, &pin); err != nil {
			return err
		}
		p.NC, p.NO = 0, pin
		return nil
	}
	type raw PinPair
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*p = PinPair(r)
	return nil
}

// PinConfig maps logical e-stop roles to GPIO pin pairs.
type PinConfig struct {
	Station1EStop PinPair `yaml:"station1_estop" json:"station1_estop"`
	Station1AStop PinPair `yaml:"station1_astop" json:"station1_astop"`
	Station2EStop PinPair `yaml:"station2_estop" json:"station2_estop"`
	Station2AStop PinPair `yaml:"station2_astop" json:"station2_astop"`
	Station3EStop PinPair `yaml:"station3_estop" json:"station3_estop"`
	Station3AStop PinPair `yaml:"station3_astop" json:"station3_astop"`
	FieldEStop    PinPair `yaml:"field_estop"    json:"field_estop"`
}

// PanelConfig is the in-memory representation of estop-panel.yaml.
type PanelConfig struct {
	Alliance string    `yaml:"alliance"  json:"alliance"` // "red" or "blue"
	HTTPPort int       `yaml:"http_port" json:"http_port"`
	GpioChip string    `yaml:"gpio_chip" json:"gpio_chip"`
	Pins     PinConfig `yaml:"pins"      json:"pins"`
}

// gpioReader abstracts GPIO access; implemented per platform.
type gpioReader interface {
	// Read returns the decoded state of every configured input.
	Read() []hardware.InputState
	// Close releases all opened GPIO lines.
	Close()
}

// noopReader is returned when GPIO is unavailable.
type noopReader struct{}

func newNoopReader() gpioReader                   { return &noopReader{} }
func (n *noopReader) Read() []hardware.InputState { return nil }
func (n *noopReader) Close()                      {}

// sampler polls a gpioReader on a fixed interval and holds the latest result,
// so /poll answers from memory and the sample rate is decoupled from HTTP.
type sampler struct {
	reader gpioReader

	mu     sync.RWMutex
	inputs []hardware.InputState
	at     time.Time

	quit chan struct{}
	done chan struct{}
}

// newSampler takes one sample immediately, then keeps sampling every interval.
// An interval of 0 skips the goroutine entirely and leaves sampling to manual
// calls, which is what the tests use.
func newSampler(r gpioReader, interval time.Duration) *sampler {
	s := &sampler{
		reader: r,
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	s.sample()
	if interval <= 0 {
		close(s.done)
		return s
	}
	go s.run(interval)
	return s
}

func (s *sampler) run(interval time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-ticker.C:
			s.sample()
		}
	}
}

func (s *sampler) sample() {
	inputs := s.reader.Read()
	now := time.Now()
	s.mu.Lock()
	s.inputs, s.at = inputs, now
	s.mu.Unlock()
}

// snapshot reports the latest sample and how long ago it was taken, measured
// against this panel's own clock so the main Pi needs no clock sync with us.
func (s *sampler) snapshot(alliance string) hardware.PanelSnapshot {
	s.mu.RLock()
	inputs, at := s.inputs, s.at
	s.mu.RUnlock()
	if inputs == nil {
		inputs = []hardware.InputState{} // return [] not null
	}
	return hardware.PanelSnapshot{
		Alliance: alliance,
		AgeMs:    time.Since(at).Milliseconds(),
		Inputs:   inputs,
	}
}

// Close stops the sampling goroutine and releases the underlying GPIO lines.
func (s *sampler) Close() {
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	<-s.done
	s.reader.Close()
}

const cfgPath = "estop-panel.yaml"

var (
	mu   sync.RWMutex
	cfg  PanelConfig
	smpl *sampler
)

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("load config: %v", err)
	}

	r, err := openGPIO(cfg.GpioChip, cfg.Pins, cfg.Alliance)
	if err != nil {
		log.Printf("WARNING: could not open GPIO: %v — polls will report no inputs", err)
		r = newNoopReader()
	}
	smpl = newSampler(r, sampleInterval)

	mux := http.NewServeMux()
	mux.HandleFunc("/poll", handlePoll)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/config", handleConfig)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	log.Printf("estop-panel (%s) listening on %s", cfg.Alliance, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}

func loadConfig() error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &cfg)
}

func saveConfig() error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

// stationNames returns [station1, station2, station3] names for the given alliance.
func stationNames(alliance string) [3]string {
	if alliance == "red" {
		return [3]string{"R1", "R2", "R3"}
	}
	return [3]string{"B1", "B2", "B3"}
}

// handleHealth reports 200 unless an input is currently faulted, so a panel
// with a broken sense loop shows up in monitoring and not only at match start.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	mu.RLock()
	snapshot := smpl.snapshot(cfg.Alliance)
	mu.RUnlock()
	for _, input := range snapshot.Inputs {
		if input.State == hardware.StopFault {
			http.Error(w, fmt.Sprintf("wiring fault on %s: %s", input.Station, input.Fault), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

func handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mu.RLock()
	snapshot := smpl.snapshot(cfg.Alliance)
	mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snapshot)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mu.RLock()
		c := cfg
		mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c)
	case http.MethodPost:
		var update PanelConfig
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Open new GPIO outside the lock to avoid holding it during I/O.
		newReader, err := openGPIO(update.GpioChip, update.Pins, update.Alliance)
		if err != nil {
			log.Printf("WARNING: re-opening GPIO after config update: %v", err)
			newReader = newNoopReader()
		}
		newSmpl := newSampler(newReader, sampleInterval)
		mu.Lock()
		old := smpl
		cfg = update
		smpl = newSmpl
		saveErr := saveConfig()
		mu.Unlock()
		old.Close()
		if saveErr != nil {
			log.Printf("WARNING: saving config: %v", saveErr)
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
