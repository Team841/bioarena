package hardware

// Compile-time interface assertions.
var _ EStopPanel = (*NoopEStopPanel)(nil)
var _ FieldEStopPanel = (*NoopFieldEStopPanel)(nil)

// NoopEStopPanel reports no inputs at all. Used when no hardware panel is
// configured — distinct from a configured panel that has gone unreachable,
// which reports faults.
type NoopEStopPanel struct{}

func (n *NoopEStopPanel) Poll() []InputState { return nil }

// NoopFieldEStopPanel never triggers. Used when no GPIO pin is configured.
type NoopFieldEStopPanel struct{}

func (n *NoopFieldEStopPanel) State() (StopState, FaultKind) { return StopOK, FaultNone }
func (n *NoopFieldEStopPanel) Clear()                        {}
func (n *NoopFieldEStopPanel) Close()                        {}
