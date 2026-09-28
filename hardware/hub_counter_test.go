package hardware

import (
	"encoding/json"

	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The receiver has to be usable wherever upstream reads its PLC Hub counters, which is
// the whole reason the arena's scoring lines can stay upstream's. Asserted structurally
// so that hardware/ need not import plc/ to prove it.
var _ interface {
	GetHubCounts() (int, int)
	ResetMatch()
} = (*HubCounterReceiver)(nil)

// counterRig is a receiver bound to the loopback, plus a socket standing in for the
// counter. Datagrams are handed to handleDatagram directly rather than sent over the
// wire: the acceptance rules are what is under test, and a synchronous call makes the
// assertions deterministic. TestHubCounterReadsFromTheSocket covers the wire.
type counterRig struct {
	t        *testing.T
	receiver *HubCounterReceiver
	client   *net.UDPConn
	from     *net.UDPAddr
	session  string
	seq      uint64
}

func newCounterRig(t *testing.T) *counterRig {
	t.Helper()

	receiver, err := NewHubCounterReceiver("127.0.0.1", 0)
	assert.Nil(t, err)
	t.Cleanup(func() { receiver.Close() })

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	assert.Nil(t, err)
	t.Cleanup(func() { client.Close() })

	return &counterRig{
		t:        t,
		receiver: receiver,
		client:   client,
		from:     client.LocalAddr().(*net.UDPAddr),
		session:  "sessionA",
	}
}

// send delivers a well-formed datagram carrying the given counts, advancing seq.
func (rig *counterRig) send(red, blue int) {
	rig.t.Helper()
	rig.seq++
	rig.sendRaw(map[string]any{
		"v": 1, "session": rig.session, "seq": rig.seq, "red": red, "blue": blue,
	})
}

// sendRaw delivers whatever fields it is given, so a test can omit or corrupt one.
func (rig *counterRig) sendRaw(fields map[string]any) {
	rig.t.Helper()
	encoded, err := json.Marshal(fields)
	assert.Nil(rig.t, err)
	rig.receiver.handleDatagram(encoded, rig.from)
}

// sendFrom delivers a valid datagram from somewhere other than the configured counter.
func (rig *counterRig) sendFrom(ip string, red, blue int) {
	rig.t.Helper()
	rig.seq++
	encoded, _ := json.Marshal(map[string]any{
		"v": 1, "session": rig.session, "seq": rig.seq, "red": red, "blue": blue,
	})
	rig.receiver.handleDatagram(encoded, &net.UDPAddr{IP: net.ParseIP(ip), Port: 9999})
}

// reply reads the status datagram the receiver owes an accepted count, failing the test
// if none arrives.
func (rig *counterRig) reply() hubCounterStatusDatagram {
	rig.t.Helper()
	rig.client.SetReadDeadline(time.Now().Add(2 * time.Second))
	buffer := make([]byte, 2048)
	count, err := rig.client.Read(buffer)
	assert.Nil(rig.t, err)

	var status hubCounterStatusDatagram
	assert.Nil(rig.t, json.Unmarshal(buffer[:count], &status))
	return status
}

// assertNoReply checks that nothing was sent back, which is how a test tells a dropped
// datagram from an accepted one.
func (rig *counterRig) assertNoReply() {
	rig.t.Helper()
	rig.client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buffer := make([]byte, 2048)
	_, err := rig.client.Read(buffer)
	assert.NotNil(rig.t, err, "expected no status reply, got one")
}

// startMatch puts the rig where the arena puts it at the start of a match: the counter
// has been heard from, and ResetMatch has taken a baseline from it. Counts sent after
// this read straight through, which is what most of these tests want to talk about.
func (rig *counterRig) startMatch() {
	rig.t.Helper()
	rig.send(0, 0)
	rig.reply()
	rig.receiver.ResetMatch()
}

// assertCounts checks what the arena would read for this match.
func (rig *counterRig) assertCounts(red, blue int) {
	rig.t.Helper()
	gotRed, gotBlue := rig.receiver.GetHubCounts()
	assert.Equal(rig.t, red, gotRed, "red count")
	assert.Equal(rig.t, blue, gotBlue, "blue count")
}

// --- Construction ---

func TestNewHubCounterReceiverRequiresASourceAddress(t *testing.T) {
	_, err := NewHubCounterReceiver("", 0)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no source address")

	_, err = NewHubCounterReceiver("not-an-address", 0)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "not an IP address")

	// A hostname is refused too: the source check compares literal addresses, and one
	// that resolves elsewhere later would silently stop matching.
	_, err = NewHubCounterReceiver("counter.local", 0)
	assert.NotNil(t, err)
}

