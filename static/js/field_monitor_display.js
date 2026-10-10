// Copyright 2018 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Client-side logic for the field monitor.
//
// Upstream's, with the handlers for data this fork does not have taken out: the realtime
// scores either side, the cycle-time and early/late footer, and the FTA notes field, which
// needs a column on the team model that does not exist here. The per-station status
// reading is upstream's and is the point of the page.

let websocket;
let currentMatchId;
let redSide;
let blueSide;
const lowBatteryThreshold = 8;
const highBtuThreshold = 7.0;


const handleArenaStatus = function (data) {
  // If getting data for the wrong match (e.g. after a server restart), reload the page.
  if (currentMatchId == null) {
    currentMatchId = data.MatchId;
  } else if (currentMatchId !== data.MatchId) {
    location.reload();
  }

  $.each(data.AllianceStations, function (station, stationStatus) {
    // Select the DOM elements corresponding to the team station.
    let teamElementPrefix;
    if (station[0] === "R") {
      teamElementPrefix = "#" + redSide + "Team" + station[1];
    } else {
      teamElementPrefix = "#" + blueSide + "Team" + station[1];
    }
    const teamIdElement = $(teamElementPrefix + "Id");
    const teamEthernetElement = $(teamElementPrefix + "Ethernet");
    const teamDsElement = $(teamElementPrefix + "Ds");
    const teamRadioElement = $(teamElementPrefix + "Radio");
    const teamRadioIconElement = $(teamElementPrefix + "Radio i");
    const teamRobotElement = $(teamElementPrefix + "Robot");
    const teamBatteryElement = $(teamElementPrefix + "Battery");
    const teamBypassElement = $(teamElementPrefix + "Bypass");
    const teamStatsElement = $(teamElementPrefix + "Stats");
    const teamBandwidthElement = $(teamElementPrefix + "Bandwidth");
    const teamTripTimeElement = $(teamElementPrefix + "TripTime");
    const teamMissedPacketsElement = $(teamElementPrefix + "MissedPackets");


    if (stationStatus.Team) {
      // Set the team number and status.
      teamIdElement.text(stationStatus.Team.Id);
      let status = "no-link";
      if (stationStatus.Bypass) {
        status = "";
      } else if (stationStatus.DsConn) {
        if (stationStatus.DsConn.WrongStation) {
          status = "wrong-station";
        } else if (stationStatus.DsConn.RobotLinked) {
          status = "robot-linked";
        } else if (stationStatus.DsConn.RioLinked) {
          status = "rio-linked";
        } else if (stationStatus.DsConn.RadioLinked) {
          status = "radio-linked";
        } else if (stationStatus.DsConn.DsLinked) {
          status = "ds-linked";
        }
      }
      teamIdElement.attr("data-status", status);
    } else {
      // No team is present in this position for this match; blank out the status.
      teamIdElement.text("");
    }

    // Format the Ethernet status box.
    teamEthernetElement.attr("data-status-ok", stationStatus.Ethernet ? "true" : "");
    if (stationStatus.DsConn && stationStatus.DsConn.DsRobotTripTimeMs > 0) {
      teamEthernetElement.text(stationStatus.DsConn.DsRobotTripTimeMs);
    } else {
      teamEthernetElement.text("ETH");
    }

    const wifiStatus = stationStatus.WifiStatus;
    teamRadioIconElement.attr("class", `bi-reception-${wifiStatus.ConnectionQuality}`);

    $("#accessPointStatus").attr("data-status", data.AccessPointStatus);
    $("#switchStatus").attr("data-status", data.SwitchStatus);

    if (stationStatus.DsConn) {
      // Format the driver station status box.
      const dsConn = stationStatus.DsConn;
      teamDsElement.attr("data-status-ok", dsConn.DsLinked);
      teamDsElement.text(dsConn.MissedPacketCount);

      // Format the radio status box according to the connection status of the robot radio.
      const radioOkay = stationStatus.Team && stationStatus.Team.Id === wifiStatus.TeamId &&
        (wifiStatus.RadioLinked || dsConn.RobotLinked);
      teamRadioElement.attr("data-status-ok", radioOkay);

      // Format the robot status box.
      const rioOkay = dsConn.RobotLinked;
      teamRobotElement.attr("data-status-ok", rioOkay);
      if (stationStatus.DsConn.SecondsSinceLastRobotLink > 1 && stationStatus.DsConn.SecondsSinceLastRobotLink < 1000) {
        teamRobotElement.text(stationStatus.DsConn.SecondsSinceLastRobotLink.toFixed());
      } else {
        teamRobotElement.text("RIO");
      }
      const batteryOkay = dsConn.BatteryVoltage > lowBatteryThreshold && dsConn.RobotLinked;
      teamBatteryElement.attr("data-status-ok", batteryOkay);
      teamBatteryElement.text(dsConn.BatteryVoltage.toFixed(1) + "V");

      const btuOkay = wifiStatus.MBits < highBtuThreshold && dsConn.RobotLinked;
      teamStatsElement.attr("data-status-ok", btuOkay);
      if (wifiStatus.MBits >= 0.01) {
        teamBandwidthElement.text(wifiStatus.MBits.toFixed(2));
        teamTripTimeElement.text(dsConn.DsRobotTripTimeMs);
        teamMissedPacketsElement.text(dsConn.MissedPacketCount);
      } else {
        teamBandwidthElement.text("-");
        teamTripTimeElement.text("-");
        teamMissedPacketsElement.text("-");
      }
    } else {
      teamDsElement.attr("data-status-ok", "");
      teamDsElement.text("DS");
      teamRobotElement.attr("data-status-ok", "");
      teamRobotElement.text("RIO");
      teamBatteryElement.text("0.0V");
      teamBandwidthElement.text("-");
      teamTripTimeElement.text("-");
      teamMissedPacketsElement.text("-");

      // Format the robot status box according to whether the AP is configured with the correct SSID.
      const expectedTeamId = stationStatus.Team ? stationStatus.Team.Id : 0;
      if (wifiStatus.TeamId === expectedTeamId) {
        if (wifiStatus.RadioLinked) {
          teamRadioElement.attr("data-status-ok", true);
        } else {
          teamRadioElement.attr("data-status-ok", "");
        }
      } else {
        teamRadioElement.attr("data-status-ok", false);
      }
    }

    // A wiring fault outranks the stop it causes: the station is stopped either
    // way, but only one of the two is fixed by releasing the button.
    teamBypassElement.attr("title", stationStatus.EStopFault || "");
    if (stationStatus.EStopFault) {
      teamBypassElement.attr("data-status-ok", false);
      teamBypassElement.text("FAULT");
    } else if (stationStatus.EStop) {
      teamBypassElement.attr("data-status-ok", false);
      teamBypassElement.text("E-STP");
    } else if (stationStatus.AStop) {
      teamBypassElement.attr("data-status-ok", true);
      teamBypassElement.text("A-STP");
    } else if (stationStatus.Bypass) {
      teamBypassElement.attr("data-status-ok", false);
      teamBypassElement.text("BYP");
    } else {
      teamBypassElement.attr("data-status-ok", true);
      teamBypassElement.text("");
    }
  });
};

