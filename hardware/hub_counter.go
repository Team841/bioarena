// Portions Copyright Team 841. All Rights Reserved.
//
// Receiver for the Hub FUEL counter feed.
//
// A webcam watching the field counts FUEL into both HUBs and reports two cumulative
// totals over UDP. This is the listening half. docs/spec-hub-fuel-counter-feed.md is
// the contract, and is the document the counter's author works from -- anything here
// that contradicts it is a bug in this file.
//
// The receiver stands in for upstream's PLC Hub counters: GetHubCounts and ResetMatch
// are the two methods a plc.HubCountSource owes, so the arena's scoring lines stay
// upstream's. Nothing here takes the arena lock, and nothing the arena calls blocks on
// the network.

package hardware

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// The only protocol version this understands. A counter speaking anything else is
	// dropped rather than guessed at: a miscounted match is worse than an obvious one.
	hubCounterProtocolVersion = 1

	// Size limit on a count datagram. The real thing is ~130 bytes; this is loose
	// enough to never bite a well-formed sender and tight enough that a stray large
	// packet is rejected before it reaches the JSON decoder.
	maxHubCountDatagramBytes = 512

	// How long without an accepted datagram before the counter is called offline. Ten
	// missed heartbeats at the specified 100ms rate: long enough that a couple of
	// dropped packets are not an outage, short enough that a dead camera is visible
	// before the next match starts.
	hubCounterOfflineAfter = time.Second

	// Bounds how often any one drop reason is logged. A counter sending malformed
	// datagrams sends them at 10Hz, and a log full of those is a log nobody reads.
	hubCounterLogInterval = time.Minute

	maxHubCounterSessionLen = 32
	maxHubCounterInfoLen    = 64
)

// Counter reachability, for the match panel badge. NOT CONFIGURED is the arena's to
// report: it is the absence of a receiver, not a state one can be in.
const (
	HubCounterOnline  = "ONLINE"
	HubCounterOffline = "OFFLINE"
)

// Drop reasons, which double as the keys rate-limiting their own log lines. A closed
// set, so the rate-limit map cannot grow without bound however much junk arrives.
const (
	dropUnknownSource = "unknown source"
	dropOversize      = "oversize"
	dropMalformed     = "malformed"
	dropStaleSeq      = "stale sequence"
	dropCountRegress  = "count went backwards"
)

// hubCountPacket is the wire form of a count datagram.
//
// The required numbers are pointers so that a field the counter forgot is
// distinguishable from one it sent as zero -- and zero is a legitimate value for all
// three, so the distinction is real. Unknown fields are ignored, leaving the counter
// free to add its own.
type hubCountPacket struct {
	V       int     `json:"v"`
	Session string  `json:"session"`
	Seq     *uint64 `json:"seq"`
	Red     *int    `json:"red"`
	Blue    *int    `json:"blue"`
	AgeMs   int     `json:"age_ms"`
	Info    string  `json:"info"`
}

// validate applies the parse half of the acceptance rules. The error is the log line,
// so it says what was wrong rather than that something was.
func (packet *hubCountPacket) validate() error {
	if packet.V != hubCounterProtocolVersion {
		return fmt.Errorf("protocol version %d, want %d", packet.V, hubCounterProtocolVersion)
	}
	if packet.Session == "" {
		return fmt.Errorf("no session")
	}
	if len(packet.Session) > maxHubCounterSessionLen {
		return fmt.Errorf("session is %d characters, limit %d", len(packet.Session), maxHubCounterSessionLen)
	}
	if !isHubCounterSessionId(packet.Session) {
		return fmt.Errorf("session has characters outside [A-Za-z0-9_-]")
	}
	if packet.Seq == nil {
		return fmt.Errorf("no seq")
	}
	if packet.Red == nil || packet.Blue == nil {
		return fmt.Errorf("both red and blue counts are required")
	}
	if *packet.Red < 0 || *packet.Blue < 0 {
		return fmt.Errorf("negative count (red %d, blue %d)", *packet.Red, *packet.Blue)
	}
	if packet.AgeMs < 0 {
		return fmt.Errorf("negative age_ms %d", packet.AgeMs)
	}
	return nil
}