func TestNewHubCounterReceiverReportsABusyPort(t *testing.T) {
	first, err := NewHubCounterReceiver("127.0.0.1", 0)
	assert.Nil(t, err)
	defer first.Close()

	_, err = NewHubCounterReceiver("127.0.0.1", first.Port())
	assert.NotNil(t, err, "a port already in use must be an error at configuration time")
}

// --- Acceptance rules ---

func TestHubCounterAcceptsAValidDatagram(t *testing.T) {
	rig := newCounterRig(t)
	rig.receiver.SetMatchContext(HubMatchContext{
		MatchState: "TELEOP_PERIOD", MatchTimeSec: 47.312, Shift: "SHIFT2",
		RedActive: true, BlueActive: false,
		RedCount: 60, BlueCount: 3,
		RedCredited: 57, BlueCredited: 1,
		RedAuto: 12, BlueAuto: 0,
	})

	rig.send(57, 0)

	status := rig.reply()
	assert.Equal(t, 1, status.V)
	assert.Equal(t, rig.seq, status.Seq)
	assert.Equal(t, "TELEOP_PERIOD", status.MatchState)
	assert.Equal(t, 47.31, status.MatchTimeS, "match time is reported to two decimals")
	assert.Equal(t, "SHIFT2", status.Shift)
	assert.Equal(t, hubPairBool{Red: true, Blue: false}, status.HubActive)
	assert.Equal(t, hubPair{Red: 60, Blue: 3}, status.MatchCount)
	assert.Equal(t, hubPair{Red: 57, Blue: 1}, status.Credited)
	assert.Equal(t, hubPair{Red: 12, Blue: 0}, status.AutoCount)
}

// Before the arena has published anything the reply still has to be well-formed, since
// a counter showing an "FMS link" indicator reads it at startup.
func TestHubCounterRepliesBeforeTheArenaPublishesContext(t *testing.T) {
	rig := newCounterRig(t)

	rig.send(0, 0)

	status := rig.reply()
	assert.Equal(t, "UNKNOWN", status.MatchState)
	assert.Equal(t, "NONE", status.Shift)
	assert.Equal(t, 0.0, status.MatchTimeS)
}

func TestHubCounterIgnoresAnUnknownSource(t *testing.T) {
	rig := newCounterRig(t)
	rig.startMatch()
	rig.send(5, 0)
	rig.reply()

	rig.sendFrom("10.9.9.9", 500, 500)

	rig.assertNoReply()
	rig.assertCounts(5, 0)
}

func TestHubCounterRejectsMalformedDatagrams(t *testing.T) {
	rig := newCounterRig(t)

	// Not JSON at all.
	rig.receiver.handleDatagram([]byte("{not json"), rig.from)
	rig.assertNoReply()

	// Over the size limit, which is checked before parsing.
	oversize, _ := json.Marshal(map[string]any{
		"v": 1, "session": "sessionA", "seq": 1, "red": 1, "blue": 0,
		"pad": strings.Repeat("x", maxHubCountDatagramBytes),
	})
	assert.Greater(t, len(oversize), maxHubCountDatagramBytes)
	rig.receiver.handleDatagram(oversize, rig.from)
	rig.assertNoReply()

	rig.assertCounts(0, 0)
}

