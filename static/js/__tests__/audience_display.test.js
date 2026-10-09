/**
 * @jest-environment jsdom
 */

const fs = require("fs");
const path = require("path");

global.$ = require("jquery");

// match_timing.js supplies translateMatchTime, getCountdownString and matchStates, exactly
// as the page loads it.
const matchTimingSrc = fs.readFileSync(
  path.join(__dirname, "..", "match_timing.js"),
  "utf8"
);
// The real match_timing.js rather than a stub, so the phase names and the clock format
// under test are the ones the display actually shows. Its top-level consts are lexical,
// so they are handed back as an expression evaluated in the same scope and put on global,
// where audience_display.js resolves them -- the browser gets this for free by loading
// both as plain scripts.
Object.assign(
  global,
  (0, eval)(
    matchTimingSrc +
      ";({translateMatchTime, getCountdownString, matchStates, handleMatchTiming})"
  )
);

const {
  handleMatchTime,
  handleMatchLoad,
  handleHubScore,
  showScreen,
  setFieldConnected,
} = require("../audience_display.js");

beforeEach(() => {
  // The timing config, which translateMatchTime reads. The display gets this from the
  // matchTiming message, which fires the moment the websocket connects -- so it is always
  // in hand before the first clock tick arrives.
  handleMatchTiming({
    WarmupDurationSec: 0,
    AutoDurationSec: 20,
    PauseDurationSec: 3,
    TeleopDurationSec: 140,
    WarningRemainingDurationSec: 30,
  });

  document.body.innerHTML = `
    <div class="ad-offline" id="offlineBanner" hidden></div>
    <div class="ad-screen" id="logoScreen"></div>
    <div class="ad-screen" id="matchScreen" hidden>
      <div id="matchName"></div>
      <div id="phaseText"></div>
      <div id="clockText"></div>
      <div id="shiftText"></div>
      <div id="teams-red"></div><div id="fuel-red">0</div>
      <div id="dark-red"></div><div id="hub-red" data-active="false"></div>
      <div id="teams-blue"></div><div id="fuel-blue">0</div>
      <div id="dark-blue"></div><div id="hub-blue" data-active="false"></div>
    </div>`;
});

const matchTime = (state, sec, overrides = {}) => ({
  MatchState: state,
  MatchTimeSec: sec,
  InShift: false,
  HubShift: "NONE",
  RedHubActive: false,
  BlueHubActive: false,
  ...overrides,
});

// ---- Which screen is up ------------------------------------------------------

// The screen follows the match state; there is no operator selector to get wrong.
describe("screen switching", () => {
  const visible = () => ({
    match: !document.getElementById("matchScreen").hidden,
    logo: !document.getElementById("logoScreen").hidden,
  });

  test("shows the logo between matches", () => {
    showScreen("PRE_MATCH");
    expect(visible()).toEqual({ match: false, logo: true });
  });

  test("shows the match from the moment it starts", () => {
    for (const state of ["START_MATCH", "WARMUP_PERIOD", "AUTO_PERIOD", "PAUSE_PERIOD",
      "TELEOP_PERIOD"]) {
      showScreen(state);
      expect(visible()).toEqual({ match: true, logo: false });
    }
  });

  // Dropping to the logo on the buzzer would take the final tallies away at the moment
  // everyone looks up at them.
  test("holds the match screen through post-match", () => {
    showScreen("POST_MATCH");
    expect(visible()).toEqual({ match: true, logo: false });
  });

  test("returns to the logo once the field clears", () => {
    showScreen("POST_MATCH");
    showScreen("PRE_MATCH");
    expect(visible()).toEqual({ match: false, logo: true });
  });
});

// ---- The match screen --------------------------------------------------------

