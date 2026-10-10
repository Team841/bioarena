//go:build linux

package main

import (
	"fmt"
	"log"
	"time"

	"github.com/team841/bioarena/hardware"
	"github.com/warthog618/go-gpiocdev"
)

type linuxGpioReader struct {
	entries []linuxPinEntry
}

type linuxPinEntry struct {
	pair    PinPair
	nc      *gpiocdev.Line // nil unless the pair has an NC pin
	no      *gpiocdev.Line // nil unless the pair has an NO pin
	filter  hardware.DiscrepancyFilter
	station string
	isAStop bool
	label   string // for log messages
}

// openGPIO opens GPIO lines for each wired pin pair in the config.
// All lines are inputs with the internal pull-up enabled; both channels are
// active-low, so a closed contact reads 0.
func openGPIO(chip string, pins PinConfig, alliance string) (gpioReader, error) {
	stations := stationNames(alliance)
	type pinDef struct {
		pair    PinPair
		station string
		isAStop bool
	}
	defs := []pinDef{
		{pins.Station1EStop, stations[0], false},
		{pins.Station1AStop, stations[0], true},
		{pins.Station2EStop, stations[1], false},
		{pins.Station2AStop, stations[1], true},
		{pins.Station3EStop, stations[2], false},
		{pins.Station3AStop, stations[2], true},
		{pins.FieldEStop, "all", false},
	}

	var entries []linuxPinEntry
	closeAll := func() {
		for _, e := range entries {
			e.close()
		}
	}

	for _, d := range defs {
		if !d.pair.wired() {
			continue
		}
		kind := "e-stop"
		if d.isAStop {
			kind = "a-stop"
		}
		label := fmt.Sprintf("%s %s", d.station, kind)

		entry := linuxPinEntry{
			pair:    d.pair,
			filter:  hardware.DiscrepancyFilter{Window: hardware.DefaultDiscrepancyWindow},
			station: d.station,
			isAStop: d.isAStop,
			label:   label,
		}
		if d.pair.NC != 0 {
			line, err := gpiocdev.RequestLine(chip, d.pair.NC, gpiocdev.AsInput, gpiocdev.WithPullUp)
			if err != nil {
				closeAll()
				return nil, fmt.Errorf("open GPIO chip %q NC pin %d for %s: %w", chip, d.pair.NC, label, err)
			}
			entry.nc = line
		}
		if d.pair.NO != 0 {
			line, err := gpiocdev.RequestLine(chip, d.pair.NO, gpiocdev.AsInput, gpiocdev.WithPullUp)
			if err != nil {
				entry.close()
				closeAll()
				return nil, fmt.Errorf("open GPIO chip %q NO pin %d for %s: %w", chip, d.pair.NO, label, err)
			}
			entry.no = line
		}
		switch {
		case d.pair.dual():
			log.Printf("estop-panel: opened %s NC pin %d / NO pin %d (%s, dual-channel)", chip, d.pair.NC, d.pair.NO, label)
		case d.pair.NC != 0:
			log.Printf("estop-panel: opened %s NC pin %d (%s, single NC contact)", chip, d.pair.NC, label)
		default:
			log.Printf("estop-panel: opened %s NO pin %d (%s, single NO contact)", chip, d.pair.NO, label)
		}
		entries = append(entries, entry)
	}
	return &linuxGpioReader{entries: entries}, nil
}

func (r *linuxGpioReader) Read() []hardware.InputState {
	if len(r.entries) == 0 {
		return nil
	}
	now := time.Now()
	states := make([]hardware.InputState, 0, len(r.entries))
	for i := range r.entries {
		states = append(states, r.entries[i].read(now))
	}
	return states
}

// read samples one input. The entry is taken by pointer because the
// discrepancy filter carries state between samples.
func (e *linuxPinEntry) read(now time.Time) hardware.InputState {
	out := hardware.InputState{Station: e.station, IsAStop: e.isAStop}

	var ncVal, noVal int
	var err error
	if e.nc != nil {
		if ncVal, err = e.nc.Value(); err != nil {
			log.Printf("estop-panel: %s NC read error: %v", e.label, err)
			out.State, out.Fault = hardware.StopFault, hardware.FaultReadError
			return out
		}
	}
	if e.no != nil {
		if noVal, err = e.no.Value(); err != nil {
			log.Printf("estop-panel: %s NO read error: %v", e.label, err)
			out.State, out.Fault = hardware.StopFault, hardware.FaultReadError
			return out
		}
	}

	out.State, out.Fault = decodeSample(e.pair, ncVal, noVal, &e.filter, now)
	return out
}

// close releases whichever lines this entry opened.
func (e *linuxPinEntry) close() {
	if e.nc != nil {
		_ = e.nc.Close()
	}
	if e.no != nil {
		_ = e.no.Close()
	}
}

func (r *linuxGpioReader) Close() {
	for i := range r.entries {
		r.entries[i].close()
	}
}