func TestHubCounterRejectsBadFields(t *testing.T) {
	for name, fields := range map[string]map[string]any{
		"wrong version": {"v": 2, "session": "s", "seq": 1, "red": 1, "blue": 0},
		"no version":    {"session": "s", "seq": 1, "red": 1, "blue": 0},
		"no session":    {"v": 1, "seq": 1, "red": 1, "blue": 0},
		"no seq":        {"v": 1, "session": "s", "red": 1, "blue": 0},
		"no red":        {"v": 1, "session": "s", "seq": 1, "blue": 0},
		"no blue":       {"v": 1, "session": "s", "seq": 1, "red": 1},
		"negative red":  {"v": 1, "session": "s", "seq": 1, "red": -1, "blue": 0},
		"negative blue": {"v": 1, "session": "s", "seq": 1, "red": 1, "blue": -1},
		"negative age":  {"v": 1, "session": "s", "seq": 1, "red": 1, "blue": 0, "age_ms": -5},
		"session too long": {
			"v": 1, "session": strings.Repeat("a", maxHubCounterSessionLen+1),
			"seq": 1, "red": 1, "blue": 0,
		},
		"session with a newline": {"v": 1, "session": "a\nb", "seq": 1, "red": 1, "blue": 0},
		"session with a space":   {"v": 1, "session": "a b", "seq": 1, "red": 1, "blue": 0},
	} {
		rig := newCounterRig(t)
		rig.sendRaw(fields)
		rig.assertNoReply()
		gotRed, gotBlue := rig.receiver.GetHubCounts()
		assert.Equal(t, 0, gotRed, name)
		assert.Equal(t, 0, gotBlue, name)
	}
}

// Zero is a legitimate value for every required number, so a counter that omits one
// must not be read as having sent zero.
func TestHubCounterDistinguishesZeroFromMissing(t *testing.T) {
	rig := newCounterRig(t)

	rig.sendRaw(map[string]any{"v": 1, "session": "s", "seq": 0, "red": 0, "blue": 0})
	status := rig.reply()
	assert.Equal(t, uint64(0), status.Seq, "seq 0 is a legal sequence number")
}

func TestHubCounterRejectsStaleAndDuplicateSequences(t *testing.T) {
	rig := newCounterRig(t)
	rig.startMatch()
	rig.send(1, 0)
	rig.reply()

	// The same seq again, carrying a different count: a duplicate, however it got here.
	rig.sendRaw(map[string]any{"v": 1, "session": rig.session, "seq": rig.seq, "red": 99, "blue": 0})
	rig.assertNoReply()

	// And one that arrived out of order.
	rig.sendRaw(map[string]any{"v": 1, "session": rig.session, "seq": rig.seq - 1, "red": 99, "blue": 0})
	rig.assertNoReply()

	rig.assertCounts(1, 0)
}

func TestHubCounterRejectsCountsThatGoBackwards(t *testing.T) {
	rig := newCounterRig(t)
	rig.startMatch()
	rig.send(10, 7)
	rig.reply()

	rig.send(9, 7) // red fell
	rig.assertNoReply()
	rig.send(10, 6) // blue fell
	rig.assertNoReply()

	rig.assertCounts(10, 7)
}

func TestHubCounterTruncatesOverlongInfo(t *testing.T) {
	rig := newCounterRig(t)

	rig.sendRaw(map[string]any{
		"v": 1, "session": "s", "seq": 1, "red": 1, "blue": 0,
		"info": strings.Repeat("x", 200),
	})
	rig.reply() // still accepted: a tooltip must not cost a score

	_, info, _ := rig.receiver.Telemetry()
	assert.Len(t, info, maxHubCounterInfoLen)
}

// age_ms describes the most recent increment, so a heartbeat must not age it.
func TestHubCounterKeepsAgeFromTheLastIncrement(t *testing.T) {
	rig := newCounterRig(t)

	rig.seq++
	rig.sendRaw(map[string]any{
		"v": 1, "session": rig.session, "seq": rig.seq, "red": 1, "blue": 0, "age_ms": 42,
	})
	rig.reply()
	ageMs, _, _ := rig.receiver.Telemetry()
	assert.Equal(t, 42, ageMs)

	// A heartbeat: same counts, and an age describing a ball scored long ago.
	rig.seq++
	rig.sendRaw(map[string]any{
		"v": 1, "session": rig.session, "seq": rig.seq, "red": 1, "blue": 0, "age_ms": 5000,
	})
	rig.reply()
	ageMs, _, _ = rig.receiver.Telemetry()
	assert.Equal(t, 42, ageMs, "a heartbeat carries no new FUEL and must not age the figure")

	// A real increment replaces it.
	rig.seq++
	rig.sendRaw(map[string]any{
		"v": 1, "session": rig.session, "seq": rig.seq, "red": 2, "blue": 0, "age_ms": 33,
	})
	rig.reply()
	ageMs, _, _ = rig.receiver.Telemetry()
	assert.Equal(t, 33, ageMs)
}

