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
	nc      *gpiocdev.Line // nil for single-channel inputs
	no      *gpiocdev.Line
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
			_ = e.no.Close()
			if e.nc != nil {
				_ = e.nc.Close()
			}
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

		noLine, err := gpiocdev.RequestLine(chip, d.pair.NO, gpiocdev.AsInput, gpiocdev.WithPullUp)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("open GPIO chip %q NO pin %d for %s: %w", chip, d.pair.NO, label, err)
		}
		entry := linuxPinEntry{
			no:      noLine,
			filter:  hardware.DiscrepancyFilter{Window: hardware.DefaultDiscrepancyWindow},
			station: d.station,
			isAStop: d.isAStop,
			label:   label,
		}
		if d.pair.dual() {
			ncLine, err := gpiocdev.RequestLine(chip, d.pair.NC, gpiocdev.AsInput, gpiocdev.WithPullUp)
			if err != nil {
				_ = noLine.Close()
				closeAll()
				return nil, fmt.Errorf("open GPIO chip %q NC pin %d for %s: %w", chip, d.pair.NC, label, err)
			}
			entry.nc = ncLine
			log.Printf("estop-panel: opened %s NC pin %d / NO pin %d (%s, dual-channel)", chip, d.pair.NC, d.pair.NO, label)
		} else {
			log.Printf("estop-panel: opened %s NO pin %d (%s, single-channel)", chip, d.pair.NO, label)
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

	noVal, err := e.no.Value()
	if err != nil {
		log.Printf("estop-panel: %s NO read error: %v", e.label, err)
		out.State, out.Fault = hardware.StopFault, hardware.FaultReadError
		return out
	}
	if e.nc == nil {
		out.State = hardware.DecodeSingle(noVal)
		return out
	}
	ncVal, err := e.nc.Value()
	if err != nil {
		log.Printf("estop-panel: %s NC read error: %v", e.label, err)
		out.State, out.Fault = hardware.StopFault, hardware.FaultReadError
		return out
	}

	state, fault := hardware.DecodeChannels(ncVal, noVal)
	out.State, out.Fault = e.filter.Update(state, fault, now)
	return out
}

func (r *linuxGpioReader) Close() {
	for _, e := range r.entries {
		_ = e.no.Close()
		if e.nc != nil {
			_ = e.nc.Close()
		}
	}
}
