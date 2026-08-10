#!/usr/bin/env bash
# Cross-compile bioarena for Raspberry Pi 4.
# Run from the repo root on any machine with Go 1.22+ installed.
#
# Targets 64-bit Raspberry Pi OS (aarch64), which is what Trixie and Bookworm
# install by default.  Confirm with "uname -m" on the Pi:
#
#   aarch64   ARCH=arm64                     (the default below)
#   armv7l    ARCH=arm  with GOARM=7 set     (32-bit images, including Buster)
#
# Override without editing this file:
#   ARCH=arm GOARM=7 ./build-pi.sh
#
# A binary built for the wrong one does not fail usefully -- the kernel refuses
# to run it and reports "cannot execute binary file: Exec format error".
#
# Output: bioarena-pi  estop-panel-pi  (Linux/ARM binaries, copy to the Pi)
# Note: outputs use a "-pi" suffix so they never shadow a local Windows build.

set -euo pipefail

OUTPUT="bioarena-pi"
PANEL_OUTPUT="estop-panel-pi"

ARCH="${ARCH:-arm64}"
GOARM="${GOARM:-}"
export GOOS=linux GOARCH="$ARCH"
if [ "$ARCH" = "arm" ]; then
	export GOARM="${GOARM:-7}"
	DESCRIPTION="linux/arm (armv7 / 32-bit Raspberry Pi OS)"
else
	DESCRIPTION="linux/arm64 (aarch64 / 64-bit Raspberry Pi OS)"
fi

echo "Building bioarena for $DESCRIPTION..."
go build -o "$OUTPUT" .

echo "Building estop-panel for $DESCRIPTION..."
go build -o "$PANEL_OUTPUT" ./cmd/estop-panel

echo "Done: $OUTPUT  $PANEL_OUTPUT"
echo ""
echo "Deploy with the scripts -- they create the service account on a Pi that has never"
echo "been deployed to, copy everything, install the service, and check it stayed up:"
echo "       ./deploy-fms.sh 10.0.100.5            # field controller"
echo "       ./deploy-panel.sh 10.0.100.11 red     # e-stop panel, per alliance"
echo "       ./deploy-panel.sh 10.0.100.12 blue"
echo ""
echo "Add your login user as a last argument if it is not admin:"
echo "       ./deploy-fms.sh 10.0.100.5 sam"
echo ""
echo "Useful service commands (run on any Pi):"
echo "  sudo systemctl status bioarena   # check it's running"
echo "  sudo journalctl -u bioarena -f   # tail live logs"
echo "  sudo systemctl restart bioarena  # restart after a new deploy"
echo ""
echo "Packages, installed on the Pi while it still has internet -- the field network has"
echo "no route out, so going back for these means moving the Pi:"
echo "       sudo apt install chrony    # makes the Pi the field's time source"
echo ""
echo "Time service (every field). Nothing here has a battery-backed clock, so without it"
echo "the switch and the controller timestamp their logs years apart:"
echo "       scp docs/chrony-bioarena.conf <USER>@<PI_IP>:~/"
echo "       # then on the Pi:"
echo "       sudo mv ~/chrony-bioarena.conf /etc/chrony/conf.d/bioarena.conf"
echo "       sudo systemctl restart chrony"
echo "       # and on the switch: ntp server 10.0.100.5"
echo ""
echo "Network note:"
echo "  Main Pi:        10.0.100.5/24  (eth0, set by bioarena.service)"
echo "  Red panel Pi:   10.0.100.11/24 (eth0, set by estop-panel.service)"
echo "  Blue panel Pi:  10.0.100.12/24 (eth0, set by estop-panel.service)"