// --- Match counting ---

// A counter that has been running all afternoon must not pour its day into the match
// that is about to start.
func TestHubCounterCountsFromTheMatchBaseline(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(500, 300)
	rig.reply()

	rig.receiver.ResetMatch()
	rig.assertCounts(0, 0)

	rig.send(503, 300)
	rig.reply()
	rig.assertCounts(3, 0)

	rig.send(503, 302)
	rig.reply()
	rig.assertCounts(3, 2)
}

// ResetMatch before the counter has said anything cannot take a baseline, so the first
// datagram has to become one -- otherwise a counter that connects late credits its
// whole history to the match.
func TestHubCounterBaselinesOnTheFirstDatagramAfterAReset(t *testing.T) {
	rig := newCounterRig(t)
	rig.receiver.ResetMatch()
	rig.assertCounts(0, 0)

	rig.send(120, 45)
	rig.reply()
	rig.assertCounts(0, 0)

	rig.send(121, 45)
	rig.reply()
	rig.assertCounts(1, 0)
}

func TestHubCounterHubsCountIndependently(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(0, 0)
	rig.reply()
	rig.receiver.ResetMatch()

	rig.send(4, 0)
	rig.reply()
	rig.assertCounts(4, 0)

	rig.send(4, 9)
	rig.reply()
	rig.assertCounts(4, 9)
}

// The counter may be restarted mid-match and lose nothing but the FUEL scored while it
// was down.
func TestHubCounterCarriesCountsAcrossARestart(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(100, 50)
	rig.reply()
	rig.receiver.ResetMatch()

	rig.send(104, 51)
	rig.reply()
	rig.assertCounts(4, 1)

	// The counter process restarts: new session, counts from zero.
	rig.session = "sessionB"
	rig.seq = 0
	rig.send(0, 0)
	rig.reply()
	// The previous session's contribution is kept.
	rig.assertCounts(4, 1)

	rig.send(2, 3)
	rig.reply()
	rig.assertCounts(6, 4)
}

// A counter that resumes from saved state rather than from zero must not be able to
// dump its history into the match. The new session is measured from wherever it is
// first seen, so the worst case is losing a ball scored in its first heartbeat.
func TestHubCounterRestartResumingFromSavedStateAddsNothing(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(10, 10)
	rig.reply()
	rig.receiver.ResetMatch()
	rig.send(11, 10)
	rig.reply()
	rig.assertCounts(1, 0)

	rig.session = "sessionB"
	rig.seq = 0
	rig.send(9000, 9000)
	rig.reply()
	rig.assertCounts(1, 0)

	rig.send(9001, 9000)
	rig.reply()
	rig.assertCounts(2, 0)
}

// A restart before the match starts leaves nothing to carry: ResetMatch is the line
// between one match and the next.
func TestHubCounterResetClearsWhatEarlierSessionsCarried(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(10, 10)
	rig.reply()
	rig.receiver.ResetMatch()
	rig.send(15, 12)
	rig.reply()
	rig.assertCounts(5, 2)

	rig.session = "sessionB"
	rig.seq = 0
	rig.send(0, 0)
	rig.reply()
	rig.assertCounts(5, 2)

	rig.receiver.ResetMatch()
	rig.assertCounts(0, 0)

	rig.send(1, 1)
	rig.reply()
	rig.assertCounts(1, 1)
}

// Within a session, seq ordering is enforced; across one it cannot be, because the new
// counter starts its own series wherever it likes.
func TestHubCounterAcceptsALowerSequenceFromANewSession(t *testing.T) {
	rig := newCounterRig(t)
	rig.seq = 9000
	rig.startMatch()
	rig.send(5, 0)
	rig.reply()

	rig.session = "sessionB"
	rig.seq = 0
	rig.send(0, 0)
	rig.reply()

	rig.send(1, 0)
	rig.reply()
	rig.assertCounts(6, 0)
}

