// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the audience display.
//
// Replaces a stale copy of upstream's, dead since this fork dropped the audience display
// template and several games out of date by the time it went. Upstream drives ten screens
// through a transition map and an operator's radio buttons; this has two and switches them
// on the match state, so there is no transition machinery and nothing for an operator to
// get wrong during a match.

var websocket;

// The stations each alliance owns, in the order they are shown.
const audienceStations = { red: ["R1", "R2", "R3"], blue: ["B1", "B2", "B3"] };

// Shift names arrive in the counter protocol's vocabulary, which is a wire format; the
// screen is read by people.
const audienceShiftLabels = {
  AUTO: "AUTONOMOUS",
  TRANSITION: "TRANSITION",
  SHIFT1: "SHIFT 1",
  SHIFT2: "SHIFT 2",
  SHIFT3: "SHIFT 3",
  SHIFT4: "SHIFT 4",
  ENDGAME: "ENDGAME",
  POST_MATCH: "",
};

// showScreen swaps between the two screens.
//
// Which one is a function of the match state alone. The match screen stays up through
// POST_MATCH, so the final tallies are readable for the few seconds the field dwells there
// before it clears itself -- dropping to the logo on the buzzer would take the numbers away
// at the moment everyone looks up.
const showScreen = function (state) {
  const matchRunning =
    state === "START_MATCH" ||
    state === "WARMUP_PERIOD" ||
    state === "AUTO_PERIOD" ||
    state === "PAUSE_PERIOD" ||
    state === "TELEOP_PERIOD" ||
    state === "POST_MATCH";

  document.getElementById("matchScreen").hidden = !matchRunning;
  document.getElementById("logoScreen").hidden = matchRunning;
};

const handleMatchTime = function (data) {
  translateMatchTime(data, function (state, stateText, countdown) {
    showScreen(state);

    document.getElementById("phaseText").textContent = stateText || "";
    document.getElementById("clockText").textContent =
      state === "POST_MATCH" ? "—" : getCountdownString(Math.max(0, countdown));

    const screen = document.getElementById("matchScreen");
    switch (state) {
      case "AUTO_PERIOD":
        screen.dataset.period = "auto";
        break;
      case "TELEOP_PERIOD":
        screen.dataset.period = "teleop";
        break;
      case "PAUSE_PERIOD":
        screen.dataset.period = "pause";
        break;
      default:
        screen.dataset.period = "";
    }
  });

  // Which HUBs are taking FUEL, from the same arena state the match panel's banner reads.
  document.getElementById("shiftText").textContent = data.InShift
    ? audienceShiftLabels[data.HubShift] ?? data.HubShift ?? ""
    : "";
  setHubActive("red", data.InShift && data.RedHubActive);
  setHubActive("blue", data.InShift && data.BlueHubActive);
};

const setHubActive = function (alliance, active) {
  const hub = document.getElementById("hub-" + alliance);
  hub.dataset.active = String(Boolean(active));
  hub.textContent = active ? "TAKING FUEL" : "HUB DARK";
};

const handleMatchLoad = function (data) {
  document.getElementById("matchName").textContent = data.Match ? data.Match.LongName : "";

  for (const alliance of ["red", "blue"]) {
    const teams = audienceStations[alliance]
      .map((station) => data.Teams && data.Teams[station])
      .filter((team) => team)
      .map((team) => team.Id);
    // Nothing rather than an empty row of dashes: on a half field the other alliance is
    // genuinely absent, and saying so loudly is just clutter.
    document.getElementById("teams-" + alliance).textContent = teams.join("  ");
  }
};

const handleHubScore = function (data) {
  for (const alliance of ["red", "blue"]) {
    const score = data[alliance === "red" ? "Red" : "Blue"];
    if (!score) continue;

    document.getElementById("fuel-" + alliance).textContent = score.Credited;

    // Said only when it happened. "0 into a dark hub" on screen all match is noise.
    const intoDark = score.Counted - score.Credited;
    document.getElementById("dark-" + alliance).textContent =
      intoDark > 0 ? intoDark + " into a dark hub" : "";
  }
};

// setFieldConnected shows or hides the banner saying this screen has stopped being told
// anything. Everything else is left exactly as it was: it was true when the field last
// said so, and is still the most useful thing on the screen -- it just is not live.
const setFieldConnected = function (connected) {
  const banner = document.getElementById("offlineBanner");
  if (banner) {
    banner.hidden = Boolean(connected);
  }
};

$(function () {
  websocket = new CheesyWebsocket("/audience_display/websocket", {
    onConnected: function () {
      setFieldConnected(true);
    },
    onDisconnected: function () {
      setFieldConnected(false);
    },
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
    hubScore: function (event) {
      handleHubScore(event.data);
    },
  });
});

// Export for Jest. No-op in the browser.
if (typeof module !== "undefined") {
  module.exports = {
    handleMatchTime, handleMatchLoad, handleHubScore, showScreen, setFieldConnected,
  };
}