// Handles a websocket message to update the match time countdown.
const handleMatchTime = function (data) {
  translateMatchTime(data, function (matchState, matchStateText, countdownSec) {
    $("#matchState").text(matchStateText);
    $("#matchTime").text(countdownSec);
    if (matchStateText === "PRE-MATCH" || matchStateText === "POST-MATCH") {
      $(".ds-dependent").attr("data-preMatch", "true");
    } else {
      $(".ds-dependent").attr("data-preMatch", "false");
    }
  });
};

// setFieldConnected shows or hides the banner saying the page has stopped being told
// anything.
//
// A page of green status boxes frozen at the field's last word looks exactly like a
// healthy field, which on a diagnostic is the worst thing it could look like. What is
// behind the banner stays put: it was true a moment ago, and is what someone working out
// what just happened wants to read.
const setFieldConnected = function (connected) {
  const banner = document.getElementById("offlineBanner");
  if (banner) {
    banner.hidden = Boolean(connected);
  }
};

// Handles a websocket message to update current match
const handleMatchLoad = function (data) {
  $("#matchName").text(data.Match.LongName);
};

$(function () {
  // Read the configuration for this display from the URL query string.
  const urlParams = new URLSearchParams(window.location.search);
  const reversed = urlParams.get("reversed");
  if (reversed === "true") {
    redSide = "right";
    blueSide = "left";
  } else {
    redSide = "left";
    blueSide = "right";
  }

  // Upstream's `ds` and `fta` modes went with the data behind them; only the side swap
  // is left, and it needs nothing from the server.
  $(".reversible-left").attr("data-reversed", reversed);
  $(".reversible-right").attr("data-reversed", reversed);

  // Set up the websocket back to the server.
  websocket = new CheesyWebsocket("/field_monitor/websocket", {
    onConnected: function () {
      setFieldConnected(true);
    },
    onDisconnected: function () {
      setFieldConnected(false);
    },
    arenaStatus: function (event) {
      handleArenaStatus(event.data);
    },
    matchLoad: function (event) {
      handleMatchLoad(event.data);
    },
    matchTiming: function (event) {
      handleMatchTiming(event.data);
    },
    matchTime: function (event) {
      handleMatchTime(event.data);
    },
  });
});

// Export for Jest. No-op in the browser.
if (typeof module !== "undefined") {
  module.exports = { handleArenaStatus, handleMatchLoad, handleMatchTime, setFieldConnected };
}