// --- Liveness ---

func TestHubCounterStatusFollowsWhatItHears(t *testing.T) {
	rig := newCounterRig(t)
	assert.Equal(t, HubCounterOffline, rig.receiver.Status(), "silent until proven otherwise")

	rig.send(1, 0)
	rig.reply()
	assert.Equal(t, HubCounterOnline, rig.receiver.Status())

	// Age the last datagram past the threshold rather than waiting for it.
	rig.receiver.mu.Lock()
	rig.receiver.last.ReceivedAt = time.Now().Add(-2 * hubCounterOfflineAfter)
	rig.receiver.mu.Unlock()
	assert.Equal(t, HubCounterOffline, rig.receiver.Status())

	rig.send(2, 0)
	rig.reply()
	assert.Equal(t, HubCounterOnline, rig.receiver.Status())
}

// A counter that goes quiet mid-match freezes; it does not zero. Inventing a drop to
// zero would hand the AUTO decision to the other alliance.
func TestHubCounterCountsSurviveGoingOffline(t *testing.T) {
	rig := newCounterRig(t)
	rig.send(0, 0)
	rig.reply()
	rig.receiver.ResetMatch()
	rig.send(7, 2)
	rig.reply()

	rig.receiver.mu.Lock()
	rig.receiver.last.ReceivedAt = time.Now().Add(-2 * hubCounterOfflineAfter)
	rig.receiver.mu.Unlock()

	assert.Equal(t, HubCounterOffline, rig.receiver.Status())
	rig.assertCounts(7, 2)
}

// --- The socket ---

// Everything above hands datagrams to handleDatagram directly. This one goes over the
// wire, so that a mistake in the listener itself cannot hide behind them.
func TestHubCounterReadsFromTheSocket(t *testing.T) {
	receiver, err := NewHubCounterReceiver("127.0.0.1", 0)
	assert.Nil(t, err)
	defer receiver.Close()
	go receiver.Run()

	receiver.SetMatchContext(HubMatchContext{MatchState: "AUTO_PERIOD", Shift: "AUTO"})

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	assert.Nil(t, err)
	defer client.Close()

	to := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: receiver.Port()}
	readReply := func() hubCounterStatusDatagram {
		t.Helper()
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 2048)
		count, err := client.Read(buffer)
		assert.Nil(t, err)
		var status hubCounterStatusDatagram
		assert.Nil(t, json.Unmarshal(buffer[:count], &status))
		return status
	}

	// The first datagram is the baseline, as it is for a counter that was already
	// running when bioarena started.
	_, err = client.WriteToUDP([]byte(`{"v":1,"session":"live","seq":1,"red":3,"blue":0}`), to)
	assert.Nil(t, err)
	status := readReply()
	assert.Equal(t, uint64(1), status.Seq)
	assert.Equal(t, "AUTO_PERIOD", status.MatchState)
	assert.Equal(t, "AUTO", status.Shift)

	// A second one proves the loop is still reading, and that the delta is scored.
	_, err = client.WriteToUDP([]byte(`{"v":1,"session":"live","seq":2,"red":5,"blue":1,"age_ms":38}`), to)
	assert.Nil(t, err)
	assert.Equal(t, uint64(2), readReply().Seq)

	red, blue := receiver.GetHubCounts()
	assert.Equal(t, 2, red)
	assert.Equal(t, 1, blue)

	ageMs, _, session := receiver.Telemetry()
	assert.Equal(t, 38, ageMs)
	assert.Equal(t, "live", session)
}

// Close has to end Run, or a settings change that rebuilds the receiver leaks a
// goroutine holding the port every time.
func TestHubCounterCloseStopsRun(t *testing.T) {
	receiver, err := NewHubCounterReceiver("127.0.0.1", 0)
	assert.Nil(t, err)

	stopped := make(chan struct{})
	go func() {
		receiver.Run()
		close(stopped)
	}()

	assert.Nil(t, receiver.Close())
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Close")
	}
}
