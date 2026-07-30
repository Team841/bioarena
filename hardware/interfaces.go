// Package hardware defines interfaces for field hardware drivers.
// Types are defined independently of field/ to avoid circular imports.
package hardware

import "github.com/team841/bioarena/game"

// MatchPhase describes the current field state for hardware drivers.
type MatchPhase int

const (
	PhaseIdle     MatchPhase = iota
	PhaseAuto
	PhasePause
	PhaseTeleop
	PhaseFinished
)

// Alliance identifies which alliance won AUTO.
type Alliance int

const (
	AllianceNone Alliance = iota // tie or randomly assigned at match start
	AllianceRed
	AllianceBlue
)

// LightingState carries all context a FieldLights driver needs.
// SetState is called at every phase transition and shift boundary.
type LightingState struct {
	Phase        MatchPhase
	Shift        game.Shift // spans the whole match; game.ShiftCount when not in one
	AutoWinner   Alliance   // which alliance's HUB goes inactive first in Shift1
	ShiftWarning bool       // true during 3s window before next HUB deactivation
}

// FieldLights controls field indicator lighting.
type FieldLights interface {
	SetState(state LightingState) error
}

// EStopEvent represents a single hardware e-stop or a-stop activation.
type EStopEvent struct {
	Station string // "R1","R2","R3","B1","B2","B3", or "all"
	IsAStop bool   // true = a-stop (driver-initiated), false = e-stop
}

// EStopPanel reads physical e-stop/a-stop inputs via polling.
// Arena calls Poll() each tick; it returns currently-active stops.
// Polling matches the PLC call semantics and avoids goroutine complexity.
type EStopPanel interface {
	Poll() []EStopEvent
}

// FieldEStopPanel is a latching field-wide e-stop button.
// Arena calls Triggered() every loop tick (~10 ms).
// Clear() is called by the web UI after the operator acknowledges the condition.
// Unlike EStopPanel, this interface carries state: once triggered the latch
// persists until Clear() is called while the button is physically released.
type FieldEStopPanel interface {
	Triggered() bool // true while latch is active (button pressed or not yet cleared)
	Clear()          // reset latch; no-op if button is still physically held
}