// isHubCounterSessionId reports whether the session identifier is within the charset
// the spec allows. Enforced because the identifier reaches log lines, and a session
// that can carry newlines can forge them.
func isHubCounterSessionId(session string) bool {
	for _, r := range session {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// hubCounterRecord is the last accepted datagram, plus when it arrived.
type hubCounterRecord struct {
	Session    string
	Seq        uint64
	Red, Blue  int // cumulative within Session
	ReceivedAt time.Time
	AgeMs      int // from the last datagram on which a count rose
	Info       string
}

// HubMatchContext is everything the status reply says about the match. The arena
// publishes one each tick; the receiver never reads arena state itself, which is what
// keeps the listener goroutine off the arena lock.
type HubMatchContext struct {
	MatchState                string
	MatchTimeSec              float64
	Shift                     string
	RedActive, BlueActive     bool
	RedCount, BlueCount       int // FUEL counted this match, active hub or not
	RedCredited, BlueCredited int
	RedAuto, BlueAuto         int
}

// HubCounterReceiver listens for count datagrams from one configured counter and
// presents them to the arena as match-relative Hub counts.
type HubCounterReceiver struct {
	sourceAddr string // the only address datagrams are accepted from, as configured
	sourceIP   net.IP // the same, parsed, so the comparison is on addresses not spelling
	conn       *net.UDPConn
	closed     atomic.Bool
	context    atomic.Pointer[HubMatchContext]

	// mu guards everything below. The listener goroutine writes the record and carries
	// the baseline forward across a counter restart; the arena goroutine reads counts
	// and re-baselines at ResetMatch. Those have to move together -- a restart observed
	// halfway through a re-baseline would lose or double a session's contribution --
	// so one lock covers the lot rather than an atomic per field.
	mu sync.Mutex

	last *hubCounterRecord // nil until the first datagram is accepted

	// baseSession and the baseline counts are where the current match started within
	// that session; carried is what earlier sessions contributed to it. Empty
	// baseSession means the next accepted datagram sets the baseline, which is how a
	// counter that was already running before the match does not dump its whole day
	// into it.
	baseSession             string
	baseRed, baseBlue       int
	carriedRed, carriedBlue int

	online     bool
	lastLogged map[string]time.Time
}

// NewHubCounterReceiver binds the listening socket and returns a receiver that accepts
// datagrams from sourceAddr only.
//
// Binding here rather than in Run means a port already in use is an error the operator
// sees when they configure it, not a line in the log an hour later. Port 0 lets the OS
// choose, which tests use; the arena treats a configured port of 0 as "no counter" and
// never gets this far.
func NewHubCounterReceiver(sourceAddr string, port int) (*HubCounterReceiver, error) {
	sourceAddr = strings.TrimSpace(sourceAddr)
	if sourceAddr == "" {
		return nil, fmt.Errorf("hub counter: no source address configured")
	}
	sourceIP := net.ParseIP(sourceAddr)
	if sourceIP == nil {
		return nil, fmt.Errorf("hub counter: %q is not an IP address", sourceAddr)
	}
	udpAddress, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("hub counter: %v", err)
	}
	conn, err := net.ListenUDP("udp4", udpAddress)
	if err != nil {
		return nil, fmt.Errorf("hub counter: could not listen on UDP port %d: %v", port, err)
	}
	return &HubCounterReceiver{
		sourceAddr: sourceAddr,
		sourceIP:   sourceIP,
		conn:       conn,
		lastLogged: make(map[string]time.Time),
	}, nil
}

// Port is the port actually bound, which differs from the configured one only when the
// OS chose it. For tests and for the startup log line.
func (receiver *HubCounterReceiver) Port() int {
	return receiver.conn.LocalAddr().(*net.UDPAddr).Port
}

// Close stops Run and releases the socket.
func (receiver *HubCounterReceiver) Close() error {
	receiver.closed.Store(true)
	return receiver.conn.Close()
}

// SetMatchContext publishes what the next status reply should say about the match.
// Called from the arena loop each tick; never blocks.
func (receiver *HubCounterReceiver) SetMatchContext(context HubMatchContext) {
	receiver.context.Store(&context)
}

// Run reads datagrams until Close. Call it in a goroutine.
//
// A read error ends the loop rather than retrying: the socket is bound for the process
// lifetime, so an error on it is not a condition that clears. The counter then reads
// OFFLINE, which blocks a Counted-mode match from starting and disturbs nothing else --
// scoring is not a safety function, and a dead camera must not stop a field.
func (receiver *HubCounterReceiver) Run() {
	log.Printf("Listening for the Hub FUEL counter on UDP port %d (accepting %s)",
		receiver.Port(), receiver.sourceAddr)

	buffer := make([]byte, 2048)
	for {
		count, from, err := receiver.conn.ReadFromUDP(buffer)
		if err != nil {
			if !receiver.closed.Load() {
				log.Printf("Hub counter: read failed, stopping: %v", err)
			}
			return
		}
		receiver.handleDatagram(buffer[:count], from)
	}
}

// handleDatagram applies the acceptance rules to one datagram and, if it is accepted,
// replies with the match status.
func (receiver *HubCounterReceiver) handleDatagram(datagram []byte, from *net.UDPAddr) {
	// Source first, before parsing: a datagram from anywhere but the configured counter
	// is not ours to read. On this field that excludes every team VLAN, so a robot
	// cannot post scores.
	// Compared as addresses rather than as text: the same IPv4 address reaches us in
	// more than one spelling, and a 4-in-6 form must not read as a stranger.
	if from == nil || !from.IP.Equal(receiver.sourceIP) {
		receiver.logDrop(dropUnknownSource, "Hub counter: ignoring datagram from %v", from)
		return
	}
	if len(datagram) > maxHubCountDatagramBytes {
		receiver.logDrop(dropOversize, "Hub counter: datagram of %d bytes, limit %d",
			len(datagram), maxHubCountDatagramBytes)
		return
	}

	var packet hubCountPacket
	if err := json.Unmarshal(datagram, &packet); err != nil {
		receiver.logDrop(dropMalformed, "Hub counter: could not parse datagram: %v", err)
		return
	}
	if err := packet.validate(); err != nil {
		receiver.logDrop(dropMalformed, "Hub counter: rejected datagram: %v", err)
		return
	}
	// Truncated rather than rejected: info is a commissioning tooltip, and throwing
	// away a score over the length of a comment would be the wrong trade.
	if len(packet.Info) > maxHubCounterInfoLen {
		packet.Info = packet.Info[:maxHubCounterInfoLen]
	}

	record, ok := receiver.record(&packet)
	if !ok {
		return
	}
	receiver.reply(record, from)
}

// record applies the ordering and monotonicity rules and, if the datagram is accepted,
// folds it into the receiver's state. It returns the stored record.
func (receiver *HubCounterReceiver) record(packet *hubCountPacket) (*hubCounterRecord, bool) {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()

	previous := receiver.last
	sameSession := previous != nil && previous.Session == packet.Session

	if sameSession {
		// Within a session, seq orders the datagrams: anything not ahead of the last one
		// is a duplicate or arrived out of order, and a cumulative count that went down
		// cannot have come from a counter that is behaving.
		if *packet.Seq <= previous.Seq {
			receiver.logDropLocked(dropStaleSeq, "Hub counter: seq %d is not ahead of %d",
				*packet.Seq, previous.Seq)
			return nil, false
		}
		if *packet.Red < previous.Red || *packet.Blue < previous.Blue {
			receiver.logDropLocked(dropCountRegress,
				"Hub counter: counts went backwards (red %d->%d, blue %d->%d)",
				previous.Red, *packet.Red, previous.Blue, *packet.Blue)
			return nil, false
		}
	}

	// A session we have not seen means the counter restarted -- or that this is the
	// first we have heard from it. Either way, bank what the outgoing session
	// contributed to this match and start measuring the new one from where we find it.
	// Measuring from the observed count rather than assuming zero costs at most a ball
	// scored in the counter's first 100ms, and makes it impossible for a counter that
	// resumes from saved state to dump its history into the match.
	if !sameSession {
		if previous != nil {
			receiver.carriedRed += previous.Red - receiver.baseRed
			receiver.carriedBlue += previous.Blue - receiver.baseBlue
			log.Printf("Hub counter restarted (session %s -> %s); carrying %d red and %d blue",
				previous.Session, packet.Session, receiver.carriedRed, receiver.carriedBlue)
		}
		receiver.baseSession = packet.Session
		receiver.baseRed = *packet.Red
		receiver.baseBlue = *packet.Blue
	}

	// age_ms describes the most recent increment, so a heartbeat carrying no new FUEL
	// must not overwrite it with the age of a ball scored long ago.
	ageMs := packet.AgeMs
	if previous != nil && *packet.Red == previous.Red && *packet.Blue == previous.Blue {
		ageMs = previous.AgeMs
	}

	record := &hubCounterRecord{
		Session:    packet.Session,
		Seq:        *packet.Seq,
		Red:        *packet.Red,
		Blue:       *packet.Blue,
		ReceivedAt: time.Now(),
		AgeMs:      ageMs,
		Info:       packet.Info,
	}
	receiver.last = record
	if !receiver.online {
		receiver.online = true
		log.Printf("Hub counter online (session %s)", record.Session)
	}
	return record, true
}

// reply answers an accepted datagram with the match status. Best effort: the counter is
// told not to depend on it, so a failed send is logged and forgotten.
func (receiver *HubCounterReceiver) reply(record *hubCounterRecord, to *net.UDPAddr) {
	context := receiver.context.Load()
	if context == nil {
		context = &HubMatchContext{}
	}
	matchState := context.MatchState
	if matchState == "" {
		matchState = "UNKNOWN"
	}
	shift := context.Shift
	if shift == "" {
		shift = "NONE"
	}

	status := hubCounterStatusDatagram{
		V:          hubCounterProtocolVersion,
		Seq:        record.Seq,
		MatchState: matchState,
		// Two decimal places: enough for a counter overlaying the match clock, and it
		// keeps float noise out of a line a person may have to read.
		MatchTimeS: math.Round(context.MatchTimeSec*100) / 100,
		Shift:      shift,
		HubActive:  hubPairBool{Red: context.RedActive, Blue: context.BlueActive},
		MatchCount: hubPair{Red: context.RedCount, Blue: context.BlueCount},
		Credited:   hubPair{Red: context.RedCredited, Blue: context.BlueCredited},
		AutoCount:  hubPair{Red: context.RedAuto, Blue: context.BlueAuto},
	}
	encoded, err := json.Marshal(&status)
	if err != nil {
		log.Printf("Hub counter: could not encode status reply: %v", err)
		return
	}
	if _, err := receiver.conn.WriteToUDP(encoded, to); err != nil && !receiver.closed.Load() {
		log.Printf("Hub counter: could not send status reply to %v: %v", to, err)
	}
}

type hubPair struct {
	Red  int `json:"red"`
	Blue int `json:"blue"`
}

type hubPairBool struct {
	Red  bool `json:"red"`
	Blue bool `json:"blue"`
}

type hubCounterStatusDatagram struct {
	V          int         `json:"v"`
	Seq        uint64      `json:"seq"`
	MatchState string      `json:"match_state"`
	MatchTimeS float64     `json:"match_time_s"`
	Shift      string      `json:"shift"`
	HubActive  hubPairBool `json:"hub_active"`
	MatchCount hubPair     `json:"match_count"`
	Credited   hubPair     `json:"credited"`
	AutoCount  hubPair     `json:"auto_count"`
}

// GetHubCounts returns the FUEL counted into each HUB since the last ResetMatch,
// which is what upstream's PLC reports from its registers and what game.Hub.UpdateState
// expects. A counter that has gone offline contributes its last counts, frozen: that is
// what a dead PLC register would do, and it is the only answer that invents nothing.
func (receiver *HubCounterReceiver) GetHubCounts() (int, int) {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()

	red, blue := receiver.carriedRed, receiver.carriedBlue
	if receiver.last != nil && receiver.last.Session == receiver.baseSession {
		red += receiver.last.Red - receiver.baseRed
		blue += receiver.last.Blue - receiver.baseBlue
	}
	return red, blue
}

// ResetMatch makes the counts read zero from here, the way upstream's ResetMatch clears
// the PLC's register mirror. The counter itself is never reset and never needs to be.
func (receiver *HubCounterReceiver) ResetMatch() {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()

	receiver.carriedRed, receiver.carriedBlue = 0, 0
	if receiver.last == nil {
		// Nothing heard yet, so there is nothing to measure from. The next accepted
		// datagram becomes the baseline.
		receiver.baseSession = ""
		receiver.baseRed, receiver.baseBlue = 0, 0
		return
	}
	receiver.baseSession = receiver.last.Session
	receiver.baseRed = receiver.last.Red
	receiver.baseBlue = receiver.last.Blue
}

// Status reports whether the counter is currently being heard from.
//
// It logs the transition to offline, which no other call is in a position to notice:
// going quiet is the absence of an event, so it is only observable when someone asks.
// The arena asks every tick.
func (receiver *HubCounterReceiver) Status() string {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()

	if receiver.last != nil && time.Since(receiver.last.ReceivedAt) < hubCounterOfflineAfter {
		return HubCounterOnline
	}
	if receiver.online {
		receiver.online = false
		log.Printf("Hub counter offline: nothing heard for %v", hubCounterOfflineAfter)
	}
	return HubCounterOffline
}

// Telemetry is what the match panel shows about the feed itself, as opposed to the
// scores. Zero values until the first datagram is accepted.
func (receiver *HubCounterReceiver) Telemetry() (ageMs int, info string, session string) {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()

	if receiver.last == nil {
		return 0, "", ""
	}
	return receiver.last.AgeMs, receiver.last.Info, receiver.last.Session
}

// logDrop reports a dropped datagram at most once per hubCounterLogInterval for each
// reason.
func (receiver *HubCounterReceiver) logDrop(reason, format string, args ...any) {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	receiver.logDropLocked(reason, format, args...)
}

func (receiver *HubCounterReceiver) logDropLocked(reason, format string, args ...any) {
	if time.Since(receiver.lastLogged[reason]) < hubCounterLogInterval {
		return
	}
	receiver.lastLogged[reason] = time.Now()
	log.Printf(format, args...)
}