describe("handleMatchTime", () => {
  test("shows the phase and the clock", () => {
    handleMatchTime(matchTime(3 /* AUTO_PERIOD */, 5));
    expect(document.getElementById("phaseText").textContent).toBe("AUTONOMOUS");
    expect(document.getElementById("clockText").textContent).not.toBe("");
    expect(document.getElementById("matchScreen").dataset.period).toBe("auto");
  });

  test("colours the screen by period", () => {
    handleMatchTime(matchTime(5 /* TELEOP_PERIOD */, 30));
    expect(document.getElementById("matchScreen").dataset.period).toBe("teleop");
    handleMatchTime(matchTime(4 /* PAUSE_PERIOD */, 21));
    expect(document.getElementById("matchScreen").dataset.period).toBe("pause");
  });

  // The HUB light is what the drivers are watching. This is the same thing for everyone
  // who is not holding a controller.
  test("lights the HUB that is taking FUEL", () => {
    handleMatchTime(matchTime(5, 40, {
      InShift: true, HubShift: "SHIFT1", RedHubActive: false, BlueHubActive: true,
    }));
    expect(document.getElementById("hub-red").dataset.active).toBe("false");
    expect(document.getElementById("hub-red").textContent).toBe("HUB DARK");
    expect(document.getElementById("hub-blue").dataset.active).toBe("true");
    expect(document.getElementById("hub-blue").textContent).toBe("TAKING FUEL");
    expect(document.getElementById("shiftText").textContent).toBe("SHIFT 1");
  });

  test("says nothing about shifts outside one", () => {
    handleMatchTime(matchTime(0 /* PRE_MATCH */, 0));
    expect(document.getElementById("shiftText").textContent).toBe("");
    expect(document.getElementById("hub-red").dataset.active).toBe("false");
    expect(document.getElementById("hub-blue").dataset.active).toBe("false");
  });
});

describe("handleMatchLoad", () => {
  test("lists each alliance's teams", () => {
    handleMatchLoad({
      Match: { LongName: "Test Match" },
      Teams: { R1: { Id: 841 }, R2: { Id: 9841 }, R3: null, B1: { Id: 254 }, B2: null, B3: null },
    });
    expect(document.getElementById("matchName").textContent).toBe("Test Match");
    expect(document.getElementById("teams-red").textContent).toBe("841  9841");
    expect(document.getElementById("teams-blue").textContent).toBe("254");
  });

  // On a half field the other alliance is genuinely absent; a row of dashes is clutter.
  test("leaves an empty alliance empty", () => {
    handleMatchLoad({
      Match: { LongName: "Test Match" },
      Teams: { R1: { Id: 841 }, R2: null, R3: null, B1: null, B2: null, B3: null },
    });
    expect(document.getElementById("teams-blue").textContent).toBe("");
  });
});

describe("handleHubScore", () => {
  const score = (o = {}) => ({ Counted: 0, Credited: 0, ...o });

  test("shows the credited count as the score", () => {
    handleHubScore({ Red: score({ Counted: 23, Credited: 18 }), Blue: score({ Counted: 4, Credited: 4 }) });
    expect(document.getElementById("fuel-red").textContent).toBe("18");
    expect(document.getElementById("fuel-blue").textContent).toBe("4");
  });

  test("calls out FUEL shot into a dark hub", () => {
    handleHubScore({ Red: score({ Counted: 23, Credited: 18 }), Blue: score() });
    expect(document.getElementById("dark-red").textContent).toBe("5 into a dark hub");
  });

  test("says nothing when every ball scored", () => {
    handleHubScore({ Red: score({ Counted: 7, Credited: 7 }), Blue: score() });
    expect(document.getElementById("dark-red").textContent).toBe("");
  });

  test("survives a message missing an alliance", () => {
    expect(() => handleHubScore({})).not.toThrow();
  });
});


// ---- Losing the field --------------------------------------------------------

// A dropped connection leaves the page showing its last state, and a frozen clock reads
// exactly like a live one on a stopped match. The screen has to say it has stopped being
// told, or nobody can tell the difference from across a field.
describe("setFieldConnected", () => {
  test("says so when the connection drops", () => {
    setFieldConnected(false);
    expect(document.getElementById("offlineBanner").hidden).toBe(false);
  });

  test("clears the warning once it is back", () => {
    setFieldConnected(false);
    setFieldConnected(true);
    expect(document.getElementById("offlineBanner").hidden).toBe(true);
  });

  // The last thing the field said is still the most useful thing on the screen; the
  // banner sits over it rather than replacing it.
  test("leaves the match screen alone", () => {
    showScreen("TELEOP_PERIOD");
    handleHubScore({ Red: { Counted: 9, Credited: 9 }, Blue: { Counted: 0, Credited: 0 } });
    setFieldConnected(false);
    expect(document.getElementById("matchScreen").hidden).toBe(false);
    expect(document.getElementById("fuel-red").textContent).toBe("9");
  });
});
